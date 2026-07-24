package cmd

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/mattn/go-isatty"
)

type fileDescriptorWriter interface {
	io.Writer
	Fd() uintptr
}

// renderOutputSupportsColor keeps the human terminal view colorful without
// contaminating redirected output. JSON bypasses the human renderer entirely.
func renderOutputSupportsColor(w io.Writer) bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	forceColor := strings.TrimSpace(os.Getenv("CLICOLOR_FORCE"))
	forced := forceColor != "" && forceColor != "0"
	if os.Getenv("CLICOLOR") == "0" && !forced {
		return false
	}
	terminal, ok := w.(fileDescriptorWriter)
	if !ok {
		return false
	}
	if forced {
		return true
	}
	return isatty.IsTerminal(terminal.Fd()) || isatty.IsCygwinTerminal(terminal.Fd())
}

func renderHighlightStyle(_ io.Writer) string {
	if strings.Contains(strings.ToLower(os.Getenv("TERM_BACKGROUND")), "light") {
		return "github"
	}
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	if len(parts) > 1 {
		background, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
		if err == nil && ansiBackgroundIsLight(background) {
			return "github"
		}
	}
	return "monokai"
}

func ansiBackgroundIsLight(color int) bool {
	switch color {
	case 3, 6, 7, 9, 10, 11, 12, 13, 14, 15:
		return true
	default:
		return false
	}
}

func highlightRenderContent(content, language, style string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if strings.TrimSpace(content) == "" || language == "" || strings.TrimSpace(style) == "" {
		return content
	}
	switch language {
	case "sql", "json", "python", "yaml":
	default:
		return content
	}
	var highlighted strings.Builder
	if err := quick.Highlight(&highlighted, content, language, "terminal16m", style); err != nil {
		return content
	}
	return highlighted.String()
}
