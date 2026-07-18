package service

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
)

const (
	orderedSQLProvenancePreHook  = "pre_hook"
	orderedSQLProvenanceMain     = "execution_sql"
	orderedSQLProvenancePostHook = "post_hook"
)

// renderExactQueryBatchExecutionStages renders the exact ordered statement
// sequence submitted by Databricks, ClickHouse, and Synapse direct operators.
// Each materializer slice element remains one stage because those elements are
// the warehouse's batch boundary; this function never splits on semicolons.
func renderExactQueryBatchExecutionStages(asset *pipeline.Asset, extractor query.QueryExtractor, compiledQuery string, fullRefresh bool) ([]AssetRenderStage, bool, error) {
	if asset == nil {
		return nil, false, nil
	}
	materializer, supported, err := newDirectQueryBatchExecutionMaterializer(asset.Type, fullRefresh)
	if err != nil || !supported {
		return nil, supported, err
	}
	return renderExactQueryBatchExecutionStagesWithMaterializer(asset, extractor, compiledQuery, materializer)
}

func renderExactQueryBatchExecutionStagesWithMaterializer(asset *pipeline.Asset, extractor query.QueryExtractor, compiledQuery string, materializer *directQueryBatchExecutionMaterializer) ([]AssetRenderStage, bool, error) {
	if asset == nil || materializer == nil {
		return nil, true, fmt.Errorf("ordered execution rendering requires an asset and materializer")
	}
	rendered, provenance, provenanceSafe, err := materializer.renderWithOrigin(asset, compiledQuery)
	if err != nil {
		return nil, true, err
	}

	// This is the same post-materialization extraction used by all three Bruin
	// direct operators. It resolves the interval Jinja introduced by their
	// time_interval materializers after hooks and DECLARE ordering are final.
	if asset.Materialization.Strategy == pipeline.MaterializationStrategyTimeInterval {
		if extractor == nil {
			return nil, true, fmt.Errorf("time_interval execution rendering requires a query extractor")
		}
		rendered, err = extractor.ReextractQueriesFromSlice(rendered)
		if err != nil {
			return nil, true, fmt.Errorf("re-render time_interval execution SQL: %w", err)
		}
		if len(rendered) != len(provenance) {
			provenanceSafe = false
		}
	}
	if len(rendered) == 0 {
		return nil, true, fmt.Errorf("ordered execution SQL rendered empty")
	}

	stages := make([]AssetRenderStage, 0, len(rendered))
	ordinals := map[string]int{}
	for index, statement := range rendered {
		kind := "execution_sql"
		if provenanceSafe && index < len(provenance) {
			kind = provenance[index]
		}
		ordinals[kind]++
		stages = append(stages, AssetRenderStage{
			Kind:     kind,
			Label:    orderedSQLStageLabel(kind, ordinals[kind]),
			Language: "sql",
			Content:  statement,
			Status:   AssetRenderStageStatusOK,
			Fidelity: AssetRenderFidelityExact,
		})
	}
	return stages, true, nil
}

func (m *directQueryBatchExecutionMaterializer) renderWithOrigin(asset *pipeline.Asset, compiledQuery string) (rendered []string, provenance []string, provenanceSafe bool, err error) {
	if m == nil || m.materializer == nil {
		return nil, nil, false, fmt.Errorf("direct query batch materializer is not configured")
	}

	capture := &capturingQueryBatchMaterializer{materializer: m.materializer}
	rendered, err = (pipeline.HookWrapperMaterializerList{
		Mat:     capture,
		Hoister: m.hoister,
	}).Render(asset, compiledQuery)
	if err != nil {
		return nil, nil, false, err
	}

	// Build the origin through Bruin's hook wrapper too, merely omitting the
	// hoister. This keeps semicolon normalization and empty-hook handling exactly
	// aligned with direct execution without rendering the base materializer a
	// second time (important for materializers that allocate temporary names).
	origin, err := (pipeline.HookWrapperMaterializerList{
		Mat: staticQueryBatchMaterializer{queries: capture.rendered},
	}).Render(asset, compiledQuery)
	if err != nil {
		return nil, nil, false, err
	}

	preCount := nonEmptyHookCount(asset.Hooks.Pre)
	mainCount := len(capture.rendered)
	postCount := nonEmptyHookCount(asset.Hooks.Post)
	provenance = make([]string, 0, preCount+mainCount+postCount)
	provenance = appendRepeated(provenance, orderedSQLProvenancePreHook, preCount)
	provenance = appendRepeated(provenance, orderedSQLProvenanceMain, mainCount)
	provenance = appendRepeated(provenance, orderedSQLProvenancePostHook, postCount)

	// A successful DECLARE hoist may move a hook or a main batch. Once that
	// happens positional attribution would be misleading, so retain the exact
	// final elements but expose all of them as generic execution SQL. Hoister
	// errors already fall back to origin inside Bruin's wrapper and remain safe
	// to attribute because the resulting sequence is byte-for-byte identical.
	provenanceSafe = len(origin) == len(provenance) && slices.Equal(rendered, origin)
	return rendered, provenance, provenanceSafe, nil
}

type capturingQueryBatchMaterializer struct {
	materializer queryBatchMaterializer
	rendered     []string
}

func (m *capturingQueryBatchMaterializer) Render(asset *pipeline.Asset, compiledQuery string) ([]string, error) {
	rendered, err := m.materializer.Render(asset, compiledQuery)
	if err != nil {
		return nil, err
	}
	m.rendered = append([]string(nil), rendered...)
	return rendered, nil
}

type staticQueryBatchMaterializer struct {
	queries []string
}

func (m staticQueryBatchMaterializer) Render(*pipeline.Asset, string) ([]string, error) {
	return append([]string(nil), m.queries...), nil
}

func nonEmptyHookCount(hooks []pipeline.Hook) int {
	count := 0
	for _, hook := range hooks {
		if strings.TrimSpace(hook.Query) != "" {
			count++
		}
	}
	return count
}

func appendRepeated(values []string, value string, count int) []string {
	for range count {
		values = append(values, value)
	}
	return values
}

func orderedSQLStageLabel(kind string, ordinal int) string {
	switch kind {
	case orderedSQLProvenancePreHook:
		return fmt.Sprintf("Pre-hook %d", ordinal)
	case orderedSQLProvenancePostHook:
		return fmt.Sprintf("Post-hook %d", ordinal)
	default:
		return fmt.Sprintf("Execution SQL %d", ordinal)
	}
}
