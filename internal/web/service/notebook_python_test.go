package service

import (
	"strings"
	"sync"
	"testing"
)

func TestBoundedLogBufferStripsPrefixAndTrims(t *testing.T) {
	var buf boundedLogBuffer
	// bruin writes one prefixed line per Write.
	_, _ = buf.Write([]byte(">> first line\n"))
	_, _ = buf.Write([]byte(">> second line\n"))

	got := buf.String()
	if got != "first line\nsecond line" {
		t.Fatalf("unexpected logs: %q", got)
	}
}

func TestBoundedLogBufferStripsANSI(t *testing.T) {
	var buf boundedLogBuffer
	// uv/rich colorize output with ANSI SGR sequences.
	_, _ = buf.Write([]byte("\x1b[36mUsing CPython 3.11\x1b[39m\n\x1b[2mInstalled 7 packages\x1b[0m\n"))

	got := buf.String()
	if got != "Using CPython 3.11\nInstalled 7 packages" {
		t.Fatalf("ANSI not stripped: %q", got)
	}
}

func TestBoundedLogBufferTruncates(t *testing.T) {
	var buf boundedLogBuffer
	chunk := strings.Repeat("a", 4096)
	for i := 0; i < (notebookLogCap/len(chunk))+10; i++ {
		_, _ = buf.Write([]byte(chunk))
	}
	got := buf.String()
	if !strings.HasSuffix(got, "… output truncated …") {
		t.Fatalf("expected truncation marker, got suffix %q", got[max(0, len(got)-40):])
	}
	if len(got) > notebookLogCap+64 {
		t.Fatalf("buffer exceeded cap: %d", len(got))
	}
}

func TestBoundedLogBufferConcurrentWrites(t *testing.T) {
	var buf boundedLogBuffer
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = buf.Write([]byte(">> concurrent\n"))
			}
		}()
	}
	wg.Wait()
	// The race detector (go test -race) is the real assertion here; just sanity
	// check that something was captured.
	if !strings.Contains(buf.String(), "concurrent") {
		t.Fatal("expected captured output")
	}
}
