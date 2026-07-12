// Package pybroker is the per-task loopback API a running Python asset talks
// to. The Go runner starts one broker per Python task; the injected
// RENART_API_URL / RENART_API_TOKEN env vars point the `renart` Python SDK at
// it. Queries execute inside the Go process through the same connection
// manager every other run path uses, so credentials never enter the Python
// process, and the broker can hold a query until an in-flight materialization
// of a referenced asset finishes (see docs on handleQuery).
package pybroker

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/query"

	"renart/internal/web/runstate"
)

// ContextDocument is the run context served at GET /v1/context. It carries
// the same values as the BRUIN_* env vars, as one typed document.
type ContextDocument struct {
	StartDate     time.Time      `json:"start_date"`
	EndDate       time.Time      `json:"end_date"`
	ExecutionDate time.Time      `json:"execution_date"`
	RunID         string         `json:"run_id"`
	Pipeline      string         `json:"pipeline"`
	Asset         string         `json:"asset"`
	Connection    string         `json:"connection,omitempty"`
	Environment   string         `json:"environment,omitempty"`
	FullRefresh   bool           `json:"full_refresh"`
	Vars          map[string]any `json:"vars,omitempty"`
}

// Config wires one broker instance to its task.
type Config struct {
	// Token authorizes requests; generated when empty.
	Token string
	// Context describes the running task; Asset/Environment/RunID also scope
	// the waiting semantics.
	Context ContextDocument
	// DefaultConnection runs queries that don't name a connection.
	DefaultConnection string
	// RunQuery executes sql on a named project connection and returns the
	// full result. Required.
	RunQuery func(ctx context.Context, connection, sql string) (*query.QueryResult, error)
	// ValidateSQL rejects non-read-only statements. Nil allows everything.
	ValidateSQL func(sql string) error
	// UsedTables extracts the table references of sql, enabling the waiting
	// and lint semantics. Nil skips both.
	UsedTables func(sql string) ([]string, error)
	// Registry answers "is something materializing this asset right now".
	// Nil skips waiting.
	Registry *runstate.Registry
	// KnownAssets are the asset names of the workspace (waiting matches
	// registry contents directly; KnownAssets powers the undeclared-
	// dependency lint).
	KnownAssets []string
	// DeclaredUpstreams are the asset's declared depends entries.
	DeclaredUpstreams []string
	// Log receives human-readable progress lines ("waiting for X…"), shown in
	// the asset's run output. Nil discards them.
	Log io.Writer
	// WaitTimeout bounds one wait on an in-flight materialization.
	// Defaults to 30 minutes.
	WaitTimeout time.Duration
}

// Broker is a running loopback listener. Close it when the task finishes.
type Broker struct {
	URL   string
	Token string

	server   *http.Server
	listener net.Listener
	cfg      Config
	baseCtx  context.Context
}

// Start launches the broker on an ephemeral loopback port.
func Start(ctx context.Context, cfg Config) (*Broker, error) {
	if cfg.RunQuery == nil {
		return nil, errors.New("pybroker requires a RunQuery implementation")
	}
	token := cfg.Token
	if token == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("failed to generate a broker token: %w", err)
		}
		token = hex.EncodeToString(raw)
	}
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = 30 * time.Minute
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to bind the python broker: %w", err)
	}

	broker := &Broker{
		URL:      "http://" + listener.Addr().String(),
		Token:    token,
		listener: listener,
		cfg:      cfg,
		baseCtx:  ctx,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/query", broker.requireToken(broker.handleQuery))
	mux.HandleFunc("GET /v1/context", broker.requireToken(broker.handleContext))
	broker.server = &http.Server{
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = broker.server.Serve(listener)
	}()
	return broker, nil
}

func (b *Broker) Close() {
	_ = b.server.Close()
}

func (b *Broker) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(b.Token)) != 1 {
			writeBrokerError(w, http.StatusForbidden, "invalid_token", "invalid or missing broker token")
			return
		}
		next(w, r)
	}
}

type queryRequest struct {
	SQL        string `json:"sql"`
	Connection string `json:"connection"`
}

func (b *Broker) handleContext(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b.cfg.Context)
}

// handleQuery runs one read-only query. Before executing, referenced assets
// with an in-flight materialization (in this environment, any run) are
// awaited; a reference to an asset planned later in the *same* run fails fast
// — waiting on it would deadlock, the fix is declaring the dependency.
func (b *Broker) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid_request", "request body must be JSON with a sql field")
		return
	}
	sql := strings.TrimSpace(req.SQL)
	if sql == "" {
		writeBrokerError(w, http.StatusBadRequest, "invalid_request", "sql is required")
		return
	}

	connection := strings.TrimSpace(req.Connection)
	if connection == "" {
		connection = b.cfg.DefaultConnection
	}
	if connection == "" {
		writeBrokerError(w, http.StatusBadRequest, "no_connection", "the asset has no connection; pass connection= explicitly")
		return
	}

	if b.cfg.ValidateSQL != nil {
		if err := b.cfg.ValidateSQL(sql); err != nil {
			writeBrokerError(w, http.StatusBadRequest, "not_read_only", err.Error())
			return
		}
	}

	if err := b.awaitReferencedAssets(r.Context(), sql); err != nil {
		status := http.StatusConflict
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeBrokerError(w, status, "upstream_not_ready", err.Error())
		return
	}

	result, err := b.cfg.RunQuery(r.Context(), connection, sql)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, "query_failed", err.Error())
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"columns":      result.Columns,
			"column_types": result.ColumnTypes,
			"rows":         result.Rows,
		})
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
	if err := writeQueryResultArrow(w, result); err != nil {
		// The stream may be half-written; the client's IPC reader will fail
		// and surface the abort.
		b.logf("failed to stream the query result: %v", err)
	}
}

func (b *Broker) awaitReferencedAssets(ctx context.Context, sql string) error {
	if b.cfg.UsedTables == nil {
		return nil
	}
	tables, err := b.cfg.UsedTables(sql)
	if err != nil || len(tables) == 0 {
		// Extraction is best effort; an unparseable query still executes.
		return nil
	}

	known := make(map[string]struct{}, len(b.cfg.KnownAssets))
	for _, name := range b.cfg.KnownAssets {
		known[normalizeRef(name)] = struct{}{}
	}
	declared := make(map[string]struct{}, len(b.cfg.DeclaredUpstreams))
	for _, name := range b.cfg.DeclaredUpstreams {
		declared[normalizeRef(name)] = struct{}{}
	}
	self := normalizeRef(b.cfg.Context.Asset)

	for _, table := range tables {
		ref := normalizeRef(table)
		if ref == "" || ref == self {
			continue
		}
		if _, isDeclared := declared[ref]; !isDeclared {
			if _, isAsset := known[ref]; isAsset {
				b.logf("note: %s reads %s without declaring it in depends; run ordering is only guaranteed for declared dependencies", b.cfg.Context.Asset, ref)
			}
		}
		if b.cfg.Registry == nil {
			continue
		}
		lookup := b.cfg.Registry.Lookup(ref, b.cfg.Context.Environment, b.cfg.Context.RunID)
		if lookup.PendingInRun {
			return fmt.Errorf("asset %q is scheduled to run later in this run; declare it in this asset's depends so it materializes first", ref)
		}
		if lookup.InFlight == nil {
			continue
		}
		b.logf("waiting for %s to finish materializing…", ref)
		waitCtx, cancel := context.WithTimeout(ctx, b.cfg.WaitTimeout)
		waitErr := lookup.InFlight.Wait(waitCtx)
		cancel()
		if waitErr != nil {
			if errors.Is(waitErr, context.DeadlineExceeded) {
				return fmt.Errorf("timed out waiting for %s to finish materializing: %w", ref, waitErr)
			}
			return fmt.Errorf("upstream %s failed to materialize: %v", ref, waitErr)
		}
		b.logf("%s is ready", ref)
	}
	return nil
}

// normalizeRef canonicalizes a table reference or asset name for matching:
// lowercase, quotes stripped, and a leading catalog dropped from three-part
// references so `db.schema.table` matches the asset `schema.table`.
func normalizeRef(ref string) string {
	cleaned := strings.ToLower(strings.TrimSpace(ref))
	cleaned = strings.NewReplacer(`"`, "", "`", "").Replace(cleaned)
	parts := strings.Split(cleaned, ".")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, ".")
}

func (b *Broker) logf(format string, args ...any) {
	if b.cfg.Log == nil {
		return
	}
	_, _ = fmt.Fprintf(b.cfg.Log, format+"\n", args...)
}

func writeBrokerError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
