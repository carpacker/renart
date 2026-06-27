package sqlformat

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// The embedded WASM is dist/polyglot_sql.wasm from @polyglot-sql/sdk@0.4.3.
//
//go:embed polyglot_sql.wasm
var wasmFS embed.FS

const DialectGeneric = "generic"

var (
	polyglotInterpreterOnce    sync.Once
	polyglotInterpreterMu      sync.RWMutex
	polyglotInterpreter        *polyglotEngine
	polyglotInterpreterErr     error
	polyglotInterpreterRetired bool

	polyglotCompilerOnce sync.Once
	polyglotCompilerMu   sync.RWMutex
	polyglotCompiler     *polyglotEngine
	polyglotCompilerErr  error

	polyglotInstanceCount uint64
)

type polyglotEngine struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	name     string
	// idle holds reusable module instances. Instantiating a module costs ~15ms
	// (it copies the 18MB module's data into fresh linear memory), so reusing
	// instances keeps SQL intellisense responsive. Each instance is closed after
	// polyglotRecycleEvery uses to reclaim the WASM linear memory, which only
	// ever grows.
	idle chan *pooledModule
}

type pooledModule struct {
	module api.Module
	uses   int
}

// polyglotPoolSize bounds the number of cached idle module instances (and thus
// the resident linear memory); polyglotRecycleEvery bounds how long any one
// instance lives before it is closed and reclaimed.
const (
	polyglotPoolSize     = 4
	polyglotRecycleEvery = 64
)

type polyglotFormatResponse struct {
	Success bool     `json:"success"`
	SQL     []string `json:"sql"`
	Error   string   `json:"error"`
}

func Format(ctx context.Context, sql, dialect string) (string, error) {
	if dialect == "" {
		dialect = DialectGeneric
	}
	output, err := Call(ctx, "format_sql_with_options", sql, dialect, "{}")
	if err != nil {
		return "", err
	}

	var response polyglotFormatResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return "", err
	}
	if !response.Success {
		if response.Error != "" {
			return "", errors.New(response.Error)
		}
		return "", errors.New("polyglot SQL formatting failed")
	}
	if len(response.SQL) == 0 {
		return "", errors.New("polyglot SQL returned no formatted SQL")
	}

	return response.SQL[0], nil
}

// polyglotCompilerGrace is how long the first call waits for the compiled
// engine before falling back to the interpreter. On a warm start the compiler
// loads from the on-disk cache within a few seconds, so this avoids building
// the memory-heavy interpreter (~160MB+) at all; on a cold start (compilation
// takes far longer) the wait lapses and the interpreter bridges the gap.
const polyglotCompilerGrace = 8 * time.Second

func Call(ctx context.Context, functionName string, args ...string) (string, error) {
	// Prefer the compiled engine. It is built in the background; once ready it
	// serves every call, so the interpreter is retired to reclaim memory.
	startPolyglotCompiler()
	if compiled := readyPolyglotCompiler(); compiled != nil {
		return callPolyglotEngine(ctx, compiled, functionName, args...)
	}

	// Compiler not ready yet. If we have not built the interpreter, give the
	// compiler a brief grace period to come up (cheap on warm cache hits) before
	// paying for the interpreter.
	if currentPolyglotInterpreter() == nil {
		if compiled := awaitPolyglotCompiler(ctx, polyglotCompilerGrace); compiled != nil {
			return callPolyglotEngine(ctx, compiled, functionName, args...)
		}
	}

	// Build (once) and use the interpreter so calls don't block on a slow (cold)
	// compilation. The read lock keeps the interpreter alive for the duration of
	// the call, so the compiler goroutine can't close it out from under us when
	// it retires it.
	if _, err := initPolyglotInterpreter(ctx); err != nil {
		return "", err
	}
	polyglotInterpreterMu.RLock()
	engine := polyglotInterpreter
	if engine == nil {
		// Retired between init and lock — the compiler is ready now.
		polyglotInterpreterMu.RUnlock()
		if compiled := readyPolyglotCompiler(); compiled != nil {
			return callPolyglotEngine(ctx, compiled, functionName, args...)
		}
		return "", errors.New("polyglot SQL engine unavailable")
	}
	defer polyglotInterpreterMu.RUnlock()
	return callPolyglotEngine(ctx, engine, functionName, args...)
}

func callPolyglotEngine(ctx context.Context, engine *polyglotEngine, functionName string, args ...string) (resultString string, resultErr error) {
	pm, err := engine.acquire(ctx)
	if err != nil {
		return "", err
	}
	// Return the instance to the pool on success so the next call reuses it
	// (avoiding the ~15ms re-instantiation). On error close it — a half-finished
	// call may have left guest state inconsistent.
	defer func() {
		if resultErr != nil {
			_ = pm.module.Close(ctx)
			return
		}
		engine.release(ctx, pm)
	}()
	module := pm.module

	malloc := module.ExportedFunction("__wbindgen_export")
	free := module.ExportedFunction("__wbindgen_export4")
	stack := module.ExportedFunction("__wbindgen_add_to_stack_pointer")
	function := module.ExportedFunction(functionName)
	memory := module.Memory()
	if malloc == nil || free == nil || stack == nil || function == nil || memory == nil {
		return "", errors.New("polyglot SQL WASM is missing required exports")
	}

	writeString := func(value string) (uint64, uint64, error) {
		bytes := []byte(value)
		result, err := malloc.Call(ctx, uint64(len(bytes)), 1)
		if err != nil {
			return 0, 0, err
		}
		ptr := uint32(result[0])
		if !memory.Write(ptr, bytes) {
			return 0, 0, errors.New("failed to write string into polyglot SQL memory")
		}
		return uint64(ptr), uint64(len(bytes)), nil
	}

	retptrResult, err := stack.Call(ctx, uint64(uint32(0xfffffff0)))
	if err != nil {
		return "", err
	}
	retptr := uint32(retptrResult[0])
	defer stack.Call(ctx, 16)

	callArgs := []uint64{uint64(retptr)}
	for _, arg := range args {
		ptr, length, err := writeString(arg)
		if err != nil {
			return "", err
		}
		callArgs = append(callArgs, ptr, length)
	}

	if _, err := function.Call(ctx, callArgs...); err != nil {
		return "", err
	}

	ret, ok := memory.Read(retptr, 8)
	if !ok {
		return "", errors.New("failed to read polyglot SQL return pointer")
	}
	outputPtr := binary.LittleEndian.Uint32(ret[0:4])
	outputLen := binary.LittleEndian.Uint32(ret[4:8])
	output, ok := memory.Read(outputPtr, outputLen)
	if !ok {
		return "", errors.New("failed to read polyglot SQL response")
	}
	defer free.Call(ctx, uint64(outputPtr), uint64(outputLen), 1)

	return string(output), nil
}

func (engine *polyglotEngine) acquire(ctx context.Context) (*pooledModule, error) {
	select {
	case pm := <-engine.idle:
		return pm, nil
	default:
	}
	instanceID := atomic.AddUint64(&polyglotInstanceCount, 1)
	module, err := engine.runtime.InstantiateModule(ctx, engine.compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("polyglot-sql-%s-%d", engine.name, instanceID)))
	if err != nil {
		return nil, err
	}
	return &pooledModule{module: module}, nil
}

func (engine *polyglotEngine) release(ctx context.Context, pm *pooledModule) {
	pm.uses++
	if pm.uses >= polyglotRecycleEvery {
		_ = pm.module.Close(ctx)
		return
	}
	select {
	case engine.idle <- pm:
	default:
		// Pool full: close the surplus instance instead of leaking it.
		_ = pm.module.Close(ctx)
	}
}

func PrewarmPolyglotCompiler() {
	startPolyglotCompiler()
}

func PolyglotCompilerReady() bool {
	return readyPolyglotCompiler() != nil
}

func initPolyglotInterpreter(ctx context.Context) (*polyglotEngine, error) {
	polyglotInterpreterOnce.Do(func() {
		// Skip the (expensive) interpreter build if the compiled engine already
		// took over while this first call was racing in.
		if readyPolyglotCompiler() != nil {
			return
		}
		engine, err := buildPolyglotEngine(ctx, wazero.NewRuntimeConfigInterpreter(), "interpreted")
		polyglotInterpreterMu.Lock()
		// The compiler can finish (and retire the interpreter) during the ~1s
		// build above; if so, discard this engine rather than publishing it,
		// otherwise it leaks ~160MB+ for the process lifetime.
		if polyglotInterpreterRetired {
			polyglotInterpreterMu.Unlock()
			if engine != nil {
				_ = engine.runtime.Close(ctx)
			}
			return
		}
		polyglotInterpreter = engine
		polyglotInterpreterErr = err
		polyglotInterpreterMu.Unlock()
	})
	polyglotInterpreterMu.RLock()
	defer polyglotInterpreterMu.RUnlock()
	return polyglotInterpreter, polyglotInterpreterErr
}

func startPolyglotCompiler() {
	polyglotCompilerOnce.Do(func() {
		go func() {
			engine, err := buildPolyglotEngine(context.Background(), compilerRuntimeConfig(), "compiled")
			polyglotCompilerMu.Lock()
			polyglotCompiler = engine
			polyglotCompilerErr = err
			polyglotCompilerMu.Unlock()
			// The compiled engine now serves every call; free the interpreter
			// (its decoded operations retain hundreds of MB of Go heap) and
			// return that transient arena to the OS.
			if err == nil && engine != nil {
				retirePolyglotInterpreter(context.Background())
				debug.FreeOSMemory()
			}
		}()
	})
}

// retirePolyglotInterpreter closes the interpreter runtime and drops it once
// the compiled engine has taken over. The write lock waits for any in-flight
// interpreter call (which holds the read lock) to finish first.
func retirePolyglotInterpreter(ctx context.Context) {
	polyglotInterpreterMu.Lock()
	defer polyglotInterpreterMu.Unlock()
	polyglotInterpreterRetired = true
	if polyglotInterpreter != nil {
		_ = polyglotInterpreter.runtime.Close(ctx)
		polyglotInterpreter = nil
	}
}

func readyPolyglotCompiler() *polyglotEngine {
	polyglotCompilerMu.RLock()
	defer polyglotCompilerMu.RUnlock()
	return polyglotCompiler
}

// awaitPolyglotCompiler waits up to timeout for the compiled engine, returning
// nil if it is still not ready (so the caller can fall back to the interpreter).
func awaitPolyglotCompiler(ctx context.Context, timeout time.Duration) *polyglotEngine {
	deadline := time.Now().Add(timeout)
	for {
		if compiled := readyPolyglotCompiler(); compiled != nil {
			return compiled
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func currentPolyglotInterpreter() *polyglotEngine {
	polyglotInterpreterMu.RLock()
	defer polyglotInterpreterMu.RUnlock()
	return polyglotInterpreter
}

var (
	wazeroCacheOnce sync.Once
	wazeroCache     wazero.CompilationCache
)

// compilerRuntimeConfig returns the optimizing-compiler runtime config backed by
// a shared on-disk compilation cache. Compiling the 18MB polyglot wasm with the
// optimizing compiler takes seconds; the disk cache makes that cost paid once
// per machine (keyed by wazero version + module) instead of once per process,
// so server restarts — and the e2e suite's many short-lived servers — get a
// ready compiler almost immediately instead of leaning on the memory-heavy
// interpreter while compilation runs.
func compilerRuntimeConfig() wazero.RuntimeConfig {
	config := wazero.NewRuntimeConfigCompiler()
	if cache := sharedWazeroCache(); cache != nil {
		config = config.WithCompilationCache(cache)
	}
	return config
}

func sharedWazeroCache() wazero.CompilationCache {
	wazeroCacheOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil || base == "" {
			base = os.TempDir()
		}
		dir := filepath.Join(base, "renart", "wazero-cache")
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return
		}
		if cache, cacheErr := wazero.NewCompilationCacheWithDir(dir); cacheErr == nil {
			wazeroCache = cache
		}
	})
	return wazeroCache
}

func buildPolyglotEngine(ctx context.Context, config wazero.RuntimeConfig, name string) (*polyglotEngine, error) {
	wasmBytes, err := wasmFS.ReadFile("polyglot_sql.wasm")
	if err != nil {
		return nil, err
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	compiled, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = runtime.Close(ctx)
		return nil, err
	}

	imports := runtime.NewHostModuleBuilder("./polyglot_sql_wasm_bg.js")
	for _, imported := range compiled.ImportedFunctions() {
		_, importName, _ := imported.Import()
		if err := addWasmBindgenImportStub(imports, importName, imported.ParamTypes(), imported.ResultTypes()); err != nil {
			_ = runtime.Close(ctx)
			return nil, err
		}
	}
	if _, err := imports.Instantiate(ctx); err != nil {
		_ = runtime.Close(ctx)
		return nil, err
	}

	return &polyglotEngine{
		runtime:  runtime,
		compiled: compiled,
		name:     name,
		idle:     make(chan *pooledModule, polyglotPoolSize),
	}, nil
}

func addWasmBindgenImportStub(builder wazero.HostModuleBuilder, name string, params, results []api.ValueType) error {
	signature := fmt.Sprintf("%v->%v", params, results)
	switch signature {
	case "[]->[127]":
		builder.NewFunctionBuilder().WithFunc(func() uint32 { return 0 }).Export(name)
	case "[127]->[]":
		builder.NewFunctionBuilder().WithFunc(func(uint32) {}).Export(name)
	case "[127]->[127]":
		builder.NewFunctionBuilder().WithFunc(func(uint32) uint32 { return 0 }).Export(name)
	case "[127 127]->[]":
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32) {}).Export(name)
	case "[127 127]->[127]":
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32) uint32 { return 0 }).Export(name)
	case "[127 127 127]->[]":
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) {}).Export(name)
	case "[127 127 127]->[127]":
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32) uint32 { return 0 }).Export(name)
	case "[124]->[127]":
		builder.NewFunctionBuilder().WithFunc(func(float64) uint32 { return 0 }).Export(name)
	case "[126]->[127]":
		builder.NewFunctionBuilder().WithFunc(func(int64) uint32 { return 0 }).Export(name)
	case "[127]->[124]":
		builder.NewFunctionBuilder().WithFunc(func(uint32) float64 { return 0 }).Export(name)
	case "[127 127]->[124]":
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32) float64 { return 0 }).Export(name)
	case "[127 127 127 127]->[]":
		builder.NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32) {}).Export(name)
	default:
		return fmt.Errorf("unsupported polyglot SQL WASM import %q with signature %s", name, signature)
	}
	return nil
}
