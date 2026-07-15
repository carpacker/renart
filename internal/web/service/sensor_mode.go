package service

import "strings"

const (
	sensorModeSkip = "skip"
	sensorModeOnce = "once"
	sensorModeWait = "wait"
)

// effectiveSensorMode keeps interactive runs bounded while scheduled runs
// retain Bruin's dependency-gate behavior and wait until a sensor succeeds or
// reaches its configured timeout.
func effectiveSensorMode(requested string, scheduled bool) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case sensorModeSkip:
		return sensorModeSkip
	case sensorModeOnce:
		return sensorModeOnce
	case sensorModeWait:
		return sensorModeWait
	}
	if scheduled {
		return sensorModeWait
	}
	return sensorModeOnce
}
