package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/scheduler"
)

func TestPipelineServiceUpdatesScheduleFieldsInPipelineYAML(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineDir := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(pipelineDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: analytics\nschedule: '@daily'\n"), 0o644))

	service := NewPipelineService(workspaceRoot)
	pipelineID := EncodeID("analytics")
	_, updated, err := service.UpdateSchedule(context.Background(), pipelineID, scheduler.UpdateScheduleRequest{Enabled: true, Schedule: "@hourly", Timezone: "Europe/Berlin", Catchup: true})
	require.NoError(t, err)
	assert.Equal(t, "@hourly", updated.Schedule)
	assert.Equal(t, "Europe/Berlin", updated.Timezone)
	assert.True(t, updated.Catchup)

	bytes, err := os.ReadFile(filepath.Join(pipelineDir, "pipeline.yml"))
	require.NoError(t, err)
	content := string(bytes)
	assert.Contains(t, content, "schedule: '@hourly'")
	assert.Contains(t, content, "timezone: Europe/Berlin")
	assert.Contains(t, content, "catchup: true")

	_, updated, err = service.UpdateSchedule(context.Background(), pipelineID, scheduler.UpdateScheduleRequest{Enabled: false, Timezone: "Europe/Berlin"})
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
	bytes, err = os.ReadFile(filepath.Join(pipelineDir, "pipeline.yml"))
	require.NoError(t, err)
	assert.NotContains(t, strings.Split(string(bytes), "\n"), "schedule: '@hourly'")
}
