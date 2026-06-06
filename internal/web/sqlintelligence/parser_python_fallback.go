//go:build renart_sqlglot_fallback

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

func ParseContextWithSchemaPython(query, dialect string, schema Schema, columnSourceMethods ...SchemaColumnSourceMethods) (*ParseContext, error) {
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
	if len(columnSourceMethods) > 0 {
		req.ColumnSourceMethods = columnSourceMethods[0]
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
