package sqllsp

import (
	"strings"
	"testing"
)

func TestProjectRenderedSQLMapsLiteralAndGeneratedRanges(t *testing.T) {
	source := "select {{ var.value }} as value, missing from known"
	rendered := "select 42 as value, missing from known"
	projection := ProjectRenderedSQL("file:///query.sql", source, rendered)

	missingStart := strings.Index(rendered, "missing")
	start, end, confidence, ok := projection.TemplateOffsetsForGenerated(missingStart, missingStart+len("missing"))
	if !ok || source[start:end] != "missing" || confidence != "high" {
		t.Fatalf("literal mapping = (%d, %d, %q, %t), text=%q", start, end, confidence, ok, source[start:end])
	}

	generatedStart := strings.Index(rendered, "42")
	start, end, confidence, ok = projection.TemplateOffsetsForGenerated(generatedStart, generatedStart+len("42"))
	if !ok || source[start:end] != "{{ var.value }}" || confidence != "medium" {
		t.Fatalf("generated mapping = (%d, %d, %q, %t), text=%q", start, end, confidence, ok, source[start:end])
	}
}

func TestProjectRenderedSQLLeavesUnalignableOutputUnmapped(t *testing.T) {
	projection := ProjectRenderedSQL("file:///query.sql", "select {% if enabled %}value{% endif %}", "generated elsewhere")
	if _, _, _, ok := projection.TemplateOffsetsForGenerated(0, len("generated")); ok {
		t.Fatal("expected unalignable generated SQL to remain unmapped")
	}
}

func TestRenderDocumentPrefersRequestLocalProjection(t *testing.T) {
	source := "select * from events where magnitude >= {{ var.notable_magnitude }}"
	rendered := "select * from events where magnitude >= 5"
	projection := ProjectRenderedSQL("file:///query.sql", source, rendered)
	doc := TextDocumentItem{
		URI:        "file:///query.sql",
		LanguageID: "sql",
		Text:       source,
		Projection: &projection,
	}

	result := NewEngine(CanonicalGraph{}).renderDocument(doc)
	if !result.changed || result.doc.Text != rendered {
		t.Fatalf("request-local projection was not analyzed: %#v", result)
	}

	generatedStart := strings.Index(rendered, "5")
	start, end, confidence, ok := result.rendered.TemplateOffsetsForGenerated(generatedStart, generatedStart+1)
	if !ok || source[start:end] != "{{ var.notable_magnitude }}" || confidence != "medium" {
		t.Fatalf("projected variable mapping = (%d, %d, %q, %t), text=%q", start, end, confidence, ok, source[start:end])
	}
}
