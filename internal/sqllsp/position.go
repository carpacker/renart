package sqllsp

import "unicode/utf8"

func ByteOffset(text string, pos Position) int {
	line, character := 0, 0
	for offset := 0; offset < len(text); {
		if line == pos.Line && character >= pos.Character {
			return offset
		}
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == '\n' {
			if line == pos.Line {
				return offset
			}
			line++
			character = 0
			offset += size
			continue
		}
		if line == pos.Line {
			if r <= 0xffff {
				character++
			} else {
				character += 2
			}
		}
		offset += size
	}
	return len(text)
}

func PositionAt(text string, target int) Position {
	if target < 0 {
		target = 0
	}
	if target > len(text) {
		target = len(text)
	}
	pos := Position{}
	for offset := 0; offset < target; {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == '\n' {
			pos.Line++
			pos.Character = 0
		} else if r <= 0xffff {
			pos.Character++
		} else {
			pos.Character += 2
		}
		offset += size
	}
	return pos
}

func RangeFromOffsets(text string, start, end int) Range {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(text) {
		end = len(text)
	}
	return Range{Start: PositionAt(text, start), End: PositionAt(text, end)}
}
