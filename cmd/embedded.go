package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/bruin-data/bruin/pkg/git"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newEmbeddedServer boots the headless service graph for CLI commands that
// run without a server (plans/cli-v1.md §2.4): the exact wiring the web
// server uses — execution dispatch, policy check, matlog recorder, staleness
// bus — minus watcher, static assets, and the River scheduler *service* (the
// store still opens so run facts and coverage land in .renart/state.db and
// the next server start sees correct staleness).
func newEmbeddedServer(ctx context.Context, workspaceRoot string) (*webServer, func(), error) {
	if _, err := git.FindRepoFromPath(workspaceRoot); err != nil {
		return nil, nil, fmt.Errorf("renart workspaces live inside a git repository: %w", err)
	}

	cfg := serverConfig{
		workspaceRoot:      workspaceRoot,
		watchMode:          "auto", // unused: headless skips the watcher
		schedulerEnabled:   false,  // never a second River scheduler on the same DB
		schedulerStatePath: filepath.Join(workspaceRoot, ".renart", "state.db"),
		headless:           true,
	}

	logger, err := newQuietLogger()
	if err != nil {
		return nil, nil, err
	}

	return newWebServer(ctx, cfg, logger)
}

// newQuietLogger keeps embedded-mode CLI output clean: the run's own output
// goes to stdout; only warnings and errors from the service graph surface.
func newQuietLogger() (*zap.Logger, error) {
	cfg := zap.NewDevelopmentConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	cfg.DisableCaller = true
	cfg.DisableStacktrace = true
	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}
	return logger, nil
}
