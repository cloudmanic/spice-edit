// =============================================================================
// File: internal/editor/indent_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"strings"
	"testing"
)

// TestRuneVisualWidth_Tab pins down the tab-stop math: a tab at col 0
// fills the whole 4-cell stop; a tab at col 3 fills just one cell to
// reach col 4; a tab at col 4 fills another whole stop. This is the
// math every viewer (cat, less, GitHub) uses, and getting it wrong is
// the kind of off-by-one nobody will notice until it's everywhere.
func TestRuneVisualWidth_Tab(t *testing.T) {
	cases := []struct {
		col, want int
	}{
		{0, 4},
		{1, 3},
		{2, 2},
		{3, 1},
		{4, 4},
		{7, 1},
		{8, 4},
	}
	for _, c := range cases {
		if got := RuneVisualWidth('\t', c.col); got != c.want {
			t.Errorf("RuneVisualWidth(\\t, %d) = %d, want %d", c.col, got, c.want)
		}
	}
}

// TestRuneVisualWidth_Other always returns 1 for non-tabs. Wide-char
// support (CJK, emoji) is intentionally not implemented yet — pinning
// 1 here makes that gap explicit.
func TestRuneVisualWidth_Other(t *testing.T) {
	for _, r := range []rune{'a', '✓', '☃'} {
		if got := RuneVisualWidth(r, 0); got != 1 {
			t.Errorf("RuneVisualWidth(%q) = %d, want 1", r, got)
		}
	}
}

// TestLineVisualCol walks through a line containing tabs and confirms
// the visual column at each rune position. "\t\tfoo" should put 'f' at
// visual col 8 — two full tab stops in.
func TestLineVisualCol(t *testing.T) {
	runes := []rune("\t\tfoo")
	cases := []struct {
		runeCol, want int
	}{
		{0, 0}, // before first tab
		{1, 4}, // after first tab
		{2, 8}, // after second tab
		{3, 9}, // after 'f'
		{5, 11},
	}
	for _, c := range cases {
		if got := LineVisualCol(runes, c.runeCol); got != c.want {
			t.Errorf("LineVisualCol(runeCol=%d) = %d, want %d", c.runeCol, got, c.want)
		}
	}
}

// TestLineVisualCol_ClampsBeyondEnd lets callers pass a past-the-end
// column without worrying about bounds — the function returns the
// total visual width.
func TestLineVisualCol_ClampsBeyondEnd(t *testing.T) {
	if got := LineVisualCol([]rune("ab"), 99); got != 2 {
		t.Fatalf("expected clamp to length, got %d", got)
	}
}

// TestRuneColAtVisual_HitInsideTab snaps clicks anywhere in a tab's
// visual span back to the tab's rune index — clicking on cell 2 of a
// 4-cell tab should select rune 0 (the tab itself), not "between" it.
func TestRuneColAtVisual_HitInsideTab(t *testing.T) {
	runes := []rune("\tfoo")
	for visCol := 0; visCol < 4; visCol++ {
		if got := RuneColAtVisual(runes, visCol); got != 0 {
			t.Errorf("visCol=%d should snap to rune 0, got %d", visCol, got)
		}
	}
	if got := RuneColAtVisual(runes, 4); got != 1 {
		t.Errorf("visCol=4 should be rune 1 (the 'f'), got %d", got)
	}
}

// TestRuneColAtVisual_BeyondEnd returns the rune count so callers can
// place the cursor at end-of-line.
func TestRuneColAtVisual_BeyondEnd(t *testing.T) {
	runes := []rune("ab")
	if got := RuneColAtVisual(runes, 99); got != len(runes) {
		t.Fatalf("expected end-of-line index, got %d", got)
	}
}

// TestRuneColAtVisual_NegativeIsZero clamps left-of-line to col 0 — a
// guard for tests and any caller that subtracts a scroll offset.
func TestRuneColAtVisual_NegativeIsZero(t *testing.T) {
	if got := RuneColAtVisual([]rune("ab"), -3); got != 0 {
		t.Fatalf("expected 0 for negative input, got %d", got)
	}
}

// TestDetectIndent_TabsWin returns "\t" when the file is dominated by
// tab-indented lines.
func TestDetectIndent_TabsWin(t *testing.T) {
	lines := []string{
		"package x",
		"",
		"\tfunc x() {",
		"\t\treturn 1",
		"\t}",
	}
	if got := DetectIndent(lines, "x.go"); got != "\t" {
		t.Fatalf("expected tab indent, got %q", got)
	}
}

// TestDetectIndent_SpacesWin_FourWide infers an indent width from the
// smallest non-zero leading-space count. Four spaces here is the most
// common indented prefix, so that's the unit.
func TestDetectIndent_SpacesWin_FourWide(t *testing.T) {
	lines := []string{
		"def foo():",
		"    if x:",
		"        return 1",
		"    return 0",
	}
	if got := DetectIndent(lines, "foo.py"); got != "    " {
		t.Fatalf("expected 4-space indent, got %q", got)
	}
}

// TestDetectIndent_SpacesWin_TwoWide proves the smallest-leading-spaces
// heuristic: a file with 2/4/6 indents should infer width 2.
func TestDetectIndent_SpacesWin_TwoWide(t *testing.T) {
	lines := []string{
		"function foo() {",
		"  if (x) {",
		"    return 1;",
		"  }",
		"}",
	}
	if got := DetectIndent(lines, "foo.js"); got != "  " {
		t.Fatalf("expected 2-space indent, got %q", got)
	}
}

// TestDetectIndent_NoSignal_GoDefaultsToTab covers the empty-file path
// for Go, where the convention is so strong (gofmt enforces tabs) that
// guessing spaces would just annoy the user.
func TestDetectIndent_NoSignal_GoDefaultsToTab(t *testing.T) {
	if got := DetectIndent([]string{""}, "new.go"); got != "\t" {
		t.Fatalf("empty .go should default to tab, got %q", got)
	}
}

// TestDetectIndent_NoSignal_MakefileDefaultsToTab covers the Makefile
// case, where tabs aren't a preference — they're literally required by
// make for recipe lines.
func TestDetectIndent_NoSignal_MakefileDefaultsToTab(t *testing.T) {
	if got := DetectIndent([]string{""}, "Makefile"); got != "\t" {
		t.Fatalf("empty Makefile should default to tab, got %q", got)
	}
}

// TestDetectIndent_NoSignal_DefaultsToFourSpaces is the catch-all path
// for a brand-new buffer with no extension hint.
func TestDetectIndent_NoSignal_DefaultsToFourSpaces(t *testing.T) {
	if got := DetectIndent([]string{""}, "untitled.txt"); got != strings.Repeat(" ", 4) {
		t.Fatalf("unknown extension should default to 4 spaces, got %q", got)
	}
}

// TestDetectIndent_MixedFavorsMajority decides ties on dominant count.
// A file with 5 tab lines and 1 space line is conceptually a tab file;
// the user opening it shouldn't have spaces inserted that would mix
// into the rest.
func TestDetectIndent_MixedFavorsMajority(t *testing.T) {
	lines := []string{
		"\tone",
		"\ttwo",
		"\tthree",
		"\tfour",
		"\tfive",
		"  oddball",
	}
	if got := DetectIndent(lines, "x.txt"); got != "\t" {
		t.Fatalf("expected tab from majority, got %q", got)
	}
}
