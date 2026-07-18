package runcontext

import "sort"

const (
	VariableSourcePipelineDefault  = "pipeline_default"
	VariableSourceScheduleOverride = "schedule_override"
	VariableSourceRunOverride      = "run_override"
)

// VariableLayer names the variables supplied by one precedence layer. Values
// are deliberately absent so this can be returned in render and run-list DTOs
// without exposing parameters that may contain sensitive data.
type VariableLayer struct {
	Source string
	Names  []string
}

type VariableProvenance struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// ValueFreeVariableProvenance applies layers in ascending precedence, keeps
// only the winning source for each variable, and returns a stable name order.
func ValueFreeVariableProvenance(layers ...VariableLayer) []VariableProvenance {
	sources := make(map[string]string)
	for _, layer := range layers {
		for _, name := range layer.Names {
			if name != "" {
				sources[name] = layer.Source
			}
		}
	}

	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]VariableProvenance, 0, len(names))
	for _, name := range names {
		result = append(result, VariableProvenance{Name: name, Source: sources[name]})
	}
	return result
}
