package profiling

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type Trace struct {
	enabled bool
	name    string
	started time.Time
	last    time.Time
	parts   []string
}

func New(envName, name string) *Trace {
	if !envEnabled(envName) {
		return &Trace{}
	}
	now := time.Now()
	return &Trace{enabled: true, name: name, started: now, last: now}
}

func (t *Trace) Enabled() bool {
	return t != nil && t.enabled
}

func (t *Trace) Step(name string) {
	if !t.Enabled() {
		return
	}
	now := time.Now()
	t.parts = append(t.parts, fmt.Sprintf("%s=%s", name, roundDuration(now.Sub(t.last))))
	t.last = now
}

func (t *Trace) Done(fields ...string) {
	if !t.Enabled() {
		return
	}
	parts := append([]string{fmt.Sprintf("total=%s", roundDuration(time.Since(t.started)))}, t.parts...)
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			parts = append(parts, field)
		}
	}
	log.Printf("[profile] %s %s", t.name, strings.Join(parts, " "))
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value != "" && value != "0" && value != "false" && value != "no" && value != "off"
}

func roundDuration(duration time.Duration) time.Duration {
	if duration >= time.Second {
		return duration.Round(time.Millisecond)
	}
	return duration.Round(time.Microsecond)
}
