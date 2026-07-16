package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type ExecutionTimeWindow struct {
	Start time.Time
	End   time.Time
}

func (w ExecutionTimeWindow) IsZero() bool {
	return w.Start.IsZero() || w.End.IsZero()
}

func (w ExecutionTimeWindow) StartRFC3339() string {
	if w.Start.IsZero() {
		return ""
	}
	return w.Start.UTC().Format(time.RFC3339Nano)
}

func (w ExecutionTimeWindow) EndRFC3339() string {
	if w.End.IsZero() {
		return ""
	}
	return w.End.UTC().Format(time.RFC3339Nano)
}

func ResolveExecutionTimeWindow(schedule, start, end string, now time.Time) (ExecutionTimeWindow, error) {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start != "" || end != "" {
		if start == "" || end == "" {
			return ExecutionTimeWindow{}, fmt.Errorf("both start and end must be provided")
		}
		parsedStart, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return ExecutionTimeWindow{}, fmt.Errorf("invalid start time: %w", err)
		}
		parsedEnd, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return ExecutionTimeWindow{}, fmt.Errorf("invalid end time: %w", err)
		}
		if !parsedEnd.After(parsedStart) {
			return ExecutionTimeWindow{}, fmt.Errorf("end time must be after start time")
		}
		return ExecutionTimeWindow{Start: parsedStart.UTC(), End: parsedEnd.UTC()}, nil
	}

	return DefaultExecutionTimeWindow(schedule, now)
}

func DefaultExecutionTimeWindow(schedule string, now time.Time) (ExecutionTimeWindow, error) {
	now = now.UTC()
	schedule = normalizeExecutionSchedule(schedule)
	switch schedule {
	case "@hourly":
		end := now.Truncate(time.Hour)
		return ExecutionTimeWindow{Start: end.Add(-time.Hour), End: end}, nil
	case "@daily":
		end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return ExecutionTimeWindow{Start: end.AddDate(0, 0, -1), End: end}, nil
	}

	parsed, err := cron.ParseStandard(schedule)
	if err != nil {
		return DefaultExecutionTimeWindow("@daily", now)
	}

	lookback := now.AddDate(-5, 0, 0)
	previous := parsed.Next(lookback)
	if previous.After(now) {
		return DefaultExecutionTimeWindow("@daily", now)
	}

	var beforePrevious time.Time
	for i := 0; i < 200000; i++ {
		next := parsed.Next(previous)
		if next.After(now) {
			if beforePrevious.IsZero() {
				return DefaultExecutionTimeWindow("@daily", now)
			}
			return ExecutionTimeWindow{Start: beforePrevious.UTC(), End: previous.UTC()}, nil
		}
		beforePrevious = previous
		previous = next
	}

	return ExecutionTimeWindow{}, fmt.Errorf("schedule %q is too frequent to resolve", schedule)
}

func normalizeExecutionSchedule(schedule string) string {
	schedule = strings.TrimSpace(strings.ToLower(schedule))
	switch schedule {
	case "", "daily":
		return "@daily"
	case "hourly":
		return "@hourly"
	default:
		return schedule
	}
}
