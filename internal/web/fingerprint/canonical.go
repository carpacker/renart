package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// hashHex returns the sha256 of all parts, length-prefixed so concatenation
// ambiguity cannot collide ("ab"+"c" vs "a"+"bc").
func hashHex(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var lenBuf [8]byte
		n := len(part)
		for i := 7; i >= 0; i-- {
			lenBuf[i] = byte(n)
			n >>= 8
		}
		h.Write(lenBuf[:])
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CanonicalSQL strips comments and collapses whitespace so formatting-only
// edits do not change the fingerprint. It is the cheap base layer (and the
// fallback when the SQL formatter cannot parse a statement); the engine
// additionally runs SQL through the embedded formatter so
// formatter-induced rewrites (keyword casing, trailing commas, layout)
// do not change fingerprints either. Deliberately conservative beyond
// that: no identifier case folding (too risky across dialects). String
// literals are preserved verbatim, including comment-like sequences
// inside them.
func CanonicalSQL(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))

	const (
		code = iota
		singleQuote
		doubleQuote
		backtick
		lineComment
		blockComment
	)
	state := code
	pendingSpace := false
	wroteAny := false

	writeRune := func(r rune) {
		if pendingSpace && wroteAny {
			out.WriteByte(' ')
		}
		pendingSpace = false
		wroteAny = true
		out.WriteRune(r)
	}

	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		switch state {
		case code:
			switch {
			case r == '-' && next == '-':
				state = lineComment
				i++
			case r == '/' && next == '*':
				state = blockComment
				i++
			case r == '\'':
				state = singleQuote
				writeRune(r)
			case r == '"':
				state = doubleQuote
				writeRune(r)
			case r == '`':
				state = backtick
				writeRune(r)
			case r == ' ' || r == '\t' || r == '\n' || r == '\r':
				pendingSpace = true
			default:
				writeRune(r)
			}
		case singleQuote:
			writeRune(r)
			if r == '\'' {
				if next == '\'' {
					writeRune(next)
					i++
				} else {
					state = code
				}
			}
		case doubleQuote:
			writeRune(r)
			if r == '"' {
				state = code
			}
		case backtick:
			writeRune(r)
			if r == '`' {
				state = code
			}
		case lineComment:
			if r == '\n' {
				state = code
				pendingSpace = true
			}
		case blockComment:
			if r == '*' && next == '/' {
				state = code
				pendingSpace = true
				i++
			}
		}
	}

	return out.String()
}

// varReferencePattern matches `var.NAME` Jinja variable reads. Renart
// records the referenced names textually rather than instrumenting the
// renderer; this over-approximates (a name in dead template branches still
// counts) which over-invalidates — the safe direction.
var varReferencePattern = regexp.MustCompile(`\bvar\.([A-Za-z_][A-Za-z0-9_]*)`)

// ConsumedVarNames returns the sorted set of declared variable names the
// content references.
func ConsumedVarNames(content string, declared map[string]struct{}) []string {
	seen := make(map[string]struct{})
	for _, match := range varReferencePattern.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if _, ok := declared[name]; ok {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// canonicalJSON marshals a value deterministically (encoding/json sorts map
// keys; struct fields keep declaration order).
func canonicalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "marshal-error:" + err.Error()
	}
	return string(encoded)
}

// VarsHash hashes the (name, value) pairs of the given variable names. The
// caller passes only the consumed names so an unrelated variable flip leaves
// the hash unchanged.
func VarsHash(vars Vars, names []string) string {
	subset := make(map[string]any, len(names))
	for _, name := range names {
		if value, ok := vars[name]; ok {
			subset[name] = value
		}
	}
	return hashHex(Version, canonicalJSON(subset))
}

// AllVarsHash hashes every variable; used as the coverage-table selection
// key and for Python assets (which assume all vars are consumed in v1).
func AllVarsHash(vars Vars) string {
	return hashHex(Version, canonicalJSON(map[string]any(vars)))
}
