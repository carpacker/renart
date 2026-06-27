package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
)

// slingDiscoverEnvName is the deterministic env-var alias under which renart
// exposes the resolved connection URI to the Sling CLI. Sling auto-detects
// connections from environment variables holding a connection URL, so
// `sling conns discover RENART_SLING_DISCOVER` resolves to whatever URI we set.
const slingDiscoverEnvName = "RENART_SLING_DISCOVER"

// SlingDiscoveryStream is a single object/stream a Sling connection exposes.
type SlingDiscoveryStream struct {
	Name   string `json:"name"`
	Schema string `json:"schema,omitempty"`
}

// SlingDiscoveryResult is the response of `sling conns discover` for intellisense.
type SlingDiscoveryResult struct {
	Status         string                 `json:"status"`
	ConnectionName string                 `json:"connection_name"`
	Pattern        string                 `json:"pattern,omitempty"`
	Streams        []SlingDiscoveryStream `json:"streams"`
	RawOutput      string                 `json:"raw_output,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

type SlingDependencies struct {
	WorkspaceRoot        string
	NewConnectionManager func(context.Context, string) (config.ConnectionAndDetailsGetter, error)
}

type SlingService struct {
	deps SlingDependencies
}

func NewSlingService(deps SlingDependencies) *SlingService {
	return &SlingService{deps: deps}
}

// Discover lists the streams/objects a bruin connection exposes, for editor
// intellisense, by bridging the connection to a URI and running
// `sling conns discover <alias> [--pattern …]`.
func (s *SlingService) Discover(ctx context.Context, connectionName, pattern, environment string) (SlingDiscoveryResult, *APIError) {
	connectionName = strings.TrimSpace(connectionName)
	if connectionName == "" {
		return SlingDiscoveryResult{}, &APIError{Status: http.StatusBadRequest, Code: "connection_required", Message: "connection is required"}
	}
	if s.deps.NewConnectionManager == nil {
		return SlingDiscoveryResult{}, &APIError{Status: http.StatusInternalServerError, Code: "connection_manager_unavailable", Message: "connection manager is not configured"}
	}

	manager, err := s.deps.NewConnectionManager(ctx, environment)
	if err != nil {
		return SlingDiscoveryResult{}, &APIError{Status: http.StatusInternalServerError, Code: "connection_manager_failed", Message: err.Error()}
	}

	uri, err := slingConnectionURI(manager, connectionName)
	if err != nil {
		return SlingDiscoveryResult{}, &APIError{Status: http.StatusBadRequest, Code: "connection_uri_failed", Message: err.Error()}
	}

	output, runErr := runSlingConnsDiscover(ctx, s.deps.WorkspaceRoot, uri, pattern)
	if runErr != nil {
		return SlingDiscoveryResult{
			Status:         "error",
			ConnectionName: connectionName,
			Pattern:        strings.TrimSpace(pattern),
			Streams:        []SlingDiscoveryStream{},
			RawOutput:      output,
			Error:          runErr.Error(),
		}, nil
	}

	return SlingDiscoveryResult{
		Status:         "ok",
		ConnectionName: connectionName,
		Pattern:        strings.TrimSpace(pattern),
		Streams:        parseSlingDiscoverStreams(output),
		RawOutput:      output,
	}, nil
}

func runSlingConnsDiscover(ctx context.Context, workspaceRoot, connectionURI, pattern string) (string, error) {
	args := []string{"conns", "discover", slingDiscoverEnvName, "-o", "json"}
	if trimmed := strings.TrimSpace(pattern); trimmed != "" {
		args = append(args, "--pattern", trimmed)
	}

	cmdName, cmdArgs, err := slingCommand(ctx, args, nil)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	if strings.TrimSpace(workspaceRoot) != "" {
		cmd.Dir = workspaceRoot
	}
	cmd.Env = append(os.Environ(),
		"SLING_DISABLE_TELEMETRY=true",
		slingDiscoverEnvName+"="+connectionURI,
	)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	return buf.String(), runErr
}

// slingDiscoverPayload is the `-o json` shape sling emits: a generic table with
// column headers in Fields and one object per Rows entry.
type slingDiscoverPayload struct {
	Fields []string `json:"fields"`
	Rows   [][]any  `json:"rows"`
}

// parseSlingDiscoverStreams reads the JSON (`-o json`) output of
// `sling conns discover`, building schema-qualified stream names from the Schema
// and Name columns. sling may prefix the JSON with log lines, so the JSON object
// is located by line.
func parseSlingDiscoverStreams(output string) []SlingDiscoveryStream {
	payload, ok := decodeSlingDiscoverPayload(output)
	if !ok {
		return []SlingDiscoveryStream{}
	}

	nameIdx, schemaIdx := -1, -1
	for i, field := range payload.Fields {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "name", "stream", "table":
			nameIdx = i
		case "schema":
			schemaIdx = i
		}
	}
	if nameIdx < 0 {
		return []SlingDiscoveryStream{}
	}

	seen := make(map[string]struct{})
	streams := make([]SlingDiscoveryStream, 0, len(payload.Rows))
	for _, row := range payload.Rows {
		if nameIdx >= len(row) {
			continue
		}
		name := slingCellString(row[nameIdx])
		if name == "" {
			continue
		}
		schema := ""
		if schemaIdx >= 0 && schemaIdx < len(row) {
			schema = slingCellString(row[schemaIdx])
		}
		qualified := name
		if schema != "" && !strings.Contains(name, ".") {
			qualified = schema + "." + name
		} else if schema == "" {
			if dot := strings.LastIndex(name, "."); dot > 0 {
				schema = name[:dot]
			}
		}
		if _, ok := seen[qualified]; ok {
			continue
		}
		seen[qualified] = struct{}{}
		streams = append(streams, SlingDiscoveryStream{Name: qualified, Schema: schema})
	}

	sort.Slice(streams, func(i, j int) bool { return streams[i].Name < streams[j].Name })
	return streams
}

// decodeSlingDiscoverPayload finds and decodes the JSON object line in mixed
// log+JSON output.
func decodeSlingDiscoverPayload(output string) (slingDiscoverPayload, bool) {
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(slingAnsiEscape.ReplaceAllString(rawLine, ""))
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "\"rows\"") {
			continue
		}
		var payload slingDiscoverPayload
		if err := json.Unmarshal([]byte(line), &payload); err == nil {
			return payload, true
		}
	}
	return slingDiscoverPayload{}, false
}

func slingCellString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

var slingAnsiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)
