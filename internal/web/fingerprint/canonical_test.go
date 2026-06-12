package fingerprint

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestCanonicalSQL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"collapses whitespace", "select  1\n\t from   t", "select 1 from t"},
		{"strips line comments", "select 1 -- trailing\nfrom t", "select 1 from t"},
		{"strips block comments", "select /* inline */ 1 from t", "select 1 from t"},
		{"strips bruin header", "/* @bruin\nname: x\n@bruin */\nselect 1", "select 1"},
		{"preserves string literals", "select '-- not a comment' from t", "select '-- not a comment' from t"},
		{"preserves escaped quotes", "select 'it''s -- fine' from t", "select 'it''s -- fine' from t"},
		{"preserves double-quoted idents", `select "weird /* col */" from t`, `select "weird /* col */" from t`},
		{"no case folding", "SELECT 1 FROM T", "SELECT 1 FROM T"},
		{"trims edges", "  select 1  ", "select 1"},
		{"unterminated comment", "select 1 /* dangling", "select 1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, CanonicalSQL(testCase.input))
		})
	}
}

func TestConsumedVarNames(t *testing.T) {
	t.Parallel()
	declared := map[string]struct{}{"region": {}, "limit": {}}
	names := ConsumedVarNames("select * from t where r = '{{ var.region }}' and x = {{ var.undeclared }} limit {{var.limit}}", declared)
	assert.Equal(t, []string{"limit", "region"}, names)
	assert.Empty(t, ConsumedVarNames("select 1", declared))
}

func TestVarsHashOnlyDependsOnConsumedNames(t *testing.T) {
	t.Parallel()
	a := VarsHash(Vars{"region": "eu", "limit": 100}, []string{"region"})
	b := VarsHash(Vars{"region": "eu", "limit": 999}, []string{"region"})
	c := VarsHash(Vars{"region": "us", "limit": 100}, []string{"region"})
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}

// TestGoldenFingerprint pins the algorithm output. If this test breaks, the
// fingerprint algorithm changed: bump Version so history invalidates
// cleanly, then update the golden value.
func TestGoldenFingerprint(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"9ccf7451e3b8dcda01a2eb8fe41258d18c0e2058f6d0d66a0cff23ff4ed43665",
		hashHex("v1", "stability", "check"))
}
