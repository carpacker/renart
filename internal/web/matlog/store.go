// Package matlog persists materialization facts and the compacted coverage
// table. Every completed run writes immutable fact rows; coverage merges
// them into one row per contiguous interval so freshness lookups stay
// O(gaps) regardless of run count.
package matlog

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Store wraps the shared scheduler SQLite database (the same one River
// uses). Tables are created by the scheduler store's goose migrations.
type Store struct {
	db *sql.DB
}

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
	// IntervalStart/IntervalEnd are nil for full-refresh assets.
	IntervalStart  *time.Time
	IntervalEnd    *time.Time
	RunID          string
	MaterializedAt time.Time
}

// CoverageRow is one merged interval (or full-refresh marker) from the
// coverage table.
type CoverageRow struct {
	AssetID     string
	Fingerprint string
	OwnContent  string
	// IntervalStart/IntervalEnd are nil for the full-refresh "built" marker.
	IntervalStart  *time.Time
	IntervalEnd    *time.Time
	MaterializedAt time.Time
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

// Record writes the immutable fact row and merges the coverage table inside
// one transaction. The merge unions the new interval with every existing
// row it overlaps or exactly touches, so a backfill interval bridging two
// existing rows collapses all three into one.
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO renart_materializations
			(asset_id, environment, fingerprint, own_content, vars_hash, interval_start, interval_end, run_id, materialized_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.AssetID, m.Environment, m.Fingerprint, m.OwnContent, m.VarsHash,
		optionalTimeString(m.IntervalStart), optionalTimeString(m.IntervalEnd),
		m.RunID, formatTime(m.MaterializedAt))
	if err != nil {
		return err
	}

	if m.IntervalStart == nil {
		// Full refresh: upsert the single '' marker row, bumping the timestamp.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO renart_coverage
				(asset_id, environment, fingerprint, own_content, vars_hash, interval_start, interval_end, materialized_at)
			VALUES (?, ?, ?, ?, ?, '', '', ?)
			ON CONFLICT (asset_id, environment, fingerprint, vars_hash, interval_start)
			DO UPDATE SET materialized_at = excluded.materialized_at, own_content = excluded.own_content`,
			m.AssetID, m.Environment, m.Fingerprint, m.OwnContent, m.VarsHash, formatTime(m.MaterializedAt))
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	if err := mergeCoverageInterval(ctx, tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

func mergeCoverageInterval(ctx context.Context, tx *sql.Tx, m Materialization) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT interval_start, interval_end, materialized_at
		FROM renart_coverage
		WHERE asset_id = ? AND environment = ? AND fingerprint = ? AND vars_hash = ?
		  AND interval_start <> ''
		  AND interval_start <= ? AND interval_end >= ?`,
		m.AssetID, m.Environment, m.Fingerprint, m.VarsHash,
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
			WHERE asset_id = ? AND environment = ? AND fingerprint = ? AND vars_hash = ? AND interval_start = ?`,
			m.AssetID, m.Environment, m.Fingerprint, m.VarsHash, startRaw)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO renart_coverage
			(asset_id, environment, fingerprint, own_content, vars_hash, interval_start, interval_end, materialized_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.AssetID, m.Environment, m.Fingerprint, m.OwnContent, m.VarsHash,
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
		SELECT asset_id, fingerprint, own_content, interval_start, interval_end, materialized_at
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
		var assetID, fingerprint, ownContent, startRaw, endRaw, atRaw string
		if err := rows.Scan(&assetID, &fingerprint, &ownContent, &startRaw, &endRaw, &atRaw); err != nil {
			return nil, err
		}
		result[assetID] = append(result[assetID], CoverageRow{
			AssetID:        assetID,
			Fingerprint:    fingerprint,
			OwnContent:     ownContent,
			IntervalStart:  optionalTime(startRaw),
			IntervalEnd:    optionalTime(endRaw),
			MaterializedAt: parseTime(atRaw),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, list := range result {
		sort.Slice(list, func(i, j int) bool {
			if list[i].IntervalStart == nil || list[j].IntervalStart == nil {
				return list[i].IntervalStart == nil
			}
			return list[i].IntervalStart.Before(*list[j].IntervalStart)
		})
	}
	return result, nil
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
