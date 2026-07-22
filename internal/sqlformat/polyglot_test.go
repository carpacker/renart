package sqlformat

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPolyglotLongLivedModuleMemory(t *testing.T) {
	wasmBytes, err := wasmFS.ReadFile("polyglot_sql.wasm")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	engine, err := buildPolyglotEngineWithWASM(ctx, compilerRuntimeConfig(), "memory-test", wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.runtime.Close(ctx) })
	pm, err := engine.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	version, err := callPolyglotModule(ctx, pm.module, "version")
	if err != nil {
		t.Fatal(err)
	}
	initial := pm.module.Memory().Size()
	originalModule := pm.module
	engine.release(ctx, pm)
	const query = "select a, missing_column from a.example_asset where a > 0"
	const schema = `{"tables":[{"name":"a.example_asset","columns":[{"name":"a","type":"INTEGER"}]}]}`
	const analysisOptions = `{"dialect":"generic","schema":` + schema + `}`
	if version != polyglotWASMVersion {
		t.Fatalf("embedded Polyglot version = %q, want %q", version, polyglotWASMVersion)
	}
	if _, err := callPolyglotEngine(ctx, engine, "analyze_query", query, analysisOptions); err != nil {
		t.Fatalf("prewarm analyze_query: %v", err)
	}
	pm, err = engine.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	initial = pm.module.Memory().Size()
	originalModule = pm.module
	engine.release(ctx, pm)
	for i := 1; i <= 512; i++ {
		for _, call := range []struct {
			name string
			args []string
		}{
			{name: "parse", args: []string{query, DialectGeneric}},
			{name: "tokenize", args: []string{query, DialectGeneric}},
			{name: "validate_with_schema", args: []string{query, schema, DialectGeneric, `{"strict":true}`}},
			{name: "analyze_query", args: []string{query, analysisOptions}},
		} {
			if _, err := callPolyglotEngine(ctx, engine, call.name, call.args...); err != nil {
				t.Fatalf("cycle %d %s: %v", i, call.name, err)
			}
		}
	}
	pm, err = engine.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.release(ctx, pm)
	if pm.module != originalModule {
		t.Fatal("long-lived stable workload replaced its reusable module")
	}
	if got := pm.module.Memory().Size(); got != initial {
		t.Fatalf("linear memory grew across repeated calls: initial=%d final=%d", initial, got)
	}
}

func TestPolyglotEngineDoesNotPoolClosedModule(t *testing.T) {
	wasmBytes, err := wasmFS.ReadFile("polyglot_sql.wasm")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	engine, err := buildPolyglotEngineWithWASM(ctx, compilerRuntimeConfig(), "closed-test", wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.runtime.Close(ctx) })
	pm, err := engine.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.module.Close(ctx); err != nil {
		t.Fatal(err)
	}
	engine.release(ctx, pm)
	if len(engine.idle) != 0 {
		t.Fatal("closed module was returned to the idle pool")
	}
}

func TestPolyglotEngineDiscardsInflatedModule(t *testing.T) {
	wasmBytes, err := wasmFS.ReadFile("polyglot_sql.wasm")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	engine, err := buildPolyglotEngineWithWASM(ctx, compilerRuntimeConfig(), "growth-test", wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.runtime.Close(ctx) })

	pm, err := engine.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldModule := pm.module
	if _, ok := oldModule.Memory().Grow(polyglotRetainedGrowthPages + 1); !ok {
		t.Fatal("failed to grow test module memory")
	}
	engine.release(ctx, pm)
	if !oldModule.IsClosed() {
		t.Fatal("inflated module was returned to the pool")
	}

	replacement, err := engine.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.release(ctx, replacement)
	if replacement.module == oldModule {
		t.Fatal("expected a fresh module after inflated module was discarded")
	}
	if max, _ := replacement.module.Memory().Definition().Max(); max != polyglotMaxMemoryPages {
		t.Fatalf("module memory max = %d, want %d", max, polyglotMaxMemoryPages)
	}
}

func TestPolyglotRepeatedCallsReuseModule(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		formatted, err := Format(ctx, "select 1 as id", DialectGeneric)
		if err != nil {
			t.Fatalf("format call %d: %v", i, err)
		}
		if formatted == "" {
			t.Fatalf("format call %d returned empty SQL", i)
		}

		parsed, err := Call(ctx, "parse", "select 1 as id", DialectGeneric)
		if err != nil {
			t.Fatalf("parse call %d: %v", i, err)
		}
		var response struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal([]byte(parsed), &response); err != nil {
			t.Fatalf("parse call %d returned invalid JSON: %v", i, err)
		}
		if !response.Success {
			t.Fatalf("parse call %d was not successful: %s", i, parsed)
		}
	}
}
