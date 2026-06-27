package notebook

import (
	"fmt"
	"strconv"
	"strings"
)

// Comment-directive family. `-- @word(args)` is a single reserved namespace:
// one parser, one set of diagnostics, many directives. Known members:
//   @viz(...)          presentation (this file)
//   @materialize(table) cell pinning (run.go)
//   @param(...)         notebook variables (parked)
//
// RULE (keep this here so it survives): every directive is a comment and is
// therefore OUTSIDE the fingerprint by construction (CanonicalSQL strips
// comments). Anything that should affect execution semantics — and thus
// staleness — must go into asset config, never a directive.

// VizKind enumerates the v1 chart kinds.
var vizKinds = map[string]bool{
	"table": true, "bar": true, "line": true, "area": true, "pie": true, "kpi": true,
}

// VizDirective is the typed result of parsing a cell's @viz line.
type VizDirective struct {
	Kind    string         `json:"kind"`
	Options map[string]any `json:"options"`
}

// VizDiagnostic is a parse or validation finding with a span into the cell
// content (1-based line, 0-based columns), for a Monaco squiggle.
type VizDiagnostic struct {
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" | "warning"
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	EndCol   int    `json:"end_col"`
}

// requiredVizKeys lists the keys each kind must declare.
var requiredVizKeys = map[string][]string{
	"bar":  {"x", "y"},
	"line": {"x", "y"},
	"area": {"x", "y"},
	"pie":  {"x", "y"},
	"kpi":  {"value"},
}

// knownVizKeys lists every accepted key per kind (required + optional).
var knownVizKeys = map[string]map[string]bool{
	"table": {"limit": true, "columns": true},
	"bar":   {"x": true, "y": true, "stacked": true, "format": true, "sort": true},
	"line":  {"x": true, "y": true, "stacked": true, "format": true, "sort": true},
	"area":  {"x": true, "y": true, "stacked": true, "format": true, "sort": true},
	"pie":   {"x": true, "y": true, "limit": true},
	"kpi":   {"value": true, "format": true, "icon": true, "compare": true},
}

// ParseViz extracts the first @viz directive from a cell's content and
// returns the typed config plus diagnostics. A nil directive with no
// error-severity diagnostics means the cell simply has no @viz line (it
// renders as a plain table). Later @viz lines produce a duplicate warning.
func ParseViz(content string) (*VizDirective, []VizDiagnostic) {
	diagnostics := make([]VizDiagnostic, 0)
	var directive *VizDirective

	for lineIndex, line := range strings.Split(content, "\n") {
		col := indexOfViz(line)
		if col < 0 {
			continue
		}
		if directive != nil {
			diagnostics = append(diagnostics, VizDiagnostic{
				Message:  "duplicate @viz directive ignored; the first one wins",
				Severity: "warning",
				Line:     lineIndex + 1,
				Col:      col,
				EndCol:   len(line),
			})
			continue
		}

		parsed, lineDiagnostics := parseVizLine(line, col, lineIndex+1)
		diagnostics = append(diagnostics, lineDiagnostics...)
		if parsed != nil {
			directive = parsed
		}
	}

	return directive, diagnostics
}

// indexOfViz returns the column where `@viz(` begins inside a comment on this
// line, or -1. Both `-- @viz(...)` line comments and `/* @viz(...) */` block
// comments are recognized: the SQL formatter rewrites line comments into block
// comments (often inline before the SELECT), so the directive must be found in
// either form to survive a format.
func indexOfViz(line string) int {
	idx := strings.Index(line, "@viz(")
	if idx < 0 {
		return -1
	}
	prefix := line[:idx]
	// Inside an open block comment: the nearest `/*` before @viz is not yet
	// closed on this line.
	if open := strings.LastIndex(prefix, "/*"); open >= 0 && !strings.Contains(prefix[open:], "*/") {
		return idx
	}
	// Line comment: the line begins with `--`.
	if strings.HasPrefix(strings.TrimSpace(line), "--") {
		return idx
	}
	return -1
}

// matchingCloseParen returns the index of the `)` that closes the `(` at
// openIdx, honoring string literals, or -1. Used instead of "last )" so a
// directive that shares its line with SQL (e.g. after a format:
// `/* @viz(bar, x: a) */ select count(*) ...`) still parses correctly.
func matchingCloseParen(s string, openIdx int) int {
	depth := 0
	var inString byte
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		switch {
		case inString != 0:
			if c == inString {
				inString = 0
			}
		case c == '\'' || c == '"':
			inString = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseVizLine(line string, col, lineNumber int) (*VizDirective, []VizDiagnostic) {
	diagnostics := make([]VizDiagnostic, 0)
	open := strings.Index(line[col:], "(")
	closeParen := -1
	if open >= 0 {
		closeParen = matchingCloseParen(line, col+open)
	}
	if open < 0 || closeParen < 0 {
		diagnostics = append(diagnostics, VizDiagnostic{
			Message: "malformed @viz directive: expected @viz(kind, ...)", Severity: "error",
			Line: lineNumber, Col: col, EndCol: len(line),
		})
		return nil, diagnostics
	}
	inner := line[col+open+1 : closeParen]

	parts := splitDirectiveArgs(inner)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		diagnostics = append(diagnostics, VizDiagnostic{
			Message: "@viz requires a kind: table, bar, line, area, pie, or kpi", Severity: "error",
			Line: lineNumber, Col: col, EndCol: len(line),
		})
		return nil, diagnostics
	}

	kind := strings.TrimSpace(parts[0])
	if !vizKinds[kind] {
		diagnostics = append(diagnostics, VizDiagnostic{
			Message: fmt.Sprintf("unknown chart kind %q; expected table, bar, line, area, pie, or kpi", kind),
			Severity: "error", Line: lineNumber, Col: col, EndCol: len(line),
		})
		return nil, diagnostics
	}

	options := map[string]any{}
	for _, pair := range parts[1:] {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		colon := strings.Index(pair, ":")
		if colon < 0 {
			diagnostics = append(diagnostics, VizDiagnostic{
				Message: fmt.Sprintf("expected key:value, got %q", pair), Severity: "error",
				Line: lineNumber, Col: col, EndCol: len(line),
			})
			continue
		}
		key := strings.TrimSpace(pair[:colon])
		value := strings.TrimSpace(pair[colon+1:])
		if known := knownVizKeys[kind]; known != nil && !known[key] {
			diagnostics = append(diagnostics, VizDiagnostic{
				Message: fmt.Sprintf("unknown key %q for %s charts", key, kind), Severity: "warning",
				Line: lineNumber, Col: col, EndCol: len(line),
			})
		}
		options[key] = parseVizValue(value)
	}

	for _, required := range requiredVizKeys[kind] {
		if _, ok := options[required]; !ok {
			diagnostics = append(diagnostics, VizDiagnostic{
				Message: fmt.Sprintf("%s charts require %q", kind, required), Severity: "error",
				Line: lineNumber, Col: col, EndCol: len(line),
			})
		}
	}

	return &VizDirective{Kind: kind, Options: options}, diagnostics
}

// splitDirectiveArgs splits on commas that are not inside [...] or quotes.
func splitDirectiveArgs(inner string) []string {
	parts := make([]string, 0)
	depth := 0
	inString := byte(0)
	var current strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case inString != 0:
			current.WriteByte(c)
			if c == inString {
				inString = 0
			}
		case c == '\'' || c == '"':
			inString = c
			current.WriteByte(c)
		case c == '[':
			depth++
			current.WriteByte(c)
		case c == ']':
			depth--
			current.WriteByte(c)
		case c == ',' && depth == 0:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// parseVizValue coerces a raw value token into string | float64 | bool |
// []string.
func parseVizValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		items := splitDirectiveArgs(raw[1 : len(raw)-1])
		list := make([]string, 0, len(items))
		for _, item := range items {
			list = append(list, unquote(strings.TrimSpace(item)))
		}
		return list
	}
	if raw == "true" {
		return true
	}
	if raw == "false" {
		return false
	}
	if (strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'")) ||
		(strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"")) {
		return unquote(raw)
	}
	if number, err := strconv.ParseFloat(raw, 64); err == nil {
		return number
	}
	return raw
}

func unquote(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// ValidateVizColumns checks the directive's referenced columns against the
// actual result columns, returning warning diagnostics for any mismatch.
// lineNumber is where the @viz directive sits in the cell.
func ValidateVizColumns(directive *VizDirective, columns []string, lineNumber int) []VizDiagnostic {
	if directive == nil {
		return nil
	}
	present := make(map[string]bool, len(columns))
	for _, column := range columns {
		present[strings.ToLower(column)] = true
	}

	diagnostics := make([]VizDiagnostic, 0)
	check := func(key string) {
		value, ok := directive.Options[key]
		if !ok {
			return
		}
		for _, column := range vizColumnRefs(value) {
			if !present[strings.ToLower(column)] {
				diagnostics = append(diagnostics, VizDiagnostic{
					Message:  fmt.Sprintf("column %q not in result", column),
					Severity: "warning",
					Line:     lineNumber,
				})
			}
		}
	}
	for _, key := range []string{"x", "y", "value", "compare"} {
		check(key)
	}
	return diagnostics
}

func vizColumnRefs(value any) []string {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		return v
	default:
		return nil
	}
}

// VizDirectiveLine renders a directive back into its `/* @viz(...) */` text
// form (deterministic key order: kind, then x, y, value, then the rest
// sorted). A block comment is used — not `--` — because the SQL formatter
// rewrites line comments into block comments; emitting the formatter's own
// form keeps the directive stable across format round-trips.
func VizDirectiveLine(directive *VizDirective) string {
	if directive == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("/* @viz(")
	builder.WriteString(directive.Kind)

	ordered := []string{"x", "y", "value", "compare", "limit", "columns", "stacked", "format", "sort", "icon"}
	written := map[string]bool{}
	writePair := func(key string) {
		value, ok := directive.Options[key]
		if !ok || written[key] {
			return
		}
		written[key] = true
		builder.WriteString(", ")
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(formatVizValue(value))
	}
	for _, key := range ordered {
		writePair(key)
	}
	for key := range directive.Options {
		writePair(key)
	}
	builder.WriteString(") */")
	return builder.String()
}

func formatVizValue(value any) string {
	switch v := value.(type) {
	case []string:
		quoted := make([]string, len(v))
		for i, item := range v {
			quoted[i] = item
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
