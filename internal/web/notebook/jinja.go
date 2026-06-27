package notebook

import "regexp"

var (
	jinjaComment     = regexp.MustCompile(`\{#[\s\S]*?#\}`)
	jinjaQuotedExpr  = regexp.MustCompile(`'\s*\{\{[\s\S]*?\}\}\s*'`)
	jinjaDQuotedExpr = regexp.MustCompile(`"\s*\{\{[\s\S]*?\}\}\s*"`)
	jinjaExpr        = regexp.MustCompile(`\{\{[\s\S]*?\}\}`)
	jinjaStmt        = regexp.MustCompile(`\{%[\s\S]*?%\}`)
)

// JinjaSafeSQL replaces Jinja constructs with parseable placeholders so the
// SQL parser can run over templated cells. Mirrors the frontend's
// jinjaSafeSQLForParsing — keep the two in sync.
func JinjaSafeSQL(sql string) string {
	out := jinjaComment.ReplaceAllString(sql, "")
	out = jinjaQuotedExpr.ReplaceAllString(out, "'__renart_jinja_value__'")
	out = jinjaDQuotedExpr.ReplaceAllString(out, `"__renart_jinja_value__"`)
	out = jinjaExpr.ReplaceAllString(out, "'__renart_jinja_value__'")
	out = jinjaStmt.ReplaceAllString(out, "")
	return out
}
