package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// decodeStrictJSONObject accepts exactly one non-null JSON object and rejects
// unknown fields. Behavior-changing request bodies use this instead of letting
// malformed input silently degrade to zero-value defaults.
func decodeStrictJSONObject[T any](reader io.Reader) (T, error) {
	var zero T
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var decoded *T
	if err := decoder.Decode(&decoded); err != nil {
		return zero, err
	}
	if decoded == nil {
		return zero, errors.New("request body must be a JSON object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("request body must contain a single JSON object")
		}
		return zero, fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return *decoded, nil
}
