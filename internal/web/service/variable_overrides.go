package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/xeipuuv/gojsonschema"
)

const maxVariableOverridesBytes = 256 << 10

// ErrInvalidVariableOverrides identifies schedule/run inputs that cannot be
// applied to the exact pipeline source. Error messages deliberately describe
// names and schema failures without echoing resolved values.
var ErrInvalidVariableOverrides = errors.New("invalid variable overrides")

type variableOverridesError struct {
	message string
}

func (e *variableOverridesError) Error() string {
	if e == nil || strings.TrimSpace(e.message) == "" {
		return ErrInvalidVariableOverrides.Error()
	}
	return e.message
}

func (e *variableOverridesError) Unwrap() error { return ErrInvalidVariableOverrides }

// normalizeVariableOverrides creates a JSON-only, request-owned value graph.
// JSON integers are restored to int64 where possible so schedule values parsed
// by net/http behave like Bruin CLI variable values and retain integer schema
// semantics through planning, rendering, execution, and fingerprints.
func normalizeVariableOverrides(overrides map[string]any) (map[string]any, error) {
	if len(overrides) == 0 {
		return nil, nil
	}
	for name := range overrides {
		if strings.TrimSpace(name) == "" {
			return nil, &variableOverridesError{message: "variable override names cannot be empty"}
		}
	}
	body, err := json.Marshal(overrides)
	if err != nil {
		return nil, &variableOverridesError{message: "variable overrides must contain JSON-compatible values"}
	}
	if len(body) > maxVariableOverridesBytes {
		return nil, &variableOverridesError{message: fmt.Sprintf("variable overrides exceed the %d byte limit", maxVariableOverridesBytes)}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, &variableOverridesError{message: "variable overrides must contain valid JSON values"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, &variableOverridesError{message: "variable overrides must contain one JSON object"}
	}
	normalized, err := normalizeVariableJSONValue(decoded)
	if err != nil {
		return nil, err
	}
	result, ok := normalized.(map[string]any)
	if !ok {
		return nil, &variableOverridesError{message: "variable overrides must contain one JSON object"}
	}
	return result, nil
}

func normalizeVariableJSONValue(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := strconv.ParseInt(string(typed), 10, 64); err == nil {
			return integer, nil
		}
		decimal, err := strconv.ParseFloat(string(typed), 64)
		if err != nil || math.IsNaN(decimal) || math.IsInf(decimal, 0) {
			return nil, &variableOverridesError{message: "variable overrides contain an invalid number"}
		}
		return decimal, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized, err := normalizeVariableJSONValue(item)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeVariableJSONValue(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func addVariableOverrides(builder *pipeline.Builder, overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}
	if builder == nil {
		return errors.New("pipeline builder is required")
	}
	mutator, err := variableOverridesMutator(overrides)
	if err != nil {
		return err
	}
	builder.AddPipelineMutator(mutator)
	return nil
}

func variableOverridesMutator(overrides map[string]any) (pipeline.PipelineMutator, error) {
	normalized, err := normalizeVariableOverrides(overrides)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, parsed *pipeline.Pipeline) (*pipeline.Pipeline, error) {
		if parsed == nil {
			return nil, &variableOverridesError{message: "pipeline variable declarations are unavailable"}
		}
		if err := parsed.Variables.Merge(normalized); err != nil {
			return nil, &variableOverridesError{message: err.Error()}
		}
		result, err := gojsonschema.Validate(
			gojsonschema.NewGoLoader(parsed.Variables.Schema()),
			gojsonschema.NewGoLoader(parsed.Variables.Value()),
		)
		if err != nil {
			return nil, &variableOverridesError{message: "pipeline variable declarations could not be validated"}
		}
		if result.Valid() {
			return parsed, nil
		}
		failure := result.Errors()[0]
		name := strings.TrimPrefix(strings.TrimSpace(failure.Field()), "(root).")
		if name == "" || name == "(root)" {
			return nil, &variableOverridesError{message: "variable overrides do not satisfy the pipeline declarations"}
		}
		return nil, &variableOverridesError{message: fmt.Sprintf(
			"variable %q does not satisfy its declared schema (%s)", name, failure.Type(),
		)}
	}, nil
}

// ValidatePipelineVariableOverrides validates values against the declarations
// in one exact pipeline source without constructing assets or connecting to a
// destination. The same mutator is used later by planning and execution.
func ValidatePipelineVariableOverrides(
	ctx context.Context,
	builder *pipeline.Builder,
	pipelinePath string,
	overrides map[string]any,
) error {
	if len(overrides) == 0 {
		return nil
	}
	if err := addVariableOverrides(builder, overrides); err != nil {
		return err
	}
	_, err := builder.CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate(), pipeline.WithOnlyPipeline())
	if err != nil {
		if errors.Is(err, ErrInvalidVariableOverrides) {
			return err
		}
		return fmt.Errorf("read pipeline variable declarations: %w", err)
	}
	return nil
}
