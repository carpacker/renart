package runcontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRejectsContextThatCannotBePreserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input Input
		want  string
	}{
		{name: "mode conflict", input: Input{FullRefresh: true, Backfill: true}, want: "mutually exclusive"},
		{name: "backfill without window", input: Input{Backfill: true}, want: "explicit start and end"},
		{name: "missing end", input: Input{Start: "2026-07-16T08:00:00Z"}, want: "provided together"},
		{name: "invalid start", input: Input{Start: "yesterday", End: "2026-07-16T09:00:00Z"}, want: "start must be an RFC3339"},
		{name: "reversed window", input: Input{Start: "2026-07-16T09:00:00Z", End: "2026-07-16T08:00:00Z"}, want: "start must be before end"},
		{name: "invalid sensor", input: Input{SensorMode: "sometimes"}, want: "invalid sensor_mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Normalize(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestNormalizeCanonicalizesExplicitContext(t *testing.T) {
	t.Parallel()

	normalized, err := Normalize(Input{
		Start:      "2026-07-16T10:00:00+02:00",
		End:        "2026-07-16T11:00:00+02:00",
		Backfill:   true,
		SensorMode: SensorModeSkip,
	})
	require.NoError(t, err)
	assert.Equal(t, "2026-07-16T08:00:00Z", normalized.StartString())
	assert.Equal(t, "2026-07-16T09:00:00Z", normalized.EndString())
	assert.Equal(t, SensorModeSkip, normalized.SensorMode)
}

func TestValidateDryRunRejectsContextTheValidatorDoesNotConsume(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		input Input
		want  string
	}{
		{name: "window", input: Input{Start: "2026-07-16T08:00:00Z", End: "2026-07-16T09:00:00Z"}, want: "start/end"},
		{name: "full refresh", input: Input{FullRefresh: true}, want: "full_refresh"},
		{name: "backfill", input: Input{Backfill: true}, want: "backfill"},
		{name: "sensor mode", input: Input{SensorMode: SensorModeSkip}, want: "sensor_mode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDryRun(true, tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	assert.NoError(t, ValidateDryRun(false, Input{FullRefresh: true}))
	assert.NoError(t, ValidateDryRun(true, Input{}))
}
