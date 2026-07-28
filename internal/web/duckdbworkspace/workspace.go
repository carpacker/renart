package duckdbworkspace

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bruin-data/bruin/pkg/config"
	duck "github.com/bruin-data/bruin/pkg/duckdb"
	"github.com/bruin-data/bruin/pkg/query"
)

// Client makes DuckDB file references resolve from one workspace without
// changing the process working directory. DuckDB connections are ephemeral, so
// the search path is set in the same multi-statement query as the user SQL.
type Client struct {
	*duck.Client
	workspaceRoot string
}

// WrapClient applies workspace-relative file resolution to a Bruin DuckDB
// client. An empty workspace root leaves the client unchanged.
func WrapClient(client *duck.Client, workspaceRoot string) duck.DuckDBClient {
	root := cleanWorkspaceRoot(workspaceRoot)
	if client == nil || root == "" {
		return client
	}
	return &Client{Client: client, workspaceRoot: root}
}

func (c *Client) RunQueryWithoutResult(ctx context.Context, q *query.Query) error {
	return c.Client.RunQueryWithoutResult(ctx, c.withWorkspace(q, false))
}

func (c *Client) Select(ctx context.Context, q *query.Query) ([][]interface{}, error) {
	return c.Client.Select(ctx, c.withWorkspace(q, false))
}

func (c *Client) SelectWithSchema(ctx context.Context, q *query.Query) (*query.QueryResult, error) {
	resultQuery := q != nil && query.IsLikelyResultQuery(q.String())
	return c.Client.SelectWithSchema(ctx, c.withWorkspace(q, resultQuery))
}

func (c *Client) withWorkspace(q *query.Query, keepResultDispatch bool) *query.Query {
	if q == nil {
		return nil
	}
	clone := *q
	root := strings.ReplaceAll(c.workspaceRoot, "'", "''")
	prefix := ""
	if keepResultDispatch {
		// Bruin dispatches SelectWithSchema by the first statement. Keep a
		// result-producing statement first while DuckDB returns the final
		// statement's rows and schema.
		prefix = "select null where false;\n"
	}
	clone.Query = prefix + "set file_search_path = '" + root + "';\n" + q.Query
	return &clone
}

type manager struct {
	config.ConnectionAndDetailsGetter
	workspaceRoot string

	mu      sync.Mutex
	clients map[string]*Client
}

// WrapManager applies workspace-relative file resolution to DuckDB
// connections while transparently preserving every other connection.
func WrapManager(base config.ConnectionAndDetailsGetter, workspaceRoot string) config.ConnectionAndDetailsGetter {
	root := cleanWorkspaceRoot(workspaceRoot)
	if base == nil || root == "" {
		return base
	}
	return &manager{
		ConnectionAndDetailsGetter: base,
		workspaceRoot:              root,
		clients:                    make(map[string]*Client),
	}
}

func (m *manager) GetConnection(name string) any {
	raw := m.ConnectionAndDetailsGetter.GetConnection(name)
	return m.wrapConnection(name, raw)
}

func (m *manager) ResolveConnection(name string) (any, error) {
	if resolver, ok := m.ConnectionAndDetailsGetter.(config.ConnectionResolver); ok {
		raw, err := resolver.ResolveConnection(name)
		if err != nil {
			return nil, err
		}
		return m.wrapConnection(name, raw), nil
	}
	return m.GetConnection(name), nil
}

func (m *manager) wrapConnection(name string, raw any) any {
	if !strings.EqualFold(m.ConnectionAndDetailsGetter.GetConnectionType(name), "duckdb") {
		return raw
	}
	client, ok := raw.(*duck.Client)
	if !ok || client == nil {
		return raw
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if wrapped, ok := m.clients[name]; ok && wrapped.Client == client {
		return wrapped
	}
	wrapped := &Client{Client: client, workspaceRoot: m.workspaceRoot}
	m.clients[name] = wrapped
	return wrapped
}

var _ config.ConnectionResolver = (*manager)(nil)

func cleanWorkspaceRoot(workspaceRoot string) string {
	trimmed := strings.TrimSpace(workspaceRoot)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}
