package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	ExecutionTargetFidelityExact       = "exact"
	ExecutionTargetFidelityRuntimeOnly = "runtime_only"
)

var (
	ErrInvalidExecutionTargetSnapshot  = errors.New("invalid execution target snapshot")
	ErrExecutionTargetSnapshotConflict = errors.New("execution target snapshot is already persisted with different evidence")
)

func validateExecutionTargetSnapshot(snapshot ExecutionTargetSnapshot) error {
	if snapshot.Version != ExecutionTargetSnapshotVersionV1 && snapshot.Version != ExecutionTargetSnapshotVersionV2 {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidExecutionTargetSnapshot, snapshot.Version)
	}
	if snapshot.Version >= ExecutionTargetSnapshotVersionV2 {
		pipelineUUID := strings.TrimSpace(snapshot.PipelineUUID)
		if pipelineUUID == "" || pipelineUUID != snapshot.PipelineUUID {
			return fmt.Errorf("%w: version %d requires a canonical pipeline_uuid", ErrInvalidExecutionTargetSnapshot, snapshot.Version)
		}
	}
	configurationDigest := strings.TrimSpace(snapshot.ConfigurationDigest)
	configurationFidelity := strings.TrimSpace(snapshot.ConfigurationFidelity)
	if configurationDigest != snapshot.ConfigurationDigest || configurationFidelity != snapshot.ConfigurationFidelity {
		return fmt.Errorf("%w: configuration identity must be canonical", ErrInvalidExecutionTargetSnapshot)
	}
	if configurationDigest != "" || configurationFidelity != "" {
		switch configurationFidelity {
		case ExecutionTargetFidelityExact:
			if err := validateRunIdentityDigest("configuration_digest", configurationDigest); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidExecutionTargetSnapshot, err)
			}
		case ExecutionTargetFidelityRuntimeOnly:
			if configurationDigest != "" {
				return fmt.Errorf("%w: runtime-only configuration cannot claim a digest", ErrInvalidExecutionTargetSnapshot)
			}
		default:
			return fmt.Errorf("%w: unsupported configuration_fidelity %q", ErrInvalidExecutionTargetSnapshot, configurationFidelity)
		}
	}
	if len(snapshot.Entries) == 0 {
		return fmt.Errorf("%w: at least one asset entry is required", ErrInvalidExecutionTargetSnapshot)
	}
	assetIDs := make(map[string]string, len(snapshot.Entries))
	for assetName, entry := range snapshot.Entries {
		canonicalName := strings.TrimSpace(assetName)
		if canonicalName == "" || canonicalName != assetName {
			return fmt.Errorf("%w: entry key %q must be a non-empty canonical asset name", ErrInvalidExecutionTargetSnapshot, assetName)
		}
		assetID := strings.TrimSpace(entry.AssetID)
		if assetID == "" || assetID != entry.AssetID {
			return fmt.Errorf("%w: entry %q requires a canonical asset_id", ErrInvalidExecutionTargetSnapshot, assetName)
		}
		if previousName, exists := assetIDs[assetID]; exists {
			return fmt.Errorf(
				"%w: entries %q and %q share asset_id %q",
				ErrInvalidExecutionTargetSnapshot,
				previousName,
				assetName,
				assetID,
			)
		}
		assetIDs[assetID] = assetName

		targetIdentity := strings.TrimSpace(entry.TargetIdentity)
		if targetIdentity != entry.TargetIdentity {
			return fmt.Errorf("%w: entry %q has a non-canonical target_identity", ErrInvalidExecutionTargetSnapshot, assetName)
		}
		switch entry.TargetFidelity {
		case ExecutionTargetFidelityExact:
			// An exact empty identity represents an asset with no mutable target,
			// such as a sensor. Writers carry their non-empty physical identity.
		case ExecutionTargetFidelityRuntimeOnly:
			if targetIdentity != "" {
				return fmt.Errorf(
					"%w: runtime-only entry %q cannot claim a target_identity",
					ErrInvalidExecutionTargetSnapshot,
					assetName,
				)
			}
		default:
			return fmt.Errorf(
				"%w: entry %q has unsupported target_fidelity %q",
				ErrInvalidExecutionTargetSnapshot,
				assetName,
				entry.TargetFidelity,
			)
		}
		if entry.TargetWriteEvidenceRequired && (entry.TargetFidelity != ExecutionTargetFidelityExact || targetIdentity == "") {
			return fmt.Errorf(
				"%w: entry %q can require target-write evidence only for an exact non-empty target",
				ErrInvalidExecutionTargetSnapshot,
				assetName,
			)
		}
		for field, value := range map[string]string{
			"fingerprint":        entry.Fingerprint,
			"own_content":        entry.OwnContent,
			"consumed_vars_hash": entry.ConsumedVarsHash,
			"vars_hash":          entry.VarsHash,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%w: entry %q requires %s", ErrInvalidExecutionTargetSnapshot, assetName, field)
			}
		}
		if snapshot.Version >= ExecutionTargetSnapshotVersionV2 {
			switch entry.CoverageMode {
			case "marker", "union_intervals", "replace_interval":
			default:
				return fmt.Errorf(
					"%w: entry %q has unsupported coverage_mode %q",
					ErrInvalidExecutionTargetSnapshot,
					assetName,
					entry.CoverageMode,
				)
			}
			for index, upstream := range entry.Upstreams {
				if strings.TrimSpace(upstream.Type) != upstream.Type {
					return fmt.Errorf("%w: entry %q upstream %d has a non-canonical type", ErrInvalidExecutionTargetSnapshot, assetName, index)
				}
				if strings.TrimSpace(upstream.Value) == "" || strings.TrimSpace(upstream.Value) != upstream.Value {
					return fmt.Errorf("%w: entry %q upstream %d requires a canonical value", ErrInvalidExecutionTargetSnapshot, assetName, index)
				}
			}
		}
	}
	return nil
}

func marshalExecutionTargetSnapshot(snapshot ExecutionTargetSnapshot) ([]byte, error) {
	if err := validateExecutionTargetSnapshot(snapshot); err != nil {
		return nil, err
	}
	return json.Marshal(snapshot)
}

func unmarshalExecutionTargetSnapshot(body string) (*ExecutionTargetSnapshot, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	var snapshot ExecutionTargetSnapshot
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrInvalidExecutionTargetSnapshot, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("%w: trailing content: %v", ErrInvalidExecutionTargetSnapshot, err)
	}
	if err := validateExecutionTargetSnapshot(snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// SetRunExecutionTargetSnapshot atomically captures immutable target and
// fingerprint evidence for an admitted run. Exact retries are accepted, while
// a second, different snapshot cannot rewrite recovery provenance.
func (s *Store) SetRunExecutionTargetSnapshot(ctx context.Context, runID string, snapshot ExecutionTargetSnapshot) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return errors.New("run id is required")
	}
	body, err := marshalExecutionTargetSnapshot(snapshot)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var status, existing string
	err = tx.QueryRowContext(ctx, `
		SELECT status, execution_target_snapshot
		FROM pipeline_runs
		WHERE id = ?`, runID).Scan(&status, &existing)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("pipeline run %s was not found", runID)
	}
	if err != nil {
		return err
	}
	if status != string(RunStatusQueued) && status != string(RunStatusRunning) {
		return fmt.Errorf("pipeline run %s is already terminal", runID)
	}
	if existing != "" {
		persisted, err := unmarshalExecutionTargetSnapshot(existing)
		if err != nil {
			return fmt.Errorf("load execution target snapshot for run %s: %w", runID, err)
		}
		persistedBody, err := marshalExecutionTargetSnapshot(*persisted)
		if err != nil {
			return fmt.Errorf("load execution target snapshot for run %s: %w", runID, err)
		}
		if string(persistedBody) == string(body) {
			return tx.Commit()
		}
		return fmt.Errorf("%w for run %s", ErrExecutionTargetSnapshotConflict, runID)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE pipeline_runs
		SET execution_target_snapshot = ?
		WHERE id = ?
		  AND status IN (?, ?)
		  AND execution_target_snapshot = ''
		  AND NOT EXISTS (
			SELECT 1 FROM pipeline_run_steps WHERE run_id = pipeline_runs.id
		  )`,
		string(body), runID, string(RunStatusQueued), string(RunStatusRunning),
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		var stepStarted bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM pipeline_run_steps WHERE run_id = ?)`, runID).Scan(&stepStarted); err != nil {
			return err
		}
		if stepStarted {
			return fmt.Errorf("cannot capture execution target snapshot for run %s after the first step started", runID)
		}
		return fmt.Errorf("capture execution target snapshot for run %s: concurrent run state change", runID)
	}
	return tx.Commit()
}
