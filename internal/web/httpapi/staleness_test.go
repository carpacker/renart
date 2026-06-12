package httpapi

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The frontend sends Date.toISOString() values ("...T10:00:00.000Z"); the
// selected time range silently falling out of the staleness computation on
// a parse failure would be invisible, so pin the accepted formats.
func TestParseQueryTimeAcceptsFrontendFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw      string
		expected time.Time
		ok       bool
	}{
		{"2026-06-12T10:00:00.000Z", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), true},
		{"2026-06-12T10:00:00Z", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), true},
		{"2026-06-12T12:00:00+02:00", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), true},
		{"", time.Time{}, false},
		{"not-a-time", time.Time{}, false},
	}
	for _, testCase := range cases {
		// URL-encode like the frontend's URLSearchParams does.
		request := httptest.NewRequest("GET", "/api/x?start="+url.QueryEscape(testCase.raw), nil)
		parsed, ok := parseQueryTime(request, "start")
		require.Equal(t, testCase.ok, ok, "input %q", testCase.raw)
		if ok {
			assert.True(t, parsed.Equal(testCase.expected), "input %q parsed to %s", testCase.raw, parsed)
		}
	}
}
