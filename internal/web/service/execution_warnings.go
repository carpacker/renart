package service

import (
	"context"
	"strings"
	"sync"
)

type executionWarningsKey struct{}

type executionWarnings struct {
	mu    sync.Mutex
	items []string
}

func withExecutionWarnings(ctx context.Context) (context.Context, *executionWarnings) {
	collector := &executionWarnings{}
	return context.WithValue(ctx, executionWarningsKey{}, collector), collector
}

func addExecutionWarning(ctx context.Context, warning string) {
	collector, _ := ctx.Value(executionWarningsKey{}).(*executionWarnings)
	warning = strings.TrimSpace(warning)
	if collector == nil || warning == "" {
		return
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	for _, existing := range collector.items {
		if existing == warning {
			return
		}
	}
	collector.items = append(collector.items, warning)
}

func (w *executionWarnings) snapshot() []string {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.items...)
}
