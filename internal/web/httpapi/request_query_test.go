package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeStrictQueryRejectsAmbiguousOrUnknownContext(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "unknown", raw: "dry_rnu=true", want: "unknown query parameter"},
		{name: "repeated", raw: "dry_run=true&dry_run=false", want: "exactly once"},
		{name: "malformed encoding", raw: "environment=%zz", want: "invalid query string"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("POST", "/api/example?"+tt.raw, nil)
			_, err := decodeStrictQuery(request, "dry_run", "environment")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestStrictQueryBoolRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("POST", "/api/example?dry_run=tru", nil)
	values, err := decodeStrictQuery(request, "dry_run")
	require.NoError(t, err)
	_, err = strictQueryBool(values, "dry_run")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "true or false")
}
