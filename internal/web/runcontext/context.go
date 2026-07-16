package runcontext

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SensorModeOnce = "once"
	SensorModeWait = "wait"
	SensorModeSkip = "skip"
)

// Input contains the execution context whose meaning must be identical across
// inline HTTP runs and queued scheduler runs. Empty start/end and sensor mode
// values mean that the caller deliberately requested the server defaults.
type Input struct {
	Start       string
	End         string
	FullRefresh bool
	Backfill    bool
	SensorMode  string
}

// Normalized is safe to persist or pass to an executor. Explicit timestamps
// are represented in UTC; omitted windows retain nil bounds.
type Normalized struct {
	Start      *time.Time
	End        *time.Time
	SensorMode string
}

func (n Normalized) StartString() string {
	if n.Start == nil {
		return ""
	}
	return n.Start.UTC().Format(time.RFC3339Nano)
}

func (n Normalized) EndString() string {
	if n.End == nil {
		return ""
	}
	return n.End.UTC().Format(time.RFC3339Nano)
}

// Normalize rejects combinations that would otherwise be silently downgraded
// by one of Renart's execution paths.
func Normalize(input Input) (Normalized, error) {
	if input.FullRefresh && input.Backfill {
		return Normalized{}, errors.New("full_refresh and backfill are mutually exclusive")
	}

	start, end, err := NormalizeWindow(input.Start, input.End)
	if err != nil {
		return Normalized{}, err
	}
	if input.Backfill && start == nil {
		return Normalized{}, errors.New("backfill requires an explicit start and end")
	}

	sensorMode, err := NormalizeSensorMode(input.SensorMode)
	if err != nil {
		return Normalized{}, err
	}
	return Normalized{Start: start, End: end, SensorMode: sensorMode}, nil
}

// ValidateDryRun rejects context that the current validation-only executor
// cannot preserve. Accepting these fields would validate a different render
// window or mode while claiming to validate the request the caller supplied.
func ValidateDryRun(dryRun bool, input Input) error {
	if !dryRun {
		return nil
	}

	unsupported := make([]string, 0, 4)
	if strings.TrimSpace(input.Start) != "" || strings.TrimSpace(input.End) != "" {
		unsupported = append(unsupported, "start/end")
	}
	if input.FullRefresh {
		unsupported = append(unsupported, "full_refresh")
	}
	if input.Backfill {
		unsupported = append(unsupported, "backfill")
	}
	if strings.TrimSpace(input.SensorMode) != "" {
		unsupported = append(unsupported, "sensor_mode")
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("dry_run does not support execution context fields: %s", strings.Join(unsupported, ", "))
	}
	return nil
}

func NormalizeWindow(startValue, endValue string) (*time.Time, *time.Time, error) {
	startValue = strings.TrimSpace(startValue)
	endValue = strings.TrimSpace(endValue)
	if (startValue == "") != (endValue == "") {
		return nil, nil, errors.New("start and end must be provided together")
	}
	if startValue == "" {
		return nil, nil, nil
	}

	start, err := time.Parse(time.RFC3339Nano, startValue)
	if err != nil {
		return nil, nil, fmt.Errorf("start must be an RFC3339 timestamp: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, endValue)
	if err != nil {
		return nil, nil, fmt.Errorf("end must be an RFC3339 timestamp: %w", err)
	}
	if !start.Before(end) {
		return nil, nil, errors.New("start must be before end")
	}

	start = start.UTC()
	end = end.UTC()
	return &start, &end, nil
}

func NormalizeSensorMode(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "", SensorModeOnce, SensorModeWait, SensorModeSkip:
		return value, nil
	default:
		return "", fmt.Errorf("invalid sensor_mode %q: expected once, wait, or skip", value)
	}
}
