package service

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"renart/internal/web/events"
	"renart/internal/web/freshness"
	webmodel "renart/internal/web/model"
)

type WorkspaceEvent struct {
	Type            string         `json:"type"`
	Path            string         `json:"path,omitempty"`
	Workspace       WorkspaceState `json:"workspace"`
	Lite            bool           `json:"lite,omitempty"`
	ChangedAssetIDs []string       `json:"changed_asset_ids,omitempty"`
}

type WorkspaceAsset struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Path                string            `json:"path"`
	Content             string            `json:"content"`
	Upstreams           []string          `json:"upstreams"`
	Parameters          map[string]string `json:"parameters,omitempty"`
	Meta                map[string]string `json:"meta,omitempty"`
	Columns             []WorkspaceColumn `json:"columns,omitempty"`
	Connection          string            `json:"connection,omitempty"`
	MaterializationType string            `json:"materialization_type,omitempty"`
	IsMaterialized      bool              `json:"is_materialized"`
	MaterializedAs      string            `json:"materialized_as,omitempty"`
	RowCount            *int64            `json:"row_count,omitempty"`
}

type WorkspaceColumnCheck struct {
	Name        string `json:"name"`
	Value       any    `json:"value,omitempty"`
	Blocking    *bool  `json:"blocking,omitempty"`
	Description string `json:"description,omitempty"`
}

type WorkspaceColumn struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	PrimaryKey    bool                   `json:"primary_key,omitempty"`
	UpdateOnMerge bool                   `json:"update_on_merge,omitempty"`
	MergeSQL      string                 `json:"merge_sql,omitempty"`
	Nullable      *bool                  `json:"nullable,omitempty"`
	Owner         string                 `json:"owner,omitempty"`
	Domains       []string               `json:"domains,omitempty"`
	Meta          map[string]string      `json:"meta,omitempty"`
	Checks        []WorkspaceColumnCheck `json:"checks,omitempty"`
}

type WorkspacePipeline struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Schedule string           `json:"schedule,omitempty"`
	Assets   []WorkspaceAsset `json:"assets"`
}

type WorkspaceState struct {
	Pipelines           []WorkspacePipeline `json:"pipelines"`
	Connections         map[string]string   `json:"connections"`
	SelectedEnvironment string              `json:"selected_environment,omitempty"`
	Errors              []string            `json:"errors,omitempty"`
	UpdatedAt           time.Time           `json:"updated_at"`
	Metadata            map[string][]string `json:"metadata"`
	Revision            int64               `json:"revision,omitempty"`
}

type WorkspaceCoordinatorDependencies struct {
	WorkspaceService *WorkspaceService
	Hub              *events.Hub
	Freshness        *freshness.Tracker
	ConvertState     func(webmodel.WorkspaceState) WorkspaceState
	RefreshHook      func(context.Context) error
}

type WorkspaceCoordinator struct {
	deps WorkspaceCoordinatorDependencies

	stateMu  sync.RWMutex
	state    WorkspaceState
	revision atomic.Int64

	recentServerWritesMu sync.Mutex
	recentServerWrites   map[string]time.Time
}

func NewWorkspaceCoordinator(deps WorkspaceCoordinatorDependencies) *WorkspaceCoordinator {
	return &WorkspaceCoordinator{
		deps:               deps,
		recentServerWrites: make(map[string]time.Time),
	}
}

func (c *WorkspaceCoordinator) CurrentState() WorkspaceState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

func (c *WorkspaceCoordinator) SetState(state WorkspaceState) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.state = state
}

func (c *WorkspaceCoordinator) Refresh(ctx context.Context) error {
	if c.deps.RefreshHook != nil {
		return c.deps.RefreshHook(ctx)
	}
	if err := c.deps.WorkspaceService.Refresh(ctx); err != nil {
		return err
	}

	state := c.deps.ConvertState(c.deps.WorkspaceService.GetState())
	state.Revision = c.revision.Add(1)
	c.SetState(state)
	return nil
}

func (c *WorkspaceCoordinator) SuppressWatcherFor(eventPath string) {
	normalized := filepath.ToSlash(eventPath)
	c.recentServerWritesMu.Lock()
	c.recentServerWrites[normalized] = time.Now()
	c.recentServerWritesMu.Unlock()
}

func (c *WorkspaceCoordinator) IsWatcherSuppressed(eventPath string) bool {
	normalized := filepath.ToSlash(eventPath)
	now := time.Now()

	c.recentServerWritesMu.Lock()
	defer c.recentServerWritesMu.Unlock()
	for path, ts := range c.recentServerWrites {
		if now.Sub(ts) > 3*time.Second {
			delete(c.recentServerWrites, path)
		}
	}

	ts, ok := c.recentServerWrites[normalized]
	return ok && now.Sub(ts) <= 3*time.Second
}

func (c *WorkspaceCoordinator) PushUpdate(ctx context.Context, eventType, eventPath string) {
	_ = c.Refresh(ctx)
	state := c.CurrentState()
	changed := c.FindDirectlyChangedAssetIDs(filepath.ToSlash(eventPath))

	now := time.Now().UTC()
	for _, id := range changed {
		if name := c.FindAssetNameByID(id); name != "" {
			c.deps.Freshness.RecordContentChange(name, now)
		}
	}

	c.deps.Hub.Publish(WorkspaceEvent{
		Type:            eventType,
		Path:            filepath.ToSlash(eventPath),
		Workspace:       StripAssetContent(state),
		Lite:            true,
		ChangedAssetIDs: changed,
	})
}

func (c *WorkspaceCoordinator) PushUpdateImmediate(ctx context.Context, eventType, eventPath string) {
	c.PushUpdateImmediateWithChangedIDs(ctx, eventType, eventPath, nil)
}

func (c *WorkspaceCoordinator) PushUpdateImmediateWithChangedIDs(ctx context.Context, eventType, eventPath string, changedAssetIDs []string) {
	_ = c.Refresh(ctx)
	state := c.CurrentState()
	changed := changedAssetIDs
	if len(changed) == 0 {
		changed = c.FindDirectlyChangedAssetIDs(filepath.ToSlash(eventPath))
	}

	now := time.Now().UTC()
	for _, id := range changed {
		if name := c.FindAssetNameByID(id); name != "" {
			c.deps.Freshness.RecordContentChange(name, now)
		}
	}

	c.deps.Hub.PublishImmediate(WorkspaceEvent{
		Type:            eventType,
		Path:            filepath.ToSlash(eventPath),
		Workspace:       StripAssetContentKeepingIDs(state, changed),
		Lite:            true,
		ChangedAssetIDs: changed,
	})
}

func (c *WorkspaceCoordinator) Subscribe() chan []byte {
	return c.deps.Hub.Subscribe()
}

func (c *WorkspaceCoordinator) Unsubscribe(ch chan []byte) {
	c.deps.Hub.Unsubscribe(ch)
}

func (c *WorkspaceCoordinator) CurrentStateLiteEvent() WorkspaceEvent {
	return WorkspaceEvent{
		Type:      "workspace.updated",
		Workspace: StripAssetContent(c.CurrentState()),
		Lite:      true,
	}
}

type workspaceAssetEntry struct {
	id        string
	name      string
	path      string
	upstreams []string
}

func (c *WorkspaceCoordinator) buildAssetIndex() ([]workspaceAssetEntry, map[string]string) {
	state := c.CurrentState()
	var all []workspaceAssetEntry
	nameToID := make(map[string]string)
	for _, p := range state.Pipelines {
		for _, a := range p.Assets {
			all = append(all, workspaceAssetEntry{id: a.ID, name: a.Name, path: a.Path, upstreams: a.Upstreams})
			nameToID[a.Name] = a.ID
		}
	}
	return all, nameToID
}

func buildDownstreamIndex(assets []workspaceAssetEntry, nameToID map[string]string) map[string][]string {
	downstream := make(map[string][]string)
	for _, a := range assets {
		for _, upName := range a.upstreams {
			if upID, ok := nameToID[upName]; ok {
				downstream[upID] = append(downstream[upID], a.id)
			}
		}
	}
	return downstream
}

func (c *WorkspaceCoordinator) FindDirectlyChangedAssetIDs(eventPath string) []string {
	assets, _ := c.buildAssetIndex()
	normalizedEvent := filepath.ToSlash(eventPath)

	var result []string
	for _, a := range assets {
		if PathContains(normalizedEvent, a.path) {
			result = append(result, a.id)
		}
	}
	sort.Strings(result)
	return result
}

func (c *WorkspaceCoordinator) FindMaterializationInspectIDs(assetIDs ...string) []string {
	assets, nameToID := c.buildAssetIndex()
	downstream := buildDownstreamIndex(assets, nameToID)

	seen := make(map[string]struct{})
	for _, id := range assetIDs {
		seen[id] = struct{}{}
		for _, child := range downstream[id] {
			seen[child] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func (c *WorkspaceCoordinator) FindAssetNameByID(assetID string) string {
	state := c.CurrentState()
	for _, p := range state.Pipelines {
		for _, a := range p.Assets {
			if a.ID == assetID {
				return a.Name
			}
		}
	}
	return ""
}

func StripAssetContent(state WorkspaceState) WorkspaceState {
	lite := state
	lite.Pipelines = make([]WorkspacePipeline, len(state.Pipelines))
	for i, p := range state.Pipelines {
		litePipeline := p
		litePipeline.Assets = make([]WorkspaceAsset, len(p.Assets))
		for j, a := range p.Assets {
			a.Content = ""
			litePipeline.Assets[j] = a
		}
		lite.Pipelines[i] = litePipeline
	}
	return lite
}

func StripAssetContentKeepingIDs(state WorkspaceState, keepIDs []string) WorkspaceState {
	if len(keepIDs) == 0 {
		return StripAssetContent(state)
	}

	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}

	lite := state
	lite.Pipelines = make([]WorkspacePipeline, len(state.Pipelines))
	for i, p := range state.Pipelines {
		litePipeline := p
		litePipeline.Assets = make([]WorkspaceAsset, len(p.Assets))
		for j, a := range p.Assets {
			if _, ok := keep[a.ID]; !ok {
				a.Content = ""
			}
			litePipeline.Assets[j] = a
		}
		lite.Pipelines[i] = litePipeline
	}

	return lite
}

func PathContains(eventPath, assetPath string) bool {
	eventPath = filepath.ToSlash(filepath.Clean(eventPath))
	assetPath = filepath.ToSlash(filepath.Clean(assetPath))

	if eventPath == assetPath {
		return true
	}
	if strings.HasPrefix(assetPath, eventPath+"/") {
		return true
	}

	base := filepath.Base(eventPath)
	if base == "pipeline.yml" || base == ".pipeline.yml" {
		assetsDir := filepath.ToSlash(filepath.Join(filepath.Dir(eventPath), "assets"))
		if strings.HasPrefix(assetPath, assetsDir+"/") {
			return true
		}
	}

	if base == "asset.yml" || base == ".asset.yml" ||
		base == "schema.yml" || base == "schema.yaml" ||
		base == "checks.yml" || base == "checks.yaml" ||
		base == "source.yml" || base == "source.yaml" {
		if filepath.Dir(eventPath) == filepath.Dir(assetPath) {
			return true
		}
	}

	return false
}
