package cmd

import (
	"context"
	"fmt"
	"io"
	"sync"

	"go.uber.org/zap"
)

const renartASCII = ` ____  _____ _   _    _    ____ _____
|  _ \| ____| \ | |  / \  |  _ \_   _|
| |_) |  _| |  \| | / _ \ | |_) || |
|  _ <| |___| |\  |/ ___ \|  _ < | |
|_| \_\_____|_| \_/_/   \_\_| \_\|_|`

func printRenartWelcome(out io.Writer, appURL, detail string) {
	_, _ = fmt.Fprintln(out, renartASCII)
	_, _ = fmt.Fprintln(out, "Welcome to Renart, your local data pipeline IDE.")
	_, _ = fmt.Fprintf(out, "Renart listening on %s%s\n", appURL, detail)
}

// startGracefulShutdown restores the default signal behavior as soon as the
// first signal cancels ctx. That lets the first Ctrl-C drain schedulers and
// remove discovery files while a second Ctrl-C still force-exits promptly.
func startGracefulShutdown(
	ctx context.Context,
	restoreSignals func(),
	logger *zap.Logger,
	shutdown func(),
) func() {
	stopped := make(chan struct{})
	done := make(chan struct{})
	var stopOnce sync.Once
	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(restoreSignals)
	}
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			restore()
			logger.Info(
				"stopping Renart",
				zap.String("hint", "press Ctrl-C again to force exit"),
			)
			shutdown()
		case <-stopped:
		}
	}()
	return func() {
		if ctx.Err() == nil {
			stopOnce.Do(func() {
				close(stopped)
			})
		}
		<-done
		restore()
	}
}
