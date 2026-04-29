package sqlintelligence

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kluctl/go-embed-python/embed_util"
	"github.com/kluctl/go-embed-python/python"
	"github.com/pkg/errors"
	"renart/internal/data"
)

type Schema map[string]map[string]string

type ParseContextRange struct {
	Start   int `json:"start"`
	End     int `json:"end"`
	Line    int `json:"line"`
	Col     int `json:"col"`
	EndLine int `json:"end_line"`
	EndCol  int `json:"end_col"`
}

type ParseContextPart struct {
	Name  string            `json:"name"`
	Kind  string            `json:"kind"`
	Range ParseContextRange `json:"range"`
}

type ParseContextTable struct {
	Name         string                       `json:"name"`
	SourceKind   string                       `json:"source_kind,omitempty"`
	ResolvedName string                       `json:"resolved_name,omitempty"`
	Alias        string                       `json:"alias"`
	Columns      []SchemaColumn               `json:"columns,omitempty"`
	ColumnRanges map[string]ParseContextRange `json:"column_ranges,omitempty"`
	Parts        []ParseContextPart           `json:"parts"`
	AliasRange   *ParseContextRange           `json:"alias_range,omitempty"`
}

type SchemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type ParseContextColumn struct {
	Name          string             `json:"name"`
	Qualifier     string             `json:"qualifier"`
	ResolvedTable string             `json:"resolved_table,omitempty"`
	Parts         []ParseContextPart `json:"parts"`
}

type ParseContextDiagnostic struct {
	Message  string             `json:"message"`
	Severity string             `json:"severity"`
	Range    *ParseContextRange `json:"range,omitempty"`
}

type ParseContext struct {
	QueryKind      string                   `json:"query_kind"`
	IsSingleSelect bool                     `json:"is_single_select"`
	Tables         []ParseContextTable      `json:"tables"`
	Columns        []ParseContextColumn     `json:"columns"`
	Diagnostics    []ParseContextDiagnostic `json:"diagnostics"`
	Errors         []string                 `json:"errors"`
}

type parseContextRequest struct {
	Query   string `json:"query"`
	Dialect string `json:"dialect"`
	Schema  Schema `json:"schema,omitempty"`
}

type parseContextResponse struct {
	ParseContext
	Error string `json:"error,omitempty"`
}

func ParseContextWithSchema(query, dialect string, schema Schema) (*ParseContext, error) {
	tmpDir := filepath.Join(os.TempDir(), "renart-sqlintelligence")

	ep, err := python.NewEmbeddedPythonWithTmpDir(tmpDir+"-python", false)
	if err != nil {
		return nil, err
	}

	sqlglotDir, err := embed_util.NewEmbeddedFilesWithTmpDir(data.Data, tmpDir+"-sqlglot-lib", false)
	if err != nil {
		return nil, err
	}
	ep.AddPythonPath(sqlglotDir.GetExtractedPath())

	sourceDir, err := embed_util.NewEmbeddedFilesWithTmpDir(pythonSource, tmpDir+"-source", false)
	if err != nil {
		return nil, err
	}

	cmd, err := ep.PythonCmd(filepath.Join(sourceDir.GetExtractedPath(), "python", "main.py"))
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	req := parseContextRequest{
		Query:   query,
		Dialect: dialect,
		Schema:  schema,
	}
	if err := json.NewEncoder(stdin).Encode(req); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return nil, err
	}
	_ = stdin.Close()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = cmd.Wait()
		return nil, errors.Wrap(err, "failed to read parse-context response")
	}

	if err := cmd.Wait(); err != nil {
		return nil, errors.Wrap(err, "parse-context process failed")
	}

	var resp parseContextResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal parse-context response")
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	return &resp.ParseContext, nil
}
