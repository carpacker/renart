package bruincompat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/sqlformat"
)

type DeclareHoister struct{}

type tokenizeResponse struct {
	Success bool    `json:"success"`
	Tokens  []token `json:"tokens"`
	Error   any     `json:"error"`
}

type token struct {
	Type string `json:"token_type"`
	Span struct {
		Start int `json:"start"`
		End   int `json:"end"`
	} `json:"span"`
}

type parseResponse struct {
	Success bool            `json:"success"`
	AST     json.RawMessage `json:"ast"`
}

func NewDeclareHoister() *DeclareHoister { return &DeclareHoister{} }

func (h *DeclareHoister) HoistDeclares(query string, assetType pipeline.AssetType) (string, error) {
	dialect, err := AssetTypeToDialect(assetType)
	if err != nil {
		return query, err
	}
	if strings.TrimSpace(query) == "" {
		return query, nil
	}

	positions, err := topLevelSemicolons(context.Background(), query, dialect)
	if err != nil {
		return query, err
	}

	slices := make([]string, 0, len(positions)+1)
	previous := 0
	for _, position := range positions {
		slices = append(slices, query[previous:position])
		previous = position + 1
	}
	if previous < len(query) {
		slices = append(slices, query[previous:])
	}

	declares := make([]string, 0)
	rest := make([]string, 0)
	sawNonDeclare := false
	needsReorder := false
	for _, statement := range slices {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		isDeclare, err := isDeclareStatement(context.Background(), trimmed, dialect)
		if err != nil {
			return query, err
		}
		if isDeclare {
			declares = append(declares, trimmed)
			needsReorder = needsReorder || sawNonDeclare
			continue
		}
		rest = append(rest, trimmed)
		sawNonDeclare = true
	}

	if len(declares) == 0 || !needsReorder {
		return query, nil
	}
	return strings.Join(append(declares, rest...), ";\n") + ";", nil
}

func (h *DeclareHoister) HoistDeclaresList(queries []string, assetType pipeline.AssetType) ([]string, error) {
	dialect, err := AssetTypeToDialect(assetType)
	if err != nil {
		return queries, err
	}
	if len(queries) == 0 {
		return queries, nil
	}

	declares := make([]string, 0)
	rest := make([]string, 0)
	sawNonDeclare := false
	needsReorder := false
	for _, query := range queries {
		isDeclare, err := isDeclareStatement(context.Background(), strings.TrimSpace(query), dialect)
		if err != nil {
			return queries, err
		}
		if isDeclare {
			declares = append(declares, query)
			needsReorder = needsReorder || sawNonDeclare
			continue
		}
		rest = append(rest, query)
		sawNonDeclare = true
	}
	if len(declares) == 0 || !needsReorder {
		return queries, nil
	}
	return append(declares, rest...), nil
}

func topLevelSemicolons(ctx context.Context, query, dialect string) ([]int, error) {
	responseJSON, err := sqlformat.Call(ctx, "tokenize", query, dialect)
	if err != nil {
		return nil, fmt.Errorf("tokenize SQL for DECLARE hoisting: %w", err)
	}
	var response tokenizeResponse
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		return nil, fmt.Errorf("decode Polyglot token response: %w", err)
	}
	if !response.Success {
		return nil, fmt.Errorf("Polyglot SQL tokenization failed: %v", response.Error)
	}

	parenDepth := 0
	beginEndDepth := 0
	caseDepth := 0
	positions := make([]int, 0)
	for index, current := range response.Tokens {
		switch current.Type {
		case "L_PAREN":
			parenDepth++
		case "R_PAREN":
			if parenDepth > 0 {
				parenDepth--
			}
		case "CASE":
			caseDepth++
		case "BEGIN":
			nextIsTransaction := index+1 < len(response.Tokens) && response.Tokens[index+1].Type == "TRANSACTION"
			if !nextIsTransaction {
				beginEndDepth++
			}
		case "END":
			if caseDepth > 0 {
				caseDepth--
			} else if beginEndDepth > 0 {
				beginEndDepth--
			}
		case "SEMICOLON":
			if parenDepth == 0 && beginEndDepth == 0 {
				positions = append(positions, current.Span.Start)
			}
		}
	}
	return positions, nil
}

func isDeclareStatement(ctx context.Context, statement, dialect string) (bool, error) {
	if !strings.Contains(strings.ToLower(statement), "declare") {
		return false, nil
	}
	responseJSON, err := sqlformat.Call(ctx, "parse", statement, dialect)
	if err != nil {
		return false, fmt.Errorf("parse SQL for DECLARE hoisting: %w", err)
	}
	var response parseResponse
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		return false, fmt.Errorf("decode Polyglot parse response: %w", err)
	}
	if !response.Success {
		return false, nil
	}

	var ast []map[string]any
	if err := json.Unmarshal(response.AST, &ast); err != nil {
		var encoded string
		if secondErr := json.Unmarshal(response.AST, &encoded); secondErr != nil {
			return false, fmt.Errorf("decode Polyglot AST: %w", err)
		}
		if secondErr := json.Unmarshal([]byte(encoded), &ast); secondErr != nil {
			return false, fmt.Errorf("decode Polyglot AST: %w", secondErr)
		}
	}
	if len(ast) != 1 || len(ast[0]) != 1 {
		return false, nil
	}
	_, ok := ast[0]["declare"]
	return ok, nil
}
