// =============================================================================
// File: internal/editor/wrap_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-11-27
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"strings"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// TestWrappedRenderingAndNavigation checks visual rows without altering text.
func TestWrappedRenderingAndNavigation(t *testing.T) {
	tab, _ := NewTab("")
	tab.InsertString("abcdefghij\nnext")
	tab.MoveCursorTo(Position{}, false)
	tab.SetWrap(true, 14) // five text cells and two margin cells
	scr := newSimScreen(t, 14, 4)
	defer scr.Fini()
	tab.Render(scr, theme.Default(), 0, 0, 14, 4)
	scr.Show()
	cells, width, _ := scr.GetContents()
	for row, want := range []string{"abcde", "fghij", "     ", "next "} {
		var got strings.Builder
		for x := 7; x < 12; x++ {
			got.WriteRune(cells[row*width+x].Runes[0])
		}
		if got.String() != want {
			t.Fatalf("row %d = %q, want %q", row, got.String(), want)
		}
	}
	p, ok := tab.HitTest(9, 1, 14, 4)
	if !ok || p != (Position{Line: 0, Col: 7}) {
		t.Fatalf("continuation hit = %+v, %v", p, ok)
	}
	tab.MoveVisual(1, 14, true)
	if tab.Cursor != (Position{Line: 0, Col: 5}) || tab.SelectionText() != "abcde" {
		t.Fatalf("visual selection: cursor %+v text %q", tab.Cursor, tab.SelectionText())
	}
	tab.Scroll(1)
	tab.Render(scr, theme.Default(), 0, 0, 14, 2)
	if tab.ScrollY != 1 {
		t.Fatalf("wheel scroll pulled back to cursor: %d", tab.ScrollY)
	}
	tab.MoveCursorTo(Position{Line: 0, Col: 10}, false)
	tab.Render(scr, theme.Default(), 0, 0, 14, 2)
	cx, cy, visible := scr.GetCursor()
	if !visible || cx != 7 || cy != 1 {
		t.Fatalf("end cursor = (%d,%d), visible %v", cx, cy, visible)
	}
	if tab.Buffer.String() != "abcdefghij\nnext" {
		t.Fatal("wrapping changed file contents")
	}
}

// TestWrappingTabsResizeAndToggle preserves hit tests and the visible line.
func TestWrappingTabsResizeAndToggle(t *testing.T) {
	tab, _ := NewTab("")
	tab.InsertString("a\tbcdefghij\nsecond\nthird")
	tab.MoveCursorTo(Position{}, false)
	tab.SetWrap(true, 12) // a tab crosses the three-cell row boundary
	scr := newSimScreen(t, 20, 6)
	defer scr.Fini()
	tab.Render(scr, theme.Default(), 0, 0, 12, 4)
	if p, ok := tab.HitTest(7, 1, 12, 4); !ok || p != (Position{Line: 0, Col: 1}) {
		t.Fatalf("split tab hit = %+v, %v", p, ok)
	}

	tab.MoveVisual(1, 12, false)
	if tab.Cursor != (Position{Line: 0, Col: 2}) {
		t.Fatalf("down into a split tab did not advance: %+v", tab.Cursor)
	}
	tab.Render(scr, theme.Default(), 0, 0, 12, 4)
	tab.Scroll(2)
	tab.Render(scr, theme.Default(), 0, 0, 12, 4)
	tab.Render(scr, theme.Default(), 0, 0, 14, 4)
	if p, ok := tab.HitTest(7, 0, 14, 4); !ok || p.Line != 0 {
		t.Fatalf("resize lost top line: %+v, %v", p, ok)
	}
	tab.SetWrap(false, 14)
	if tab.ScrollY != 0 {
		t.Fatalf("unwrapping lost top line: %d", tab.ScrollY)
	}
	tab.ScrollH(4)
	if tab.ScrollX != 4 {
		t.Fatalf("horizontal scroll disabled after unwrapping: %d", tab.ScrollX)
	}
	tab.SetWrap(true, 14)
	tab.ScrollH(4)
	if tab.ScrollX != 0 {
		t.Fatalf("wrapped view horizontally scrolled: %d", tab.ScrollX)
	}
}

// TestWordWrappingKeepsWordsAndMargin checks rendering, cursor and hit testing.
func TestWordWrappingKeepsWordsAndMargin(t *testing.T) {
	tab, _ := NewTab("")
	tab.InsertString("a good editor test")
	tab.MoveCursorTo(Position{}, false)
	tab.SetWrap(true, 19) // ten text cells plus a two-cell right margin
	scr := newSimScreen(t, 19, 4)
	defer scr.Fini()
	tab.Render(scr, theme.Default(), 0, 0, 19, 4)
	scr.Show()
	cells, width, _ := scr.GetContents()
	for row, want := range []string{"a good      ", "editor      ", "test        "} {
		var got strings.Builder
		for x := 7; x < 19; x++ {
			got.WriteRune(cells[row*width+x].Runes[0])
		}
		if got.String() != want {
			t.Fatalf("row %d = %q, want %q", row, got.String(), want)
		}
	}
	if p, ok := tab.HitTest(8, 1, 19, 4); !ok || p != (Position{Line: 0, Col: 8}) {
		t.Fatalf("word continuation hit = %+v, %v", p, ok)
	}
	tab.MoveVisual(1, 19, false)
	if tab.Cursor != (Position{Line: 0, Col: 7}) {
		t.Fatalf("down should land at start of editor: %+v", tab.Cursor)
	}
	tab.Render(scr, theme.Default(), 0, 0, 19, 4)
	x, y, visible := scr.GetCursor()
	if !visible || x != 7 || y != 1 {
		t.Fatalf("word cursor = (%d,%d), visible %v", x, y, visible)
	}
	if tab.Buffer.String() != "a good editor test" {
		t.Fatal("word wrapping changed the buffer")
	}
}

// TestWordWrapBoundaries covers oversized words, whitespace and exact fits.
func TestWordWrapBoundaries(t *testing.T) {
	tests := []struct {
		text string
		want []wrappedRow
	}{
		{"hi editor", []wrappedRow{{0, 3}, {3, 9}, {9, 9}}},
		{"abcdefghij", []wrappedRow{{0, 6}, {6, 10}}},
		{"ab\tcd", []wrappedRow{{0, 6}, {6, 6}}},
		{"hi\u00a0world", []wrappedRow{{0, 3}, {3, 8}}},
		{"", []wrappedRow{{0, 0}}},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := appendWrappedRows(nil, tt.text, 6)
			if len(got) != len(tt.want) {
				t.Fatalf("rows = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("rows = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestWordWrapReflowsAfterResizeAndEdit keeps layout in sync with the buffer.
func TestWordWrapReflowsAfterResizeAndEdit(t *testing.T) {
	tab, _ := NewTab("")
	tab.InsertString("one editor tail")
	tab.MoveCursorTo(Position{}, false)
	scr := newSimScreen(t, 22, 4)
	defer scr.Fini()
	tab.SetWrap(true, 18)
	tab.Render(scr, theme.Default(), 0, 0, 18, 4)
	if p, ok := tab.HitTest(7, 1, 18, 4); !ok || p.Col != 4 {
		t.Fatalf("editor should start on row 1: %+v, %v", p, ok)
	}
	tab.Render(scr, theme.Default(), 0, 0, 22, 4)
	if p, ok := tab.HitTest(11, 0, 22, 4); !ok || p.Col != 4 {
		t.Fatalf("editor should reflow onto row 0: %+v, %v", p, ok)
	}
	tab.InsertString("extra ")
	tab.Render(scr, theme.Default(), 0, 0, 22, 4)
	if p, ok := tab.HitTest(7, 1, 22, 4); !ok || p.Col != 10 {
		t.Fatalf("edited text should reflow editor to row 1: %+v, %v", p, ok)
	}
}
