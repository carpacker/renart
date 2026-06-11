package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
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
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// workspaceState is the canonical workspace DTO from the model package,
// re-exported by the service package.
type workspaceState = service.WorkspaceState

type webServer struct {
	workspaceRoot    string
	staticDir        string
	staticHandler    http.Handler
	watchMode        string
	watchPoll        time.Duration
	workspaceSvc     *service.WorkspaceService
	configSvc        *service.ConfigService
	pipelineSvc      *service.PipelineService
	executionSvc     *service.ExecutionService
	assetSvc         *service.AssetService
	sqlSvc           *service.SQLService
	suggestionsSvc   *service.SuggestionsService
	parseContextSvc  *service.ParseContextService
	jinjaRenderSvc   *service.JinjaRenderService
	runSvc           *service.RunService
	onboardingSvc    *service.OnboardingService
	sourceControlSvc *service.SourceControlService
	schedulerSvc     *webscheduler.Service
	schedulerStore   *webscheduler.Store
	workspaceCoord   *service.WorkspaceCoordinator

	hub       *events.Hub
	executor  service.BruinCommandExecutor
	freshness *freshness.Tracker
	logger    *zap.Logger

	duckDBOpsMu sync.Mutex
	duckDBOps   map[string]*sync.Mutex
}

func Web() *cli.Command {
	return &cli.Command{
		Name:      "web",
		Usage:     "start Renart UI server",
		ArgsUsage: "[workspace root]",
		Flags: append(serverFlags(),
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
		),
		Action: func(ctx context.Context, c *cli.Command) error {
			cfg, err := serverConfigFromCommand(c)
			if err != nil {
				return err
			}

			logger, err := newServerLogger()
			if err != nil {
				return err
			}
			defer func() { _ = logger.Sync() }()

			server, cleanup, err := newWebServer(ctx, cfg, logger)
			if err != nil {
				return err
			}
			defer cleanup()

			router := server.buildRouter()

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

			httpServer := newHTTPServer(address, router)
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
	webhttpapi.RegisterExecutionRoutes(router, &webhttpapi.ExecutionAPI{Service: s.executionSvc})
	webhttpapi.RegisterAssetRoutes(router, &webhttpapi.AssetsAPI{Service: s.assetSvc})
	webhttpapi.RegisterAssetColumnRoutes(router, &webhttpapi.AssetColumnsAPI{Service: s.assetSvc})
	webhttpapi.RegisterPipelineExecutionRoutes(router, &webhttpapi.PipelineExecutionAPI{Service: s.executionSvc})
	webhttpapi.RegisterSQLRoutes(router, &webhttpapi.SQLAPI{Service: s.sqlSvc})
	webhttpapi.RegisterSuggestionRoutes(router, &webhttpapi.SuggestionsAPI{Service: s.suggestionsSvc})
	webhttpapi.RegisterParseContextRoutes(router, &webhttpapi.ParseContextAPI{Service: s.parseContextSvc})
	webhttpapi.RegisterJinjaRenderRoutes(router, &webhttpapi.JinjaRenderAPI{Service: s.jinjaRenderSvc})
	webhttpapi.RegisterRunRoutes(router, &webhttpapi.RunAPI{Service: s.runSvc})
	webhttpapi.RegisterSchedulerRoutes(router, &webhttpapi.SchedulerAPI{Service: s})
	webhttpapi.RegisterOnboardingRoutes(router, &webhttpapi.OnboardingAPI{Service: s.onboardingSvc, Publisher: s})
	webhttpapi.RegisterSourceControlRoutes(router, &webhttpapi.SourceControlAPI{Service: s.sourceControlSvc})
	router.Get("/api/assets/freshness", s.handleGetAssetFreshness)

	router.Get("/*", s.handleStatic)
}

func (s *webServer) currentState() workspaceState {
	return s.workspaceCoord.CurrentState()
}

func (s *webServer) refreshWorkspace(ctx context.Context) error {
	return s.workspaceCoord.Refresh(ctx)
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
		go func() {
			if err := s.schedulerSvc.Reconcile(context.Background()); err != nil && s.logger != nil {
				s.logger.Warn("scheduler reconcile failed", zap.Error(err))
			}
		}()
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

func (s *webServer) ListRuns(ctx context.Context, filter webscheduler.RunFilter) (webscheduler.RunList, error) {
	if s.schedulerSvc == nil {
		return webscheduler.RunList{}, fmt.Errorf("scheduler is not initialized")
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
func (s *webServer) handleGetAssetFreshness(w http.ResponseWriter, r *http.Request) {
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))
	all := s.freshness.GetAll()
	if environment != "" {
		all = s.freshness.GetAllForEnvironment(environment)
	}

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
