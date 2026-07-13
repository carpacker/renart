package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/git"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"github.com/urfave/cli/v3"
	"renart/internal/web/bus"
	"renart/internal/web/events"
	"renart/internal/web/fingerprint"
	webhttpapi "renart/internal/web/httpapi"
	"renart/internal/web/identity"
	"renart/internal/web/matlog"
	"renart/internal/web/policy"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"
	"renart/internal/web/snapshot"
	"renart/internal/web/sqlformat"
	"renart/internal/web/staleness"
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
	// headless is the embedded-CLI mode (plans/cli-v1.md §2.4): the same
	// service graph without the pieces only a long-lived UI server needs —
	// no static assets, no filesystem watcher, no fingerprint pre-warm (and
	// callers keep the scheduler off so two River schedulers never share a
	// state DB).
	headless bool
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

	// Every served workspace gets a stable identity; the health endpoint and
	// discovery file report it so CLI clients can address the right project.
	project, err := identity.EnsureProject(
		afero.NewOsFs(),
		filepath.Join(absRoot, ".renart", "project.yml"),
		filepath.Base(absRoot),
	)
	if err != nil {
		return nil, nil, err
	}

	server := &webServer{
		projectID:     project.ID,
		projectName:   project.Name,
		workspaceRoot: absRoot,
		staticDir:     cfg.staticDir,
		watchMode:     cfg.watchMode,
		watchPoll:     cfg.watchPoll,
		workspaceSvc:  service.NewWorkspaceService(absRoot, resolveConfigFilePath(absRoot)),
		configSvc:     service.NewConfigService(absRoot, resolveConfigFilePath(absRoot)),
		pipelineSvc:   service.NewPipelineService(absRoot),
		hub:           events.NewDebouncedHub(150 * time.Millisecond),
		executor:      nil,
		eventBus:      bus.New(),
		logger:        logger,
	}

	hybridExecutor := service.NewHybridBruinExecutor(absRoot, "", server.newConnectionManager, server.newPipelineBuilder)
	server.executor = hybridExecutor
	server.policyLoader = policy.NewLoader(filepath.Join(absRoot, ".renart", "environments.yml"))

	server.executionSvc = service.NewExecutionService(service.ExecutionDependencies{
		WorkspaceRoot:        absRoot,
		ConfigPath:           resolveConfigFilePath(absRoot),
		Executor:             server.executor,
		ResolveAssetByID:     server.resolveAssetByID,
		ResolveAssetNameByID: server.findAssetNameByID,
		FindInspectIDs:       server.findMaterializationInspectIDs,
		CurrentPipelines: func() []service.PipelineView {
			state := server.currentState()
			pipelines := make([]service.PipelineView, 0, len(state.Pipelines))
			for _, pipeline := range state.Pipelines {
				assets := make([]service.AssetView, 0, len(pipeline.Assets))
				for _, asset := range pipeline.Assets {
					assets = append(assets, service.AssetView{ID: asset.ID, Name: asset.Name})
				}
				pipelines = append(pipelines, service.PipelineView{ID: pipeline.ID, UUID: pipeline.UUID, Assets: assets})
			}
			return pipelines
		},
		Events: server.eventBus,
		PolicyFor: func(environment string) policy.EnvironmentPolicy {
			if strings.TrimSpace(environment) == "" {
				environment = server.currentState().SelectedEnvironment
			}
			return server.policyLoader.For(environment)
		},
		ParseQueryOutput:   service.ParseQueryJSONOutput,
		NewPipelineBuilder: server.newPipelineBuilder,
	})

	server.assetSvc = service.NewAssetService(service.AssetDependencies{
		Fs:                           afero.NewOsFs(),
		WorkspaceRoot:                absRoot,
		Executor:                     server.executor,
		ResolveAssetByID:             server.resolveAssetByID,
		DefaultAssetContent:          defaultAssetContent,
		DerivedAssetContent:          defaultDerivedSQLAssetContent,
		EnsurePythonProject:          ensurePythonProjectFile,
		SuppressWatcher:              server.suppressWatcherFor,
		PushWorkspaceUpdate:          server.pushWorkspaceUpdate,
		PushWorkspaceUpdateImmediate: server.pushWorkspaceUpdateImmediate,
		PushWorkspaceUpdateImmediateWithChangedIDs: server.pushWorkspaceUpdateImmediateWithChangedIDs,
		PushAssetContentUpdateImmediate:            server.pushAssetContentUpdateImmediate,
		ConnectionTypeFor: func(connectionName string) string {
			return server.currentState().Connections[connectionName]
		},
	})

	server.sqlSvc = service.NewSQLService(service.SQLDependencies{
		Executor:             server.executor,
		NewConnectionManager: server.newConnectionManager,
		RunConnectionQuery:   server.executionSvc.RunConnectionQueryForEnvironment,
	})

	server.loadSvc = service.NewLoadService(service.LoadDependencies{
		WorkspaceRoot:        absRoot,
		NewConnectionManager: server.newConnectionManager,
	})

	server.notebookSvc = service.NewNotebookService(service.NotebookDependencies{
		WorkspaceRoot:       absRoot,
		ConfigPath:          resolveConfigFilePath(absRoot),
		CurrentState:        func() service.WorkspaceState { return server.currentState() },
		RunConnectionQuery:  server.executionSvc.RunConnectionQueryForEnvironment,
		PushWorkspaceUpdate: server.pushWorkspaceUpdate,
		// Validate cells for server-side auto-recompute with the same
		// parse-context the editor uses (constructed below; referenced lazily).
		ValidateSQL: func(ctx context.Context, assetID, content string, schemaTables []service.ParseContextSchemaTable) (service.ParseContextResult, *service.APIError) {
			return server.parseContextSvc.Parse(ctx, assetID, content, schemaTables)
		},
		PublishEvent: func(payload any) { server.hub.PublishImmediate(payload) },
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
	server.sqlLSPSvc = service.NewSQLLSPService(service.SQLLSPDependencies{
		WorkspaceRoot: absRoot,
		CurrentState: func() service.WorkspaceState {
			return server.currentState()
		},
		PolyglotClient: service.NewLazyPolyglotClient(),
	})
	sqlformat.PrewarmPolyglotCompiler()
	server.jinjaRenderSvc = service.NewJinjaRenderService(service.JinjaRenderDependencies{
		ResolveAssetByID: server.resolveAssetByID,
	})

	server.runSvc = service.NewRunService(service.RunDependencies{Executor: server.executor})
	server.onboardingSvc = service.NewOnboardingService(absRoot, resolveConfigFilePath(absRoot), server.executor)
	server.sourceControlSvc = service.NewSourceControlService(absRoot)

	server.schedulerStore, err = webscheduler.OpenStore(cfg.schedulerStatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize scheduler store: %w", err)
	}
	server.fingerprintEngine = fingerprint.NewEngine()
	server.matlogStore = matlog.NewStore(server.schedulerStore.DB())
	server.snapshotStore = snapshot.NewStore(server.schedulerStore.DB())
	recorder := matlog.NewRecorder(server.matlogStore, server.fingerprintEngine, server.resolvePipelineByUUID, server.parsePipelineDir, logger)
	// Subscription order matters: the recorder writes coverage before any
	// later subscriber (the staleness service) re-reads it.
	server.eventBus.OnRunCompleted(recorder.HandleRunCompleted)
	server.eventBus.OnAssetSaved(func(event bus.AssetSaved) {
		server.fingerprintEngine.Invalidate(event.AssetID)
	})

	server.stalenessSvc = staleness.New(staleness.Dependencies{
		Store:   server.matlogStore,
		Engine:  server.fingerprintEngine,
		Resolve: server.resolvePipelineByUUID,
		Publish: func(event any) {
			server.hub.PublishImmediate(event)
		},
		Verify: server.verifyMaterializedAssets,
		Logger: logger,
	})
	server.stalenessSvc.AttachBus(server.eventBus)

	server.schedulerSvc = webscheduler.New(webscheduler.Options{
		Store:     server.schedulerStore,
		StateDir:  filepath.Dir(cfg.schedulerStatePath),
		Pipelines: server.pipelineSvc.ListSchedules,
		Publish: func(event any) {
			server.hub.PublishImmediate(event)
		},
		Housekeeping: func(ctx context.Context) error {
			_, pruneErr := server.matlogStore.Prune(ctx, time.Now().UTC().AddDate(0, 0, -90))
			return pruneErr
		},
		ResolvePipelineRef: func(ctx context.Context, pipelineUUID string) (webscheduler.PipelineRef, bool) {
			for _, p := range server.currentState().Pipelines {
				if p.UUID == pipelineUUID {
					return webscheduler.PipelineRef{EncodedID: p.ID, Name: p.Name}, true
				}
			}
			return webscheduler.PipelineRef{}, false
		},
		DefaultEnvironment: func() string {
			return server.currentState().SelectedEnvironment
		},
		PipelineIntervalAware: func(ctx context.Context, pipelineUUID string) bool {
			parsed, err := server.resolvePipelineByUUID(ctx, pipelineUUID)
			if err != nil {
				return false
			}
			for _, asset := range parsed.Assets {
				if matlog.IntervalAware(asset) {
					return true
				}
			}
			return false
		},
		DeployPipeline: func(ctx context.Context, pipelineUUID string) (string, error) {
			for _, p := range server.currentState().Pipelines {
				if p.UUID != pipelineUUID {
					continue
				}
				absPath, err := service.SafeJoin(absRoot, p.Path)
				if err != nil {
					return "", err
				}
				deployed, _, err := server.snapshotStore.Deploy(ctx, pipelineUUID, absPath, "schedule")
				if err != nil {
					return "", err
				}
				return deployed.VersionID, nil
			}
			return "", fmt.Errorf("pipeline %s not found in workspace", pipelineUUID)
		},
		RecoverRun: server.replayRecoveredRun,
		Runner: func(ctx context.Context, req webscheduler.RunRequest, onLog func(string)) webscheduler.RunResult {
			spec := service.PipelineRunSpec{
				RunID:             req.RunID,
				PipelineID:        req.PipelineID,
				Environment:       req.Environment,
				StartDate:         req.Start,
				EndDate:           req.End,
				SnapshotVersionID: req.SnapshotVersionID,
			}
			// Scheduled runs execute the schedule's pinned snapshot (or the
			// latest deployed one); build mode keeps running the working tree.
			cleanupSnapshot := server.resolveScheduledRunSnapshot(ctx, &spec, onLog)
			defer cleanupSnapshot()

			result := server.executionSvc.MaterializePipelineRun(ctx, spec, func(chunk []byte) {
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
		Logger:           logger,
		Events:           server.eventBus,
	})

	if !cfg.headless {
		embeddedStaticFS, distErr := webui.DistFS()
		if distErr != nil {
			logger.Warn("embedded web assets unavailable, falling back to static dir", zap.Error(distErr))
			embeddedStaticFS = nil
		}

		server.staticHandler, err = webstatic.NewHandler(embeddedStaticFS, cfg.staticDir)
		if err != nil {
			server.schedulerStore.Close()
			return nil, nil, fmt.Errorf("failed to initialize static asset handler: %w", err)
		}
	}

	if err := server.refreshWorkspace(ctx); err != nil {
		logger.Warn("initial workspace parse failed", zap.Error(err))
	}

	// Notebook session DBs are disposable: remove files for notebooks that
	// no longer exist (covers kill -9 mid-session and deleted notebooks).
	if removed, err := server.notebookSvc.SweepSessions(); err != nil {
		logger.Warn("notebook session sweep failed", zap.Error(err))
	} else if len(removed) > 0 {
		logger.Info("removed stale notebook sessions", zap.Strings("notebooks", removed))
	}

	if !cfg.headless {
		// Warm the fingerprint engine's formatter cache off the request path:
		// the first SQL normalization per asset costs tens of milliseconds in
		// the wasm formatter, and the first staleness fetch should not pay it.
		go server.warmFingerprintCache(ctx)
	}

	if cfg.schedulerEnabled {
		if err := server.schedulerSvc.Start(ctx); err != nil {
			logger.Warn("failed to start local scheduler", zap.Error(err))
		}
	}

	if !cfg.headless {
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
	}

	cleanup := func() {
		server.schedulerSvc.Stop()
		server.schedulerStore.Close()
	}

	return server, cleanup, nil
}

// buildRouter assembles the chi router with the standard middleware stack
// and all API routes.
func (s *webServer) buildRouter(sessionToken string) chi.Router {
	router := chi.NewRouter()
	router.Use(
		webhttpapi.Recoverer(s.logger),
		webhttpapi.SameOriginGuardWithToken(sessionToken),
		webhttpapi.RequestLogger(s.logger),
	)
	s.registerRoutes(router)
	return router
}

// newSessionToken mints the per-process secret written into the discovery
// files; CLI requests present it to bypass the Origin guard explicitly.
func newSessionToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Token auth is an additive trust path; without entropy we simply
		// don't offer it and requests fall back to the no-Origin allowance.
		return ""
	}
	return hex.EncodeToString(raw)
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
