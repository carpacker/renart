package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	gogit "github.com/go-git/go-git/v5"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineTemplatesExposeFeatureFocusedStarters(t *testing.T) {
	t.Parallel()

	templates := PipelineTemplates()
	ids := make([]string, 0, len(templates))
	for _, template := range templates {
		ids = append(ids, template.ID)
		assert.NotEmpty(t, template.Title)
		assert.NotEmpty(t, template.Description)
		assert.NotEmpty(t, template.SuggestedPath)
		assert.NotNil(t, template.AssetNames)
		assert.NotEmpty(t, template.Features)
		if template.ID != PipelineTemplateBlank {
			assert.NotEmpty(t, template.AssetNames)
		}
	}

	assert.Equal(t, []string{
		PipelineTemplateBlank,
		PipelineTemplateProductDemo,
		ProjectTemplateRetailDemo,
		PipelineTemplateOperationsDemo,
		PipelineTemplatePythonDemo,
		ProjectTemplateChessDemo,
	}, ids)
}

func TestPipelineServiceCreatesEveryTemplateAsParseablePipeline(t *testing.T) {
	for _, template := range PipelineTemplates() {
		t.Run(template.ID, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			_, err := gogit.PlainInit(workspaceRoot, false)
			require.NoError(t, err)
			service := NewPipelineService(workspaceRoot)

			relPath, err := service.Create(
				context.Background(),
				template.SuggestedPath,
				template.Title,
				"",
				template.ID,
			)
			require.NoError(t, err)
			assert.Equal(t, template.SuggestedPath, relPath)
			assert.DirExists(t, filepath.Join(workspaceRoot, relPath, "assets"))

			parsed, err := NewRenartPipelineBuilder(afero.NewOsFs()).CreatePipelineFromPath(
				context.Background(),
				filepath.Join(workspaceRoot, relPath),
				pipeline.WithMutate(),
			)
			require.NoError(t, err)
			assert.Equal(t, template.Title, parsed.Name)

			names := make([]string, 0, len(parsed.Assets))
			for _, asset := range parsed.Assets {
				names = append(names, asset.Name)
			}
			assert.ElementsMatch(t, template.AssetNames, names)
			for _, asset := range parsed.Assets {
				for _, upstream := range asset.Upstreams {
					assert.Contains(t, names, upstream.Value, "%s depends on %s", asset.Name, upstream.Value)
				}
			}

			report := runTypeCheck(t, parsed, workspaceRoot)
			assert.Zero(t, report.Summary.Errors, "generated starter should not have typecheck errors: %+v", report.Assets)
			assert.Zero(t, report.Summary.Warnings, "generated starter should not have typecheck warnings: %+v", report.Assets)

			if template.ID != PipelineTemplateBlank {
				configContents, readErr := os.ReadFile(filepath.Join(workspaceRoot, ".bruin.yml"))
				require.NoError(t, readErr)
				assert.Contains(t, string(configContents), "duckdb-default")
			}
		})
	}
}

func TestPipelineServiceTemplateCreationRejectsUnsafeOverwritesAndRollsBackInvalidContent(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	service := NewPipelineService(workspaceRoot)
	existing := filepath.Join(workspaceRoot, "existing")
	require.NoError(t, os.MkdirAll(existing, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(existing, "keep.txt"), []byte("keep"), 0o644))

	_, err := service.Create(context.Background(), "existing", "", "", PipelineTemplateProductDemo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.FileExists(t, filepath.Join(existing, "keep.txt"))

	_, err = service.Create(context.Background(), "unknown", "", "", "demo:unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown pipeline template")
	assert.NoDirExists(t, filepath.Join(workspaceRoot, "unknown"))

	_, err = service.Create(
		context.Background(),
		"mixed",
		"",
		"name: mixed\n",
		PipelineTemplateProductDemo,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be combined")
	assert.NoDirExists(t, filepath.Join(workspaceRoot, "mixed"))

	_, err = service.Create(
		context.Background(),
		"broken",
		"",
		"name: [\n",
		PipelineTemplateBlank,
	)
	require.Error(t, err)
	assert.NoDirExists(t, filepath.Join(workspaceRoot, "broken"))
}

func TestPipelineTemplatePythonQueryDeclaresItsDependency(t *testing.T) {
	t.Parallel()

	template, ok := pipelineTemplateByID(PipelineTemplatePythonDemo)
	require.True(t, ok)
	files := template.files("Python risk scoring")
	pythonAsset := files["assets/risk/scored_accounts.py"]
	assert.Contains(t, pythonAsset, "from renart import query")
	assert.Contains(t, pythonAsset, "select * from risk.account_features")
	assert.True(t, strings.Contains(pythonAsset, "depends:\n  - risk.account_features"))
}
