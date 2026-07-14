package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrimLegacyOutputReplay(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "removes aggregate replay after streamed chunks",
			lines: []string{"executing snapshot\n", "Analyzed pipeline\n", "\nInterval\n", "Analyzed pipeline\n\nInterval\n"},
			want:  []string{"executing snapshot\n", "Analyzed pipeline\n", "\nInterval\n"},
		},
		{
			name:  "preserves ansi and chunk boundaries",
			lines: []string{"snapshot\n", "\x1b[32mPASS\x1b[0m asset\n", "done\n", "\x1b[32mPASS\x1b[0m asset\ndone\n"},
			want:  []string{"snapshot\n", "\x1b[32mPASS\x1b[0m asset\n", "done\n"},
		},
		{
			name:  "keeps an ordinary repeated chunk",
			lines: []string{"same\n", "same\n"},
			want:  []string{"same\n", "same\n"},
		},
		{
			name:  "keeps a partial suffix match",
			lines: []string{"first\n", "second\n", "third\n", "second\nthird"},
			want:  []string{"first\n", "second\n", "third\n", "second\nthird"},
		},
		{
			name:  "keeps intentional blank chunks",
			lines: []string{"section\n", "\n", "next\n"},
			want:  []string{"section\n", "\n", "next\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := make([]LogLine, 0, len(tt.lines))
			for _, line := range tt.lines {
				logs = append(logs, LogLine{At: now, Line: line})
			}

			got := trimLegacyOutputReplay(logs)
			gotLines := make([]string, 0, len(got))
			for _, log := range got {
				gotLines = append(gotLines, log.Line)
			}
			assert.Equal(t, tt.want, gotLines)
		})
	}
}

func TestServiceGetRunTrimsLegacyOutputReplay(t *testing.T) {
	t.Parallel()

	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{
		PipelineID: "pipeline-id",
		Pipeline:   "analytics",
		Trigger:    RunTriggerSchedule,
		Status:     RunStatusSuccess,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	for _, line := range []string{"snapshot\n", "first\n", "second\n", "first\nsecond\n"} {
		require.NoError(t, store.AppendLog(ctx, runID, LogLine{At: now, Line: line}))
	}

	service := New(Options{Store: store})
	_, logs, _, err := service.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Len(t, logs, 3)
	assert.Equal(t, "snapshot\nfirst\nsecond\n", logs[0].Line+logs[1].Line+logs[2].Line)
}
