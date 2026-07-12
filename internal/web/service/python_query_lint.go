package service

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/sqlintelligence"
)

type pythonQueryLiteral struct {
	SQL    string
	Line   int
	Column int
}

// pythonQueryDependencyFindings statically checks literal query("...") calls
// in Python assets. The runtime broker performs the same check for every
// query, including dynamic SQL; type-check intentionally stays best-effort and
// warn-only because it cannot evaluate Python expressions.
func pythonQueryDependencyFindings(asset *pipeline.Asset, pp *pipeline.Pipeline) []TypeCheckFinding {
	if asset == nil || pp == nil || !strings.Contains(strings.ToLower(string(asset.Type)), "python") {
		return nil
	}
	literals := pythonQueryStringLiterals(asset.ExecutableFile.Content)
	if len(literals) == 0 {
		return nil
	}

	known := make(map[string]string, len(pp.Assets))
	schema := sqlintelligence.Schema{}
	for _, candidate := range pp.Assets {
		if candidate == nil || strings.TrimSpace(candidate.Name) == "" {
			continue
		}
		ref := normalizePythonQueryRef(candidate.Name)
		known[ref] = candidate.Name
		schema[candidate.Name] = map[string]string{}
	}
	declared := make(map[string]struct{}, len(asset.Upstreams))
	for _, upstream := range asset.Upstreams {
		if upstream.Type == "asset" {
			declared[normalizePythonQueryRef(upstream.Value)] = struct{}{}
		}
	}
	self := normalizePythonQueryRef(asset.Name)
	seen := map[string]struct{}{}
	findings := make([]TypeCheckFinding, 0)

	for _, literal := range literals {
		parsed, err := sqlintelligence.ParseContextWithSchema(
			literal.SQL,
			"duckdb",
			schema,
			sqlintelligence.SchemaColumnSourceMethods{},
		)
		if err != nil {
			continue
		}
		for _, table := range parsed.Tables {
			name := table.ResolvedName
			if strings.TrimSpace(name) == "" {
				name = table.Name
			}
			ref := normalizePythonQueryRef(name)
			displayName, isAsset := known[ref]
			if !isAsset || ref == self {
				continue
			}
			if _, ok := declared[ref]; ok {
				continue
			}
			if _, duplicate := seen[ref]; duplicate {
				continue
			}
			seen[ref] = struct{}{}
			findings = append(findings, TypeCheckFinding{
				Severity: typeCheckSeverityWarning,
				Message: "Reads asset " + displayName + " through renart.query() without declaring it in depends; " +
					"run ordering is only guaranteed for declared dependencies.",
				Line:      literal.Line,
				Column:    literal.Column,
				EndLine:   literal.Line,
				EndColumn: literal.Column + len("query"),
			})
		}
	}
	return findings
}

func normalizePythonQueryRef(ref string) string {
	cleaned := strings.ToLower(strings.TrimSpace(ref))
	cleaned = strings.NewReplacer(`"`, "", "`", "").Replace(cleaned)
	parts := strings.Split(cleaned, ".")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, ".")
}

// pythonQueryStringLiterals extracts the first argument of direct query(...)
// and renart.query(...) calls when it is a plain string literal. Comments,
// strings containing query-like text, f-strings, and dynamic expressions are
// ignored.
func pythonQueryStringLiterals(source string) []pythonQueryLiteral {
	result := make([]pythonQueryLiteral, 0)
	for i := 0; i < len(source); {
		switch source[i] {
		case '#':
			if newline := strings.IndexByte(source[i:], '\n'); newline >= 0 {
				i += newline + 1
			} else {
				return result
			}
			continue
		case '\'', '"':
			_, end := readPythonString(source, i, false)
			if end <= i {
				return result
			}
			i = end
			continue
		}

		if !isPythonIdentifierStart(rune(source[i])) {
			i++
			continue
		}
		start := i
		i++
		for i < len(source) && isPythonIdentifierContinue(rune(source[i])) {
			i++
		}
		if source[start:i] != "query" {
			continue
		}

		literal, end, ok := parsePythonQueryLiteral(source, start, i)
		if !ok {
			continue
		}
		result = append(result, literal)
		i = end
	}
	return result
}

func parsePythonQueryLiteral(source string, callStart, afterName int) (pythonQueryLiteral, int, bool) {
	i := skipPythonWhitespace(source, afterName)
	if i >= len(source) || source[i] != '(' {
		return pythonQueryLiteral{}, afterName, false
	}
	i = skipPythonWhitespace(source, i+1)
	prefixStart := i
	for i < len(source) && strings.ContainsRune("rRuUbBfF", rune(source[i])) {
		i++
	}
	prefix := strings.ToLower(source[prefixStart:i])
	if strings.Contains(prefix, "f") || strings.Contains(prefix, "b") {
		return pythonQueryLiteral{}, afterName, false
	}
	if i >= len(source) || (source[i] != '\'' && source[i] != '"') {
		return pythonQueryLiteral{}, afterName, false
	}
	quoteStart := i
	body, end := readPythonString(source, quoteStart, strings.Contains(prefix, "r"))
	if end <= quoteStart {
		return pythonQueryLiteral{}, afterName, false
	}
	line, column := pythonLineColumn(source, callStart)
	return pythonQueryLiteral{SQL: body, Line: line, Column: column}, end, true
}

func readPythonString(source string, quoteStart int, raw bool) (string, int) {
	if quoteStart >= len(source) || (source[quoteStart] != '\'' && source[quoteStart] != '"') {
		return "", quoteStart
	}
	quote := source[quoteStart]
	triple := quoteStart+2 < len(source) && source[quoteStart+1] == quote && source[quoteStart+2] == quote
	delimiterLen := 1
	if triple {
		delimiterLen = 3
	}
	bodyStart := quoteStart + delimiterLen
	for i := bodyStart; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		if source[i] != quote {
			continue
		}
		if triple {
			if i+2 >= len(source) || source[i+1] != quote || source[i+2] != quote {
				continue
			}
			return source[bodyStart:i], i + 3
		}
		body := source[bodyStart:i]
		if !raw {
			if decoded, err := strconv.Unquote(string(quote) + body + string(quote)); err == nil {
				body = decoded
			}
		}
		return body, i + 1
	}
	return "", quoteStart
}

func skipPythonWhitespace(source string, i int) int {
	for i < len(source) && unicode.IsSpace(rune(source[i])) {
		i++
	}
	return i
}

func isPythonIdentifierStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isPythonIdentifierContinue(r rune) bool {
	return isPythonIdentifierStart(r) || unicode.IsDigit(r)
}

func pythonLineColumn(source string, offset int) (int, int) {
	line := 1 + strings.Count(source[:offset], "\n")
	lastNewline := strings.LastIndex(source[:offset], "\n")
	return line, offset - lastNewline
}
