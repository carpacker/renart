package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStandaloneFallsBackToWebWhenGUIIsUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	opened := make(chan struct{})
	var output bytes.Buffer

	err := waitForStandaloneWebFallback(
		ctx,
		&output,
		"http://127.0.0.1:43123/",
		"127.0.0.1:43123",
		make(chan error),
		errors.New("webview libraries are missing"),
		func(_ context.Context, url, address string) {
			if url != "http://127.0.0.1:43123/" || address != "127.0.0.1:43123" {
				t.Errorf("unexpected browser target %q (%q)", url, address)
			}
			close(opened)
			cancel()
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	<-opened
	message := output.String()
	if !strings.Contains(message, "GUI is unavailable") ||
		!strings.Contains(message, "Falling back to the web interface") ||
		!strings.Contains(message, "http://127.0.0.1:43123/") {
		t.Fatalf("unexpected fallback message %q", message)
	}
}
