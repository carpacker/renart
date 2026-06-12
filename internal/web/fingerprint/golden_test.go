package fingerprint_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/fingerprint"
	"renart/internal/web/service"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the fingerprint golden file")

// TestFixtureProjectGoldenFingerprints is the cross-cutting stability
// guard: it fingerprints the committed fixture project and compares
// against committed goldens on every build. Fingerprint stability is the
// load-bearing wall — any nondeterminism (map iteration order,
// locale-dependent formatting, parser drift) corrupts everything above it.
//
// If this fails because the algorithm intentionally changed, bump
// fingerprint.Version and regenerate with:
//
//	go test ./internal/web/fingerprint/ -run TestFixtureProjectGolden -update-golden
func TestFixtureProjectGoldenFingerprints(t *testing.T) {
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "fixture"))
	require.NoError(t, err)

	builder := service.NewDefaultPipelineBuilder()
	parsed, err := builder.CreatePipelineFromPath(context.Background(), fixtureDir, pipeline.WithMutate())
	require.NoError(t, err)

	engine := fingerprint.NewEngine()
	vars := fingerprint.EffectiveVars(parsed, nil)
	results, err := engine.DAG(parsed, vars)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	type goldenEntry struct {
		FP           string   `json:"fp"`
		OwnContent   string   `json:"own_content"`
		ConsumedVars []string `json:"consumed_vars"`
	}
	actual := make(map[string]goldenEntry, len(results))
	for assetID, result := range results {
		consumed := result.ConsumedVars
		sort.Strings(consumed)
		actual[assetID] = goldenEntry{
			FP:           string(result.FP),
			OwnContent:   string(result.OwnContent),
			ConsumedVars: consumed,
		}
	}

	goldenPath := filepath.Join("testdata", "fixture-golden.json")
	if *updateGolden {
		encoded, err := json.MarshalIndent(actual, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(goldenPath, append(encoded, '\n'), 0o644))
		t.Logf("golden file rewritten: %s", goldenPath)
		return
	}

	goldenBytes, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file missing; run with -update-golden to create it")
	var expected map[string]goldenEntry
	require.NoError(t, json.Unmarshal(goldenBytes, &expected))

	assert.Equal(t, expected, actual,
		"fingerprints diverged from the committed goldens; if the algorithm changed intentionally, bump fingerprint.Version and regenerate with -update-golden")
}
