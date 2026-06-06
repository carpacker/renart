//go:build !renart_sqlglot_fallback

package sqlintelligence

import "github.com/pkg/errors"

func ParseContextWithSchemaPython(query, dialect string, schema Schema, columnSourceMethods ...SchemaColumnSourceMethods) (*ParseContext, error) {
	return nil, errors.New("RENART_SQL_PARSER=python requires building Renart with -tags renart_sqlglot_fallback")
}
