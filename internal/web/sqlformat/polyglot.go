package sqlformat

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// The embedded WASM is dist/polyglot_sql.wasm from @polyglot-sql/sdk@0.4.3.
//
//go:embed polyglot_sql.wasm
var wasmFS embed.FS

const DialectGeneric = "generic"

var (
	polyglotInterpreterOnce sync.Once
	polyglotInterpreter     *polyglotEngine
	polyglotInterpreterErr  error

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
}

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

func Call(ctx context.Context, functionName string, args ...string) (string, error) {
	engine, err := polyglotEngineForCall(ctx)
	if err != nil {
		return "", err
	}

	instanceID := atomic.AddUint64(&polyglotInstanceCount, 1)
	module, err := engine.runtime.InstantiateModule(ctx, engine.compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("polyglot-sql-%s-%d", engine.name, instanceID)))
	if err != nil {
		return "", err
	}
	defer module.Close(ctx)

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

func polyglotEngineForCall(ctx context.Context) (*polyglotEngine, error) {
	engine, err := initPolyglotInterpreter(ctx)
	if err != nil {
		return nil, err
	}
	startPolyglotCompiler()
	if compiled := readyPolyglotCompiler(); compiled != nil {
		return compiled, nil
	}
	return engine, nil
}

func initPolyglotInterpreter(ctx context.Context) (*polyglotEngine, error) {
	polyglotInterpreterOnce.Do(func() {
		polyglotInterpreter, polyglotInterpreterErr = buildPolyglotEngine(ctx, wazero.NewRuntimeConfigInterpreter(), "interpreted")
	})
	return polyglotInterpreter, polyglotInterpreterErr
}

func startPolyglotCompiler() {
	polyglotCompilerOnce.Do(func() {
		go func() {
			engine, err := buildPolyglotEngine(context.Background(), wazero.NewRuntimeConfigCompiler(), "compiled")
			polyglotCompilerMu.Lock()
			defer polyglotCompilerMu.Unlock()
			polyglotCompiler = engine
			polyglotCompilerErr = err
		}()
	})
}

func readyPolyglotCompiler() *polyglotEngine {
	polyglotCompilerMu.RLock()
	defer polyglotCompilerMu.RUnlock()
	return polyglotCompiler
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

	return &polyglotEngine{runtime: runtime, compiled: compiled, name: name}, nil
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
