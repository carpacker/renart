package service

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"github.com/fatih/color"
)

type directRunFormatting struct {
	startDate time.Time
	endDate   time.Time
}

type directRunSummary struct {
	results      []*scheduler.TaskExecutionResult
	failedAssets []string
	duration     time.Duration
}

var directRunTimePrinter = color.New(color.FgWhite, color.Faint).SprintfFunc()
var directRunFaintPrinter = color.New(color.Faint).SprintfFunc()
var directRunGreenPrinter = color.New(color.FgGreen).SprintfFunc()
var directRunRedPrinter = color.New(color.FgRed).SprintfFunc()

func directColorPrinter(attrs ...color.Attribute) func(format string, a ...interface{}) string {
	c := color.New(attrs...)
	c.EnableColor()
	return c.SprintfFunc()
}

func writeDirectRunAnalysis(w io.Writer, pl *pipeline.Pipeline, asset *pipeline.Asset) {
	if pl == nil || w == nil {
		return
	}

	_, _ = fmt.Fprintf(w, "Analyzed the pipeline '%s' with %d assets.\n", pl.Name, len(pl.Assets))
	if asset != nil {
		_, _ = fmt.Fprintf(w, "Running only the asset '%s'\n", asset.Name)
	}
}

func writeDirectRunWindow(w io.Writer, formatting directRunFormatting) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "\nInterval: %s - %s\n", formatting.startDate.Format(time.RFC3339), formatting.endDate.Format(time.RFC3339))
	_, _ = fmt.Fprint(w, "\nStarting the pipeline execution...\n\n")
}

func writeDirectRunLifecycle(w io.Writer, instance scheduler.TaskInstance, err error, running bool, duration time.Duration) {
	if w == nil || instance == nil {
		return
	}

	timestamp := directRunTimePrinter("[%s]", time.Now().Format("15:04:05"))
	if running {
		_, _ = fmt.Fprintf(w, "%s %s\n", timestamp, directRunFaintPrinter("Running:  %s", instance.GetHumanID()))
		return
	}

	status := "Finished"
	statusPrinter := directRunGreenPrinter
	if err != nil {
		status = "Failed"
		statusPrinter = directRunRedPrinter
	}
	durationSuffix := ""
	if duration > 0 {
		durationSuffix = directRunFaintPrinter(" (%s)", duration.Truncate(time.Millisecond).String())
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", timestamp, statusPrinter("%s: %s%s", status, instance.GetHumanID(), durationSuffix))
}

func buildDirectRunSummary(results []*scheduler.TaskExecutionResult, duration time.Duration) directRunSummary {
	summary := directRunSummary{results: results, duration: duration}
	seenFailed := make(map[string]struct{})
	for _, result := range results {
		if result == nil || result.Instance == nil || result.Error == nil {
			continue
		}
		assetName := result.Instance.GetAsset().Name
		if _, ok := seenFailed[assetName]; ok {
			continue
		}
		seenFailed[assetName] = struct{}{}
		summary.failedAssets = append(summary.failedAssets, assetName)
	}
	return summary
}

func writeDirectRunSummary(w io.Writer, summary directRunSummary) {
	if w == nil {
		return
	}

	_, _ = fmt.Fprint(w, "\n==================================================\n\n")
	mainSucceeded := 0
	for _, result := range summary.results {
		if result == nil || result.Instance == nil || result.Instance.GetType() != scheduler.TaskInstanceTypeMain {
			continue
		}
		status := "PASS"
		statusPrinter := directRunGreenPrinter
		if result.Error != nil {
			status = "FAIL"
			statusPrinter = directRunRedPrinter
		} else {
			mainSucceeded++
		}
		_, _ = fmt.Fprintf(w, "%s %s\n", statusPrinter(status), result.Instance.GetAsset().Name)
	}

	if len(summary.failedAssets) > 0 {
		_, _ = fmt.Fprintf(w, "\n\nbruin run completed with %s in %s\n\n", directRunRedPrinter("failures"), summary.duration.Truncate(time.Millisecond))
		_, _ = fmt.Fprintf(w, " %s Assets executed      %s\n", directRunRedPrinter("✗"), directRunRedPrinter("%d failed", len(summary.failedAssets)))
		_, _ = fmt.Fprintf(w, "%d assets failed\n", len(summary.failedAssets))
		for _, result := range summary.results {
			if result == nil || result.Instance == nil || result.Error == nil || result.Instance.GetType() != scheduler.TaskInstanceTypeMain {
				continue
			}
			_, _ = fmt.Fprintf(w, "└── %s\n", result.Instance.GetAsset().Name)
			for _, line := range strings.Split(strings.TrimSpace(result.Error.Error()), "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					_, _ = fmt.Fprintf(w, "└── %s\n", trimmed)
				}
			}
		}
		return
	}

	_, _ = fmt.Fprintf(w, "\n\nbruin run completed %s in %s\n\n", directRunGreenPrinter("successfully"), summary.duration.Truncate(time.Millisecond))
	_, _ = fmt.Fprintf(w, " %s Assets executed      %s\n", directRunGreenPrinter("✓"), directRunGreenPrinter("%d succeeded", mainSucceeded))
}
