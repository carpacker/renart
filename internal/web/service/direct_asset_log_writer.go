package service

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/fatih/color"
)

var directAssetLogPalette = []color.Attribute{
	color.FgBlue,
	color.FgMagenta,
	color.FgCyan,
	color.FgWhite,
	color.FgGreen,
	color.FgYellow,
}

// directAssetLogWriter is the task-local writer used by every direct runner.
// It buffers incomplete lines so every emitted line gets exactly one timestamp,
// one colored asset label, and one child-output marker even when Sling writes a
// multiline or arbitrarily split byte chunk.
type directAssetLogWriter struct {
	mu           sync.Mutex
	output       io.Writer
	assetName    string
	assetPrinter func(format string, a ...interface{}) string
	now          func() time.Time
	pending      []byte
}

func newDirectAssetLogWriter(output io.Writer, pl *pipeline.Pipeline, asset *pipeline.Asset) *directAssetLogWriter {
	assetName := "asset"
	if asset != nil && asset.Name != "" {
		assetName = asset.Name
	}
	index := directAssetColorIndex(pl, asset)
	return &directAssetLogWriter{
		output:       output,
		assetName:    assetName,
		assetPrinter: directColorPrinter(directAssetLogPalette[index%len(directAssetLogPalette)]),
		now:          time.Now,
	}
}

func directAssetColorIndex(pl *pipeline.Pipeline, target *pipeline.Asset) int {
	if pl == nil || target == nil {
		return 0
	}
	for index, asset := range pl.Assets {
		if asset == target || (asset != nil && asset.Name == target.Name) {
			return index
		}
	}
	return 0
}

func (w *directAssetLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline < 0 {
			break
		}
		line := w.pending[:newline+1]
		if err := w.writeLine(line); err != nil {
			return 0, err
		}
		w.pending = w.pending[newline+1:]
	}
	return len(p), nil
}

func (w *directAssetLogWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return nil
	}
	if err := w.writeLine(w.pending); err != nil {
		return err
	}
	w.pending = nil
	return nil
}

func (w *directAssetLogWriter) writeLine(line []byte) error {
	if w.output == nil {
		return nil
	}
	line = trimNestedSlingTimestamp(line)

	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	timestamp := directColorPrinter(color.FgWhite, color.Faint)("[%s]", now.Format("15:04:05"))
	asset := w.assetPrinter("[%s]", w.assetName)
	marker := ">> "
	if bytes.HasPrefix(line, []byte(marker)) {
		marker = ""
	}
	prefix := fmt.Sprintf("%s %s %s", timestamp, asset, marker)
	formatted := make([]byte, 0, len(prefix)+len(line))
	formatted = append(formatted, prefix...)
	formatted = append(formatted, line...)
	return writeAll(w.output, formatted)
}

// Sling prefixes its own structured lines with a 12-hour timestamp and a
// three-letter level. Renart already adds the canonical task timestamp, so
// keeping Sling's timestamp would produce lines such as
// "[12:59:52] [asset] >> 12:59PM INF ...". Strip only that distinctive
// timestamp+level shape and retain the level and message.
func trimNestedSlingTimestamp(line []byte) []byte {
	separator := bytes.IndexByte(line, ' ')
	if separator < 0 {
		return line
	}
	if _, err := time.Parse("3:04PM", string(stripANSISGRToken(line[:separator]))); err != nil {
		return line
	}

	remainder := line[separator+1:]
	levelEnd := bytes.IndexByte(remainder, ' ')
	if levelEnd < 0 {
		return line
	}
	switch string(stripANSISGRToken(remainder[:levelEnd])) {
	case "TRC", "DBG", "INF", "WRN", "ERR", "FTL":
		return remainder
	default:
		return line
	}
}

func stripANSISGRToken(token []byte) []byte {
	plain := make([]byte, 0, len(token))
	for index := 0; index < len(token); index++ {
		if token[index] == '\x1b' && index+1 < len(token) && token[index+1] == '[' {
			end := index + 2
			for end < len(token) && ((token[end] >= '0' && token[end] <= '9') || token[end] == ';') {
				end++
			}
			if end < len(token) && token[end] == 'm' {
				index = end
				continue
			}
		}
		plain = append(plain, token[index])
	}
	return plain
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

var _ io.Writer = (*directAssetLogWriter)(nil)
