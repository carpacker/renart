package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/git"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"github.com/urfave/cli/v3"
	"renart/internal/web/events"
	"renart/internal/web/freshness"
	webhttpapi "renart/internal/web/httpapi"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"
	webstatic "renart/internal/web/static"
	"renart/internal/web/watch"
	webui "renart/web"

	"go.uber.org/zap"
)

// serverConfig holds the validated settings shared by the web and
// standalone entry points.
type serverConfig struct {
	workspaceRoot      string
	staticDir          string
	watchMode          string
	watchPoll          time.Duration
	schedulerEnabled   bool
	schedulerStatePath string
}

// serverFlags returns the CLI flags shared by every mode that boots the
// Renart server. Callers get fresh flag instances so commands do not share
// flag state.
func serverFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "static-dir",
			Value: "web/dist",
			Usage: "optional override directory for static web assets",
		},
		&cli.StringFlag{
			Name:  "watch-mode",
			Value: "auto",
			Usage: "workspace watcher mode: auto, fsnotify, or poll",
		},
		&cli.DurationFlag{
			Name:  "watch-poll-interval",
			Value: 2 * time.Second,
			Usage: "poll interval used when watch-mode is poll or auto",
		},
		&cli.BoolFlag{
			Name:  "scheduler",
			Value: true,
			Usage: "run the local in-process scheduler",
		},
		&cli.StringFlag{
			Name:  "scheduler-state",
			Value: ".renart/state.db",
			Usage: "local scheduler SQLite state path",
		},
	}
}

// serverConfigFromCommand resolves and validates the shared server settings
// from CLI flags and the positional workspace root argument.
func serverConfigFromCommand(c *cli.Command) (serverConfig, error) {
	root := c.Args().Get(0)
	if root == "" {
		root = "."
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return serverConfig{}, fmt.Errorf("failed to resolve workspace root: %w", err)
	}

	if _, err := git.FindRepoFromPath(absRoot); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Renart must be started inside a git repository.\n")
		return serverConfig{}, fmt.Errorf("renart must be started inside a git repository: %w", err)
	}

	staticDir := c.String("static-dir")
	if !filepath.IsAbs(staticDir) {
		staticDir = filepath.Join(absRoot, staticDir)
	}

	watchMode := strings.ToLower(strings.TrimSpace(c.String("watch-mode")))
	if watchMode == "" {
		watchMode = "auto"
	}
	if watchMode != "auto" && watchMode != "fsnotify" && watchMode != "poll" {
		return serverConfig{}, fmt.Errorf("invalid watch-mode %q, expected one of: auto, fsnotify, poll", watchMode)
	}

	watchPoll := c.Duration("watch-poll-interval")
	if watchPoll <= 0 {
		return serverConfig{}, fmt.Errorf("watch-poll-interval must be greater than zero")
	}

	statePath := c.String("scheduler-state")
	if !filepath.IsAbs(statePath) {
		statePath = filepath.Join(absRoot, statePath)
	}

	return serverConfig{
		workspaceRoot:      absRoot,
		staticDir:          staticDir,
		watchMode:          watchMode,
		watchPoll:          watchPoll,
		schedulerEnabled:   c.Bool("scheduler"),
		schedulerStatePath: statePath,
	}, nil
}

// newWebServer wires all services, starts the scheduler (when enabled) and
// the filesystem watcher, and performs the initial workspace parse. The
// returned cleanup stops the scheduler and closes its store; the watcher
// stops when ctx is cancelled.
func newWebServer(ctx context.Context, cfg serverConfig, logger *zap.Logger) (*webServer, func(), error) {
	absRoot := cfg.workspaceRoot

	server := &webServer{
		workspaceRoot: absRoot,
		staticDir:     cfg.staticDir,
		watchMode:     cfg.watchMode,
		watchPoll:     cfg.watchPoll,
		workspaceSvc:  service.NewWorkspaceService(absRoot, resolveConfigFilePath(absRoot)),
		configSvc:     service.NewConfigService(absRoot, resolveConfigFilePath(absRoot)),
		pipelineSvc:   service.NewPipelineService(absRoot),
		hub:           events.NewDebouncedHub(150 * time.Millisecond),
		executor:      nil,
		freshness:     freshness.New(),
		duckDBOps:     make(map[string]*sync.Mutex),
		logger:        logger,
	}

	server.executor = service.NewHybridBruinExecutor(absRoot, "", server.newConnectionManager, server.newPipelineBuilder)

	server.executionSvc = service.NewExecutionService(service.ExecutionDependencies{
		WorkspaceRoot:                       absRoot,
		ConfigPath:                          resolveConfigFilePath(absRoot),
		Executor:                            server.executor,
		ResolveAssetByID:                    server.resolveAssetByID,
		ResolveAssetNameByID:                server.findAssetNameByID,
		FindInspectIDs:                      server.findMaterializationInspectIDs,
		RecordMaterialization:               server.freshness.RecordMaterialization,
		RecordMaterializationForEnvironment: server.freshness.RecordMaterializationForEnvironment,
		CurrentPipelines: func() []service.PipelineView {
			state := server.currentState()
			pipelines := make([]service.PipelineView, 0, len(state.Pipelines))
			for _, pipeline := range state.Pipelines {
				assets := make([]service.AssetView, 0, len(pipeline.Assets))
				for _, asset := range pipeline.Assets {
					assets = append(assets, service.AssetView{ID: asset.ID, Name: asset.Name})
				}
				pipelines = append(pipelines, service.PipelineView{ID: pipeline.ID, Assets: assets})
			}
			return pipelines
		},
		DuckDBLock: func(lockKey string) *sync.Mutex {
			return server.getDuckDBOperationMutex(lockKey)
		},
		ParseQueryOutput:   service.ParseQueryJSONOutput,
		NewPipelineBuilder: server.newPipelineBuilder,
		FreshnessSnapshot: func() map[string]service.AssetTimestamps {
			items := server.freshness.GetAll()
			result := make(map[string]service.AssetTimestamps, len(items))
			for key, item := range items {
				result[key] = service.AssetTimestamps{
					MaterializedAt:   item.MaterializedAt,
					ContentChangedAt: item.ContentChangedAt,
					LastStatus:       item.MaterializedStatus,
				}
			}
			return result
		},
	})

	server.assetSvc = service.NewAssetService(service.AssetDependencies{
		Fs:                           afero.NewOsFs(),
		WorkspaceRoot:                absRoot,
		Executor:                     server.executor,
		ResolveAssetByID:             server.resolveAssetByID,
		DefaultAssetContent:          defaultAssetContent,
		DerivedAssetContent:          defaultDerivedSQLAssetContent,
		EnsurePythonRequirements:     ensurePythonRequirementsFile,
		SuppressWatcher:              server.suppressWatcherFor,
		PushWorkspaceUpdate:          server.pushWorkspaceUpdate,
		PushWorkspaceUpdateImmediate: server.pushWorkspaceUpdateImmediate,
		PushWorkspaceUpdateImmediateWithChangedIDs: server.pushWorkspaceUpdateImmediateWithChangedIDs,
	})

	server.sqlSvc = service.NewSQLService(service.SQLDependencies{
		Executor:             server.executor,
		NewConnectionManager: server.newConnectionManager,
		RunConnectionQuery:   server.executionSvc.RunConnectionQueryForEnvironment,
	})

	server.suggestionsSvc = service.NewSuggestionsService(service.SuggestionsDependencies{
		WorkspaceRoot: absRoot,
		ConfigPath:    resolveConfigFilePath(absRoot),
		ResolveAssetByID: func(ctx context.Context, assetID string) (string, any, any, error) {
			path, parsed, asset, err := server.resolveAssetByID(ctx, assetID)
			return path, parsed, asset, err
		},
		NewConnectionManager: server.newConnectionManager,
	})

	server.parseContextSvc = service.NewParseContextService(service.ParseContextDependencies{
		ResolveAssetByID: server.resolveAssetByID,
	})
	server.jinjaRenderSvc = service.NewJinjaRenderService(service.JinjaRenderDependencies{
		ResolveAssetByID: server.resolveAssetByID,
	})

	server.runSvc = service.NewRunService(service.RunDependencies{Executor: server.executor})
	server.onboardingSvc = service.NewOnboardingService(absRoot, resolveConfigFilePath(absRoot), server.executor)
	server.sourceControlSvc = service.NewSourceControlService(absRoot)

	var err error
	server.schedulerStore, err = webscheduler.OpenStore(cfg.schedulerStatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize scheduler store: %w", err)
	}
	server.schedulerSvc = webscheduler.New(webscheduler.Options{
		Store:     server.schedulerStore,
		StateDir:  filepath.Dir(cfg.schedulerStatePath),
		Pipelines: server.pipelineSvc.ListSchedules,
		Publish: func(event any) {
			server.hub.PublishImmediate(event)
		},
		Runner: func(ctx context.Context, req webscheduler.RunRequest, onLog func(string)) webscheduler.RunResult {
			result := server.executionSvc.MaterializePipelineStreamWithAssetEvents(ctx, req.PipelineID, req.Environment, false, req.Start, req.End, func(chunk []byte) {
				if onLog != nil {
					onLog(string(chunk))
				}
			}, func(event service.ExecutionAssetEvent) {
				if req.OnStep == nil {
					return
				}
				req.OnStep(webscheduler.RunStepEvent{
					Asset:      event.Asset,
					Status:     schedulerStatusFromExecutionStatus(event.Status),
					StartedAt:  event.StartedAt,
					FinishedAt: event.FinishedAt,
					Error:      event.Error,
				})
			})
			if result.Output != "" && onLog != nil {
				onLog(result.Output)
			}
			return webscheduler.RunResult{Status: result.Status, Error: result.Error}
		},
	})

	server.workspaceCoord = service.NewWorkspaceCoordinator(service.WorkspaceCoordinatorDependencies{
		WorkspaceService: server.workspaceSvc,
		Hub:              server.hub,
		Freshness:        server.freshness,
		Logger:           logger,
	})

	embeddedStaticFS, err := webui.DistFS()
	if err != nil {
		logger.Warn("embedded web assets unavailable, falling back to static dir", zap.Error(err))
		embeddedStaticFS = nil
	}

	server.staticHandler, err = webstatic.NewHandler(embeddedStaticFS, cfg.staticDir)
	if err != nil {
		server.schedulerStore.Close()
		return nil, nil, fmt.Errorf("failed to initialize static asset handler: %w", err)
	}

	// Bootstrap materialization timestamps from existing run logs.
	logsDir := filepath.Join(absRoot, "logs")
	if err := server.freshness.LoadFromRunLogs(logsDir); err != nil {
		logger.Warn("failed to load run logs for freshness tracking", zap.Error(err))
	}

	if err := server.refreshWorkspace(ctx); err != nil {
		logger.Warn("initial workspace parse failed", zap.Error(err))
	}

	if cfg.schedulerEnabled {
		if err := server.schedulerSvc.Start(ctx); err != nil {
			logger.Warn("failed to start local scheduler", zap.Error(err))
		}
	}

	go watch.New(watch.Config{
		WorkspaceRoot: absRoot,
		Mode:          cfg.watchMode,
		PollInterval:  cfg.watchPoll,
	}, func(ctx context.Context, eventType, eventPath string) {
		if server.isWatcherSuppressed(eventPath) {
			return
		}
		server.pushWorkspaceUpdate(ctx, eventType, eventPath)
	}).Start(ctx)

	cleanup := func() {
		server.schedulerSvc.Stop()
		server.schedulerStore.Close()
	}

	return server, cleanup, nil
}

// buildRouter assembles the chi router with the standard middleware stack
// and all API routes.
func (s *webServer) buildRouter() chi.Router {
	router := chi.NewRouter()
	router.Use(
		webhttpapi.Recoverer(s.logger),
		webhttpapi.SameOriginGuard(),
		webhttpapi.RequestLogger(s.logger),
	)
	s.registerRoutes(router)
	return router
}

// newHTTPServer returns the http.Server used by both modes.
// No ReadTimeout/WriteTimeout: both would sever long-lived SSE streams;
// IdleTimeout only applies between requests.
func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func newServerLogger() (*zap.Logger, error) {
	logger, err := zap.NewDevelopment(zap.WithCaller(false))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}
	return logger, nil
}
