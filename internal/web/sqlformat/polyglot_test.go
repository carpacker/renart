package sqlformat

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPolyglotRepeatedCallsReuseModule(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		formatted, err := Format(ctx, "select 1 as id", DialectGeneric)
		if err != nil {
			t.Fatalf("format call %d: %v", i, err)
		}
		if formatted == "" {
			t.Fatalf("format call %d returned empty SQL", i)
		}

		parsed, err := Call(ctx, "parse", "select 1 as id", DialectGeneric)
		if err != nil {
			t.Fatalf("parse call %d: %v", i, err)
		}
		var response struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal([]byte(parsed), &response); err != nil {
			t.Fatalf("parse call %d returned invalid JSON: %v", i, err)
		}
		if !response.Success {
			t.Fatalf("parse call %d was not successful: %s", i, parsed)
		}
	}
}
