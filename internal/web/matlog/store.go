// Package matlog persists materialization facts and the compacted coverage
// table. Every completed run writes immutable fact rows; coverage merges
// them into one row per contiguous interval so freshness lookups stay
// O(gaps) regardless of run count.
package matlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Store wraps the shared scheduler SQLite database (the same one River
// uses). Tables are created by the scheduler store's goose migrations.
type Store struct {
	db *sql.DB
}

// ErrTargetWriterAmbiguous means two target writes could not be ordered from
// their persisted completion coordinates. The writer row is durably marked
// ambiguous before this error is returned, so freshness fails closed until a
// strictly newer completion establishes a new generation.
var ErrTargetWriterAmbiguous = errors.New("matlog: target writer completion order is ambiguous")

// ErrMaterializationReplayConflict means a scheduled run's durable fact key
// was reused with different completion evidence. Treating that as an
// idempotent replay could move or invalidate a physical writer, so callers must
// fail recovery closed rather than acknowledging it.
var ErrMaterializationReplayConflict = errors.New("matlog: materialization replay conflicts with the recorded fact")

// ErrTargetWriteClaimActive means another execution has already claimed the
// same non-empty physical target. Callers must not start a concurrent write:
// the target's eventual contents could not be ordered reliably.
var ErrTargetWriteClaimActive = errors.New("matlog: physical target already has an active write claim")

// ErrTargetWriteClaimNotFound means a terminal outcome was reported for a
// target write that was never durably claimed (or whose successful recorder
// transaction already resolved the claim).
var ErrTargetWriteClaimNotFound = errors.New("matlog: target write claim was not found")

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Materialization is one asset's outcome within a completed run.
type Materialization struct {
	AssetID     string
	Environment string
	Fingerprint string
	// OwnContent is the asset's own-definition sub-hash, persisted so the
	// staleness service can tell an asset's own edit apart from an
	// upstream-inherited change.
	OwnContent string
	VarsHash   string
	// IntervalStart/IntervalEnd are nil when the result has no physical
	// execution-window contract.
	IntervalStart *time.Time
	IntervalEnd   *time.Time
	// ReplaceCoverage makes this outcome the complete known coverage for its
	// asset/environment/fingerprint/vars variant instead of unioning it with
	// prior rows. Window-filtered full refreshes and non-replay-safe windowed
	// loaders use this rather than claiming universal or cumulative coverage.
	ReplaceCoverage bool
	RunID           string
	// TargetIdentity is the secret-free canonical identity of the physical
	// object this outcome wrote. Empty identities retain the generation-zero
	// legacy behavior and do not claim a latest physical writer.
	TargetIdentity string
	// CompletionID and CompletionOrdinal provide a stable ordering seam for
	// target writes that share a completion timestamp. CompletionID defaults to
	// RunID when available. Target-aware writes without either ID fail closed.
	CompletionID      string
	CompletionOrdinal int64
	MaterializedAt    time.Time
}

// CoverageRow is one merged interval (or full-refresh marker) from the
// coverage table.
type CoverageRow struct {
	AssetID          string
	Fingerprint      string
	OwnContent       string
	TargetIdentity   string
	TargetGeneration int64
	// IntervalStart/IntervalEnd are nil for the full-refresh "built" marker.
	IntervalStart  *time.Time
	IntervalEnd    *time.Time
	MaterializedAt time.Time
}

// LatestSuccessfulWriter identifies the successful write whose output is
// currently present at one physical target. The row is global by target:
// AssetID and Environment describe the winning writer, not the storage key.
type LatestSuccessfulWriter struct {
	TargetIdentity    string
	TargetGeneration  int64
	AssetID           string
	Environment       string
	Fingerprint       string
	VarsHash          string
	RunID             string
	MaterializedAt    time.Time
	CompletionID      string
	CompletionOrdinal int64
	Ambiguous         bool
}

// TargetWriteClaim identifies one physical write before execution begins.
// TargetIdentity is deliberately secret-free. CompletionID and AssetID bind
// the claim to the same stable coordinates later supplied to Record.
type TargetWriteClaim struct {
	TargetIdentity string
	CompletionID   string
	AssetID        string
	ClaimedAt      time.Time
}

// FullRefresh reports whether the row is a full-refresh "built" marker.
func (r CoverageRow) FullRefresh() bool {
	return r.IntervalStart == nil
}

const timeLayout = time.RFC3339Nano

func formatTime(value time.Time) string {
	return value.UTC().Format(timeLayout)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(timeLayout, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func optionalTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func optionalTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed := parseTime(value)
	return &parsed
}

// ClaimTargetWrite durably claims an exact physical target before execution.
// Empty targets are an intentional no-op for runtime-only or unresolved
// outputs. Dirty claims do not block a repair, but any active claim on the same
// target rejects the new write synchronously.
func (s *Store) ClaimTargetWrite(ctx context.Context, claim TargetWriteClaim) error {
	if claim.TargetIdentity == "" {
		return nil
	}
	if claim.CompletionID == "" || claim.AssetID == "" {
		return fmt.Errorf("matlog: completion_id and asset_id are required for a target write claim")
	}
	if claim.ClaimedAt.IsZero() {
		claim.ClaimedAt = time.Now().UTC()
	}
	at := formatTime(claim.ClaimedAt)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Reclaiming the exact same dirty completion is a legitimate repair. Delete
	// it first so the new attempt receives a new sequence and can resolve every
	// dirty claim that predates this attempt if recording succeeds.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM renart_target_write_claims
		WHERE target_identity = ? AND completion_id = ? AND asset_id = ?
		  AND state = 'dirty'`,
		claim.TargetIdentity,
		claim.CompletionID,
		claim.AssetID,
	); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO renart_target_write_claims
			(target_identity, completion_id, asset_id, state, claimed_at, updated_at)
		SELECT ?, ?, ?, 'active', ?, ?
		WHERE NOT EXISTS (
			SELECT 1
			FROM renart_target_write_claims
			WHERE target_identity = ? AND state = 'active'
		)`,
		claim.TargetIdentity,
		claim.CompletionID,
		claim.AssetID,
		at,
		at,
		claim.TargetIdentity,
	)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted != 1 {
		return fmt.Errorf("%w for %q", ErrTargetWriteClaimActive, claim.TargetIdentity)
	}
	return tx.Commit()
}

// MarkTargetWriteClaimDirty records that a claimed write failed, was
// cancelled, or was interrupted after it may have changed the physical
// target. Dirty claims do not block a repair execution, but they suppress all
// freshness reads until a later claimed success resolves them.
func (s *Store) MarkTargetWriteClaimDirty(
	ctx context.Context,
	claim TargetWriteClaim,
	at time.Time,
) error {
	if claim.TargetIdentity == "" {
		return nil
	}
	if claim.CompletionID == "" || claim.AssetID == "" {
		return fmt.Errorf("matlog: completion_id and asset_id are required for a target write claim")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE renart_target_write_claims
		SET state = 'dirty', updated_at = ?
		WHERE target_identity = ? AND completion_id = ? AND asset_id = ?`,
		formatTime(at),
		claim.TargetIdentity,
		claim.CompletionID,
		claim.AssetID,
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf(
			"%w for target %q, completion %q, asset %q",
			ErrTargetWriteClaimNotFound,
			claim.TargetIdentity,
			claim.CompletionID,
			claim.AssetID,
		)
	}
	return nil
}

// MarkActiveTargetWriteClaimsDirty converts every claim left active by a
// stopped process into durable uncertainty. It is idempotent and intended to
// run during startup before any new execution can begin.
func (s *Store) MarkActiveTargetWriteClaimsDirty(ctx context.Context, at time.Time) (int64, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE renart_target_write_claims
		SET state = 'dirty', updated_at = ?
		WHERE state = 'active'`, formatTime(at))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// HasTargetWriteEvidence reports whether an operator durably claimed a target
// before writing it, or whether that exact completion already committed its
// materialization fact. The latter keeps completion-outbox replay idempotent if
// a process stops after Store.Record clears the claim but before acknowledgement.
func (s *Store) HasTargetWriteEvidence(ctx context.Context, claim TargetWriteClaim) (bool, error) {
	if claim.TargetIdentity == "" || claim.CompletionID == "" || claim.AssetID == "" {
		return false, nil
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM renart_target_write_claims
			WHERE target_identity = ? AND completion_id = ? AND asset_id = ?
			UNION ALL
			SELECT 1
			FROM renart_materializations
			WHERE target_identity = ? AND completion_id = ? AND asset_id = ?
		)`,
		claim.TargetIdentity, claim.CompletionID, claim.AssetID,
		claim.TargetIdentity, claim.CompletionID, claim.AssetID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

// Record writes the immutable fact row, current-generation coverage, and (for
// target-aware writes) the global latest-successful-writer row inside one
// transaction. The merge unions the new interval with every existing row it
// overlaps or exactly touches, so a backfill interval bridging two existing
// rows collapses all three into one.
//
// A target starts at generation one. A newer completion from the same
// asset/environment with the same fingerprint and full variables hash reuses
// that generation; changing the writer scope or either identity advances it.
// Older completions and exact replays are no-ops, while conflicting replay
// evidence fails before writer mutation. Two independent equal-time
// completions mark the writer ambiguous and fail closed until a strictly newer
// completion establishes a new generation.
func (s *Store) Record(ctx context.Context, m Materialization) error {
	if m.AssetID == "" || m.Fingerprint == "" {
		return fmt.Errorf("matlog: asset_id and fingerprint are required")
	}
	if (m.IntervalStart == nil) != (m.IntervalEnd == nil) {
		return fmt.Errorf("matlog: interval start and end must both be set or both be nil")
	}
	if m.MaterializedAt.IsZero() {
		m.MaterializedAt = time.Now().UTC()
	}
	if m.CompletionOrdinal < 0 {
		return fmt.Errorf("matlog: completion ordinal must not be negative")
	}
	if m.TargetIdentity != "" && m.CompletionID == "" {
		m.CompletionID = m.RunID
	}
	if m.TargetIdentity != "" && m.CompletionID == "" {
		return fmt.Errorf("matlog: completion_id or run_id is required for a target-aware materialization")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if m.RunID != "" {
		replayed, err := validateRecordedMaterializationReplay(ctx, tx, m)
		if err != nil {
			return err
		}
		if replayed {
			if err := clearResolvedTargetWriteClaims(ctx, tx, m); err != nil {
				return err
			}
			return tx.Commit()
		}
	}

	targetGeneration, previousWriter, action, err := resolveTargetWrite(ctx, tx, m)
	if err != nil {
		return err
	}
	if action == targetWriteIgnore {
		if err := clearResolvedTargetWriteClaims(ctx, tx, m); err != nil {
			return err
		}
		return tx.Commit()
	}
	if action == targetWriteAmbiguous {
		if previousWriter == nil {
			return fmt.Errorf("%w: missing prior writer for %q", ErrTargetWriterAmbiguous, m.TargetIdentity)
		}
		if !previousWriter.Ambiguous {
			if err := markLatestWriterAmbiguous(ctx, tx, previousWriter); err != nil {
				return err
			}
		}
		if err := markTargetWriteClaimDirtyIfPresent(ctx, tx, m, m.MaterializedAt); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return fmt.Errorf(
			"%w for %q at %s",
			ErrTargetWriterAmbiguous,
			m.TargetIdentity,
			formatTime(m.MaterializedAt),
		)
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO renart_materializations
			(asset_id, environment, fingerprint, own_content, vars_hash,
			 target_identity, target_generation, interval_start, interval_end,
			 run_id, materialized_at, completion_id, completion_ordinal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		m.AssetID, m.Environment, m.Fingerprint, m.OwnContent, m.VarsHash,
		m.TargetIdentity, targetGeneration,
		optionalTimeString(m.IntervalStart), optionalTimeString(m.IntervalEnd),
		m.RunID, formatTime(m.MaterializedAt), m.CompletionID, m.CompletionOrdinal)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return fmt.Errorf(
			"%w for asset %q in environment %q and run %q",
			ErrMaterializationReplayConflict,
			m.AssetID,
			m.Environment,
			m.RunID,
		)
	}

	if m.ReplaceCoverage {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM renart_coverage
			WHERE asset_id = ? AND environment = ? AND fingerprint = ? AND vars_hash = ?
			  AND target_identity = ? AND target_generation = ?`,
			m.AssetID, m.Environment, m.Fingerprint, m.VarsHash,
			m.TargetIdentity, targetGeneration); err != nil {
			return err
		}
	}

	if m.IntervalStart == nil {
		// Non-windowed result: upsert the single '' marker row, bumping the
		// timestamp.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO renart_coverage
				(asset_id, environment, fingerprint, own_content, vars_hash,
				 target_identity, target_generation, interval_start, interval_end, materialized_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?)
			ON CONFLICT (
				asset_id, environment, fingerprint, vars_hash,
				target_identity, target_generation, interval_start
			)
			DO UPDATE SET materialized_at = excluded.materialized_at, own_content = excluded.own_content`,
			m.AssetID, m.Environment, m.Fingerprint, m.OwnContent, m.VarsHash,
			m.TargetIdentity, targetGeneration, formatTime(m.MaterializedAt))
		if err != nil {
			return err
		}
	} else if err := mergeCoverageInterval(ctx, tx, m, targetGeneration); err != nil {
		return err
	}

	if err := persistLatestWriter(ctx, tx, m, targetGeneration, previousWriter); err != nil {
		return err
	}
	if err := clearResolvedTargetWriteClaims(ctx, tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

// clearResolvedTargetWriteClaims is deliberately conditional on finding the
// successful write's own durable claim. Legacy target-aware records that were
// never claimed must not erase unrelated uncertainty. Claim sequence, rather
// than wall-clock time, defines which dirty claims predate the repair.
func clearResolvedTargetWriteClaims(ctx context.Context, tx *sql.Tx, m Materialization) error {
	if m.TargetIdentity == "" || m.CompletionID == "" || m.AssetID == "" {
		return nil
	}
	var ownSequence int64
	err := tx.QueryRowContext(ctx, `
		SELECT claim_sequence
		FROM renart_target_write_claims
		WHERE target_identity = ? AND completion_id = ? AND asset_id = ?`,
		m.TargetIdentity,
		m.CompletionID,
		m.AssetID,
	).Scan(&ownSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		DELETE FROM renart_target_write_claims
		WHERE target_identity = ?
		  AND (
			claim_sequence = ?
			OR (state = 'dirty' AND claim_sequence < ?)
		  )`,
		m.TargetIdentity,
		ownSequence,
		ownSequence,
	)
	return err
}

func markTargetWriteClaimDirtyIfPresent(
	ctx context.Context,
	tx *sql.Tx,
	m Materialization,
	at time.Time,
) error {
	if m.TargetIdentity == "" || m.CompletionID == "" || m.AssetID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE renart_target_write_claims
		SET state = 'dirty', updated_at = ?
		WHERE target_identity = ? AND completion_id = ? AND asset_id = ?`,
		formatTime(at),
		m.TargetIdentity,
		m.CompletionID,
		m.AssetID,
	)
	return err
}

type recordedMaterializationFact struct {
	Fingerprint       string
	OwnContent        string
	VarsHash          string
	TargetIdentity    string
	TargetGeneration  int64
	IntervalStart     string
	IntervalEnd       string
	MaterializedAt    string
	CompletionID      string
	CompletionOrdinal int64
}

// validateRecordedMaterializationReplay checks the partial unique fact key
// before resolving or mutating a physical writer. This ordering is important:
// a conflicting recovery payload must not mark an unrelated target ambiguous
// merely because its fact insert would later collide.
func validateRecordedMaterializationReplay(
	ctx context.Context,
	tx *sql.Tx,
	m Materialization,
) (bool, error) {
	var recorded recordedMaterializationFact
	err := tx.QueryRowContext(ctx, `
		SELECT fingerprint, own_content, vars_hash,
		       target_identity, target_generation,
		       interval_start, interval_end, materialized_at,
		       completion_id, completion_ordinal
		FROM renart_materializations
		WHERE asset_id = ? AND environment = ? AND run_id = ?`,
		m.AssetID,
		m.Environment,
		m.RunID,
	).Scan(
		&recorded.Fingerprint,
		&recorded.OwnContent,
		&recorded.VarsHash,
		&recorded.TargetIdentity,
		&recorded.TargetGeneration,
		&recorded.IntervalStart,
		&recorded.IntervalEnd,
		&recorded.MaterializedAt,
		&recorded.CompletionID,
		&recorded.CompletionOrdinal,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	exact := recorded.Fingerprint == m.Fingerprint &&
		recorded.OwnContent == m.OwnContent &&
		recorded.VarsHash == m.VarsHash &&
		recorded.TargetIdentity == m.TargetIdentity &&
		recorded.IntervalStart == optionalTimeString(m.IntervalStart) &&
		recorded.IntervalEnd == optionalTimeString(m.IntervalEnd) &&
		recorded.MaterializedAt == formatTime(m.MaterializedAt) &&
		recorded.CompletionID == m.CompletionID &&
		recorded.CompletionOrdinal == m.CompletionOrdinal
	if m.TargetIdentity == "" {
		exact = exact && recorded.TargetGeneration == 0
	} else {
		exact = exact && recorded.TargetGeneration > 0
	}
	if exact {
		return true, nil
	}
	return false, fmt.Errorf(
		"%w for asset %q in environment %q and run %q",
		ErrMaterializationReplayConflict,
		m.AssetID,
		m.Environment,
		m.RunID,
	)
}

func resolveTargetWrite(
	ctx context.Context,
	tx *sql.Tx,
	m Materialization,
) (int64, *latestWriterState, targetWriteAction, error) {
	if m.TargetIdentity == "" {
		return 0, nil, targetWriteAccept, nil
	}

	previous, err := loadLatestWriterState(ctx, tx, m.TargetIdentity)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil, targetWriteAccept, nil
	}
	if err != nil {
		return 0, nil, targetWriteIgnore, err
	}

	if m.MaterializedAt.UTC().Before(previous.MaterializedAt) {
		return previous.TargetGeneration, previous, targetWriteIgnore, nil
	}
	if m.MaterializedAt.UTC().Equal(previous.MaterializedAt) {
		if m.CompletionID != previous.CompletionID {
			return previous.TargetGeneration, previous, targetWriteAmbiguous, nil
		}
		if m.CompletionOrdinal < previous.CompletionOrdinal {
			return previous.TargetGeneration, previous, targetWriteIgnore, nil
		}
		if m.CompletionOrdinal > previous.CompletionOrdinal {
			if previous.Ambiguous {
				return previous.TargetGeneration, previous, targetWriteAmbiguous, nil
			}
			generation := previous.TargetGeneration
			if targetWriterVariantChanged(previous, m) {
				generation++
			}
			return generation, previous, targetWriteAccept, nil
		}
		if previous.AssetID == m.AssetID &&
			previous.Environment == m.Environment &&
			previous.Fingerprint == m.Fingerprint &&
			previous.VarsHash == m.VarsHash &&
			previous.RunID == m.RunID {
			return previous.TargetGeneration, previous, targetWriteIgnore, nil
		}
		return previous.TargetGeneration, previous, targetWriteAmbiguous, nil
	}

	generation := previous.TargetGeneration
	if previous.Ambiguous || targetWriterVariantChanged(previous, m) {
		generation++
	}
	return generation, previous, targetWriteAccept, nil
}

func targetWriterVariantChanged(previous *latestWriterState, m Materialization) bool {
	return previous.AssetID != m.AssetID ||
		previous.Environment != m.Environment ||
		previous.Fingerprint != m.Fingerprint ||
		previous.VarsHash != m.VarsHash
}

type targetWriteAction uint8

const (
	targetWriteAccept targetWriteAction = iota
	targetWriteIgnore
	targetWriteAmbiguous
)

type latestWriterState struct {
	LatestSuccessfulWriter
	materializedAtRaw string
}

func loadLatestWriterState(ctx context.Context, tx *sql.Tx, targetIdentity string) (*latestWriterState, error) {
	var state latestWriterState
	var ambiguous int
	err := tx.QueryRowContext(ctx, `
		SELECT target_identity, target_generation, asset_id, environment,
		       fingerprint, vars_hash, run_id, materialized_at,
		       completion_id, completion_ordinal, ambiguous
		FROM renart_latest_successful_writers
		WHERE target_identity = ?`, targetIdentity).Scan(
		&state.TargetIdentity,
		&state.TargetGeneration,
		&state.AssetID,
		&state.Environment,
		&state.Fingerprint,
		&state.VarsHash,
		&state.RunID,
		&state.materializedAtRaw,
		&state.CompletionID,
		&state.CompletionOrdinal,
		&ambiguous,
	)
	if err != nil {
		return nil, err
	}
	state.Ambiguous = ambiguous != 0
	state.MaterializedAt = parseTime(state.materializedAtRaw)
	if state.MaterializedAt.IsZero() {
		return nil, fmt.Errorf("matlog: latest writer for %q has an invalid completion timestamp", targetIdentity)
	}
	return &state, nil
}

func markLatestWriterAmbiguous(ctx context.Context, tx *sql.Tx, previous *latestWriterState) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE renart_latest_successful_writers
		SET ambiguous = 1
		WHERE target_identity = ?
		  AND target_generation = ?
		  AND materialized_at = ?
		  AND completion_id = ?
		  AND completion_ordinal = ?`,
		previous.TargetIdentity,
		previous.TargetGeneration,
		previous.materializedAtRaw,
		previous.CompletionID,
		previous.CompletionOrdinal,
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("matlog: latest writer for %q changed while marking ambiguity", previous.TargetIdentity)
	}
	return nil
}

func persistLatestWriter(
	ctx context.Context,
	tx *sql.Tx,
	m Materialization,
	targetGeneration int64,
	previous *latestWriterState,
) error {
	if m.TargetIdentity == "" {
		return nil
	}
	if previous == nil {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO renart_latest_successful_writers
				(target_identity, target_generation, asset_id, environment,
				 fingerprint, vars_hash, run_id, materialized_at,
				 completion_id, completion_ordinal, ambiguous)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			m.TargetIdentity,
			targetGeneration,
			m.AssetID,
			m.Environment,
			m.Fingerprint,
			m.VarsHash,
			m.RunID,
			formatTime(m.MaterializedAt),
			m.CompletionID,
			m.CompletionOrdinal,
		)
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE renart_latest_successful_writers
		SET target_generation = ?, asset_id = ?, environment = ?,
		    fingerprint = ?, vars_hash = ?, run_id = ?, materialized_at = ?,
		    completion_id = ?, completion_ordinal = ?, ambiguous = 0
		WHERE target_identity = ?
		  AND target_generation = ?
		  AND materialized_at = ?
		  AND completion_id = ?
		  AND completion_ordinal = ?`,
		targetGeneration,
		m.AssetID,
		m.Environment,
		m.Fingerprint,
		m.VarsHash,
		m.RunID,
		formatTime(m.MaterializedAt),
		m.CompletionID,
		m.CompletionOrdinal,
		m.TargetIdentity,
		previous.TargetGeneration,
		previous.materializedAtRaw,
		previous.CompletionID,
		previous.CompletionOrdinal,
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("matlog: latest writer for %q changed while recording completion", m.TargetIdentity)
	}
	return nil
}

func mergeCoverageInterval(ctx context.Context, tx *sql.Tx, m Materialization, targetGeneration int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT interval_start, interval_end, materialized_at
		FROM renart_coverage
		WHERE asset_id = ? AND environment = ? AND fingerprint = ? AND vars_hash = ?
		  AND target_identity = ? AND target_generation = ?
		  AND interval_start <> ''
		  AND interval_start <= ? AND interval_end >= ?`,
		m.AssetID, m.Environment, m.Fingerprint, m.VarsHash,
		m.TargetIdentity, targetGeneration,
		optionalTimeString(m.IntervalEnd), optionalTimeString(m.IntervalStart))
	if err != nil {
		return err
	}

	unionStart := m.IntervalStart.UTC()
	unionEnd := m.IntervalEnd.UTC()
	latest := m.MaterializedAt.UTC()
	matchedStarts := make([]string, 0, 2)

	for rows.Next() {
		var startRaw, endRaw, atRaw string
		if err := rows.Scan(&startRaw, &endRaw, &atRaw); err != nil {
			rows.Close()
			return err
		}
		matchedStarts = append(matchedStarts, startRaw)
		if start := parseTime(startRaw); start.Before(unionStart) {
			unionStart = start
		}
		if end := parseTime(endRaw); end.After(unionEnd) {
			unionEnd = end
		}
		if at := parseTime(atRaw); at.After(latest) {
			latest = at
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, startRaw := range matchedStarts {
		_, err := tx.ExecContext(ctx, `
			DELETE FROM renart_coverage
			WHERE asset_id = ? AND environment = ? AND fingerprint = ? AND vars_hash = ?
			  AND target_identity = ? AND target_generation = ? AND interval_start = ?`,
			m.AssetID, m.Environment, m.Fingerprint, m.VarsHash,
			m.TargetIdentity, targetGeneration, startRaw)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO renart_coverage
			(asset_id, environment, fingerprint, own_content, vars_hash,
			 target_identity, target_generation, interval_start, interval_end, materialized_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.AssetID, m.Environment, m.Fingerprint, m.OwnContent, m.VarsHash,
		m.TargetIdentity, targetGeneration,
		formatTime(unionStart), formatTime(unionEnd), formatTime(latest))
	return err
}

// Coverage fetches all coverage rows for the given assets in one batched
// query, grouped by asset ID. The caller filters to current fingerprints in
// memory — rows for abandoned fingerprints are kept (they are tiny and
// enable cross-version reuse later).
func (s *Store) Coverage(ctx context.Context, assetIDs []string, environment, varsHash string) (map[string][]CoverageRow, error) {
	if len(assetIDs) == 0 {
		return map[string][]CoverageRow{}, nil
	}

	query := `
		SELECT asset_id, fingerprint, own_content, target_identity, target_generation,
		       interval_start, interval_end, materialized_at
		FROM renart_coverage
		WHERE environment = ? AND vars_hash = ? AND asset_id IN (?` +
		repeatPlaceholder(len(assetIDs)-1) + `)`
	args := make([]any, 0, len(assetIDs)+2)
	args = append(args, environment, varsHash)
	for _, id := range assetIDs {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]CoverageRow)
	for rows.Next() {
		var assetID, fingerprint, ownContent, targetIdentity, startRaw, endRaw, atRaw string
		var targetGeneration int64
		if err := rows.Scan(
			&assetID,
			&fingerprint,
			&ownContent,
			&targetIdentity,
			&targetGeneration,
			&startRaw,
			&endRaw,
			&atRaw,
		); err != nil {
			return nil, err
		}
		result[assetID] = append(result[assetID], CoverageRow{
			AssetID:          assetID,
			Fingerprint:      fingerprint,
			OwnContent:       ownContent,
			TargetIdentity:   targetIdentity,
			TargetGeneration: targetGeneration,
			IntervalStart:    optionalTime(startRaw),
			IntervalEnd:      optionalTime(endRaw),
			MaterializedAt:   parseTime(atRaw),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortCoverageRows(result)
	return result, nil
}

// LatestWriters returns the durable latest-successful-writer row for each
// requested non-empty physical target. Missing and legacy empty targets are
// omitted, as is any target with an active or dirty write claim.
func (s *Store) LatestWriters(
	ctx context.Context,
	targetIdentities []string,
) (map[string]LatestSuccessfulWriter, error) {
	nonEmpty := make([]string, 0, len(targetIdentities))
	seen := make(map[string]struct{}, len(targetIdentities))
	for _, targetIdentity := range targetIdentities {
		if targetIdentity == "" {
			continue
		}
		if _, ok := seen[targetIdentity]; ok {
			continue
		}
		seen[targetIdentity] = struct{}{}
		nonEmpty = append(nonEmpty, targetIdentity)
	}
	if len(nonEmpty) == 0 {
		return map[string]LatestSuccessfulWriter{}, nil
	}

	query := `
		SELECT target_identity, target_generation, asset_id, environment,
		       fingerprint, vars_hash, run_id, materialized_at,
		       completion_id, completion_ordinal, ambiguous
		FROM renart_latest_successful_writers AS writer
		WHERE writer.target_identity IN (?` + repeatPlaceholder(len(nonEmpty)-1) + `)
		  AND NOT EXISTS (
			SELECT 1
			FROM renart_target_write_claims AS claim
			WHERE claim.target_identity = writer.target_identity
		  )`
	args := make([]any, 0, len(nonEmpty))
	for _, targetIdentity := range nonEmpty {
		args = append(args, targetIdentity)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]LatestSuccessfulWriter, len(nonEmpty))
	for rows.Next() {
		var writer LatestSuccessfulWriter
		var atRaw string
		var ambiguous int
		if err := rows.Scan(
			&writer.TargetIdentity,
			&writer.TargetGeneration,
			&writer.AssetID,
			&writer.Environment,
			&writer.Fingerprint,
			&writer.VarsHash,
			&writer.RunID,
			&atRaw,
			&writer.CompletionID,
			&writer.CompletionOrdinal,
			&ambiguous,
		); err != nil {
			return nil, err
		}
		writer.MaterializedAt = parseTime(atRaw)
		if writer.MaterializedAt.IsZero() {
			return nil, fmt.Errorf(
				"matlog: latest writer for %q has an invalid completion timestamp",
				writer.TargetIdentity,
			)
		}
		writer.Ambiguous = ambiguous != 0
		result[writer.TargetIdentity] = writer
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CurrentTargetCoverage fetches coverage for each asset's selected physical
// target, restricted to the generation named by that target's durable latest
// writer row. Assets with an empty/unknown target, no writer, or any active or
// dirty write claim are omitted and therefore remain stale until rebuilt.
func (s *Store) CurrentTargetCoverage(
	ctx context.Context,
	assetTargets map[string]string,
	environment string,
	varsHash string,
) (map[string][]CoverageRow, error) {
	assetIDs := make([]string, 0, len(assetTargets))
	for assetID, targetIdentity := range assetTargets {
		if assetID == "" || targetIdentity == "" {
			continue
		}
		assetIDs = append(assetIDs, assetID)
	}
	if len(assetIDs) == 0 {
		return map[string][]CoverageRow{}, nil
	}
	sort.Strings(assetIDs)

	values := "(?, ?)"
	if len(assetIDs) > 1 {
		values += repeatPairPlaceholder(len(assetIDs) - 1)
	}
	query := `
		WITH requested(asset_id, target_identity) AS (VALUES ` + values + `)
		SELECT c.asset_id, c.fingerprint, c.own_content,
		       c.target_identity, c.target_generation,
		       c.interval_start, c.interval_end, c.materialized_at
		FROM requested AS requested
		JOIN renart_latest_successful_writers AS writer
		  ON writer.target_identity = requested.target_identity
		 AND writer.ambiguous = 0
		JOIN renart_coverage AS c
		  ON c.asset_id = requested.asset_id
		 AND c.target_identity = requested.target_identity
		 AND c.target_generation = writer.target_generation
		 AND c.asset_id = writer.asset_id
		 AND c.environment = writer.environment
		 AND c.fingerprint = writer.fingerprint
		 AND c.vars_hash = writer.vars_hash
		WHERE c.environment = ? AND c.vars_hash = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM renart_target_write_claims AS claim
			WHERE claim.target_identity = writer.target_identity
		  )`
	args := make([]any, 0, len(assetIDs)*2+2)
	for _, assetID := range assetIDs {
		args = append(args, assetID, assetTargets[assetID])
	}
	args = append(args, environment, varsHash)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]CoverageRow, len(assetIDs))
	for rows.Next() {
		var row CoverageRow
		var startRaw, endRaw, atRaw string
		if err := rows.Scan(
			&row.AssetID,
			&row.Fingerprint,
			&row.OwnContent,
			&row.TargetIdentity,
			&row.TargetGeneration,
			&startRaw,
			&endRaw,
			&atRaw,
		); err != nil {
			return nil, err
		}
		row.IntervalStart = optionalTime(startRaw)
		row.IntervalEnd = optionalTime(endRaw)
		row.MaterializedAt = parseTime(atRaw)
		result[row.AssetID] = append(result[row.AssetID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortCoverageRows(result)
	return result, nil
}

// CurrentTargetOwnContent returns the own-definition hash for each requested
// asset whose selected target is currently and unambiguously written by that
// asset in the selected environment. Unlike LatestOwnContent, this query is
// restricted to the global writer's current target generation and deliberately
// ignores historical, displaced, ambiguous, and targetless coverage.
//
// The lookup does not filter by variables hash: staleness needs the currently
// present output's own-content hash to distinguish an own edit from an
// upstream/variables change when selected-context coverage itself does not
// match.
func (s *Store) CurrentTargetOwnContent(
	ctx context.Context,
	assetTargets map[string]string,
	environment string,
) (map[string]string, error) {
	assetIDs := make([]string, 0, len(assetTargets))
	for assetID, targetIdentity := range assetTargets {
		if assetID == "" || targetIdentity == "" {
			continue
		}
		assetIDs = append(assetIDs, assetID)
	}
	if len(assetIDs) == 0 {
		return map[string]string{}, nil
	}
	sort.Strings(assetIDs)

	values := "(?, ?)"
	if len(assetIDs) > 1 {
		values += repeatPairPlaceholder(len(assetIDs) - 1)
	}
	query := `
		WITH requested(asset_id, target_identity) AS (VALUES ` + values + `)
		SELECT DISTINCT c.asset_id, c.own_content
		FROM requested AS requested
		JOIN renart_latest_successful_writers AS writer
		  ON writer.target_identity = requested.target_identity
		 AND writer.ambiguous = 0
		JOIN renart_coverage AS c
		  ON c.asset_id = requested.asset_id
		 AND c.target_identity = requested.target_identity
		 AND c.target_generation = writer.target_generation
		 AND c.asset_id = writer.asset_id
		 AND c.environment = writer.environment
		 AND c.fingerprint = writer.fingerprint
		 AND c.vars_hash = writer.vars_hash
		WHERE c.environment = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM renart_target_write_claims AS claim
			WHERE claim.target_identity = writer.target_identity
		  )`
	args := make([]any, 0, len(assetIDs)*2+1)
	for _, assetID := range assetIDs {
		args = append(args, assetID, assetTargets[assetID])
	}
	args = append(args, environment)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string, len(assetIDs))
	for rows.Next() {
		var assetID, ownContent string
		if err := rows.Scan(&assetID, &ownContent); err != nil {
			return nil, err
		}
		if previous, ok := result[assetID]; ok && previous != ownContent {
			return nil, fmt.Errorf("matlog: current target generation for %q has conflicting own-content hashes", assetID)
		}
		result[assetID] = ownContent
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func sortCoverageRows(result map[string][]CoverageRow) {
	for _, list := range result {
		sort.Slice(list, func(i, j int) bool {
			left, right := list[i], list[j]
			if (left.IntervalStart == nil) != (right.IntervalStart == nil) {
				return left.IntervalStart == nil
			}
			if left.IntervalStart != nil && !left.IntervalStart.Equal(*right.IntervalStart) {
				return left.IntervalStart.Before(*right.IntervalStart)
			}
			if left.TargetIdentity != right.TargetIdentity {
				return left.TargetIdentity < right.TargetIdentity
			}
			if left.TargetGeneration != right.TargetGeneration {
				return left.TargetGeneration < right.TargetGeneration
			}
			return left.Fingerprint < right.Fingerprint
		})
	}
}

// HasAnyCoverage reports which of the given assets have at least one
// coverage row in the environment under any fingerprint or vars hash —
// the never_built check.
func (s *Store) HasAnyCoverage(ctx context.Context, assetIDs []string, environment string) (map[string]bool, error) {
	if len(assetIDs) == 0 {
		return map[string]bool{}, nil
	}
	query := `
		SELECT DISTINCT asset_id FROM renart_coverage
		WHERE environment = ? AND asset_id IN (?` + repeatPlaceholder(len(assetIDs)-1) + `)`
	args := make([]any, 0, len(assetIDs)+1)
	args = append(args, environment)
	for _, id := range assetIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool, len(assetIDs))
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, err
		}
		result[assetID] = true
	}
	return result, rows.Err()
}

// LatestOwnContent returns, per asset, the own-content hash of the most
// recently materialized coverage row in the environment, across all
// fingerprints and vars hashes. Used to classify a fingerprint mismatch as
// stale_edited (own content moved) vs stale_upstream (inherited).
func (s *Store) LatestOwnContent(ctx context.Context, assetIDs []string, environment string) (map[string]string, error) {
	if len(assetIDs) == 0 {
		return map[string]string{}, nil
	}
	query := `
		SELECT asset_id, own_content FROM renart_coverage
		WHERE environment = ? AND asset_id IN (?` + repeatPlaceholder(len(assetIDs)-1) + `)
		ORDER BY materialized_at ASC`
	args := make([]any, 0, len(assetIDs)+1)
	args = append(args, environment)
	for _, id := range assetIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Ascending order means the last row scanned per asset is the latest.
	result := make(map[string]string)
	for rows.Next() {
		var assetID, ownContent string
		if err := rows.Scan(&assetID, &ownContent); err != nil {
			return nil, err
		}
		result[assetID] = ownContent
	}
	return result, rows.Err()
}

// LatestFingerprint returns, per asset, the fingerprint recorded at its most
// recent materialization in the environment (across all fingerprints and vars
// hashes). This is the identity of the data physically present in the asset's
// table — what a downstream reads when it materializes — and is folded into
// the downstream's achieved fingerprint so a build on a stale upstream records
// as stale.
func (s *Store) LatestFingerprint(ctx context.Context, assetIDs []string, environment string) (map[string]string, error) {
	if len(assetIDs) == 0 {
		return map[string]string{}, nil
	}
	query := `
		SELECT asset_id, fingerprint FROM renart_materializations
		WHERE environment = ? AND asset_id IN (?` + repeatPlaceholder(len(assetIDs)-1) + `)
		ORDER BY materialized_at ASC`
	args := make([]any, 0, len(assetIDs)+1)
	args = append(args, environment)
	for _, id := range assetIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Ascending order means the last row scanned per asset is the latest.
	result := make(map[string]string)
	for rows.Next() {
		var assetID, fingerprint string
		if err := rows.Scan(&assetID, &fingerprint); err != nil {
			return nil, err
		}
		result[assetID] = fingerprint
	}
	return result, rows.Err()
}

// AssetRunRecord is the most recent run attempt for one asset in one
// environment — success or failure. It distinguishes an untested edit from a run
// that was attempted and failed, and surfaces an unchanged asset whose last run
// failed.
type AssetRunRecord struct {
	AssetID     string
	Environment string
	// Fingerprint is the target fingerprint of the content that ran, compared
	// against the asset's current fingerprint to tell whether the failing run
	// was on the content still on disk.
	Fingerprint string
	Status      string // "succeeded" | "failed" | "cancelled"
	RunID       string
	RanAt       time.Time
}

// RecordRun upserts the latest run attempt for an asset+environment. Older or
// equal-time recovery events are ignored, so replay cannot overwrite a newer
// result that was already recorded.
func (s *Store) RecordRun(ctx context.Context, r AssetRunRecord) error {
	if r.AssetID == "" || r.Fingerprint == "" || r.Status == "" {
		return fmt.Errorf("matlog: asset_id, fingerprint and status are required")
	}
	if r.RanAt.IsZero() {
		r.RanAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existingRaw string
	err = tx.QueryRowContext(ctx, `
		SELECT ran_at FROM renart_asset_runs
		WHERE asset_id = ? AND environment = ?`, r.AssetID, r.Environment).Scan(&existingRaw)
	if err == nil {
		existing := parseTime(existingRaw)
		if !existing.IsZero() && !existing.Before(r.RanAt.UTC()) {
			return tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO renart_asset_runs
			(asset_id, environment, fingerprint, status, run_id, ran_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (asset_id, environment)
		DO UPDATE SET fingerprint = excluded.fingerprint, status = excluded.status,
			run_id = excluded.run_id, ran_at = excluded.ran_at`,
		r.AssetID, r.Environment, r.Fingerprint, r.Status, r.RunID, formatTime(r.RanAt))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// LastRuns returns the most recent run attempt per asset in the environment.
func (s *Store) LastRuns(ctx context.Context, assetIDs []string, environment string) (map[string]AssetRunRecord, error) {
	if len(assetIDs) == 0 {
		return map[string]AssetRunRecord{}, nil
	}
	query := `
		SELECT asset_id, fingerprint, status, run_id, ran_at FROM renart_asset_runs
		WHERE environment = ? AND asset_id IN (?` + repeatPlaceholder(len(assetIDs)-1) + `)`
	args := make([]any, 0, len(assetIDs)+1)
	args = append(args, environment)
	for _, id := range assetIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]AssetRunRecord)
	for rows.Next() {
		var rec AssetRunRecord
		var ranAt string
		if err := rows.Scan(&rec.AssetID, &rec.Fingerprint, &rec.Status, &rec.RunID, &ranAt); err != nil {
			return nil, err
		}
		rec.Environment = environment
		rec.RanAt = parseTime(ranAt)
		result[rec.AssetID] = rec
	}
	return result, rows.Err()
}

// Prune deletes raw materialization facts older than the cutoff. Coverage
// rows are the durable summary and are never pruned here.
func (s *Store) Prune(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM renart_materializations WHERE materialized_at < ?`, formatTime(olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CountFacts returns the number of raw fact rows (test/diagnostics helper).
func (s *Store) CountFacts(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM renart_materializations`).Scan(&count)
	return count, err
}

func repeatPlaceholder(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		out = append(out, ',', '?')
	}
	return string(out)
}

func repeatPairPlaceholder(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*8)
	for i := 0; i < n; i++ {
		out = append(out, ',', ' ', '(', '?', ',', ' ', '?', ')')
	}
	return string(out)
}
