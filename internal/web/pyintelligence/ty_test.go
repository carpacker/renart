package pyintelligence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTyWASIFormatsAndChecksPython(t *testing.T) {
	req := Request{
		Root:    "/",
		Path:    "/example.py",
		Content: "def returns_int(value:str)->int:\n return value\n",
		Options: map[string]any{
			"environment": map[string]any{"python-version": "3.11"},
		},
	}

	formatted, err := Format(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", formatted.Status)
	require.NotNil(t, formatted.Result)
	assert.Equal(t, "def returns_int(value: str) -> int:\n    return value\n", *formatted.Result)

	checked, err := Check(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", checked.Status)
	require.Len(t, checked.Diagnostics, 1)
	assert.Equal(t, "invalid-return-type", checked.Diagnostics[0].ID)
	assert.Contains(t, checked.Diagnostics[0].Message, "expected `int`, found `str`")
	require.NotNil(t, checked.Diagnostics[0].Range)
	assert.Equal(t, Position{Line: 2, Column: 9}, checked.Diagnostics[0].Range.Start)
	assert.Equal(t, Position{Line: 2, Column: 14}, checked.Diagnostics[0].Range.End)
}

func TestTyWASICompletesPythonSymbols(t *testing.T) {
	req := Request{
		Root:    "/",
		Path:    "/example.py",
		Content: "def local_value() -> int:\n    return 1\n\nlocal_val",
		Line:    4,
		Column:  10,
		Options: map[string]any{
			"environment": map[string]any{"python-version": "3.11"},
		},
	}

	completed, err := Complete(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", completed.Status)
	assert.Contains(t, completionLabels(completed.Result), "local_value")
}

func TestTyWASICompletesAutoImports(t *testing.T) {
	req := Request{
		Root:    "/",
		Path:    "/example.py",
		Content: "walktr",
		Line:    1,
		Column:  7,
		Options: map[string]any{
			"environment": map[string]any{"python-version": "3.11"},
		},
	}

	completed, err := Complete(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", completed.Status)
	match := findCompletion(completed.Result, "walktree")
	require.NotNil(t, match)
	assert.Equal(t, "inspect", match.ModuleName)
	require.Len(t, match.AdditionalTextEdits, 1)
	assert.Equal(t, "from inspect import walktree\n", match.AdditionalTextEdits[0].Text)
}

func TestTyWASICompletesPackageStubAttributes(t *testing.T) {
	req := Request{
		Root:    "/",
		Path:    "/example.py",
		Content: "import pandas as pd\n\npd.",
		Line:    3,
		Column:  4,
		Options: map[string]any{
			"environment": map[string]any{
				"python-version": "3.11",
				"extra-paths":    []string{"/site-packages"},
			},
		},
		Files: []VirtualFile{
			{
				Path:    "/site-packages/pandas/__init__.pyi",
				Content: "from typing import Any\n\nDataFrame: Any\nSeries: Any\n\ndef __getattr__(name: str) -> Any: ...\n",
			},
		},
	}

	completed, err := Complete(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", completed.Status)
	assert.Contains(t, completionLabels(completed.Result), "DataFrame")
}

func TestTyWASISessionUpdatesOpenFileContent(t *testing.T) {
	req := Request{
		Root:               "/",
		Path:               "/example.py",
		Content:            "def returns_int(value: str) -> int:\n    return value\n",
		SessionID:          "test-session",
		SessionFingerprint: "same-options",
		Options: map[string]any{
			"environment": map[string]any{"python-version": "3.11"},
		},
	}

	checked, err := Check(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", checked.Status)
	require.NotEmpty(t, checked.Diagnostics)
	assert.Equal(t, "invalid-return-type", checked.Diagnostics[0].ID)

	req.Content = "def returns_int(value: str) -> int:\n    return 1\n"
	checked, err = Check(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", checked.Status)
	for _, diagnostic := range checked.Diagnostics {
		assert.NotEqual(t, "invalid-return-type", diagnostic.ID)
	}
}

func TestTyWASILanguageFeatures(t *testing.T) {
	content := "class Widget:\n    def method(self, value: int) -> str:\n        return str(value)\n\nwidget = Widget()\nresult = widget.method(1)\n"
	req := Request{
		Root:               "/",
		Path:               "/example.py",
		Content:            content,
		SessionID:          "language-feature-session",
		SessionFingerprint: "same-options",
		Options: map[string]any{
			"environment": map[string]any{"python-version": "3.11"},
		},
	}

	req.Line = 6
	req.Column = 18
	hovered, err := HoverAt(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", hovered.Status)
	require.NotNil(t, hovered.Result)
	assert.Contains(t, hovered.Result.Contents, "int")
	assert.Contains(t, hovered.Result.Contents, "str")

	req.Line = 6
	req.Column = 24
	signature, err := SignatureHelpAt(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", signature.Status)
	require.NotNil(t, signature.Result)
	require.NotEmpty(t, signature.Result.Signatures)
	assert.Contains(t, signature.Result.Signatures[0].Label, "value: int")

	req.Line = 6
	req.Column = 17
	gotoDefinition, err := GotoDefinition(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", gotoDefinition.Status)
	require.NotEmpty(t, gotoDefinition.Result)
	assert.Equal(t, "/example.py", gotoDefinition.Result[0].Path)
}

func completionLabels(completions []Completion) []string {
	labels := make([]string, 0, len(completions))
	for _, completion := range completions {
		labels = append(labels, completion.Label)
	}
	return labels
}

func findCompletion(completions []Completion, label string) *Completion {
	for i := range completions {
		if completions[i].Label == label {
			return &completions[i]
		}
	}
	return nil
}
