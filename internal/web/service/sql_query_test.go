package service

import (
	"context"
	"errors"
	"testing"
)

func TestSQLServiceQueryCapsRows(t *testing.T) {
	svc := NewSQLService(SQLDependencies{
		RunConnectionQuery: func(_ context.Context, _, _, _ string) ([]string, []map[string]any, error) {
			rows := make([]map[string]any, 5)
			for i := range rows {
				rows[i] = map[string]any{"value": i}
			}
			return []string{"value"}, rows, nil
		},
	})

	result := svc.Query(context.Background(), "duckdb-default", "", "select * from t", 3)
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %q (error: %s)", result.Status, result.Error)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("expected 3 rows after cap, got %d", len(result.Rows))
	}
	if !result.Truncated {
		t.Fatal("expected truncated flag when rows exceed limit")
	}
	if len(result.Columns) != 1 || result.Columns[0] != "value" {
		t.Fatalf("unexpected columns: %v", result.Columns)
	}
}

func TestSQLServiceQueryNoCapWithinLimit(t *testing.T) {
	svc := NewSQLService(SQLDependencies{
		RunConnectionQuery: func(_ context.Context, _, _, _ string) ([]string, []map[string]any, error) {
			return []string{"value"}, []map[string]any{{"value": 1}}, nil
		},
	})

	result := svc.Query(context.Background(), "duckdb-default", "", "select * from t", 10)
	if result.Truncated {
		t.Fatal("did not expect truncated flag")
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestSQLServiceQueryError(t *testing.T) {
	svc := NewSQLService(SQLDependencies{
		RunConnectionQuery: func(_ context.Context, _, _, _ string) ([]string, []map[string]any, error) {
			return nil, nil, errors.New("boom")
		},
	})

	result := svc.Query(context.Background(), "duckdb-default", "", "select * from t", 10)
	if result.Status != "error" {
		t.Fatalf("expected error status, got %q", result.Status)
	}
	if result.Error != "boom" {
		t.Fatalf("unexpected error message: %q", result.Error)
	}
	if result.Rows == nil || result.Columns == nil {
		t.Fatal("expected non-nil empty rows/columns on error")
	}
}
