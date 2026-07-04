package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"sync"

	"github.com/bruin-data/bruin/pkg/git"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"go.uber.org/zap"

	webapi "renart/internal/web/api"
	webhttpapi "renart/internal/web/httpapi"
	"renart/internal/web/identity"
	"renart/internal/web/registry"
)

// projectRuntime is one open project: the fully wired per-root server plus
// its own router, exactly what cmd/server.go used to wire for the single
// argv root.
type projectRuntime struct {
	id      string
	name    string
	root    string
	server  *webServer
	router  chi.Router
	cleanup func()
}

// projectManager holds one runtime per open project with lazy open. The
// default project (the argv root) is always open; others open on first
// request, keyed by the stable UUID from .renart/project.yml.
type projectManager struct {
	ctx      context.Context
	logger   *zap.Logger
	baseCfg  serverConfig
	registry *registry.Registry

	mu        sync.Mutex
	defaultID string
	runtimes  map[string]*projectRuntime
}

// registryPath resolves the projects.json location; RENART_PROJECTS_REGISTRY
// overrides it for tests.
func registryPath() (string, error) {
	if override := os.Getenv("RENART_PROJECTS_REGISTRY"); override != "" {
		return override, nil
	}
	return registry.DefaultPath()
}

func newProjectManager(ctx context.Context, logger *zap.Logger, baseCfg serverConfig, defaultRuntime *projectRuntime) (*projectManager, error) {
	regPath, err := registryPath()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project registry path: %w", err)
	}

	manager := &projectManager{
		ctx:       ctx,
		logger:    logger,
		baseCfg:   baseCfg,
		registry:  registry.New(regPath),
		defaultID: defaultRuntime.id,
		runtimes:  map[string]*projectRuntime{defaultRuntime.id: defaultRuntime},
	}
	manager.record(defaultRuntime)
	return manager, nil
}

// newProjectRuntime wires a per-root server. The returned runtime's router
// carries only routes (no middleware): the root router's middleware stack
// already ran by the time requests are rewritten into it.
func newProjectRuntime(ctx context.Context, logger *zap.Logger, cfg serverConfig) (*projectRuntime, error) {
	project, err := identity.EnsureProject(
		afero.NewOsFs(),
		filepath.Join(cfg.workspaceRoot, ".renart", "project.yml"),
		filepath.Base(cfg.workspaceRoot),
	)
	if err != nil {
		return nil, err
	}

	server, cleanup, err := newWebServer(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	router := chi.NewRouter()
	server.registerRoutes(router)

	return &projectRuntime{
		id:      project.ID,
		name:    project.Name,
		root:    cfg.workspaceRoot,
		server:  server,
		router:  router,
		cleanup: cleanup,
	}, nil
}

func (m *projectManager) record(rt *projectRuntime) {
	if _, err := m.registry.Upsert(registry.Entry{
		ID:           rt.id,
		Name:         rt.name,
		Path:         rt.root,
		Type:         "local",
		LastOpenedAt: time.Now().UTC(),
	}); err != nil {
		m.logger.Warn("failed to record project in registry", zap.Error(err))
	}
}

// OpenProject opens the project at path (or returns the already-open
// runtime) and records it in the registry.
func (m *projectManager) OpenProject(path string) (webhttpapi.ProjectInfo, error) {
	rt, err := m.openPath(path)
	if err != nil {
		return webhttpapi.ProjectInfo{}, err
	}
	return webhttpapi.ProjectInfo{
		ID:           rt.id,
		Name:         rt.name,
		Path:         rt.root,
		Type:         "local",
		LastOpenedAt: time.Now().UTC(),
		Open:         true,
		Exists:       true,
		Default:      rt.id == m.defaultID,
	}, nil
}

func (m *projectManager) openPath(path string) (*projectRuntime, error) {
	absRoot, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project path: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", absRoot)
	}
	if _, err := git.FindRepoFromPath(absRoot); err != nil {
		return nil, fmt.Errorf("projects must live inside a git repository: %w", err)
	}

	// Resolve identity first so an already-open project (even under a
	// different path spelling) reuses its runtime.
	project, err := identity.EnsureProject(
		afero.NewOsFs(),
		filepath.Join(absRoot, ".renart", "project.yml"),
		filepath.Base(absRoot),
	)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if rt, ok := m.runtimes[project.ID]; ok {
		m.record(rt)
		return rt, nil
	}

	cfg := m.baseCfg
	cfg.workspaceRoot = absRoot
	cfg.schedulerStatePath = filepath.Join(absRoot, ".renart", "state.db")

	rt, err := newProjectRuntime(m.ctx, m.logger, cfg)
	if err != nil {
		return nil, err
	}
	m.runtimes[rt.id] = rt
	m.record(rt)
	m.logger.Info("opened project runtime",
		zap.String("project_id", rt.id),
		zap.String("root", rt.root),
	)
	return rt, nil
}

// runtimeByID returns the open runtime for id, lazily opening it from its
// registered path when needed.
func (m *projectManager) runtimeByID(id string) (*projectRuntime, error) {
	m.mu.Lock()
	rt, ok := m.runtimes[id]
	m.mu.Unlock()
	if ok {
		return rt, nil
	}

	file, err := m.registry.Load()
	if err != nil {
		return nil, err
	}
	for _, entry := range file.Projects {
		if entry.ID != id {
			continue
		}
		rt, err := m.openPath(entry.Path)
		if err != nil {
			return nil, err
		}
		if rt.id != id {
			return nil, fmt.Errorf("project at %s now identifies as %s (expected %s); its project.yml changed", entry.Path, rt.id, id)
		}
		return rt, nil
	}
	return nil, fmt.Errorf("unknown project %s", id)
}

func (m *projectManager) ListProjects() webhttpapi.ProjectListResponse {
	response := webhttpapi.ProjectListResponse{
		Status:           "ok",
		DefaultProjectID: m.defaultID,
		Projects:         []webhttpapi.ProjectInfo{},
	}

	file, err := m.registry.Load()
	if err != nil {
		m.logger.Warn("failed to load project registry", zap.Error(err))
		return response
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range file.Projects {
		_, open := m.runtimes[entry.ID]
		exists := false
		if info, err := os.Stat(entry.Path); err == nil && info.IsDir() {
			exists = true
		}
		response.Projects = append(response.Projects, webhttpapi.ProjectInfo{
			ID:           entry.ID,
			Name:         entry.Name,
			Path:         entry.Path,
			Type:         entry.Type,
			LastOpenedAt: entry.LastOpenedAt,
			Open:         open,
			Exists:       exists,
			Default:      entry.ID == m.defaultID,
		})
	}
	return response
}

func (m *projectManager) RemoveProject(id string) error {
	if id == m.defaultID {
		return fmt.Errorf("cannot remove the project this server was started on")
	}
	m.mu.Lock()
	_, open := m.runtimes[id]
	m.mu.Unlock()
	if open {
		return fmt.Errorf("project is currently open; close its tabs first")
	}
	_, err := m.registry.Remove(id)
	return err
}

// mountProjectRoutes exposes every open (or lazily openable) project under
// /api/projects/{projectID}/... by rewriting the path back to the /api/...
// shape the per-project routers expect. The unprefixed /api/* routes stay
// aliased to the default project so existing pages and the e2e suite keep
// working during the migration.
func (m *projectManager) mountProjectRoutes(router chi.Router) {
	webhttpapi.RegisterProjectRoutes(router, &webhttpapi.ProjectsAPI{Directory: m})

	router.HandleFunc("/api/projects/{projectID}/*", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "projectID")
		rt, err := m.runtimeByID(id)
		if err != nil {
			webapi.WriteNotFound(w, "project_not_found", err.Error())
			return
		}

		rest := chi.URLParam(r, "*")
		proxied := r.Clone(r.Context())
		proxied.URL.Path = "/api/" + rest
		proxied.URL.RawPath = ""
		// The per-project router must match from scratch, not inherit this
		// route's chi context.
		routeCtx := chi.NewRouteContext()
		proxied = proxied.WithContext(context.WithValue(proxied.Context(), chi.RouteCtxKey, routeCtx))
		rt.router.ServeHTTP(w, proxied)
	})
}

// buildRootRouter assembles the process-level router: middleware, project
// directory routes, per-project mounts, and the default project's routes
// (API alias + static assets) at the root.
func buildRootRouter(manager *projectManager, defaultRuntime *projectRuntime) chi.Router {
	router := chi.NewRouter()
	router.Use(
		webhttpapi.Recoverer(defaultRuntime.server.logger),
		webhttpapi.SameOriginGuard(),
		webhttpapi.RequestLogger(defaultRuntime.server.logger),
	)
	manager.mountProjectRoutes(router)
	defaultRuntime.server.registerRoutes(router)
	return router
}

// closeAll tears down every runtime except the default one, whose cleanup
// the caller already owns.
func (m *projectManager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, rt := range m.runtimes {
		if id == m.defaultID {
			continue
		}
		rt.cleanup()
		delete(m.runtimes, id)
	}
}
