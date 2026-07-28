package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPServerShutdownDrainsLifecycleBoundEventStream(t *testing.T) {
	t.Parallel()
	serverCtx, cancelServer := context.WithCancel(context.Background())
	handlerDone := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ready\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := newHTTPServer(serverCtx, listener.Addr().String(), handler)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	response, err := http.Get(fmt.Sprintf("http://%s", listener.Addr()))
	require.NoError(t, err)
	defer response.Body.Close()
	_, err = bufio.NewReader(response.Body).ReadString('\n')
	require.NoError(t, err)

	cancelServer()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	require.NoError(t, server.Shutdown(shutdownCtx))

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("event stream did not leave after the server context was cancelled")
	}
	assert.ErrorIs(t, <-serveDone, http.ErrServerClosed)
}

func TestServerLoggerOmitsRoutineWarningCallersAndStacks(t *testing.T) {
	t.Parallel()
	cfg := serverLoggerConfig()
	logPath := filepath.Join(t.TempDir(), "server.log")
	cfg.OutputPaths = []string{logPath}
	cfg.ErrorOutputPaths = []string{logPath}

	assert.True(t, cfg.DisableCaller)
	assert.True(t, cfg.DisableStacktrace)

	logger, err := cfg.Build()
	require.NoError(t, err)
	logger.Warn("routine warning", zap.String("warning", "expected"))
	require.NoError(t, logger.Sync())

	logged, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(logged), "routine warning")
	assert.NotContains(t, string(logged), "http_server_test.go")
	assert.NotContains(t, string(logged), "\nrenart/")
}
