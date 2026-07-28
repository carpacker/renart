package sqllsp

import (
	"fmt"
	"regexp"
	"strings"
)

var jinjaSQLCallPattern = regexp.MustCompile(`(?is)\{\{\s*(ref|source)\s*\((.*?)\)\s*\}\}`)
var jinjaTemplatePattern = regexp.MustCompile(`(?s)\{\{.*?\}\}|\{%.*?%\}|\{#.*?#\}`)

type renderProjection struct {
	doc      TextDocumentItem
	rendered RenderedSQL
	changed  bool
}

func (e *Engine) renderDocument(doc TextDocumentItem) renderProjection {
	rendered := renderTemplateSQL(doc, e)
	if doc.Projection != nil && doc.Projection.TemplateURI == doc.URI {
		rendered = *doc.Projection
	}
	if rendered.RenderedSQL == doc.Text {
		return renderProjection{doc: doc, rendered: rendered}
	}
	return renderProjection{
		doc: TextDocumentItem{
			URI:        doc.URI,
			LanguageID: "sql",
			Version:    doc.Version,
			Text:       rendered.RenderedSQL,
		},
		rendered: rendered,
		changed:  true,
	}
}

func renderTemplateSQL(doc TextDocumentItem, engine *Engine) RenderedSQL {
	var builder strings.Builder
	segments := []SourceSegment{}
	invocations := []TemplateCall{}
	last := 0
	text := doc.Text
	for _, match := range jinjaSQLCallPattern.FindAllStringSubmatchIndex(text, -1) {
		if match[0] > last {
			start := builder.Len()
			builder.WriteString(text[last:match[0]])
			templateStart, templateEnd := last, match[0]
			segments = append(segments, SourceSegment{
				GeneratedStart: start,
				GeneratedEnd:   builder.Len(),
				TemplateStart:  &templateStart,
				TemplateEnd:    &templateEnd,
				Kind:           "LiteralSql",
				Confidence:     "high",
			})
		}

		callKind := strings.ToLower(text[match[2]:match[3]])
		args := text[match[4]:match[5]]
		replacement, refKind := engine.renderTemplateCall(callKind, args)
		if replacement == "" {
			replacement = "__template__"
			refKind = "Synthetic"
		}
		start := builder.Len()
		builder.WriteString(replacement)
		templateStart, templateEnd := match[0], match[1]
		segments = append(segments, SourceSegment{
			GeneratedStart: start,
			GeneratedEnd:   builder.Len(),
			TemplateStart:  &templateStart,
			TemplateEnd:    &templateEnd,
			Kind:           refKind,
			Confidence:     "medium",
		})
		invocations = append(invocations, TemplateCall{
			ID:    fmt.Sprintf("%s:%d", callKind, match[0]),
			Kind:  callKind,
			Range: RangeFromOffsets(text, match[0], match[1]),
		})
		last = match[1]
	}
	if last < len(text) {
		start := builder.Len()
		builder.WriteString(text[last:])
		templateStart, templateEnd := last, len(text)
		segments = append(segments, SourceSegment{
			GeneratedStart: start,
			GeneratedEnd:   builder.Len(),
			TemplateStart:  &templateStart,
			TemplateEnd:    &templateEnd,
			Kind:           "LiteralSql",
			Confidence:     "high",
		})
	}
	return RenderedSQL{
		ID:          string(doc.URI) + "#rendered",
		TemplateURI: doc.URI,
		RenderedSQL: builder.String(),
		SourceMap: SQLSourceMap{
			TemplateURI: doc.URI,
			Segments:    segments,
		},
		Invocations: invocations,
	}
}

func (e *Engine) renderTemplateCall(kind, args string) (string, string) {
	quoted := quotedArgs(args)
	switch kind {
	case "ref":
		if len(quoted) >= 1 {
			if relation := e.resolveRelation(quoted[0]); relation != nil {
				return relation.Name, "RefExpansion"
			}
			return quoted[0], "RefExpansion"
		}
	case "source":
		if len(quoted) >= 2 {
			name := quoted[0] + "." + quoted[1]
			if relation := e.resolveRelation(name); relation != nil {
				return relation.Name, "RefExpansion"
			}
			return name, "RefExpansion"
		}
	}
	return "", ""
}

func quotedArgs(args string) []string {
	pattern := regexp.MustCompile(`['"]([^'"]+)['"]`)
	matches := pattern.FindAllStringSubmatch(args, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, match[1])
	}
	return result
}

func (r RenderedSQL) TemplateRangeForGenerated(text string, generatedRange Range) Range {
	start := ByteOffset(r.RenderedSQL, generatedRange.Start)
	end := ByteOffset(r.RenderedSQL, generatedRange.End)
	return r.TemplateRangeForGeneratedOffsets(text, start, end)
}

func (r RenderedSQL) TemplateRangeForGeneratedOffsets(templateText string, start, end int) Range {
	templateStart, templateEnd, _, ok := r.TemplateOffsetsForGenerated(start, end)
	if ok {
		return RangeFromOffsets(templateText, templateStart, templateEnd)
	}
	return RangeFromOffsets(templateText, 0, min(1, len(templateText)))
}

func (r RenderedSQL) TemplateOffsetsForGenerated(start, end int) (int, int, string, bool) {
	if end < start {
		end = start
	}
	templateStart, startConfidence, okStart := r.templateOffsetForGeneratedBoundary(start, false)
	endLookup := end
	if end > start {
		endLookup = end - 1
	}
	templateEnd, endConfidence, okEnd := r.templateOffsetForGeneratedBoundary(endLookup, true)
	if okStart && okEnd {
		if templateEnd < templateStart {
			templateEnd = templateStart
		}
		confidence := startConfidence
		if confidenceRank(endConfidence) < confidenceRank(confidence) {
			confidence = endConfidence
		}
		return templateStart, templateEnd, confidence, true
	}
	return 0, 0, "low", false
}

func (r RenderedSQL) templateOffsetForGeneratedBoundary(offset int, endBoundary bool) (int, string, bool) {
	for _, segment := range r.SourceMap.Segments {
		if segment.TemplateStart == nil || segment.TemplateEnd == nil {
			continue
		}
		if offset < segment.GeneratedStart || offset >= segment.GeneratedEnd {
			continue
		}
		if segment.Kind != "LiteralSql" {
			if endBoundary {
				return *segment.TemplateEnd, segment.Confidence, true
			}
			return *segment.TemplateStart, segment.Confidence, true
		}
		delta := offset - segment.GeneratedStart
		if endBoundary {
			delta++
		}
		return min(*segment.TemplateStart+delta, *segment.TemplateEnd), segment.Confidence, true
	}
	return 0, "low", false
}

func confidenceRank(confidence string) int {
	switch confidence {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

// ProjectRenderedSQL builds an honest best-effort source map from a fully
// rendered/extracted SQL unit back to its template. Literal source fragments
// are mapped exactly. Generated text between those fragments maps to the
// corresponding template expression at medium confidence; output that cannot
// be aligned remains unmapped.
func ProjectRenderedSQL(templateURI URI, templateText, renderedSQL string) RenderedSQL {
	result := RenderedSQL{
		TemplateURI: templateURI,
		RenderedSQL: renderedSQL,
		SourceMap:   SQLSourceMap{TemplateURI: templateURI},
	}
	if renderedSQL == templateText {
		start, end := 0, len(templateText)
		result.SourceMap.Segments = []SourceSegment{{GeneratedStart: 0, GeneratedEnd: len(renderedSQL), TemplateStart: &start, TemplateEnd: &end, Kind: "LiteralSql", Confidence: "high"}}
		return result
	}
	if index := strings.Index(templateText, renderedSQL); index >= 0 {
		start, end := index, index+len(renderedSQL)
		result.SourceMap.Segments = []SourceSegment{{GeneratedStart: 0, GeneratedEnd: len(renderedSQL), TemplateStart: &start, TemplateEnd: &end, Kind: "LiteralSql", Confidence: "high"}}
		return result
	}

	matches := jinjaTemplatePattern.FindAllStringIndex(templateText, -1)
	if len(matches) == 0 {
		return result
	}
	generatedCursor := 0
	templateCursor := 0
	pendingStart, pendingEnd := -1, -1
	hasLiteralAnchor := false
	for _, match := range matches {
		literal := templateText[templateCursor:match[0]]
		if literal != "" {
			if relative := strings.Index(renderedSQL[generatedCursor:], literal); relative >= 0 {
				generatedStart := generatedCursor + relative
				if generatedStart > generatedCursor && pendingStart >= 0 {
					start, end := pendingStart, pendingEnd
					result.SourceMap.Segments = append(result.SourceMap.Segments, SourceSegment{GeneratedStart: generatedCursor, GeneratedEnd: generatedStart, TemplateStart: &start, TemplateEnd: &end, Kind: "TemplateExpansion", Confidence: "medium"})
				}
				start, end := templateCursor, match[0]
				result.SourceMap.Segments = append(result.SourceMap.Segments, SourceSegment{GeneratedStart: generatedStart, GeneratedEnd: generatedStart + len(literal), TemplateStart: &start, TemplateEnd: &end, Kind: "LiteralSql", Confidence: "high"})
				generatedCursor = generatedStart + len(literal)
				hasLiteralAnchor = true
				pendingStart, pendingEnd = -1, -1
			}
		}
		if pendingStart < 0 {
			pendingStart = match[0]
		}
		pendingEnd = match[1]
		templateCursor = match[1]
	}

	literal := templateText[templateCursor:]
	if literal != "" {
		if relative := strings.Index(renderedSQL[generatedCursor:], literal); relative >= 0 {
			generatedStart := generatedCursor + relative
			if generatedStart > generatedCursor && pendingStart >= 0 {
				start, end := pendingStart, pendingEnd
				result.SourceMap.Segments = append(result.SourceMap.Segments, SourceSegment{GeneratedStart: generatedCursor, GeneratedEnd: generatedStart, TemplateStart: &start, TemplateEnd: &end, Kind: "TemplateExpansion", Confidence: "medium"})
			}
			start, end := templateCursor, len(templateText)
			result.SourceMap.Segments = append(result.SourceMap.Segments, SourceSegment{GeneratedStart: generatedStart, GeneratedEnd: generatedStart + len(literal), TemplateStart: &start, TemplateEnd: &end, Kind: "LiteralSql", Confidence: "high"})
			generatedCursor = generatedStart + len(literal)
			hasLiteralAnchor = true
			pendingStart, pendingEnd = -1, -1
		}
	}
	if generatedCursor < len(renderedSQL) && pendingStart >= 0 && hasLiteralAnchor {
		start, end := pendingStart, pendingEnd
		result.SourceMap.Segments = append(result.SourceMap.Segments, SourceSegment{GeneratedStart: generatedCursor, GeneratedEnd: len(renderedSQL), TemplateStart: &start, TemplateEnd: &end, Kind: "TemplateExpansion", Confidence: "medium"})
	}
	return result
}

func (r RenderedSQL) GeneratedOffsetForTemplateOffset(templateOffset int) int {
	for _, segment := range r.SourceMap.Segments {
		if segment.TemplateStart == nil || segment.TemplateEnd == nil {
			continue
		}
		if templateOffset < *segment.TemplateStart || templateOffset > *segment.TemplateEnd {
			continue
		}
		if segment.Kind != "LiteralSql" {
			return segment.GeneratedEnd
		}
		return segment.GeneratedStart + min(templateOffset-*segment.TemplateStart, segment.GeneratedEnd-segment.GeneratedStart)
	}
	return min(templateOffset, len(r.RenderedSQL))
}
