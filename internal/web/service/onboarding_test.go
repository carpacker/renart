package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnboardingImportDatabaseReturnsSchemaAssetPaths(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	runner := &stubRunRunner{output: []byte(`{"status":"ok"}`)}
	svc := NewOnboardingService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"), runner, nil)

	result := svc.ImportDatabase(context.Background(), OnboardingImportRequest{
		ConnectionName:  "postgres-default",
		EnvironmentName: "default",
		PipelineName:    "analytics",
		Schema:          "analytics",
		Tables:          []string{"analytics.orders"},
		CreateIfMissing: true,
	})

	require.Equal(t, "ok", result.Status)
	assert.Equal(t, []string{"analytics/assets/analytics/orders.asset.yml"}, result.AssetPaths)
	assert.Equal(t, []string{"patch", "fill-asset-dependencies", "analytics"}, runner.args)

	contents, err := os.ReadFile(filepath.Join(workspaceRoot, "analytics", "pipeline.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "name: analytics")
}

func TestCreateDuckDBQuickstartCreatesChessAnalysisAssetsAndDatabaseFile(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	runner := &stubRunRunner{output: []byte("quickstart run complete")}
	materializer := &stubRunMaterializer{result: MaterializeResult{Status: "ok", Output: "quickstart run complete"}}
	svc := NewOnboardingService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"), runner, materializer)

	result := svc.CreateDuckDBQuickstart(context.Background(), OnboardingQuickstartRequest{
		EnvironmentName: "default",
		PipelineName:    "quickstart",
		ConnectionName:  "duckdb-default",
		DatabasePath:    "duckdb-files/chess_playground.duckdb",
		Materialize:     true,
	})

	require.Equal(t, "ok", result.Status)
	assert.Empty(t, runner.args, "quickstart materialization must not bypass the completion-aware execution service")
	assert.Equal(t, EncodeID("quickstart"), materializer.pipelineID)
	assert.Equal(t, "default", materializer.pipelineEnvironment)
	assert.Equal(t, []string{
		"quickstart/assets/quickstart/players.asset.yml",
		"quickstart/assets/quickstart/games.asset.yml",
		"quickstart/assets/quickstart/game_results.sql",
		"quickstart/assets/quickstart/player_performance.sql",
		"quickstart/assets/quickstart/opening_repertoire.sql",
	}, result.AssetPaths)

	configContents, err := os.ReadFile(filepath.Join(workspaceRoot, ".bruin.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(configContents), "duckdb-files/chess_playground.duckdb")
	assert.NotContains(t, string(configContents), "chess-default")

	pipelineContents, err := os.ReadFile(filepath.Join(workspaceRoot, "quickstart", "pipeline.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(pipelineContents), "concurrency: 1")
	assert.NotContains(t, string(pipelineContents), "schedule:")

	playersAsset, err := os.ReadFile(filepath.Join(workspaceRoot, "quickstart", "assets", "quickstart", "players.asset.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(playersAsset), "\nname:")
	assert.Contains(t, string(playersAsset), "type: api")
	assert.Contains(t, string(playersAsset), "parameters:")
	assert.Contains(t, string(playersAsset), "https://api.chess.com/pub/player/{{ username }}")
	assert.NotContains(t, string(playersAsset), "destination:")
	assert.NotContains(t, string(playersAsset), "object: quickstart.players")
	assert.NotContains(t, string(playersAsset), "mode: full-refresh")
	assert.Contains(t, string(playersAsset), "MagnusCarlsen")
	assert.Contains(t, string(playersAsset), "AlexandraBotez")

	gamesAsset, err := os.ReadFile(filepath.Join(workspaceRoot, "quickstart", "assets", "quickstart", "games.asset.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(gamesAsset), "\nname:")
	assert.Contains(t, string(gamesAsset), "type: api")
	assert.Contains(t, string(gamesAsset), "parameters:")
	assert.Contains(t, string(gamesAsset), "https://api.chess.com/pub/player/{{ username }}/games/")
	assert.NotContains(t, string(gamesAsset), "destination:")
	assert.NotContains(t, string(gamesAsset), "object: quickstart.games")
	assert.NotContains(t, string(gamesAsset), "mode: full-refresh")
	assert.Contains(t, string(gamesAsset), "MagnusCarlsen")
	assert.Contains(t, string(gamesAsset), "time_class: time_class")
	assert.Contains(t, string(gamesAsset), "white_accuracy: accuracies.white")
	assert.Contains(t, string(gamesAsset), "eco: eco")

	gameResultsAsset, err := os.ReadFile(filepath.Join(workspaceRoot, "quickstart", "assets", "quickstart", "game_results.sql"))
	require.NoError(t, err)
	assert.Contains(t, string(gameResultsAsset), "name: quickstart.game_results")
	assert.Contains(t, string(gameResultsAsset), "- quickstart.players")
	assert.Contains(t, string(gameResultsAsset), "- quickstart.games")
	assert.Contains(t, string(gameResultsAsset), "PARTITION BY url")
	assert.Contains(t, string(gameResultsAsset), "AS outcome")
	assert.Contains(t, string(gameResultsAsset), "AS eco_code")

	performanceAsset, err := os.ReadFile(filepath.Join(workspaceRoot, "quickstart", "assets", "quickstart", "player_performance.sql"))
	require.NoError(t, err)
	assert.Contains(t, string(performanceAsset), "name: quickstart.player_performance")
	assert.Contains(t, string(performanceAsset), "web_view: chart")
	assert.Contains(t, string(performanceAsset), "score_percent")

	repertoireAsset, err := os.ReadFile(filepath.Join(workspaceRoot, "quickstart", "assets", "quickstart", "opening_repertoire.sql"))
	require.NoError(t, err)
	assert.Contains(t, string(repertoireAsset), "name: quickstart.opening_repertoire")
	assert.Contains(t, string(repertoireAsset), "repertoire_rank <= 5")
	assert.NoFileExists(t, filepath.Join(workspaceRoot, "quickstart", "assets", "quickstart", "my_python_asset.py"))

	_, err = os.Stat(filepath.Join(workspaceRoot, "duckdb-files"))
	require.NoError(t, err)
}

func TestCreateDuckDBQuickstartRemovesStaleEmptyDuckDBPlaceholder(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, "duckdb-files"), 0o755))
	databasePath := filepath.Join(workspaceRoot, "duckdb-files", "chess_playground.duckdb")
	require.NoError(t, os.WriteFile(databasePath, nil, 0o644))

	runner := &stubRunRunner{output: []byte("quickstart run complete")}
	materializer := &stubRunMaterializer{result: MaterializeResult{Status: "ok", Output: "quickstart run complete"}}
	svc := NewOnboardingService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"), runner, materializer)

	result := svc.CreateDuckDBQuickstart(context.Background(), OnboardingQuickstartRequest{
		EnvironmentName: "default",
		PipelineName:    "quickstart",
		ConnectionName:  "duckdb-default",
		DatabasePath:    "duckdb-files/chess_playground.duckdb",
		Materialize:     true,
	})

	require.Equal(t, "ok", result.Status)
	_, err := os.Stat(databasePath)
	require.True(t, os.IsNotExist(err), "empty placeholder should be removed so DuckDB can create a valid database")
}

func TestCreateDuckDBQuickstartPreparesDuckDBPathRelativeToConfigFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	workspaceRoot := filepath.Join(repoRoot, "onboarding")
	require.NoError(t, os.MkdirAll(workspaceRoot, 0o755))

	runner := &stubRunRunner{output: []byte("quickstart run complete")}
	materializer := &stubRunMaterializer{result: MaterializeResult{Status: "ok", Output: "quickstart run complete"}}
	svc := NewOnboardingService(workspaceRoot, filepath.Join(repoRoot, ".bruin.yml"), runner, materializer)

	result := svc.CreateDuckDBQuickstart(context.Background(), OnboardingQuickstartRequest{
		EnvironmentName: "default",
		PipelineName:    "quickstart",
		ConnectionName:  "duckdb-default",
		DatabasePath:    "duckdb-files/chess_playground.duckdb",
		Materialize:     true,
	})

	require.Equal(t, "ok", result.Status)
	_, err := os.Stat(filepath.Join(repoRoot, "duckdb-files"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(workspaceRoot, "duckdb-files"))
	require.True(t, os.IsNotExist(err))
}
