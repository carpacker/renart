package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultExecutionTimeWindowDaily(t *testing.T) {
	now := time.Date(2026, 5, 28, 13, 14, 0, 0, time.UTC)
	window, err := DefaultExecutionTimeWindow("@daily", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC), window.Start)
	assert.Equal(t, time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC), window.End)
}

func TestDefaultExecutionTimeWindowHourly(t *testing.T) {
	now := time.Date(2026, 5, 28, 13, 14, 0, 0, time.UTC)
	window, err := DefaultExecutionTimeWindow("@hourly", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC), window.Start)
	assert.Equal(t, time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC), window.End)
}

func TestDefaultExecutionTimeWindowStandardCron(t *testing.T) {
	now := time.Date(2026, 5, 28, 13, 14, 0, 0, time.UTC)
	window, err := DefaultExecutionTimeWindow("15 */6 * * *", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 28, 6, 15, 0, 0, time.UTC), window.Start)
	assert.Equal(t, time.Date(2026, 5, 28, 12, 15, 0, 0, time.UTC), window.End)
}

func TestDefaultExecutionTimeWindowStandardCronWithCommaHours(t *testing.T) {
	now := time.Date(2026, 5, 28, 13, 14, 0, 0, time.UTC)
	window, err := DefaultExecutionTimeWindow("0 0,12 * * *", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC), window.Start)
	assert.Equal(t, time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC), window.End)
}

func TestResolveExecutionTimeWindowExplicit(t *testing.T) {
	window, err := ResolveExecutionTimeWindow(
		"@daily",
		"2026-05-26T00:00:00Z",
		"2026-05-27T00:00:00Z",
		time.Date(2026, 5, 28, 13, 14, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-26T00:00:00Z", window.StartRFC3339())
	assert.Equal(t, "2026-05-27T00:00:00Z", window.EndRFC3339())
}
