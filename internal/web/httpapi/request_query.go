package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// decodeStrictQuery parses a query once, rejects malformed encoding, unknown
// keys, and repeated values. Behavior-changing inputs must never be silently
// treated as omitted because a client misspelled a key or supplied two values.
func decodeStrictQuery(r *http.Request, allowedKeys ...string) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}

	unknown := make([]string, 0)
	for key, entries := range values {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
			continue
		}
		if len(entries) != 1 {
			return nil, fmt.Errorf("query parameter %q must be provided exactly once", key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown query parameter(s): %s", strings.Join(unknown, ", "))
	}
	return values, nil
}

func strictQueryBool(values url.Values, key string) (bool, error) {
	entries, found := values[key]
	if !found {
		return false, nil
	}
	// decodeStrictQuery has already enforced exactly one value.
	switch entries[0] {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("query parameter %q must be true or false", key)
	}
}
