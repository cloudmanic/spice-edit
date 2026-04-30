// =============================================================================
// File: internal/editor/buffer.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package editor provides the text-buffer primitives, the syntax highlighter,
// and the Tab type that combines them with view state and rendering. The
// buffer is intentionally simple: one Go string per line. That's plenty fast
// for the review-and-light-edit workloads this editor is aimed at, and keeps
// the rendering and tokenisation code uncluttered.
package editor

import "strings"

// Position is a buffer location measured in lines and rune-indexed columns.
// Line is 0-based; Col is 0-based and counts runes (not bytes, not screen
// cells), so a multi-byte character or a CJK glyph each count as one column.
type Position struct {
	Line int
	Col  int
}

// Buffer is a simple editable text buffer backed by one string per line.
// String-per-line keeps the surrounding code readable; Go string ops are
// fast enough that rebuilding a single line on each edit is fine for the
// file sizes this editor actually opens (review + small edits).
type Buffer struct {
	Lines []string
}

// NewBuffer constructs a Buffer from a string by splitting on newlines.
// A trailing newline produces an empty final line, mirroring how files
// commonly end and what most editors display.
func NewBuffer(text string) *Buffer {
	if text == "" {
		return &Buffer{Lines: []string{""}}
	}
	return &Buffer{Lines: strings.Split(text, "\n")}
}

// String serialises the buffer back to a single newline-joined string,
// suitable for writing back to disk.
func (b *Buffer) String() string {
	return strings.Join(b.Lines, "\n")
}

// LineCount returns the total number of lines in the buffer; always >= 1.
func (b *Buffer) LineCount() int {
	return len(b.Lines)
}

// LineRunes returns the runes of the line at i, or nil if i is out of range.
// The caller should treat the returned slice as read-only.
func (b *Buffer) LineRunes(i int) []rune {
	if i < 0 || i >= len(b.Lines) {
		return nil
	}
	return []rune(b.Lines[i])
}

// Clamp adjusts a position so that Line and Col fall within the buffer.
// Col is clamped to the rune length of its line (so it can sit one past the
// last rune, which is where the cursor lives at end-of-line).
func (b *Buffer) Clamp(p Position) Position {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.Lines) {
		p.Line = len(b.Lines) - 1
	}
	runes := []rune(b.Lines[p.Line])
	if p.Col < 0 {
		p.Col = 0
	}
	if p.Col > len(runes) {
		p.Col = len(runes)
	}
	return p
}

// InsertString inserts text (which may contain newlines) at p and returns
// the position immediately after the inserted text. p is clamped first.
func (b *Buffer) InsertString(p Position, text string) Position {
	p = b.Clamp(p)
	if text == "" {
		return p
	}
	line := []rune(b.Lines[p.Line])
	before := string(line[:p.Col])
	after := string(line[p.Col:])

	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		b.Lines[p.Line] = before + parts[0] + after
		return Position{Line: p.Line, Col: p.Col + len([]rune(parts[0]))}
	}

	// Multi-line insert: splice new lines into the buffer.
	newLines := make([]string, 0, len(parts))
	newLines = append(newLines, before+parts[0])
	for i := 1; i < len(parts)-1; i++ {
		newLines = append(newLines, parts[i])
	}
	last := parts[len(parts)-1]
	newLines = append(newLines, last+after)

	out := make([]string, 0, len(b.Lines)+len(newLines)-1)
	out = append(out, b.Lines[:p.Line]...)
	out = append(out, newLines...)
	out = append(out, b.Lines[p.Line+1:]...)
	b.Lines = out

	return Position{Line: p.Line + len(parts) - 1, Col: len([]rune(last))}
}

// DeleteRange removes everything between a and b (in any order) and returns
// the resulting position (the smaller of the two). Both endpoints are
// clamped first; an empty range is a no-op.
func (b *Buffer) DeleteRange(a, c Position) Position {
	a = b.Clamp(a)
	c = b.Clamp(c)
	if posLess(c, a) {
		a, c = c, a
	}
	if a == c {
		return a
	}
	aRunes := []rune(b.Lines[a.Line])
	cRunes := []rune(b.Lines[c.Line])
	head := string(aRunes[:a.Col])
	tail := string(cRunes[c.Col:])

	out := make([]string, 0, len(b.Lines)-(c.Line-a.Line))
	out = append(out, b.Lines[:a.Line]...)
	out = append(out, head+tail)
	out = append(out, b.Lines[c.Line+1:]...)
	b.Lines = out
	return a
}

// Substring returns the text between a and b. The returned string is always
// in document order, regardless of the order of the inputs.
func (b *Buffer) Substring(a, c Position) string {
	a = b.Clamp(a)
	c = b.Clamp(c)
	if posLess(c, a) {
		a, c = c, a
	}
	if a == c {
		return ""
	}
	if a.Line == c.Line {
		runes := []rune(b.Lines[a.Line])
		return string(runes[a.Col:c.Col])
	}
	var sb strings.Builder
	aRunes := []rune(b.Lines[a.Line])
	sb.WriteString(string(aRunes[a.Col:]))
	sb.WriteByte('\n')
	for i := a.Line + 1; i < c.Line; i++ {
		sb.WriteString(b.Lines[i])
		sb.WriteByte('\n')
	}
	cRunes := []rune(b.Lines[c.Line])
	sb.WriteString(string(cRunes[:c.Col]))
	return sb.String()
}

// EndPos returns the position just after the last rune of the buffer.
// Useful for select-all and end-of-document navigation.
func (b *Buffer) EndPos() Position {
	last := len(b.Lines) - 1
	return Position{Line: last, Col: len([]rune(b.Lines[last]))}
}

// posLess reports whether a comes before b in document order.
func posLess(a, b Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Col < b.Col
}

// PosLess is the exported version of posLess for use from neighbouring
// packages that need to compare positions.
func PosLess(a, b Position) bool { return posLess(a, b) }

// PosOrdered returns (a, b) sorted in document order.
func PosOrdered(a, b Position) (Position, Position) {
	if posLess(b, a) {
		return b, a
	}
	return a, b
}
