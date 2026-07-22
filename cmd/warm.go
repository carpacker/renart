package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"

	"renart/internal/sqlformat"
	"renart/internal/web/pyintelligence"
)

// WarmCache compiles the embedded WASM modules (the polyglot SQL formatter and
// the ty Python linter) into the shared on-disk wazero compilation cache, then
// exits. Compiling an 18MB module with the optimizing compiler is slow, so a
// process that hits a cold cache pays seconds-to-tens-of-seconds and meanwhile
// leans on the memory-heavy interpreter. Running this once up front (e.g.
// before the live e2e suite, whose many short-lived servers would otherwise
// each start cold) warms the cache so subsequent servers get a ready compiler
// almost immediately and stay lean.
func WarmCache() *cli.Command {
	return &cli.Command{
		Name:  "warm-cache",
		Usage: "compile the embedded WASM modules into the on-disk cache, then exit",
		Action: func(ctx context.Context, _ *cli.Command) error {
			return warmWasmCaches(ctx)
		},
	}
}

func warmWasmCaches(ctx context.Context) error {
	// ty: any call forces the runtime to compile and cache before it runs.
	if _, err := pyintelligence.Check(ctx, pyintelligence.Request{
		Path:    "warm.py",
		Content: "x = 1\n",
	}); err != nil {
		return fmt.Errorf("warm ty wasm: %w", err)
	}

	// polyglot SQL: the compiler builds in the background and writes the cache
	// once done. Wait for it rather than calling Format, so we never build the
	// interpreter here.
	sqlformat.PrewarmPolyglotCompiler()
	deadline := time.Now().Add(5 * time.Minute)
	for !sqlformat.PolyglotCompilerReady() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("warm polyglot wasm: compiler not ready before timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
