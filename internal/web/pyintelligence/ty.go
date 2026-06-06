package pyintelligence

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed ty_wasi.wasm
var tyWASM embed.FS

type Request struct {
	Root               string         `json:"root"`
	Path               string         `json:"path"`
	Content            string         `json:"content"`
	Options            map[string]any `json:"options,omitempty"`
	Files              []VirtualFile  `json:"files,omitempty"`
	Line               int            `json:"line,omitempty"`
	Column             int            `json:"column,omitempty"`
	Snippets           bool           `json:"snippets,omitempty"`
	SessionID          string         `json:"session_id,omitempty"`
	SessionFingerprint string         `json:"session_fingerprint,omitempty"`
}

type VirtualFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type FormatResponse struct {
	Status string  `json:"status"`
	Result *string `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type CheckResponse struct {
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Error       string       `json:"error,omitempty"`
}

type CompletionResponse struct {
	Status string       `json:"status"`
	Result []Completion `json:"result,omitempty"`
	Error  string       `json:"error,omitempty"`
}

type Diagnostic struct {
	ID       string `json:"id"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Range    *Range `json:"range,omitempty"`
	Display  string `json:"display,omitempty"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Completion struct {
	Label               string     `json:"label"`
	Kind                string     `json:"kind,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	InsertText          string     `json:"insert_text,omitempty"`
	InsertTextFormat    string     `json:"insert_text_format"`
	Documentation       string     `json:"documentation,omitempty"`
	ModuleName          string     `json:"module_name,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additional_text_edits,omitempty"`
}

type TextEdit struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}

type runtimeState struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	module   api.Module
	err      error
}

var state struct {
	once sync.Once
	data runtimeState
}

var executionMu sync.Mutex

func Format(ctx context.Context, req Request) (FormatResponse, error) {
	var resp FormatResponse
	if err := call(ctx, "ty_format_python", req, &resp); err != nil {
		return FormatResponse{}, err
	}
	return resp, nil
}

func Check(ctx context.Context, req Request) (CheckResponse, error) {
	var resp CheckResponse
	if err := call(ctx, "ty_check_python", req, &resp); err != nil {
		return CheckResponse{}, err
	}
	return resp, nil
}

func Complete(ctx context.Context, req Request) (CompletionResponse, error) {
	var resp CompletionResponse
	if err := call(ctx, "ty_complete_python", req, &resp); err != nil {
		return CompletionResponse{}, err
	}
	return resp, nil
}

func call(ctx context.Context, functionName string, req Request, response any) error {
	initialized := initRuntime(ctx)
	if initialized.err != nil {
		return initialized.err
	}

	executionMu.Lock()
	defer executionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	execCtx := context.WithoutCancel(ctx)

	memory := initialized.module.Memory()
	alloc := initialized.module.ExportedFunction("ty_alloc")
	dealloc := initialized.module.ExportedFunction("ty_dealloc")
	freeResult := initialized.module.ExportedFunction("ty_result_free")
	fn := initialized.module.ExportedFunction(functionName)
	if memory == nil || alloc == nil || dealloc == nil || freeResult == nil || fn == nil {
		return fmt.Errorf("ty wasm missing required exports")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal ty request: %w", err)
	}
	ptr, length, err := writeRequest(execCtx, memory, alloc, body)
	if err != nil {
		return err
	}
	defer dealloc.Call(execCtx, ptr, length)

	result, err := fn.Call(execCtx, ptr, length)
	if err != nil {
		return fmt.Errorf("call ty wasm %s: %w", functionName, err)
	}
	if len(result) != 1 {
		return fmt.Errorf("ty wasm %s returned %d values", functionName, len(result))
	}
	packed := result[0]
	defer freeResult.Call(execCtx, packed)

	bytes, err := readPacked(memory, packed)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(bytes, response); err != nil {
		return fmt.Errorf("unmarshal ty response: %w", err)
	}
	return nil
}

func initRuntime(ctx context.Context) runtimeState {
	state.once.Do(func() {
		runtime := wazero.NewRuntime(ctx)
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
			state.data = runtimeState{err: fmt.Errorf("instantiate WASI: %w", err)}
			_ = runtime.Close(ctx)
			return
		}
		wasm, err := tyWASM.ReadFile("ty_wasi.wasm")
		if err != nil {
			state.data = runtimeState{err: fmt.Errorf("read ty wasm: %w", err)}
			_ = runtime.Close(ctx)
			return
		}
		compiled, err := runtime.CompileModule(ctx, wasm)
		if err != nil {
			state.data = runtimeState{err: fmt.Errorf("compile ty wasm: %w", err)}
			_ = runtime.Close(ctx)
			return
		}
		module, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
		if err != nil {
			state.data = runtimeState{err: fmt.Errorf("instantiate ty wasm: %w", err)}
			_ = compiled.Close(ctx)
			_ = runtime.Close(ctx)
			return
		}
		state.data = runtimeState{runtime: runtime, compiled: compiled, module: module}
	})
	return state.data
}

func writeRequest(ctx context.Context, memory wazeroMemory, alloc wazeroFunction, body []byte) (uint64, uint64, error) {
	result, err := alloc.Call(ctx, uint64(len(body)))
	if err != nil {
		return 0, 0, fmt.Errorf("allocate ty request: %w", err)
	}
	if len(result) != 1 {
		return 0, 0, fmt.Errorf("ty_alloc returned %d values", len(result))
	}
	ptr := uint32(result[0])
	if !memory.Write(ptr, body) {
		return 0, 0, fmt.Errorf("write ty request to wasm memory")
	}
	return uint64(ptr), uint64(len(body)), nil
}

func readPacked(memory wazeroMemory, packed uint64) ([]byte, error) {
	ptr := uint32(packed >> 32)
	length := uint32(packed & 0xffff_ffff)
	bytes, ok := memory.Read(ptr, length)
	if !ok {
		return nil, fmt.Errorf("read ty response from wasm memory")
	}
	return bytes, nil
}

type wazeroMemory interface {
	Write(offset uint32, v []byte) bool
	Read(offset uint32, byteCount uint32) ([]byte, bool)
}

type wazeroFunction interface {
	Call(context.Context, ...uint64) ([]uint64, error)
}
