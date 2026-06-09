package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/connection"
	"github.com/bruin-data/bruin/pkg/git"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"github.com/urfave/cli/v3"
	webapi "renart/internal/web/api"
	"renart/internal/web/events"
	"renart/internal/web/freshness"
	webhttpapi "renart/internal/web/httpapi"
	webmodel "renart/internal/web/model"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"
	webstatic "renart/internal/web/static"
	"renart/internal/web/watch"
	webui "renart/web"

	"golang.org/x/net/http2"
)

type webAsset struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Path                string            `json:"path"`
	Content             string            `json:"content"`
	Upstreams           []string          `json:"upstreams"`
	Parameters          map[string]string `json:"parameters,omitempty"`
	Meta                map[string]string `json:"meta,omitempty"`
	Columns             []webColumn       `json:"columns,omitempty"`
	Connection          string            `json:"connection,omitempty"`
	MaterializationType string            `json:"materialization_type,omitempty"`
	IsMaterialized      bool              `json:"is_materialized"`
	MaterializedAs      string            `json:"materialized_as,omitempty"`
	RowCount            *int64            `json:"row_count,omitempty"`
}

type webColumnCheck struct {
	Name        string `json:"name"`
	Value       any    `json:"value,omitempty"`
	Blocking    *bool  `json:"blocking,omitempty"`
	Description string `json:"description,omitempty"`
}

type webColumn struct {
	Name          string            `json:"name"`
	Type          string            `json:"type,omitempty"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	PrimaryKey    bool              `json:"primary_key,omitempty"`
	UpdateOnMerge bool              `json:"update_on_merge,omitempty"`
	MergeSQL      string            `json:"merge_sql,omitempty"`
	Nullable      *bool             `json:"nullable,omitempty"`
	Owner         string            `json:"owner,omitempty"`
	Domains       []string          `json:"domains,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
	Checks        []webColumnCheck  `json:"checks,omitempty"`
}

type apiError = webhttpapi.APIError
type assetMutationResponse = webhttpapi.AssetMutationResponse
type formatSQLAssetResponse = webhttpapi.FormatSQLAssetResponse
type formatPythonAssetResponse = webhttpapi.FormatPythonAssetResponse
type pythonDiagnosticsResponse = webhttpapi.PythonDiagnosticsResponse
type pythonCompletionsResponse = webhttpapi.PythonCompletionsResponse
type pythonHoverResponse = webhttpapi.PythonHoverResponse
type pythonSignatureHelpResponse = webhttpapi.PythonSignatureHelpResponse
type pythonGotoDefinitionResponse = webhttpapi.PythonGotoDefinitionResponse
type statusResponse = webhttpapi.StatusResponse

type ingestrSuggestionItem struct {
	Value  string `json:"value"`
	Kind   string `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type sqlDiscoveryTableItem struct {
	Name         string `json:"name"`
	ShortName    string `json:"short_name"`
	SchemaName   string `json:"schema_name,omitempty"`
	DatabaseName string `json:"database_name,omitempty"`
}

type webPipeline struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Schedule string     `json:"schedule,omitempty"`
	Assets   []webAsset `json:"assets"`
}

type workspaceState struct {
	Pipelines           []webPipeline       `json:"pipelines"`
	Connections         map[string]string   `json:"connections"`
	SelectedEnvironment string              `json:"selected_environment"`
	Errors              []string            `json:"errors"`
	UpdatedAt           time.Time           `json:"updated_at"`
	Metadata            map[string][]string `json:"metadata"`
	Revision            int64               `json:"revision,omitempty"`
}

type workspaceConfigFieldDef = service.WorkspaceConfigFieldDef

type webServer struct {
	workspaceRoot   string
	staticDir       string
	staticHandler   http.Handler
	watchMode       string
	watchPoll       time.Duration
	workspaceSvc    *service.WorkspaceService
	configSvc       *service.ConfigService
	pipelineSvc     *service.PipelineService
	executionSvc    *service.ExecutionService
	assetSvc        *service.AssetService
	sqlSvc          *service.SQLService
	suggestionsSvc  *service.SuggestionsService
	parseContextSvc *service.ParseContextService
	jinjaRenderSvc  *service.JinjaRenderService
	runSvc          *service.RunService
	onboardingSvc   *service.OnboardingService
	schedulerSvc    *webscheduler.Service
	schedulerStore  *webscheduler.Store
	workspaceCoord  *service.WorkspaceCoordinator

	hub       *events.Hub
	executor  service.BruinCommandExecutor
	freshness *freshness.Tracker

	duckDBOpsMu sync.Mutex
	duckDBOps   map[string]*sync.Mutex
}

func Web() *cli.Command {
	return &cli.Command{
		Name:      "web",
		Usage:     "start Renart UI server",
		ArgsUsage: "[workspace root]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: "127.0.0.1",
				Usage: "host interface to bind",
			},
			&cli.IntFlag{
				Name:  "port",
				Value: 8080,
				Usage: "HTTP port",
			},
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
			&cli.StringFlag{
				Name:  "tls-cert",
				Usage: "optional TLS certificate path; enables HTTPS and HTTP/2 when used with --tls-key",
			},
			&cli.StringFlag{
				Name:  "tls-key",
				Usage: "optional TLS private key path; enables HTTPS and HTTP/2 when used with --tls-cert",
			},
			&cli.BoolFlag{
				Name:  "no-open",
				Usage: "do not open Renart in the default browser after startup",
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
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			root := c.Args().Get(0)
			if root == "" {
				root = "."
			}

			absRoot, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("failed to resolve workspace root: %w", err)
			}

			if _, err := git.FindRepoFromPath(absRoot); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Renart must be started inside a git repository.\n")
				return fmt.Errorf("renart must be started inside a git repository: %w", err)
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
				return fmt.Errorf("invalid watch-mode %q, expected one of: auto, fsnotify, poll", watchMode)
			}

			watchPoll := c.Duration("watch-poll-interval")
			if watchPoll <= 0 {
				return fmt.Errorf("watch-poll-interval must be greater than zero")
			}

			server := &webServer{
				workspaceRoot: absRoot,
				staticDir:     staticDir,
				watchMode:     watchMode,
				watchPoll:     watchPoll,
				workspaceSvc:  service.NewWorkspaceService(absRoot, resolveConfigFilePath(absRoot)),
				configSvc:     service.NewConfigService(absRoot, resolveConfigFilePath(absRoot)),
				pipelineSvc:   service.NewPipelineService(absRoot),
				hub:           events.NewDebouncedHub(150 * time.Millisecond),
				executor:      nil,
				freshness:     freshness.New(),
				duckDBOps:     make(map[string]*sync.Mutex),
			}

			server.executor = service.NewHybridBruinExecutor(absRoot, "", server.newConnectionManager, server.newPipelineBuilder)

			server.executionSvc = service.NewExecutionService(service.ExecutionDependencies{
				WorkspaceRoot:         absRoot,
				ConfigPath:            resolveConfigFilePath(absRoot),
				Executor:              server.executor,
				ResolveAssetByID:      server.resolveAssetByID,
				ResolveAssetNameByID:  server.findAssetNameByID,
				FindInspectIDs:        server.findMaterializationInspectIDs,
				RecordMaterialization: server.freshness.RecordMaterialization,
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

			statePath := c.String("scheduler-state")
			if !filepath.IsAbs(statePath) {
				statePath = filepath.Join(absRoot, statePath)
			}
			server.schedulerStore, err = webscheduler.OpenStore(statePath)
			if err != nil {
				return fmt.Errorf("failed to initialize scheduler store: %w", err)
			}
			defer server.schedulerStore.Close()
			server.schedulerSvc = webscheduler.New(webscheduler.Options{
				Store:     server.schedulerStore,
				StateDir:  filepath.Dir(statePath),
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
				ConvertState: func(state webmodel.WorkspaceState) service.WorkspaceState {
					return workspaceCoordStateFromModel(state)
				},
			})

			embeddedStaticFS, err := webui.DistFS()
			if err != nil {
				embeddedStaticFS = nil
			}

			server.staticHandler, err = webstatic.NewHandler(embeddedStaticFS, staticDir)
			if err != nil {
				return fmt.Errorf("failed to initialize static asset handler: %w", err)
			}

			// Bootstrap materialization timestamps from existing run logs.
			logsDir := filepath.Join(absRoot, "logs")
			if err := server.freshness.LoadFromRunLogs(logsDir); err != nil {
				fmt.Printf("warning: failed to load run logs for freshness tracking: %v\n", err)
			}

			if err := server.refreshWorkspace(ctx); err != nil {
				fmt.Printf("warning: initial workspace parse failed: %v\n", err)
			}

			if c.Bool("scheduler") {
				if err := server.schedulerSvc.Start(ctx); err != nil {
					fmt.Printf("warning: failed to start local scheduler: %v\n", err)
				}
			}

			go watch.New(watch.Config{
				WorkspaceRoot: absRoot,
				Mode:          watchMode,
				PollInterval:  watchPoll,
			}, func(ctx context.Context, eventType, eventPath string) {
				if server.isWatcherSuppressed(eventPath) {
					return
				}
				server.pushWorkspaceUpdate(ctx, eventType, eventPath)
			}).Start(ctx)

			router := chi.NewRouter()
			server.registerRoutes(router)

			host := c.String("host")
			port := c.Int("port")
			tlsCert := strings.TrimSpace(c.String("tls-cert"))
			tlsKey := strings.TrimSpace(c.String("tls-key"))
			if (tlsCert == "") != (tlsKey == "") {
				return fmt.Errorf("--tls-cert and --tls-key must be provided together")
			}

			listener, address, err := listenWithDefaultPortFallback(host, port)
			if err != nil {
				return err
			}
			defer listener.Close()

			httpServer := &http.Server{
				Addr:              address,
				Handler:           router,
				ReadHeaderTimeout: 10 * time.Second,
			}
			if tlsCert != "" {
				if err := http2.ConfigureServer(httpServer, &http2.Server{}); err != nil {
					return fmt.Errorf("failed to configure HTTP/2: %w", err)
				}
				fmt.Printf("Renart listening on https://%s (HTTP/2 enabled)\n", address)
			} else {
				fmt.Printf("Renart listening on http://%s\n", address)
			}

			if !c.Bool("no-open") {
				scheme := "http"
				if tlsCert != "" {
					scheme = "https"
				}
				go openBrowserWhenReachable(ctx, scheme+"://"+address, address)
			}

			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpServer.Shutdown(shutdownCtx)
			}()

			if tlsCert != "" {
				err = httpServer.ServeTLS(listener, tlsCert, tlsKey)
			} else {
				err = httpServer.Serve(listener)
			}
			if err != nil && err != http.ErrServerClosed {
				return err
			}

			return nil
		},
		Before: telemetry.BeforeCommand,
		After:  telemetry.AfterCommand,
	}
}

func openBrowserWhenReachable(ctx context.Context, url, address string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			if err := openBrowser(url); err != nil {
				fmt.Printf("warning: failed to open browser: %v\n", err)
			}
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("warning: server did not become reachable quickly enough to open browser automatically; open %s manually\n", url)
}

func openBrowser(url string) error {
	var command string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{url}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		command = "xdg-open"
		args = []string{url}
	}

	return exec.Command(command, args...).Start()
}

func listenWithDefaultPortFallback(host string, port int) (net.Listener, string, error) {
	address := fmt.Sprintf("%s:%d", host, port)
	listener, err := net.Listen("tcp", address)
	if err == nil {
		return listener, address, nil
	}

	if port != 8080 {
		return nil, "", fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	firstErr := err
	for fallbackPort := 8081; fallbackPort <= 8099; fallbackPort++ {
		fallbackAddress := fmt.Sprintf("%s:%d", host, fallbackPort)
		listener, err = net.Listen("tcp", fallbackAddress)
		if err == nil {
			fmt.Printf("warning: %s is unavailable, using fallback port %d instead\n", address, fallbackPort)
			return listener, fallbackAddress, nil
		}
	}

	return nil, "", fmt.Errorf("failed to listen on %s and no fallback port from 8081 to 8099 was available: %w", address, firstErr)
}

func (s *webServer) registerRoutes(router chi.Router) {
	webhttpapi.RegisterWorkspaceRoutes(router, &webhttpapi.WorkspaceHandlers{Reader: s})
	webhttpapi.RegisterConfigRoutes(router, &webhttpapi.ConfigHandlers{Service: s.configSvc, Publisher: s})
	webhttpapi.RegisterPipelineRoutes(router, &webhttpapi.PipelineHandlers{Service: s.pipelineSvc, Publisher: s})
	webhttpapi.RegisterExecutionRoutes(router, &webhttpapi.ExecutionAPI{Service: s})
	webhttpapi.RegisterAssetRoutes(router, &webhttpapi.AssetsAPI{Service: s})
	webhttpapi.RegisterAssetColumnRoutes(router, &webhttpapi.AssetColumnsAPI{Service: s})
	webhttpapi.RegisterPipelineExecutionRoutes(router, &webhttpapi.PipelineExecutionAPI{Service: s})
	webhttpapi.RegisterSQLRoutes(router, &webhttpapi.SQLAPI{Service: s})
	webhttpapi.RegisterSuggestionRoutes(router, &webhttpapi.SuggestionsAPI{Service: s})
	webhttpapi.RegisterParseContextRoutes(router, &webhttpapi.ParseContextAPI{Service: s})
	webhttpapi.RegisterJinjaRenderRoutes(router, &webhttpapi.JinjaRenderAPI{Service: s})
	webhttpapi.RegisterRunRoutes(router, &webhttpapi.RunAPI{Service: s})
	webhttpapi.RegisterSchedulerRoutes(router, &webhttpapi.SchedulerAPI{Service: s})
	webhttpapi.RegisterOnboardingRoutes(router, &webhttpapi.OnboardingAPI{Service: s.onboardingSvc, Publisher: s})
	router.Get("/api/assets/freshness", s.handleGetAssetFreshness)

	router.Get("/*", s.handleStatic)
}

func (s *webServer) currentState() workspaceState {
	return workspaceStateFromCoord(s.workspaceCoord.CurrentState())
}

func (s *webServer) setState(state workspaceState) {
	s.workspaceCoord.SetState(workspaceCoordStateFromWeb(state))
}

func (s *webServer) refreshWorkspace(ctx context.Context) error {
	return s.workspaceCoord.Refresh(ctx)
}

func workspaceStateFromModel(state webmodel.WorkspaceState) workspaceState {
	result := workspaceState{
		Pipelines:           make([]webPipeline, 0, len(state.Pipelines)),
		Connections:         mapsClone(state.Connections),
		SelectedEnvironment: state.SelectedEnvironment,
		Errors:              append([]string(nil), state.Errors...),
		UpdatedAt:           state.UpdatedAt,
		Metadata:            mapSliceClone(state.Metadata),
	}

	for _, pipeline := range state.Pipelines {
		result.Pipelines = append(result.Pipelines, webPipelineFromModel(pipeline))
	}

	return result
}

func workspaceCoordStateFromModel(state webmodel.WorkspaceState) service.WorkspaceState {
	result := service.WorkspaceState{
		Pipelines:           make([]service.WorkspacePipeline, 0, len(state.Pipelines)),
		Connections:         mapsClone(state.Connections),
		SelectedEnvironment: state.SelectedEnvironment,
		Errors:              append([]string(nil), state.Errors...),
		UpdatedAt:           state.UpdatedAt,
		Metadata:            mapSliceClone(state.Metadata),
	}

	for _, pipeline := range state.Pipelines {
		result.Pipelines = append(result.Pipelines, workspaceCoordPipelineFromModel(pipeline))
	}

	return result
}

func workspaceCoordPipelineFromModel(pipeline webmodel.Pipeline) service.WorkspacePipeline {
	result := service.WorkspacePipeline{
		ID:       pipeline.ID,
		Name:     pipeline.Name,
		Path:     pipeline.Path,
		Schedule: pipeline.Schedule,
		Assets:   make([]service.WorkspaceAsset, 0, len(pipeline.Assets)),
	}

	for _, asset := range pipeline.Assets {
		result.Assets = append(result.Assets, workspaceCoordAssetFromModel(asset))
	}

	return result
}

func workspaceCoordAssetFromModel(asset webmodel.Asset) service.WorkspaceAsset {
	return service.WorkspaceAsset{
		ID:                  asset.ID,
		Name:                asset.Name,
		Type:                asset.Type,
		Path:                asset.Path,
		Content:             asset.Content,
		Upstreams:           append([]string(nil), asset.Upstreams...),
		Parameters:          mapsClone(asset.Parameters),
		Meta:                mapsClone(asset.Meta),
		Columns:             workspaceCoordColumnsFromModel(asset.Columns),
		Connection:          asset.Connection,
		MaterializationType: asset.MaterializationType,
		IsMaterialized:      asset.IsMaterialized,
		MaterializedAs:      asset.MaterializedAs,
		RowCount:            asset.RowCount,
	}
}

func workspaceStateFromCoord(state service.WorkspaceState) workspaceState {
	result := workspaceState{
		Pipelines:           make([]webPipeline, 0, len(state.Pipelines)),
		Connections:         mapsClone(state.Connections),
		SelectedEnvironment: state.SelectedEnvironment,
		Errors:              append([]string(nil), state.Errors...),
		UpdatedAt:           state.UpdatedAt,
		Metadata:            mapSliceClone(state.Metadata),
		Revision:            state.Revision,
	}

	for _, pipeline := range state.Pipelines {
		result.Pipelines = append(result.Pipelines, workspacePipelineFromCoord(pipeline))
	}

	return result
}

func workspaceCoordStateFromWeb(state workspaceState) service.WorkspaceState {
	result := service.WorkspaceState{
		Pipelines:           make([]service.WorkspacePipeline, 0, len(state.Pipelines)),
		Connections:         mapsClone(state.Connections),
		SelectedEnvironment: state.SelectedEnvironment,
		Errors:              append([]string(nil), state.Errors...),
		UpdatedAt:           state.UpdatedAt,
		Metadata:            mapSliceClone(state.Metadata),
		Revision:            state.Revision,
	}

	for _, pipeline := range state.Pipelines {
		assets := make([]service.WorkspaceAsset, 0, len(pipeline.Assets))
		for _, asset := range pipeline.Assets {
			assets = append(assets, service.WorkspaceAsset{
				ID:                  asset.ID,
				Name:                asset.Name,
				Type:                asset.Type,
				Path:                asset.Path,
				Content:             asset.Content,
				Upstreams:           append([]string(nil), asset.Upstreams...),
				Parameters:          mapsClone(asset.Parameters),
				Meta:                mapsClone(asset.Meta),
				Columns:             workspaceCoordColumnsFromWeb(asset.Columns),
				Connection:          asset.Connection,
				MaterializationType: asset.MaterializationType,
				IsMaterialized:      asset.IsMaterialized,
				MaterializedAs:      asset.MaterializedAs,
				RowCount:            asset.RowCount,
			})
		}
		result.Pipelines = append(result.Pipelines, service.WorkspacePipeline{
			ID:       pipeline.ID,
			Name:     pipeline.Name,
			Path:     pipeline.Path,
			Schedule: pipeline.Schedule,
			Assets:   assets,
		})
	}

	return result
}

func workspacePipelineFromCoord(pipeline service.WorkspacePipeline) webPipeline {
	result := webPipeline{
		ID:       pipeline.ID,
		Name:     pipeline.Name,
		Path:     pipeline.Path,
		Schedule: pipeline.Schedule,
		Assets:   make([]webAsset, 0, len(pipeline.Assets)),
	}

	for _, asset := range pipeline.Assets {
		result.Assets = append(result.Assets, webAsset{
			ID:                  asset.ID,
			Name:                asset.Name,
			Type:                asset.Type,
			Path:                asset.Path,
			Content:             asset.Content,
			Upstreams:           append([]string(nil), asset.Upstreams...),
			Parameters:          mapsClone(asset.Parameters),
			Meta:                mapsClone(asset.Meta),
			Columns:             webColumnsFromCoord(asset.Columns),
			Connection:          asset.Connection,
			MaterializationType: asset.MaterializationType,
			IsMaterialized:      asset.IsMaterialized,
			MaterializedAs:      asset.MaterializedAs,
			RowCount:            asset.RowCount,
		})
	}

	return result
}

func webPipelineFromModel(pipeline webmodel.Pipeline) webPipeline {
	result := webPipeline{
		ID:       pipeline.ID,
		Name:     pipeline.Name,
		Path:     pipeline.Path,
		Schedule: pipeline.Schedule,
		Assets:   make([]webAsset, 0, len(pipeline.Assets)),
	}

	for _, asset := range pipeline.Assets {
		result.Assets = append(result.Assets, webAssetFromModel(asset))
	}

	return result
}

func webAssetFromModel(asset webmodel.Asset) webAsset {
	result := webAsset{
		ID:                  asset.ID,
		Name:                asset.Name,
		Type:                asset.Type,
		Path:                asset.Path,
		Content:             asset.Content,
		Upstreams:           append([]string(nil), asset.Upstreams...),
		Parameters:          mapsClone(asset.Parameters),
		Meta:                mapsClone(asset.Meta),
		Columns:             make([]webColumn, 0, len(asset.Columns)),
		Connection:          asset.Connection,
		MaterializationType: asset.MaterializationType,
		IsMaterialized:      asset.IsMaterialized,
		MaterializedAs:      asset.MaterializedAs,
		RowCount:            asset.RowCount,
	}

	for _, column := range asset.Columns {
		result.Columns = append(result.Columns, webColumnFromModel(column))
	}

	return result
}

func webColumnFromModel(column webmodel.Column) webColumn {
	result := webColumn{
		Name:          column.Name,
		Type:          column.Type,
		Description:   column.Description,
		Tags:          append([]string(nil), column.Tags...),
		PrimaryKey:    column.PrimaryKey,
		UpdateOnMerge: column.UpdateOnMerge,
		MergeSQL:      column.MergeSQL,
		Nullable:      column.Nullable,
		Owner:         column.Owner,
		Domains:       append([]string(nil), column.Domains...),
		Meta:          mapsClone(column.Meta),
		Checks:        make([]webColumnCheck, 0, len(column.Checks)),
	}

	for _, check := range column.Checks {
		result.Checks = append(result.Checks, webColumnCheck{
			Name:        check.Name,
			Value:       check.Value,
			Blocking:    check.Blocking,
			Description: check.Description,
		})
	}

	return result
}

func workspaceCoordColumnsFromModel(columns []webmodel.Column) []service.WorkspaceColumn {
	result := make([]service.WorkspaceColumn, 0, len(columns))
	for _, column := range columns {
		checks := make([]service.WorkspaceColumnCheck, 0, len(column.Checks))
		for _, check := range column.Checks {
			checks = append(checks, service.WorkspaceColumnCheck{
				Name:        check.Name,
				Value:       check.Value,
				Blocking:    check.Blocking,
				Description: check.Description,
			})
		}

		result = append(result, service.WorkspaceColumn{
			Name:          column.Name,
			Type:          column.Type,
			Description:   column.Description,
			Tags:          append([]string(nil), column.Tags...),
			PrimaryKey:    column.PrimaryKey,
			UpdateOnMerge: column.UpdateOnMerge,
			MergeSQL:      column.MergeSQL,
			Nullable:      column.Nullable,
			Owner:         column.Owner,
			Domains:       append([]string(nil), column.Domains...),
			Meta:          mapsClone(column.Meta),
			Checks:        checks,
		})
	}
	return result
}

func workspaceCoordColumnsFromWeb(columns []webColumn) []service.WorkspaceColumn {
	result := make([]service.WorkspaceColumn, 0, len(columns))
	for _, column := range columns {
		checks := make([]service.WorkspaceColumnCheck, 0, len(column.Checks))
		for _, check := range column.Checks {
			checks = append(checks, service.WorkspaceColumnCheck{
				Name:        check.Name,
				Value:       check.Value,
				Blocking:    check.Blocking,
				Description: check.Description,
			})
		}

		result = append(result, service.WorkspaceColumn{
			Name:          column.Name,
			Type:          column.Type,
			Description:   column.Description,
			Tags:          append([]string(nil), column.Tags...),
			PrimaryKey:    column.PrimaryKey,
			UpdateOnMerge: column.UpdateOnMerge,
			MergeSQL:      column.MergeSQL,
			Nullable:      column.Nullable,
			Owner:         column.Owner,
			Domains:       append([]string(nil), column.Domains...),
			Meta:          mapsClone(column.Meta),
			Checks:        checks,
		})
	}
	return result
}

func webColumnsFromCoord(columns []service.WorkspaceColumn) []webColumn {
	result := make([]webColumn, 0, len(columns))
	for _, column := range columns {
		checks := make([]webColumnCheck, 0, len(column.Checks))
		for _, check := range column.Checks {
			checks = append(checks, webColumnCheck{
				Name:        check.Name,
				Value:       check.Value,
				Blocking:    check.Blocking,
				Description: check.Description,
			})
		}

		result = append(result, webColumn{
			Name:          column.Name,
			Type:          column.Type,
			Description:   column.Description,
			Tags:          append([]string(nil), column.Tags...),
			PrimaryKey:    column.PrimaryKey,
			UpdateOnMerge: column.UpdateOnMerge,
			MergeSQL:      column.MergeSQL,
			Nullable:      column.Nullable,
			Owner:         column.Owner,
			Domains:       append([]string(nil), column.Domains...),
			Meta:          mapsClone(column.Meta),
			Checks:        checks,
		})
	}
	return result
}

func mapsClone(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}

	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func pipelineExecutionStatesToAPI(input []service.PipelineMaterializationState) []webhttpapi.PipelineMaterializationState {
	result := make([]webhttpapi.PipelineMaterializationState, 0, len(input))
	for _, item := range input {
		result = append(result, webhttpapi.PipelineMaterializationState{
			AssetID:         item.AssetID,
			IsMaterialized:  item.IsMaterialized,
			MaterializedAs:  item.MaterializedAs,
			FreshnessStatus: item.FreshnessStatus,
			RowCount:        item.RowCount,
			Connection:      item.Connection,
			DeclaredMatType: item.DeclaredMatType,
		})
	}
	return result
}

func mapSliceClone(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return map[string][]string{}
	}

	result := make(map[string][]string, len(input))
	for key, values := range input {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func (s *webServer) newPipelineBuilder() *pipeline.Builder {
	osFS := afero.NewOsFs()
	return pipeline.NewBuilder(
		service.BuilderConfig,
		pipeline.CreateTaskFromYamlDefinition(osFS),
		pipeline.CreateTaskFromFileComments(osFS),
		osFS,
		service.DefaultGlossaryReader,
		jinja.VariantRendererFactory,
	)
}

func resolveConfigFilePath(workspaceRoot string) string {
	repoRoot, err := git.FindRepoFromPath(workspaceRoot)
	if err == nil && repoRoot != nil && strings.TrimSpace(repoRoot.Path) != "" {
		return filepath.Join(repoRoot.Path, ".bruin.yml")
	}

	return filepath.Join(workspaceRoot, ".bruin.yml")
}

func (s *webServer) resolveConfigFilePath() string {
	return resolveConfigFilePath(s.workspaceRoot)
}

func (s *webServer) ConfigChanged(ctx context.Context, relPath, eventType string) {
	s.workspaceCoord.SuppressWatcherFor(relPath)
	s.workspaceCoord.PushUpdateImmediate(ctx, eventType, relPath)
}

func (s *webServer) WorkspaceChanged(ctx context.Context, relPath, eventType string) {
	s.workspaceCoord.SuppressWatcherFor(relPath)
	s.workspaceCoord.PushUpdateImmediate(ctx, eventType, relPath)
	if s.schedulerSvc != nil {
		go func() { _ = s.schedulerSvc.Reconcile(context.Background()) }()
	}
}

func (s *webServer) ListSchedules(ctx context.Context) ([]webscheduler.PipelineSchedule, error) {
	if s.schedulerSvc != nil {
		return s.schedulerSvc.ListSchedules(ctx)
	}
	return s.pipelineSvc.ListSchedules(ctx)
}

func (s *webServer) GetPipelineSchedule(ctx context.Context, pipelineID string) (webscheduler.PipelineSchedule, error) {
	item, err := s.pipelineSvc.GetSchedule(ctx, pipelineID)
	if err != nil {
		return webscheduler.PipelineSchedule{}, err
	}
	return s.applyLocalScheduleSettings(ctx, item)
}

func (s *webServer) UpdatePipelineSchedule(ctx context.Context, pipelineID string, req webscheduler.UpdateScheduleRequest) (webscheduler.PipelineSchedule, error) {
	current, err := s.pipelineSvc.GetSchedule(ctx, pipelineID)
	if err != nil {
		return webscheduler.PipelineSchedule{}, err
	}
	desiredSchedule := strings.TrimSpace(req.Schedule)
	if desiredSchedule == "" {
		desiredSchedule = strings.TrimSpace(current.Schedule)
	}
	desiredTimezone := strings.TrimSpace(req.Timezone)
	if desiredTimezone == "" {
		desiredTimezone = current.Timezone
	}
	if desiredTimezone == "" {
		desiredTimezone = "UTC"
	}
	if req.Enabled && desiredSchedule == "" {
		return webscheduler.PipelineSchedule{}, fmt.Errorf("schedule is required when scheduling is enabled")
	}

	updated := current
	if desiredSchedule != strings.TrimSpace(current.Schedule) || desiredTimezone != strings.TrimSpace(current.Timezone) || req.Catchup != current.Catchup {
		var relPath string
		relPath, updated, err = s.pipelineSvc.UpdateSchedule(ctx, pipelineID, webscheduler.UpdateScheduleRequest{Enabled: req.Enabled, Schedule: desiredSchedule, Timezone: desiredTimezone, Catchup: req.Catchup})
		if err != nil {
			return webscheduler.PipelineSchedule{}, err
		}
		s.WorkspaceChanged(ctx, relPath, "pipeline.updated")
	}
	if s.schedulerStore != nil {
		if err := s.schedulerStore.SetScheduleEnabled(ctx, pipelineID, req.Enabled); err != nil {
			return webscheduler.PipelineSchedule{}, err
		}
	}
	if s.schedulerSvc != nil {
		_ = s.schedulerSvc.Reconcile(ctx)
		items, err := s.schedulerSvc.ListSchedules(ctx)
		if err == nil {
			for _, item := range items {
				if item.PipelineID == pipelineID {
					return item, nil
				}
			}
		}
	}
	return s.applyLocalScheduleSettings(ctx, updated)
}

func (s *webServer) applyLocalScheduleSettings(ctx context.Context, item webscheduler.PipelineSchedule) (webscheduler.PipelineSchedule, error) {
	if s.schedulerStore == nil {
		return item, nil
	}
	enabled, ok, err := s.schedulerStore.ScheduleEnabled(ctx, item.PipelineID)
	if err != nil {
		return webscheduler.PipelineSchedule{}, err
	}
	if ok {
		item.Enabled = enabled && strings.TrimSpace(item.Schedule) != ""
	}
	return item, nil
}

func (s *webServer) TriggerPipeline(ctx context.Context, pipelineID string, req webscheduler.TriggerRequest) (webscheduler.PipelineRun, error) {
	if s.schedulerSvc == nil {
		return webscheduler.PipelineRun{}, fmt.Errorf("scheduler is not initialized")
	}
	pipelineSchedule, err := s.pipelineSvc.GetSchedule(ctx, pipelineID)
	if err != nil {
		return webscheduler.PipelineRun{}, err
	}
	if strings.TrimSpace(req.Trigger) == "" {
		req.Trigger = string(webscheduler.RunTriggerManual)
	}
	return s.schedulerSvc.Trigger(ctx, pipelineSchedule, req)
}

func (s *webServer) ListRuns(ctx context.Context, filter webscheduler.RunFilter) ([]webscheduler.PipelineRun, error) {
	if s.schedulerSvc == nil {
		return nil, fmt.Errorf("scheduler is not initialized")
	}
	return s.schedulerSvc.ListRuns(ctx, filter)
}

func (s *webServer) GetRun(ctx context.Context, runID string) (webscheduler.PipelineRun, []webscheduler.LogLine, []webscheduler.PipelineRunStep, error) {
	if s.schedulerSvc == nil {
		return webscheduler.PipelineRun{}, nil, nil, fmt.Errorf("scheduler is not initialized")
	}
	return s.schedulerSvc.GetRun(ctx, runID)
}

func schedulerStatusFromExecutionStatus(status string) webscheduler.RunStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "ok", "finished":
		return webscheduler.RunStatusSuccess
	case "failed", "failure", "error", "errored":
		return webscheduler.RunStatusFailed
	case "cancelled", "canceled":
		return webscheduler.RunStatusCancelled
	case "queued":
		return webscheduler.RunStatusQueued
	default:
		return webscheduler.RunStatusRunning
	}
}

func (s *webServer) CurrentWorkspace() any {
	return s.currentState()
}

func (s *webServer) CurrentWorkspaceLite() any {
	return s.workspaceCoord.CurrentStateLiteEvent()
}

func (s *webServer) SubscribeWorkspaceEvents() chan []byte {
	return s.workspaceCoord.Subscribe()
}

func (s *webServer) UnsubscribeWorkspaceEvents(ch chan []byte) {
	s.workspaceCoord.Unsubscribe(ch)
}

func (s *webServer) writeJSON(w http.ResponseWriter, status int, body any) {
	webapi.WriteJSON(w, status, body)
}

type executionInspectResult = webhttpapi.InspectExecutionResult

type executionMaterializeEvent = webhttpapi.MaterializeExecutionEvent

func (s *webServer) InspectAsset(ctx context.Context, assetID, limit, environment, startDate, endDate string) executionInspectResult {
	return executionInspectResult(s.executionSvc.InspectAsset(ctx, assetID, limit, environment, startDate, endDate))
}

func (s *webServer) MaterializeAssetStream(ctx context.Context, assetID, environment, scope, startDate, endDate string, onChunk func([]byte)) executionMaterializeEvent {
	return executionMaterializeEvent(s.executionSvc.MaterializeAssetStream(ctx, assetID, environment, scope, startDate, endDate, onChunk))
}

func (s *webServer) GetPipelineMaterialization(ctx context.Context, pipelineID, environment string) (webhttpapi.PipelineMaterializationResponse, *apiError) {
	response, err := s.executionSvc.GetPipelineMaterialization(ctx, pipelineID, environment)
	if err != nil {
		message := err.Error()
		switch {
		case strings.Contains(message, "invalid pipeline id"):
			return webhttpapi.PipelineMaterializationResponse{}, &apiError{Status: http.StatusBadRequest, Code: "invalid_pipeline_id", Message: "invalid pipeline id"}
		case strings.Contains(message, "invalid path"):
			return webhttpapi.PipelineMaterializationResponse{}, &apiError{Status: http.StatusBadRequest, Code: "invalid_pipeline_path", Message: message}
		default:
			return webhttpapi.PipelineMaterializationResponse{}, &apiError{Status: http.StatusBadRequest, Code: "pipeline_parse_failed", Message: message}
		}
	}
	return webhttpapi.PipelineMaterializationResponse{PipelineID: response.PipelineID, Assets: pipelineExecutionStatesToAPI(response.Assets)}, nil
}

func (s *webServer) CreateAsset(ctx context.Context, pipelineID string, req webhttpapi.CreateAssetRequest) (assetMutationResponse, *apiError) {
	result, err := s.assetSvc.Create(ctx, pipelineID, service.CreateAssetParams(req))
	if err != nil {
		return assetMutationResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return assetMutationResponse(result), nil
}

type updateAssetColumnsRequest struct {
	Columns []webColumn `json:"columns"`
}

func (s *webServer) UpdateAsset(ctx context.Context, assetID string, req webhttpapi.UpdateAssetRequest) (assetMutationResponse, *apiError) {
	result, err := s.assetSvc.Update(ctx, assetID, service.AssetUpdateRequest{
		Name:                req.Name,
		Type:                req.Type,
		Content:             req.Content,
		MaterializationType: req.MaterializationType,
		Meta:                req.Meta,
		Upstreams:           req.Upstreams,
	})
	if err != nil {
		return assetMutationResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return assetMutationResponse(result), nil
}

func (s *webServer) FormatSQLAsset(ctx context.Context, assetID string, req webhttpapi.FormatSQLAssetRequest) (formatSQLAssetResponse, *apiError) {
	result, err := s.assetSvc.FormatSQL(ctx, assetID, service.FormatSQLAssetRequest{Content: req.Content})
	if err != nil {
		return formatSQLAssetResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return formatSQLAssetResponse(result), nil
}

func (s *webServer) FormatPythonAsset(ctx context.Context, assetID string, req webhttpapi.FormatPythonAssetRequest) (formatPythonAssetResponse, *apiError) {
	result, err := s.assetSvc.FormatPython(ctx, assetID, service.FormatPythonAssetRequest{Content: req.Content})
	if err != nil {
		return formatPythonAssetResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return formatPythonAssetResponse(result), nil
}

func (s *webServer) PythonDiagnostics(ctx context.Context, assetID string, req webhttpapi.PythonDiagnosticsRequest) (pythonDiagnosticsResponse, *apiError) {
	result, err := s.assetSvc.PythonDiagnostics(ctx, assetID, service.PythonDiagnosticsRequest{Content: req.Content})
	if err != nil {
		return pythonDiagnosticsResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return pythonDiagnosticsResponse{
		Status:      result.Status,
		AssetID:     result.AssetID,
		Diagnostics: pythonDiagnosticsToAPI(result.Diagnostics),
		Error:       result.Error,
	}, nil
}

func (s *webServer) PythonCompletions(ctx context.Context, assetID string, req webhttpapi.PythonCompletionsRequest) (pythonCompletionsResponse, *apiError) {
	result, err := s.assetSvc.PythonCompletions(ctx, assetID, service.PythonCompletionsRequest{Content: req.Content, Line: req.Line, Column: req.Column, Snippets: req.Snippets})
	if err != nil {
		return pythonCompletionsResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return pythonCompletionsResponse{
		Status:      result.Status,
		AssetID:     result.AssetID,
		Completions: pythonCompletionsToAPI(result.Completions),
		Error:       result.Error,
	}, nil
}

func (s *webServer) PythonHover(ctx context.Context, assetID string, req webhttpapi.PythonPositionRequest) (pythonHoverResponse, *apiError) {
	result, err := s.assetSvc.PythonHover(ctx, assetID, service.PythonPositionRequest{Content: req.Content, Line: req.Line, Column: req.Column})
	if err != nil {
		return pythonHoverResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return pythonHoverResponse{
		Status:  result.Status,
		AssetID: result.AssetID,
		Hover:   pythonHoverToAPI(result.Hover),
		Error:   result.Error,
	}, nil
}

func (s *webServer) PythonSignatureHelp(ctx context.Context, assetID string, req webhttpapi.PythonPositionRequest) (pythonSignatureHelpResponse, *apiError) {
	result, err := s.assetSvc.PythonSignatureHelp(ctx, assetID, service.PythonPositionRequest{Content: req.Content, Line: req.Line, Column: req.Column})
	if err != nil {
		return pythonSignatureHelpResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return pythonSignatureHelpResponse{
		Status:        result.Status,
		AssetID:       result.AssetID,
		SignatureHelp: pythonSignatureHelpToAPI(result.SignatureHelp),
		Error:         result.Error,
	}, nil
}

func (s *webServer) PythonGotoDefinition(ctx context.Context, assetID string, req webhttpapi.PythonPositionRequest) (pythonGotoDefinitionResponse, *apiError) {
	result, err := s.assetSvc.PythonGotoDefinition(ctx, assetID, service.PythonPositionRequest{Content: req.Content, Line: req.Line, Column: req.Column})
	if err != nil {
		return pythonGotoDefinitionResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return pythonGotoDefinitionResponse{
		Status:  result.Status,
		AssetID: result.AssetID,
		Targets: pythonGotoTargetsToAPI(result.Targets),
		Error:   result.Error,
	}, nil
}

func pythonDiagnosticsToAPI(input []service.PythonDiagnostic) []webhttpapi.PythonDiagnostic {
	result := make([]webhttpapi.PythonDiagnostic, 0, len(input))
	for _, diagnostic := range input {
		result = append(result, webhttpapi.PythonDiagnostic{
			ID:       diagnostic.ID,
			Message:  diagnostic.Message,
			Severity: diagnostic.Severity,
			Range:    pythonRangeToAPI(diagnostic.Range),
			Display:  diagnostic.Display,
		})
	}
	return result
}

func pythonRangeToAPI(input *service.PythonRange) *webhttpapi.PythonRange {
	if input == nil {
		return nil
	}
	return &webhttpapi.PythonRange{
		Start: webhttpapi.PythonPosition{Line: input.Start.Line, Column: input.Start.Column},
		End:   webhttpapi.PythonPosition{Line: input.End.Line, Column: input.End.Column},
	}
}

func pythonCompletionsToAPI(input []service.PythonCompletion) []webhttpapi.PythonCompletion {
	result := make([]webhttpapi.PythonCompletion, 0, len(input))
	for _, completion := range input {
		result = append(result, webhttpapi.PythonCompletion{
			Label:               completion.Label,
			Kind:                completion.Kind,
			Detail:              completion.Detail,
			InsertText:          completion.InsertText,
			InsertTextFormat:    completion.InsertTextFormat,
			Documentation:       completion.Documentation,
			ModuleName:          completion.ModuleName,
			AdditionalTextEdits: pythonTextEditsToAPI(completion.AdditionalTextEdits),
		})
	}
	return result
}

func pythonTextEditsToAPI(input []service.PythonTextEdit) []webhttpapi.PythonTextEdit {
	result := make([]webhttpapi.PythonTextEdit, 0, len(input))
	for _, edit := range input {
		result = append(result, webhttpapi.PythonTextEdit{
			Range: webhttpapi.PythonRange{
				Start: webhttpapi.PythonPosition{Line: edit.Range.Start.Line, Column: edit.Range.Start.Column},
				End:   webhttpapi.PythonPosition{Line: edit.Range.End.Line, Column: edit.Range.End.Column},
			},
			Text: edit.Text,
		})
	}
	return result
}

func pythonHoverToAPI(input *service.PythonHover) *webhttpapi.PythonHover {
	if input == nil {
		return nil
	}
	return &webhttpapi.PythonHover{Contents: input.Contents, Range: pythonRangeToAPI(input.Range)}
}

func pythonSignatureHelpToAPI(input *service.PythonSignatureHelp) *webhttpapi.PythonSignatureHelp {
	if input == nil {
		return nil
	}
	return &webhttpapi.PythonSignatureHelp{
		Signatures:      pythonSignaturesToAPI(input.Signatures),
		ActiveSignature: input.ActiveSignature,
		ActiveParameter: input.ActiveParameter,
	}
}

func pythonSignaturesToAPI(input []service.PythonSignature) []webhttpapi.PythonSignature {
	result := make([]webhttpapi.PythonSignature, 0, len(input))
	for _, signature := range input {
		result = append(result, webhttpapi.PythonSignature{
			Label:           signature.Label,
			Documentation:   signature.Documentation,
			Parameters:      pythonSignatureParametersToAPI(signature.Parameters),
			ActiveParameter: signature.ActiveParameter,
		})
	}
	return result
}

func pythonSignatureParametersToAPI(input []service.PythonSignatureParameter) []webhttpapi.PythonSignatureParameter {
	result := make([]webhttpapi.PythonSignatureParameter, 0, len(input))
	for _, parameter := range input {
		result = append(result, webhttpapi.PythonSignatureParameter{
			Label:         parameter.Label,
			Name:          parameter.Name,
			Type:          parameter.Type,
			Documentation: parameter.Documentation,
		})
	}
	return result
}

func pythonGotoTargetsToAPI(input []service.PythonGotoTarget) []webhttpapi.PythonGotoTarget {
	result := make([]webhttpapi.PythonGotoTarget, 0, len(input))
	for _, target := range input {
		result = append(result, webhttpapi.PythonGotoTarget{
			Path:       target.Path,
			FocusRange: pythonRangeValueToAPI(target.FocusRange),
			FullRange:  pythonRangeValueToAPI(target.FullRange),
		})
	}
	return result
}

func pythonRangeValueToAPI(input service.PythonRange) webhttpapi.PythonRange {
	return webhttpapi.PythonRange{
		Start: webhttpapi.PythonPosition{Line: input.Start.Line, Column: input.Start.Column},
		End:   webhttpapi.PythonPosition{Line: input.End.Line, Column: input.End.Column},
	}
}

func (s *webServer) FillColumnsFromDB(ctx context.Context, assetID string) (int, map[string]any, *apiError) {
	relAssetPath, _, asset, err := s.resolveAssetByID(ctx, assetID)
	if err != nil {
		return 0, nil, &apiError{Status: http.StatusBadRequest, Code: "asset_resolve_failed", Message: err.Error()}
	}

	assetType := strings.ToLower(string(asset.Type))
	if !strings.Contains(assetType, "sql") && !strings.HasSuffix(strings.ToLower(relAssetPath), ".sql") {
		return 0, nil, &apiError{Status: http.StatusBadRequest, Code: "unsupported_asset_type", Message: "fill-columns-from-db is supported for sql assets only"}
	}

	normalizedPath := filepath.ToSlash(relAssetPath)
	withDot := "./" + strings.TrimPrefix(normalizedPath, "./")
	withoutDot := strings.TrimPrefix(normalizedPath, "./")

	type cmdResult struct {
		Operation webmodel.OperationMetadata `json:"operation"`
		Output    string                     `json:"output"`
		ExitCode  int                        `json:"exit_code"`
		Error     string                     `json:"error,omitempty"`
	}

	targets := []string{withDot, withoutDot}
	results := make([]cmdResult, 0, len(targets))
	allSucceeded := true

	for _, targetPath := range targets {
		out, runErr := s.executor.ApplyPatch(ctx, service.PatchRequest{
			Operation:  "fill-columns-from-db",
			TargetPath: targetPath,
		})

		result := cmdResult{
			Operation: webmodel.OperationMetadata{Type: "patch", Operation: "fill-columns-from-db", Target: targetPath, TargetPath: targetPath},
			Output:    string(out),
			ExitCode:  0,
		}

		if runErr != nil {
			allSucceeded = false
			result.ExitCode = 1
			result.Error = runErr.Error()
		}

		results = append(results, result)
	}

	s.suppressWatcherFor(relAssetPath)
	s.pushWorkspaceUpdateImmediate(ctx, "asset.updated", relAssetPath)

	status := http.StatusOK
	responseStatus := "ok"
	if !allSucceeded {
		status = http.StatusBadRequest
		responseStatus = "error"
	}

	return status, map[string]any{
		"status":  responseStatus,
		"results": results,
	}, nil
}

func (s *webServer) InferAssetColumns(ctx context.Context, assetID string) (int, map[string]any, *apiError) {
	_, parsedPipeline, asset, err := s.resolveAssetByID(ctx, assetID)
	if err != nil {
		return 0, nil, &apiError{Status: http.StatusBadRequest, Code: "asset_resolve_failed", Message: err.Error()}
	}

	queryReq, err := service.BuildInferAssetColumnsQuery(parsedPipeline, asset, "")
	if err != nil {
		return 0, nil, &apiError{Status: http.StatusBadRequest, Code: "infer_columns_command_build_failed", Message: err.Error()}
	}
	operation := webmodel.OperationMetadata{Type: "query_connection", ConnectionName: queryReq.ConnectionName, Query: queryReq.Query, Environment: queryReq.Environment}

	output, err := s.executor.QueryConnection(ctx, queryReq)
	if err != nil {
		return http.StatusBadRequest, map[string]any{
			"status":     "error",
			"columns":    []webColumn{},
			"raw_output": string(output),
			"operation":  operation,
			"error":      err.Error(),
		}, nil
	}

	inferred := inferWebColumnsFromQueryOutput(output)
	return http.StatusOK, map[string]any{
		"status":     "ok",
		"columns":    inferred,
		"raw_output": string(output),
		"operation":  operation,
	}, nil
}

func (s *webServer) UpdateAssetColumns(ctx context.Context, assetID string, columns []any) (statusResponse, *apiError) {
	_, parsedPipeline, asset, err := s.resolveAssetByID(ctx, assetID)
	if err != nil {
		return statusResponse{}, &apiError{Status: http.StatusBadRequest, Code: "asset_resolve_failed", Message: err.Error()}
	}

	columnBytes, err := json.Marshal(columns)
	if err != nil {
		return statusResponse{}, &apiError{Status: http.StatusBadRequest, Code: "invalid_request_body", Message: err.Error()}
	}

	var req updateAssetColumnsRequest
	if err := json.Unmarshal(columnBytes, &req.Columns); err != nil {
		return statusResponse{}, &apiError{Status: http.StatusBadRequest, Code: "invalid_request_body", Message: err.Error()}
	}

	asset.Columns = webColumnsToPipelineColumns(req.Columns)
	err = asset.Persist(afero.NewOsFs(), parsedPipeline)
	if err != nil {
		return statusResponse{}, &apiError{Status: http.StatusInternalServerError, Code: "asset_persist_failed", Message: err.Error()}
	}

	relAssetPath, decodeErr := decodeID(assetID)
	if decodeErr == nil {
		s.suppressWatcherFor(relAssetPath)
		s.pushWorkspaceUpdateImmediate(ctx, "asset.columns.updated", relAssetPath)
	}

	return statusResponse{Status: "ok"}, nil
}

func (s *webServer) DeleteAsset(ctx context.Context, assetID string) (statusResponse, *apiError) {
	result, err := s.assetSvc.Delete(ctx, assetID)
	if err != nil {
		return statusResponse{}, &apiError{Status: err.Status, Code: err.Code, Message: err.Message}
	}
	return statusResponse(result), nil
}

func (s *webServer) resolveAssetByID(ctx context.Context, assetID string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
	return s.workspaceSvc.ResolveAssetByID(ctx, assetID)
}

func (s *webServer) getDuckDBOperationMutex(lockKey string) *sync.Mutex {
	s.duckDBOpsMu.Lock()
	defer s.duckDBOpsMu.Unlock()

	if existing, ok := s.duckDBOps[lockKey]; ok {
		return existing
	}

	mu := &sync.Mutex{}
	s.duckDBOps[lockKey] = mu
	return mu
}

func (s *webServer) ResolvePipelineRunTarget(pipelineID string) error {
	_, err := resolvePipelineRunTarget(pipelineID)
	return err
}

func (s *webServer) newConnectionManager(ctx context.Context, environment string) (config.ConnectionAndDetailsGetter, error) {
	configPath := s.resolveConfigFilePath()
	cfg, err := config.LoadOrCreate(afero.NewOsFs(), configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if environment != "" {
		if err := cfg.SelectEnvironment(environment); err != nil {
			return nil, fmt.Errorf("failed to select environment '%s': %w", environment, err)
		}
	}

	manager, errs := connection.NewManagerFromConfigWithContext(ctx, cfg)
	if len(errs) > 0 {
		return nil, errs[0]
	}

	return manager, nil
}

func (s *webServer) MaterializePipelineStream(ctx context.Context, pipelineID, environment string, dryRun bool, startDate, endDate string, onChunk func([]byte)) executionMaterializeEvent {
	return executionMaterializeEvent(s.executionSvc.MaterializePipelineStream(ctx, pipelineID, environment, dryRun, startDate, endDate, onChunk))
}

func (s *webServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.staticHandler != nil {
		s.staticHandler.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("Renart UI assets are unavailable."))
}

// suppressWatcherFor marks a path as recently handled by a server-initiated
// write (API handler or patch timer). Filesystem watcher events for this
// path will be suppressed for a short window to avoid duplicate notifications.
func (s *webServer) suppressWatcherFor(eventPath string) {
	s.workspaceCoord.SuppressWatcherFor(eventPath)
}

// isWatcherSuppressed returns true if the given path was recently handled by
// a server-initiated write and the filesystem watcher event should be skipped.
func (s *webServer) isWatcherSuppressed(eventPath string) bool {
	return s.workspaceCoord.IsWatcherSuppressed(eventPath)
}

func (s *webServer) pushWorkspaceUpdate(ctx context.Context, eventType, eventPath string) {
	s.workspaceCoord.PushUpdate(ctx, eventType, eventPath)
}

// pushWorkspaceUpdateImmediate publishes immediately (bypasses debounce).
// Used by API handlers that need the client to see the change right away.
func (s *webServer) pushWorkspaceUpdateImmediate(ctx context.Context, eventType, eventPath string) {
	s.workspaceCoord.PushUpdateImmediate(ctx, eventType, eventPath)
}

func (s *webServer) pushWorkspaceUpdateImmediateWithChangedIDs(ctx context.Context, eventType, eventPath string, changedAssetIDs []string) {
	s.workspaceCoord.PushUpdateImmediateWithChangedIDs(ctx, eventType, eventPath, changedAssetIDs)
}

// findDirectlyChangedAssetIDs returns only the asset IDs whose source file
// matches the given event path. No downstream expansion — used for file-edit
// events where only the edited asset's inspect result would change (its SQL
// changed, but no table data changed yet).
func (s *webServer) findDirectlyChangedAssetIDs(eventPath string) []string {
	return s.workspaceCoord.FindDirectlyChangedAssetIDs(eventPath)
}

// findMaterializationInspectIDs returns the given asset IDs plus their direct
// (1-level) downstream dependents. Used after materialization — the materialized
// asset's table now has new data, so queries that read from it (direct
// downstreams) may return different results. Transitive downstreams (2+ hops)
// still read from the direct downstream's un-materialized table, so they are
// not affected for inspect purposes.
func (s *webServer) findMaterializationInspectIDs(assetIDs ...string) []string {
	return s.workspaceCoord.FindMaterializationInspectIDs(assetIDs...)
}

// findAssetNameByID looks up the asset name for a given encoded asset ID
// from the current workspace state.
func (s *webServer) findAssetNameByID(assetID string) string {
	return s.workspaceCoord.FindAssetNameByID(assetID)
}

// handleGetAssetFreshness returns freshness timestamps for all tracked assets.
// Each entry includes both materialization and content-change timestamps so
// the frontend can compute staleness from either perspective.
func (s *webServer) handleGetAssetFreshness(w http.ResponseWriter, _ *http.Request) {
	all := s.freshness.GetAll()

	type assetFreshnessEntry struct {
		AssetName          string     `json:"asset_name"`
		MaterializedAt     *time.Time `json:"materialized_at,omitempty"`
		MaterializedStatus string     `json:"materialized_status,omitempty"`
		ContentChangedAt   *time.Time `json:"content_changed_at,omitempty"`
	}

	entries := make([]assetFreshnessEntry, 0, len(all))
	for name, ts := range all {
		entry := assetFreshnessEntry{
			AssetName:          name,
			MaterializedAt:     ts.MaterializedAt,
			MaterializedStatus: ts.MaterializedStatus,
			ContentChangedAt:   ts.ContentChangedAt,
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].AssetName < entries[j].AssetName
	})

	s.writeJSON(w, http.StatusOK, map[string]any{
		"assets": entries,
	})
}

func safeJoin(root, relPath string) (string, error) {
	return service.SafeJoin(root, relPath)
}

func pipelinePathsReferToSameRoot(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}

	normalize := func(path string) string {
		cleaned := filepath.Clean(path)
		base := strings.ToLower(filepath.Base(cleaned))
		if base == "pipeline.yml" || base == "pipeline.yaml" || base == ".pipeline.yml" || base == ".pipeline.yaml" {
			return filepath.Dir(cleaned)
		}
		return cleaned
	}

	return normalize(left) == normalize(right)
}

func slug(input string) string {
	return strings.ReplaceAll(service.Slug(strings.ReplaceAll(input, ".", " ")), "-", "_")
}

func extensionForAssetType(assetType string) string {
	assetType = strings.ToLower(assetType)
	if strings.Contains(assetType, "python") || strings.HasSuffix(assetType, ".py") {
		return ".py"
	}
	if strings.Contains(assetType, "ingestr") {
		return ".asset.yaml"
	}
	if strings.Contains(assetType, "r") || strings.HasSuffix(assetType, ".r") {
		return ".r"
	}
	return ".sql"
}

func inferAssetTypeFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py":
		return "python"
	case ".r":
		return "r"
	case ".yml", ".yaml":
		return "yaml"
	default:
		return "duckdb.sql"
	}
}

func deriveDownstreamAssetName(sourceAssetName string, parsedPipeline *pipeline.Pipeline) string {
	trimmed := strings.TrimSpace(sourceAssetName)
	if trimmed == "" {
		trimmed = "asset"
	}

	prefix := ""
	leaf := trimmed
	if lastDot := strings.LastIndex(trimmed, "."); lastDot >= 0 {
		prefix = trimmed[:lastDot]
		leaf = trimmed[lastDot+1:]
	}

	baseLeaf := leaf + "_child"
	existing := make(map[string]struct{})
	if parsedPipeline != nil {
		for _, asset := range parsedPipeline.Assets {
			existing[strings.ToLower(strings.TrimSpace(asset.Name))] = struct{}{}
		}
	}

	for index := 1; ; index++ {
		candidateLeaf := fmt.Sprintf("%s_%d", baseLeaf, index)
		candidate := candidateLeaf
		if prefix != "" {
			candidate = prefix + "." + candidateLeaf
		}

		if _, ok := existing[strings.ToLower(candidate)]; !ok {
			return candidate
		}
	}
}

func deriveSQLAssetTypeForSource(sourceAsset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sourceConnectionName string) string {
	if sourceAsset == nil {
		return string(pipeline.AssetTypeDuckDBQuery)
	}

	lowerType := strings.ToLower(string(sourceAsset.Type))
	if strings.Contains(lowerType, "sql") {
		return string(sourceAsset.Type)
	}

	if lowerType == "ingestr" {
		if destination := strings.TrimSpace(strings.ToLower(sourceAsset.Parameters["destination"])); destination != "" {
			if mapped, ok := pipeline.IngestrTypeConnectionMapping[destination]; ok {
				return string(mapped)
			}
		}
	}

	if connectionType, ok := pipeline.AssetTypeConnectionMapping[sourceAsset.Type]; ok {
		if assetType := preferredSQLAssetTypeForConnectionType(connectionType); assetType != "" {
			return assetType
		}
	}

	if parsedPipeline != nil && sourceConnectionName != "" {
		if connectionType := resolveConnectionTypeForName(parsedPipeline, sourceConnectionName); connectionType != "" {
			if assetType := preferredSQLAssetTypeForConnectionType(connectionType); assetType != "" {
				return assetType
			}
		}
	}

	if parsedPipeline != nil {
		return string(parsedPipeline.GetMajorityAssetTypesFromSQLAssets(pipeline.AssetTypeDuckDBQuery))
	}

	return string(pipeline.AssetTypeDuckDBQuery)
}

func preferredSQLAssetTypeForConnectionType(connectionType string) string {
	switch strings.ToLower(strings.TrimSpace(connectionType)) {
	case "google_cloud_platform":
		return string(pipeline.AssetTypeBigqueryQuery)
	case "snowflake":
		return string(pipeline.AssetTypeSnowflakeQuery)
	case "postgres":
		return string(pipeline.AssetTypePostgresQuery)
	case "redshift":
		return string(pipeline.AssetTypeRedshiftQuery)
	case "mssql":
		return string(pipeline.AssetTypeMsSQLQuery)
	case "fabric":
		return string(pipeline.AssetTypeFabricQuery)
	case "databricks":
		return string(pipeline.AssetTypeDatabricksQuery)
	case "synapse":
		return string(pipeline.AssetTypeSynapseQuery)
	case "athena":
		return string(pipeline.AssetTypeAthenaQuery)
	case "duckdb":
		return string(pipeline.AssetTypeDuckDBQuery)
	case "motherduck":
		return string(pipeline.AssetTypeMotherduckQuery)
	case "clickhouse":
		return string(pipeline.AssetTypeClickHouse)
	case "trino":
		return string(pipeline.AssetTypeTrinoQuery)
	case "oracle":
		return string(pipeline.AssetTypeOracleQuery)
	case "mysql":
		return string(pipeline.AssetTypeMySQLQuery)
	case "vertica":
		return string(pipeline.AssetTypeVerticaQuery)
	default:
		return ""
	}
}

func resolveConnectionTypeForName(parsedPipeline *pipeline.Pipeline, connectionName string) string {
	if parsedPipeline == nil || strings.TrimSpace(connectionName) == "" {
		return ""
	}

	normalizedConnectionName := strings.TrimSpace(connectionName)

	for connectionType, configuredName := range parsedPipeline.DefaultConnections {
		if strings.EqualFold(strings.TrimSpace(configuredName), normalizedConnectionName) {
			return connectionType
		}
	}

	for _, connectionType := range supportedSQLConnectionTypes() {
		if strings.EqualFold(defaultConnectionNameForType(connectionType), normalizedConnectionName) {
			return connectionType
		}
	}

	return ""
}

func supportedSQLConnectionTypes() []string {
	return []string{
		"google_cloud_platform",
		"snowflake",
		"postgres",
		"redshift",
		"mssql",
		"fabric",
		"databricks",
		"synapse",
		"athena",
		"duckdb",
		"motherduck",
		"clickhouse",
		"trino",
		"oracle",
		"mysql",
		"vertica",
	}
}

func defaultConnectionNameForType(connectionType string) string {
	switch strings.ToLower(strings.TrimSpace(connectionType)) {
	case "google_cloud_platform":
		return "gcp-default"
	default:
		return strings.ToLower(strings.TrimSpace(connectionType)) + "-default"
	}
}

func defaultAssetContent(assetName, assetType, assetPath string) string {
	base := service.DefaultAssetContent(assetName, assetType, assetPath)
	if strings.HasSuffix(strings.ToLower(assetPath), ".sql") {
		return fmt.Sprintf("/* @bruin\n\nname: %s\ntype: %s\nmaterialization:\n  type: view\n\n@bruin */\n", assetName, assetType)
	}
	return base
}

func defaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName string) string {
	return service.DefaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName)
}

func ensurePythonRequirementsFile(absAssetPath, assetType, relAssetPath string) error {
	return service.EnsurePythonRequirementsFile(absAssetPath, assetType, relAssetPath)
}

func splitBruinHeader(content string) (header string, separator string, body string, found bool) {
	return service.SplitBruinHeader(content)
}

func extractExecutableContent(content string) string {
	return service.ExtractExecutableContent(content)
}

func mergeExecutableContent(currentFileContent, executableContent string) string {
	return service.MergeExecutableContent(currentFileContent, executableContent)
}

func encodeID(value string) string {
	return service.EncodeID(value)
}

func decodeID(value string) (string, error) {
	return service.DecodeID(value)
}

func (s *webServer) runConnectionQueryForEnvironment(ctx context.Context, connectionName, environment, query string) ([]string, []map[string]any, error) {
	return s.executionSvc.RunConnectionQueryForEnvironment(ctx, connectionName, environment, query)
}

func (s *webServer) ColumnValues(ctx context.Context, connectionName, environment, query string) service.SQLColumnValuesResult {
	return s.sqlSvc.ColumnValues(ctx, connectionName, environment, query)
}

func (s *webServer) Databases(ctx context.Context, connectionName, environment string) (service.SQLDatabaseDiscoveryResult, *service.SQLAPIError) {
	return s.sqlSvc.Databases(ctx, connectionName, environment)
}

func (s *webServer) Tables(ctx context.Context, connectionName, databaseName, environment string) (service.SQLTableDiscoveryResult, *service.SQLAPIError) {
	return s.sqlSvc.Tables(ctx, connectionName, databaseName, environment)
}

func (s *webServer) TableColumns(ctx context.Context, connectionName, tableName, environment string) (service.SQLTableColumnsResult, int) {
	return s.sqlSvc.TableColumns(ctx, connectionName, tableName, environment)
}

func (s *webServer) Ingestr(ctx context.Context, connectionName, prefix, environment string) (service.IngestrSuggestionsResult, *service.SuggestionAPIError) {
	return s.suggestionsSvc.Ingestr(ctx, connectionName, prefix, environment)
}

func (s *webServer) SQLPath(ctx context.Context, assetID, prefix, environment string) (service.SQLPathSuggestionsResult, *service.SuggestionAPIError) {
	return s.suggestionsSvc.SQLPath(ctx, assetID, prefix, environment)
}

func (s *webServer) ParseContext(ctx context.Context, assetID, content string, schema []service.ParseContextSchemaTable) (service.ParseContextResult, *service.ParseContextAPIError) {
	return s.parseContextSvc.Parse(ctx, assetID, content, schema)
}

func (s *webServer) RenderJinja(ctx context.Context, assetID string, req service.JinjaRenderRequest) (service.JinjaRenderResult, *service.JinjaRenderAPIError) {
	return s.jinjaRenderSvc.Render(ctx, assetID, req)
}

func (s *webServer) Run(ctx context.Context, req service.RunRequest) service.RunResult {
	return s.runSvc.Execute(ctx, req)
}

func extractColumnNames(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}

	columns := make([]string, 0, len(items))
	for _, item := range items {
		if name, ok := item.(string); ok {
			columns = append(columns, name)
			continue
		}

		columnMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		nameValue, ok := columnMap["name"]
		if !ok {
			continue
		}

		if name, ok := nameValue.(string); ok {
			columns = append(columns, name)
		}
	}

	return columns
}

func castRows(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if ok {
			rows = append(rows, row)
		}
	}

	return rows
}

func castRowsByColumns(value any, columns []string) []map[string]any {
	if len(columns) == 0 {
		return []map[string]any{}
	}

	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		cellValues, ok := item.([]any)
		if !ok {
			continue
		}

		row := make(map[string]any, len(columns))
		for idx, column := range columns {
			if idx < len(cellValues) {
				row[column] = cellValues[idx]
				continue
			}
			row[column] = nil
		}

		rows = append(rows, row)
	}

	return rows
}

func inferColumns(rows []map[string]any) []string {
	if len(rows) == 0 {
		return []string{}
	}

	columns := make([]string, 0)
	for key := range rows[0] {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	return columns
}

func inferWebColumnsFromQueryOutput(output []byte) []webColumn {
	columns := service.InferSQLColumnsFromQueryOutput(output)
	result := make([]webColumn, 0, len(columns))
	for _, column := range columns {
		result = append(result, webColumn{Name: column.Name, Type: column.Type})
	}
	return result
}

func pipelineColumnsToWebColumns(columns []pipeline.Column) []webColumn {
	result := make([]webColumn, 0, len(columns))
	for _, column := range columns {
		var nullable *bool
		if column.Nullable.Value != nil {
			value := *column.Nullable.Value
			nullable = &value
		}

		checks := make([]webColumnCheck, 0, len(column.Checks))
		for _, check := range column.Checks {
			checks = append(checks, webColumnCheck{
				Name:        check.Name,
				Value:       columnCheckValueToAny(check.Value),
				Blocking:    check.Blocking.Value,
				Description: check.Description,
			})
		}

		result = append(result, webColumn{
			Name:          column.Name,
			Type:          column.Type,
			Description:   column.Description,
			Tags:          column.Tags,
			PrimaryKey:    column.PrimaryKey,
			UpdateOnMerge: column.UpdateOnMerge,
			MergeSQL:      column.MergeSQL,
			Nullable:      nullable,
			Owner:         column.Owner,
			Domains:       column.Domains,
			Meta:          column.Meta,
			Checks:        checks,
		})
	}

	return result
}

func webColumnsToPipelineColumns(columns []webColumn) []pipeline.Column {
	result := make([]pipeline.Column, 0, len(columns))
	for _, column := range columns {
		checks := make([]pipeline.ColumnCheck, 0, len(column.Checks))
		for _, check := range column.Checks {
			checks = append(checks, pipeline.ColumnCheck{
				Name:        check.Name,
				Value:       anyToColumnCheckValue(check.Value),
				Blocking:    pipeline.DefaultTrueBool{Value: check.Blocking},
				Description: check.Description,
			})
		}

		result = append(result, pipeline.Column{
			Name:          column.Name,
			Type:          column.Type,
			Description:   column.Description,
			Tags:          column.Tags,
			PrimaryKey:    column.PrimaryKey,
			UpdateOnMerge: column.UpdateOnMerge,
			MergeSQL:      column.MergeSQL,
			Nullable:      pipeline.DefaultTrueBool{Value: column.Nullable},
			Owner:         column.Owner,
			Domains:       column.Domains,
			Meta:          column.Meta,
			Checks:        checks,
		})
	}

	return result
}

func columnCheckValueToAny(value pipeline.ColumnCheckValue) any {
	if value.IntArray != nil {
		return *value.IntArray
	}
	if value.Int != nil {
		return *value.Int
	}
	if value.Float != nil {
		return *value.Float
	}
	if value.StringArray != nil {
		return *value.StringArray
	}
	if value.String != nil {
		return *value.String
	}
	if value.Bool != nil {
		return *value.Bool
	}

	return nil
}

func anyToColumnCheckValue(value any) pipeline.ColumnCheckValue {
	result := pipeline.ColumnCheckValue{}
	if value == nil {
		return result
	}

	switch v := value.(type) {
	case bool:
		result.Bool = &v
	case string:
		result.String = &v
	case int:
		result.Int = &v
	case int64:
		converted := int(v)
		result.Int = &converted
	case float64:
		if v == float64(int(v)) {
			converted := int(v)
			result.Int = &converted
		} else {
			result.Float = &v
		}
	case []string:
		result.StringArray = &v
	case []any:
		stringArr := make([]string, 0, len(v))
		intArr := make([]int, 0, len(v))
		allStrings := true
		allInts := true
		for _, item := range v {
			s, sOK := item.(string)
			if sOK {
				stringArr = append(stringArr, s)
			} else {
				allStrings = false
			}

			n, nOK := item.(float64)
			if nOK && n == float64(int(n)) {
				intArr = append(intArr, int(n))
			} else {
				allInts = false
			}
		}

		if allStrings {
			result.StringArray = &stringArr
		} else if allInts {
			result.IntArray = &intArr
		}
	}

	return result
}

func buildWorkspaceConfigConnectionTypes() []service.WorkspaceConfigConnectionType {
	return service.BuildWorkspaceConfigConnectionTypes()
}
