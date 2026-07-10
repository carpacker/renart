package service

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
)

func TestFormatImportedViewDefinitionCleansUpEngineViewSQL(t *testing.T) {
	// pg_get_viewdef-style output: leading space, odd indentation, trailing semicolon.
	definition := " SELECT id,\n    user_id,\n    \"accessTokenExpiresAt\"\n   FROM accounts;"

	formatted := formatImportedViewDefinition(context.Background(), definition, pipeline.AssetType("pg.sql"))

	assert.NotEmpty(t, formatted)
	assert.False(t, strings.HasSuffix(strings.TrimSpace(formatted), ";"))
	assert.Equal(t, formatted, strings.TrimSpace(formatted))
	assert.Contains(t, strings.ToLower(formatted), "from accounts")
	assert.Contains(t, formatted, `"accessTokenExpiresAt"`)
}

func TestFormatImportedViewDefinitionFallsBackToTrimmedInput(t *testing.T) {
	definition := "  not really ((( sql at all ;  "

	formatted := formatImportedViewDefinition(context.Background(), definition, pipeline.AssetType("pg.sql"))

	assert.NotEmpty(t, formatted)
	assert.False(t, strings.HasSuffix(formatted, ";"))
}
