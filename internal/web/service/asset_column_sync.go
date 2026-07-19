package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"

	webmodel "renart/internal/web/model"
	"renart/internal/web/service/assetmeta"
)

const (
	columnSyncStatusApplied   = "applied"
	columnSyncStatusUnchanged = "unchanged"
	columnSyncStatusConflicts = "conflicts"
)

type columnSchemaAnalysis struct {
	rows             []webmodel.ColumnSchemaMergeRow
	managedColumns   []WorkspaceColumn
	candidateColumns []WorkspaceColumn
	hasChanges       bool
	hasConflicts     bool
}

// SyncAssetColumns automatically observes the asset definition plus any
// explicitly selected advisory sources. Additions and unknown-to-known type
// refinements are safe and are reconciled immediately. Any deletion, known
// type change, or disagreement between sources is returned for resolution
// without mutating the asset.
func (s *AssetService) SyncAssetColumns(
	ctx context.Context,
	assetID string,
	additionalSourceIDs []string,
	environment string,
) (webmodel.ColumnSchemaSyncResult, *APIError) {
	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return webmodel.ColumnSchemaSyncResult{}, badRequestError("asset_resolve_failed", err.Error())
	}

	capabilities := columnInferenceSourcesForPipelineAsset(asset, parsedPipeline)
	selected := make(map[string]struct{}, len(additionalSourceIDs))
	for _, sourceID := range additionalSourceIDs {
		if sourceID = strings.TrimSpace(sourceID); sourceID != "" {
			selected[sourceID] = struct{}{}
		}
	}

	availableObserved := make(map[string]webmodel.ColumnInferenceSource)
	for _, source := range capabilities {
		if source.Category == "observed" {
			availableObserved[source.ID] = source
		}
	}
	for sourceID := range selected {
		if _, ok := availableObserved[sourceID]; !ok {
			return webmodel.ColumnSchemaSyncResult{}, badRequestError(
				"unsupported_column_source",
				fmt.Sprintf("advisory schema source %q is not available for this asset", sourceID),
			)
		}
	}

	snapshots := make([]webmodel.ColumnSchemaSourceSnapshot, 0, len(capabilities))
	notes := make([]string, 0)
	for _, source := range capabilities {
		_, explicitlySelected := selected[source.ID]
		if source.Category != "definition" && !explicitlySelected {
			continue
		}
		columns, sourceNotes, sampleRecords, apiErr := s.observeAssetColumnSource(
			ctx,
			assetID,
			parsedPipeline,
			asset,
			source,
			environment,
		)
		if apiErr != nil {
			// API assets may intentionally omit a declarative response schema. A
			// selected live request is then the best available primary observation.
			_, liveSelected := selected[columnSourceLiveResponse]
			if source.Category == "definition" && isAPIAsset(asset) && liveSelected && apiErr.Code == "column_inference_failed" {
				notes = append(notes, "No response schema is declared; using the live request as the primary inference source.")
				continue
			}
			return webmodel.ColumnSchemaSyncResult{}, apiErr
		}
		if columns == nil {
			columns = []WorkspaceColumn{}
		}
		snapshots = append(snapshots, webmodel.ColumnSchemaSourceSnapshot{
			Source:        source,
			Columns:       columns,
			Notes:         sourceNotes,
			SampleRecords: sampleRecords,
		})
	}
	if len(snapshots) == 0 {
		return webmodel.ColumnSchemaSyncResult{}, badRequestError(
			"column_source_required",
			"select at least one available schema source",
		)
	}

	analysis := analyzeColumnSchema(asset.Columns, assetmeta.Parse(asset.Meta), snapshots)
	result := webmodel.ColumnSchemaSyncResult{
		Sources:          snapshots,
		Rows:             analysis.rows,
		ManagedColumns:   analysis.managedColumns,
		CandidateColumns: analysis.candidateColumns,
		Columns:          PipelineColumnsToModelColumns(asset.Columns),
		Notes:            notes,
	}
	if analysis.hasConflicts {
		result.Status = columnSyncStatusConflicts
		return result, nil
	}
	if !analysis.hasChanges {
		result.Status = columnSyncStatusUnchanged
		return result, nil
	}

	reconciled, apiErr := s.ReconcileAssetColumns(ctx, assetID, analysis.managedColumns)
	if apiErr != nil {
		return webmodel.ColumnSchemaSyncResult{}, apiErr
	}
	result.Status = columnSyncStatusApplied
	result.Columns = reconciled.Columns
	return result, nil
}

// ApplyAssetColumnSchemaResolution applies the user's merge choices in one
// provenance-aware write. Keeping the saved type takes ownership of that field;
// choosing the definition releases ownership; removing an inferred candidate
// records a durable drop marker.
func (s *AssetService) ApplyAssetColumnSchemaResolution(
	ctx context.Context,
	assetID string,
	managedColumns []WorkspaceColumn,
	candidateColumns []WorkspaceColumn,
	expectedCurrent []WorkspaceColumn,
	resolutions []webmodel.ColumnSchemaResolution,
) (ColumnReconcileResult, *APIError) {
	candidates := ModelColumnsToPipelineColumns(candidateColumns)
	candidateByName := make(map[string]pipeline.Column, len(candidates))
	for _, candidate := range candidates {
		if key := columnNameKey(candidate.Name); key != "" {
			candidateByName[key] = candidate
		}
	}
	managedNames := make(map[string]struct{}, len(managedColumns))
	for _, managed := range managedColumns {
		if key := columnNameKey(managed.Name); key != "" {
			managedNames[key] = struct{}{}
		}
	}

	return s.reconcileAssetColumns(ctx, assetID, func(asset *pipeline.Asset, meta *assetmeta.RenartMeta) ([]pipeline.Column, *APIError) {
		if !sameColumnSchema(asset.Columns, expectedCurrent) {
			return nil, newAPIError(
				http.StatusConflict,
				"schema_sync_stale",
				"the saved schema changed while the resolver was open; sync again before applying",
			)
		}
		inferred := ModelColumnsToPipelineColumns(managedColumns)
		currentByName := make(map[string]pipeline.Column, len(asset.Columns))
		for _, column := range asset.Columns {
			if key := columnNameKey(column.Name); key != "" {
				currentByName[key] = column
			}
		}

		// Keep an existing ignore durable when its advisory source was selected.
		for _, droppedName := range meta.ColDrop {
			if candidate, ok := candidateByName[columnNameKey(droppedName)]; ok {
				inferred = upsertColumn(inferred, candidate)
			}
		}

		seen := make(map[string]struct{}, len(resolutions))
		for _, resolution := range resolutions {
			key := columnNameKey(resolution.Column)
			if key == "" {
				return nil, badRequestError("invalid_schema_resolution", "resolution column is required")
			}
			if _, duplicate := seen[key]; duplicate {
				return nil, badRequestError("invalid_schema_resolution", fmt.Sprintf("column %q has more than one resolution", resolution.Column))
			}
			seen[key] = struct{}{}
			candidate, isCandidate := candidateByName[key]
			_, isManagedCandidate := managedNames[key]

			switch strings.TrimSpace(resolution.Action) {
			case "remove":
				if isCandidate {
					inferred = upsertColumn(inferred, candidate)
					meta.ColDrop = appendName(meta.ColDrop, candidate.Name)
					meta.ColAdd = removeName(meta.ColAdd, candidate.Name)
					meta.ColOwn = disownField(meta.ColOwn, candidate.Name, "type")
				} else {
					asset.Columns = removeColumnByName(asset.Columns, resolution.Column)
					meta.ColAdd = removeName(meta.ColAdd, resolution.Column)
					meta.ColDrop = removeName(meta.ColDrop, resolution.Column)
					meta.ColOwn = disownField(meta.ColOwn, resolution.Column, "type")
				}

			case "use":
				source := strings.TrimSpace(resolution.Source)
				if source == "current" {
					currentColumn, ok := currentByName[key]
					if !ok {
						return nil, badRequestError("invalid_schema_resolution", fmt.Sprintf("saved column %q is not available", resolution.Column))
					}
					if isCandidate && isManagedCandidate {
						candidate.Type = currentColumn.Type
						inferred = upsertColumn(inferred, candidate)
						meta.ColAdd = removeName(meta.ColAdd, candidate.Name)
						meta.ColDrop = removeName(meta.ColDrop, candidate.Name)
						meta.ColOwn = ownField(meta.ColOwn, candidate.Name, "type")
					} else {
						meta.ColAdd = appendName(meta.ColAdd, currentColumn.Name)
						meta.ColDrop = removeName(meta.ColDrop, currentColumn.Name)
						meta.ColOwn = ownField(meta.ColOwn, currentColumn.Name, "type")
					}
					continue
				}
				if source == "" || !isCandidate {
					return nil, badRequestError("invalid_schema_resolution", fmt.Sprintf("inferred source for column %q is not available", resolution.Column))
				}
				candidate.Type = strings.TrimSpace(resolution.Type)
				if !isManagedCandidate {
					asset.Columns = upsertColumnType(asset.Columns, candidate)
					meta.ColAdd = appendName(meta.ColAdd, candidate.Name)
					meta.ColDrop = removeName(meta.ColDrop, candidate.Name)
					meta.ColOwn = ownField(meta.ColOwn, candidate.Name, "type")
					continue
				}
				inferred = upsertColumn(inferred, candidate)
				meta.ColAdd = removeName(meta.ColAdd, candidate.Name)
				meta.ColDrop = removeName(meta.ColDrop, candidate.Name)
				if source == columnSourceDefinition {
					meta.ColOwn = disownField(meta.ColOwn, candidate.Name, "type")
				} else {
					// Ownership makes the reconciler preserve the saved value, so
					// first update that value to the explicitly selected advisory
					// type. Otherwise choosing Current table or Live request would
					// leave the previous saved type in place.
					asset.Columns = upsertColumnType(asset.Columns, candidate)
					meta.ColOwn = ownField(meta.ColOwn, candidate.Name, "type")
				}

			default:
				return nil, badRequestError("invalid_schema_resolution", fmt.Sprintf("unknown action for column %q", resolution.Column))
			}
		}
		return inferred, nil
	})
}

func upsertColumnType(columns []pipeline.Column, selected pipeline.Column) []pipeline.Column {
	for index := range columns {
		if columnNameKey(columns[index].Name) == columnNameKey(selected.Name) {
			columns[index].Type = selected.Type
			return columns
		}
	}
	return append(columns, selected)
}

func sameColumnSchema(current []pipeline.Column, expected []WorkspaceColumn) bool {
	if len(current) != len(expected) {
		return false
	}
	for index := range current {
		if columnNameKey(current[index].Name) != columnNameKey(expected[index].Name) ||
			!equivalentColumnType(current[index].Type, expected[index].Type) {
			return false
		}
	}
	return true
}

func analyzeColumnSchema(
	current []pipeline.Column,
	meta assetmeta.RenartMeta,
	snapshots []webmodel.ColumnSchemaSourceSnapshot,
) columnSchemaAnalysis {
	analysis := columnSchemaAnalysis{
		rows:             []webmodel.ColumnSchemaMergeRow{},
		managedColumns:   []WorkspaceColumn{},
		candidateColumns: []WorkspaceColumn{},
	}
	if len(snapshots) == 0 {
		return analysis
	}

	primaryIndex := 0
	for index, snapshot := range snapshots {
		if snapshot.Source.Category == "definition" {
			primaryIndex = index
			break
		}
	}

	sourceMaps := make([]map[string]WorkspaceColumn, len(snapshots))
	for index, snapshot := range snapshots {
		sourceMaps[index] = columnsByName(snapshot.Columns)
	}
	managed := append([]WorkspaceColumn(nil), snapshots[primaryIndex].Columns...)
	managedIndex := make(map[string]int, len(managed))
	for index := range managed {
		key := columnNameKey(managed[index].Name)
		if key == "" {
			continue
		}
		managedIndex[key] = index
		if strings.TrimSpace(managed[index].Type) == "" {
			if inferredType := firstKnownSourceType(key, sourceMaps); inferredType != "" {
				managed[index].Type = inferredType
			}
		}
	}
	primaryOmitted := make(map[string]struct{})
	if snapshots[primaryIndex].Source.MayOmitColumns {
		// A sampled response is useful evidence for fields it contains, but
		// cannot prove that an existing optional field was removed. When it is
		// the fallback primary source, retain unobserved saved columns in the
		// managed projection. A complete advisory source may still refine their
		// types or surface real drift below.
		for _, currentColumn := range current {
			key := columnNameKey(currentColumn.Name)
			if key == "" {
				continue
			}
			if _, present := managedIndex[key]; present {
				continue
			}
			columnType := firstKnownSourceType(key, sourceMaps)
			if columnType == "" {
				columnType = currentColumn.Type
			}
			managedIndex[key] = len(managed)
			managed = append(managed, WorkspaceColumn{Name: currentColumn.Name, Type: columnType})
			primaryOmitted[key] = struct{}{}
		}
	}

	currentMap := make(map[string]pipeline.Column, len(current))
	for _, column := range current {
		if key := columnNameKey(column.Name); key != "" {
			currentMap[key] = column
		}
	}
	dropped := stringSet(meta.ColDrop)
	manual := stringSet(meta.ColAdd)

	rowOrder := make([]string, 0, len(managed)+len(current))
	rowNames := make(map[string]string)
	appendOrder := func(name string) {
		key := columnNameKey(name)
		if key == "" {
			return
		}
		if _, exists := rowNames[key]; exists {
			return
		}
		rowNames[key] = strings.TrimSpace(name)
		rowOrder = append(rowOrder, key)
	}
	for _, column := range snapshots[primaryIndex].Columns {
		appendOrder(column.Name)
	}
	for _, column := range current {
		appendOrder(column.Name)
	}
	for _, snapshot := range snapshots {
		for _, column := range snapshot.Columns {
			appendOrder(column.Name)
		}
	}

	for _, key := range rowOrder {
		currentColumn, currentPresent := currentMap[key]
		managedPosition, proposedPresent := managedIndex[key]
		var proposedColumn WorkspaceColumn
		if proposedPresent {
			proposedColumn = managed[managedPosition]
		}
		_, ignored := dropped[key]
		_, manuallyAdded := manual[key]
		ownedType := columnTypeOwned(meta, key)
		_, omittedByPartialPrimary := primaryOmitted[key]

		anySourcePresent := false
		for _, sourceMap := range sourceMaps {
			if _, ok := sourceMap[key]; ok {
				anySourcePresent = true
				break
			}
		}
		observedOnly := !proposedPresent && anySourcePresent
		if ignored && !currentPresent {
			continue
		}

		sourceTypeConflict := sourceTypesConflict(key, sourceMaps)
		if sourceTypeConflict && ownedType && currentPresent && currentMatchesAnySource(currentColumn.Type, key, sourceMaps) {
			sourceTypeConflict = false
		}
		sourceMissing := false
		if proposedPresent {
			for index, snapshot := range snapshots {
				if index == primaryIndex || snapshot.Source.MayOmitColumns {
					continue
				}
				if _, ok := sourceMaps[index][key]; !ok {
					sourceMissing = true
					break
				}
			}
		}

		row := webmodel.ColumnSchemaMergeRow{
			Column:          rowNames[key],
			CurrentPresent:  currentPresent,
			CurrentType:     currentColumn.Type,
			ProposedPresent: proposedPresent,
			ProposedType:    proposedColumn.Type,
		}

		switch {
		case observedOnly && manuallyAdded && currentPresent:
			row.Kind = "manual"
			row.Detail = "Kept as an explicit metadata column."
			row.ProposedPresent = true
			row.ProposedType = currentColumn.Type
		case observedOnly:
			row.Kind = "observed_only"
			row.Detail = "An advisory source reports a column that the primary inference does not declare."
			row.Conflict = true
		case sourceMissing:
			row.Kind = "source_missing"
			row.Detail = "An advisory source does not report this schema column."
			row.Conflict = true
		case sourceTypeConflict:
			row.Kind = "source_conflict"
			row.Detail = "The selected schema sources report different known types."
			row.Conflict = true
		case proposedPresent && !currentPresent:
			row.Kind = "added"
			row.Detail = "New inferred column; safe to add automatically."
			analysis.hasChanges = true
		case proposedPresent && currentPresent && ownedType:
			row.Kind = "owned"
			row.Detail = "The saved type is explicitly owned and remains unchanged."
			row.ProposedType = currentColumn.Type
		case proposedPresent && currentPresent && omittedByPartialPrimary && equivalentColumnType(currentColumn.Type, proposedColumn.Type):
			row.Kind = "partial_unobserved"
			row.Detail = "The sampled source did not include this saved column, so it was retained."
		case proposedPresent && currentPresent && equivalentColumnType(currentColumn.Type, proposedColumn.Type):
			row.Kind = "unchanged"
			row.Detail = "The inferred and saved types match."
			// Avoid cosmetic rewrites between equivalent aliases such as int32
			// and integer.
			managed[managedPosition].Type = currentColumn.Type
			row.ProposedType = currentColumn.Type
		case proposedPresent && currentPresent && strings.TrimSpace(currentColumn.Type) == "" && strings.TrimSpace(proposedColumn.Type) != "":
			row.Kind = "type_filled"
			row.Detail = "The previously unknown type can be filled automatically."
			analysis.hasChanges = true
		case proposedPresent && currentPresent:
			row.Kind = "type_conflict"
			row.Detail = "Changing a known type requires an explicit choice."
			row.Conflict = true
		case currentPresent && manuallyAdded:
			row.Kind = "manual"
			row.Detail = "Kept as an explicit metadata column."
			row.ProposedPresent = true
			row.ProposedType = currentColumn.Type
		case currentPresent:
			row.Kind = "removed"
			row.Detail = "The primary inference no longer reports this saved column."
			row.Conflict = true
		default:
			row.Kind = "unchanged"
			row.Detail = "No schema change."
		}

		if row.Conflict {
			analysis.hasConflicts = true
		}
		analysis.rows = append(analysis.rows, row)
	}

	// Definition/fallback columns are the provenance-managed schema. Advisory-
	// only columns are added only after an explicit resolution. Previously
	// ignored advisory columns stay managed while selected so their drop marker
	// is not pruned by a safe reconciliation.
	analysis.managedColumns = append(analysis.managedColumns, managed...)
	for _, snapshot := range snapshots {
		for _, column := range snapshot.Columns {
			key := columnNameKey(column.Name)
			if _, exists := managedIndex[key]; exists {
				continue
			}
			if _, isDropped := dropped[key]; isDropped {
				analysis.managedColumns = append(analysis.managedColumns, column)
				managedIndex[key] = len(analysis.managedColumns) - 1
			}
		}
	}

	candidateSeen := make(map[string]struct{})
	for _, column := range analysis.managedColumns {
		key := columnNameKey(column.Name)
		if key == "" {
			continue
		}
		candidateSeen[key] = struct{}{}
		analysis.candidateColumns = append(analysis.candidateColumns, column)
	}
	for _, snapshot := range snapshots {
		for _, column := range snapshot.Columns {
			key := columnNameKey(column.Name)
			if key == "" {
				continue
			}
			if _, exists := candidateSeen[key]; exists {
				continue
			}
			candidateSeen[key] = struct{}{}
			analysis.candidateColumns = append(analysis.candidateColumns, column)
		}
	}
	return analysis
}

func columnsByName(columns []WorkspaceColumn) map[string]WorkspaceColumn {
	result := make(map[string]WorkspaceColumn, len(columns))
	for _, column := range columns {
		if key := columnNameKey(column.Name); key != "" {
			result[key] = column
		}
	}
	return result
}

func firstKnownSourceType(key string, sourceMaps []map[string]WorkspaceColumn) string {
	for _, sourceMap := range sourceMaps {
		if column, ok := sourceMap[key]; ok && strings.TrimSpace(column.Type) != "" {
			return column.Type
		}
	}
	return ""
}

func sourceTypesConflict(key string, sourceMaps []map[string]WorkspaceColumn) bool {
	known := make(map[string]struct{})
	for _, sourceMap := range sourceMaps {
		column, ok := sourceMap[key]
		if !ok || strings.TrimSpace(column.Type) == "" {
			continue
		}
		known[canonicalColumnType(column.Type)] = struct{}{}
	}
	return len(known) > 1
}

func currentMatchesAnySource(currentType, key string, sourceMaps []map[string]WorkspaceColumn) bool {
	if strings.TrimSpace(currentType) == "" {
		return false
	}
	for _, sourceMap := range sourceMaps {
		if column, ok := sourceMap[key]; ok && strings.TrimSpace(column.Type) != "" && equivalentColumnType(currentType, column.Type) {
			return true
		}
	}
	return false
}

func columnTypeOwned(meta assetmeta.RenartMeta, key string) bool {
	for column, fields := range meta.ColOwn {
		if columnNameKey(column) != key {
			continue
		}
		for _, field := range fields {
			if strings.EqualFold(strings.TrimSpace(field), "type") {
				return true
			}
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if key := columnNameKey(value); key != "" {
			result[key] = struct{}{}
		}
	}
	return result
}

func columnNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
