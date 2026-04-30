// =============================================================================
// File: internal/editor/buffer_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the line-per-string Buffer primitive. Buffer is the foundation
// every higher-level editor operation rests on, so we exhaustively pin down
// its behavior around clamping, multi-line splices, rune-vs-byte indexing,
// and selection ordering. If any of these regress, the whole editor will
// misbehave in subtle, hard-to-debug ways.

package editor

import (
	"testing"
)

// TestNewBuffer_Empty verifies that constructing a buffer from an empty
// string still yields a single empty line — every Buffer invariant assumes
// LineCount() >= 1, and downstream code crashes if we ever return zero lines.
func TestNewBuffer_Empty(t *testing.T) {
	b := NewBuffer("")
	if b.LineCount() != 1 {
		t.Fatalf("empty buffer should have 1 line, got %d", b.LineCount())
	}
	if b.Lines[0] != "" {
		t.Fatalf("empty buffer line 0 should be empty, got %q", b.Lines[0])
	}
}

// TestNewBuffer_TrailingNewline confirms that a trailing newline produces an
// empty final line — that's how POSIX text files end and how editors render.
func TestNewBuffer_TrailingNewline(t *testing.T) {
	b := NewBuffer("hello\n")
	if b.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", b.LineCount())
	}
	if b.Lines[0] != "hello" || b.Lines[1] != "" {
		t.Fatalf("unexpected lines: %#v", b.Lines)
	}
}

// TestNewBuffer_MultiLine ensures multi-line input is split correctly.
func TestNewBuffer_MultiLine(t *testing.T) {
	b := NewBuffer("a\nb\nc")
	if b.LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", b.LineCount())
	}
	if b.Lines[1] != "b" {
		t.Fatalf("line 1 wrong: %q", b.Lines[1])
	}
}

// TestBuffer_String_RoundTrip checks that String() is the inverse of
// NewBuffer for the common cases — load and save must agree.
func TestBuffer_String_RoundTrip(t *testing.T) {
	cases := []string{"", "x", "a\nb", "trailing\n", "a\n\nb"}
	for _, src := range cases {
		got := NewBuffer(src).String()
		if got != src {
			t.Fatalf("round-trip mismatch: %q -> %q", src, got)
		}
	}
}

// TestBuffer_LineRunes_OutOfRange returns nil for negative or past-end
// indices so callers can branch on a nil slice without panicking.
func TestBuffer_LineRunes_OutOfRange(t *testing.T) {
	b := NewBuffer("hello")
	if got := b.LineRunes(-1); got != nil {
		t.Fatalf("expected nil for -1, got %v", got)
	}
	if got := b.LineRunes(99); got != nil {
		t.Fatalf("expected nil for 99, got %v", got)
	}
}

// TestBuffer_LineRunes_Multibyte verifies rune-counting (not byte-counting)
// on lines containing multi-byte characters.
func TestBuffer_LineRunes_Multibyte(t *testing.T) {
	b := NewBuffer("héllo")
	got := b.LineRunes(0)
	if len(got) != 5 {
		t.Fatalf("expected 5 runes, got %d (%v)", len(got), got)
	}
}

// TestBuffer_Clamp_AllAxes verifies clamping in every direction (negative
// line, past-end line, negative col, past-end col) and confirms that a col
// equal to the rune length is allowed (cursor sits at end-of-line).
func TestBuffer_Clamp_AllAxes(t *testing.T) {
	b := NewBuffer("ab\ncde")

	cases := []struct {
		in, want Position
	}{
		{Position{Line: -5, Col: 0}, Position{Line: 0, Col: 0}},
		{Position{Line: 99, Col: 0}, Position{Line: 1, Col: 0}},
		{Position{Line: 0, Col: -3}, Position{Line: 0, Col: 0}},
		{Position{Line: 0, Col: 99}, Position{Line: 0, Col: 2}},
		{Position{Line: 1, Col: 3}, Position{Line: 1, Col: 3}}, // end-of-line allowed
		{Position{Line: 0, Col: 1}, Position{Line: 0, Col: 1}}, // already valid
	}
	for _, c := range cases {
		got := b.Clamp(c.in)
		if got != c.want {
			t.Errorf("Clamp(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

// TestBuffer_InsertString_Empty is a no-op insertion: the returned position
// is the (clamped) input position and the buffer is unchanged.
func TestBuffer_InsertString_Empty(t *testing.T) {
	b := NewBuffer("hello")
	end := b.InsertString(Position{Line: 0, Col: 2}, "")
	if end != (Position{Line: 0, Col: 2}) {
		t.Fatalf("unexpected end: %+v", end)
	}
	if b.String() != "hello" {
		t.Fatalf("buffer changed: %q", b.String())
	}
}

// TestBuffer_InsertString_SingleLine inserts text without newlines and checks
// the returned end position points just after the inserted runes.
func TestBuffer_InsertString_SingleLine(t *testing.T) {
	b := NewBuffer("hello")
	end := b.InsertString(Position{Line: 0, Col: 5}, " world")
	if b.String() != "hello world" {
		t.Fatalf("got %q", b.String())
	}
	if end != (Position{Line: 0, Col: 11}) {
		t.Fatalf("end pos wrong: %+v", end)
	}
}

// TestBuffer_InsertString_AcrossLineBoundary verifies that inserting text
// containing newlines splits a single buffer line into the right number of
// lines and that the returned end-position points at the final inserted col.
func TestBuffer_InsertString_AcrossLineBoundary(t *testing.T) {
	b := NewBuffer("abXYZ")
	end := b.InsertString(Position{Line: 0, Col: 2}, "1\n22\n333")
	want := "ab1\n22\n333XYZ"
	if b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
	if end != (Position{Line: 2, Col: 3}) {
		t.Fatalf("end pos wrong: %+v", end)
	}
	if b.LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", b.LineCount())
	}
}

// TestBuffer_InsertString_TwoLine covers exactly two parts (one newline) —
// the boundary between single-line and 3+ line insert paths.
func TestBuffer_InsertString_TwoLine(t *testing.T) {
	b := NewBuffer("abcd")
	end := b.InsertString(Position{Line: 0, Col: 2}, "X\nY")
	if b.String() != "abX\nYcd" {
		t.Fatalf("got %q", b.String())
	}
	if end != (Position{Line: 1, Col: 1}) {
		t.Fatalf("end pos wrong: %+v", end)
	}
}

// TestBuffer_InsertString_Multibyte confirms rune-indexed columns behave
// correctly when inserting around multi-byte text.
func TestBuffer_InsertString_Multibyte(t *testing.T) {
	b := NewBuffer("héllo")
	end := b.InsertString(Position{Line: 0, Col: 1}, "X")
	if b.String() != "hXéllo" {
		t.Fatalf("got %q", b.String())
	}
	if end != (Position{Line: 0, Col: 2}) {
		t.Fatalf("end pos wrong: %+v", end)
	}
}

// TestBuffer_DeleteRange_SameLine deletes a span on a single line and
// returns the smaller endpoint as the new cursor.
func TestBuffer_DeleteRange_SameLine(t *testing.T) {
	b := NewBuffer("abcdef")
	pos := b.DeleteRange(Position{Line: 0, Col: 1}, Position{Line: 0, Col: 4})
	if b.String() != "aef" {
		t.Fatalf("got %q", b.String())
	}
	if pos != (Position{Line: 0, Col: 1}) {
		t.Fatalf("pos wrong: %+v", pos)
	}
}

// TestBuffer_DeleteRange_ReversedOrder ensures DeleteRange normalises its
// inputs — passing endpoints out of order yields the same result.
func TestBuffer_DeleteRange_ReversedOrder(t *testing.T) {
	b := NewBuffer("abcdef")
	pos := b.DeleteRange(Position{Line: 0, Col: 4}, Position{Line: 0, Col: 1})
	if b.String() != "aef" {
		t.Fatalf("got %q", b.String())
	}
	if pos != (Position{Line: 0, Col: 1}) {
		t.Fatalf("pos wrong: %+v", pos)
	}
}

// TestBuffer_DeleteRange_Empty is a no-op when both positions are equal.
func TestBuffer_DeleteRange_Empty(t *testing.T) {
	b := NewBuffer("abc")
	pos := b.DeleteRange(Position{Line: 0, Col: 1}, Position{Line: 0, Col: 1})
	if b.String() != "abc" {
		t.Fatalf("buffer changed: %q", b.String())
	}
	if pos != (Position{Line: 0, Col: 1}) {
		t.Fatalf("pos wrong: %+v", pos)
	}
}

// TestBuffer_DeleteRange_MultiLine joins lines correctly when a range spans
// multiple lines.
func TestBuffer_DeleteRange_MultiLine(t *testing.T) {
	b := NewBuffer("hello\nbig\nworld")
	pos := b.DeleteRange(Position{Line: 0, Col: 2}, Position{Line: 2, Col: 2})
	if b.String() != "herld" {
		t.Fatalf("got %q", b.String())
	}
	if pos != (Position{Line: 0, Col: 2}) {
		t.Fatalf("pos wrong: %+v", pos)
	}
	if b.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", b.LineCount())
	}
}

// TestBuffer_Substring_SameLine returns the slice of text on one line.
func TestBuffer_Substring_SameLine(t *testing.T) {
	b := NewBuffer("hello world")
	got := b.Substring(Position{Line: 0, Col: 6}, Position{Line: 0, Col: 11})
	if got != "world" {
		t.Fatalf("got %q", got)
	}
}

// TestBuffer_Substring_Reversed returns text in document order regardless of
// the input order.
func TestBuffer_Substring_Reversed(t *testing.T) {
	b := NewBuffer("hello world")
	got := b.Substring(Position{Line: 0, Col: 11}, Position{Line: 0, Col: 6})
	if got != "world" {
		t.Fatalf("got %q", got)
	}
}

// TestBuffer_Substring_MultiLine joins lines with '\n' between the start
// and end positions.
func TestBuffer_Substring_MultiLine(t *testing.T) {
	b := NewBuffer("foo\nbar\nbaz")
	got := b.Substring(Position{Line: 0, Col: 1}, Position{Line: 2, Col: 2})
	want := "oo\nbar\nba"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestBuffer_Substring_Empty returns "" when the range is empty.
func TestBuffer_Substring_Empty(t *testing.T) {
	b := NewBuffer("hello")
	got := b.Substring(Position{Line: 0, Col: 2}, Position{Line: 0, Col: 2})
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// TestBuffer_EndPos returns the position just after the last rune.
func TestBuffer_EndPos(t *testing.T) {
	cases := []struct {
		src  string
		want Position
	}{
		{"", Position{Line: 0, Col: 0}},
		{"abc", Position{Line: 0, Col: 3}},
		{"abc\n", Position{Line: 1, Col: 0}},
		{"abc\nde", Position{Line: 1, Col: 2}},
	}
	for _, c := range cases {
		got := NewBuffer(c.src).EndPos()
		if got != c.want {
			t.Errorf("EndPos(%q) = %+v want %+v", c.src, got, c.want)
		}
	}
}

// TestPosLess_AndOrdered exercises the position-comparison helpers used by
// selection logic — every selection operation depends on this being correct.
func TestPosLess_AndOrdered(t *testing.T) {
	a := Position{Line: 0, Col: 5}
	b := Position{Line: 1, Col: 0}
	c := Position{Line: 0, Col: 5}

	if !PosLess(a, b) {
		t.Fatal("a should be < b")
	}
	if PosLess(b, a) {
		t.Fatal("b should not be < a")
	}
	if PosLess(a, c) {
		t.Fatal("equal positions should not be Less")
	}

	// Same line, different col.
	d := Position{Line: 1, Col: 3}
	e := Position{Line: 1, Col: 7}
	if !PosLess(d, e) {
		t.Fatal("d should be < e")
	}

	// PosOrdered normalises pairs.
	x, y := PosOrdered(b, a)
	if x != a || y != b {
		t.Fatalf("PosOrdered wrong: %+v %+v", x, y)
	}
	x, y = PosOrdered(a, b)
	if x != a || y != b {
		t.Fatalf("PosOrdered should be stable for already-ordered: %+v %+v", x, y)
	}
}
