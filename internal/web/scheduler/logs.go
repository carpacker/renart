package scheduler

import "strings"

// trimLegacyOutputReplay hides the aggregate output record written by older
// scheduler builds after they had already persisted the same streamed chunks.
// Requiring at least two preceding chunks avoids treating an ordinary repeated
// line as the legacy replay shape.
func trimLegacyOutputReplay(logs []LogLine) []LogLine {
	if len(logs) < 3 {
		return logs
	}

	replay := logs[len(logs)-1].Line
	if replay == "" {
		return logs
	}

	suffixLength := 0
	for start := len(logs) - 2; start >= 0; start-- {
		suffixLength += len(logs[start].Line)
		if suffixLength < len(replay) {
			continue
		}
		if suffixLength > len(replay) || len(logs)-1-start < 2 {
			return logs
		}

		var suffix strings.Builder
		suffix.Grow(suffixLength)
		for _, log := range logs[start : len(logs)-1] {
			suffix.WriteString(log.Line)
		}
		if suffix.String() == replay {
			return logs[:len(logs)-1]
		}
		return logs
	}

	return logs
}
