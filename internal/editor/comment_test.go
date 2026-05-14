// =============================================================================
// File: internal/editor/comment_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-14
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import "testing"

// TestLineCommentPrefix_CommonExtensions pins the filename and extension
// lookup used by the toggle action before it mutates a buffer.
func TestLineCommentPrefix_CommonExtensions(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"main.go", "//", true},
		{"script.py", "#", true},
		{"query.sql", "--", true},
		{"config.ini", ";", true},
		{"Dockerfile", "#", true},
		{"index.html", "", false},
	}
	for _, c := range cases {
		got, ok := LineCommentPrefix(c.path)
		if got != c.want || ok != c.ok {
			t.Fatalf("LineCommentPrefix(%q) = %q, %v; want %q, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}

// TestToggleLineComment_CommentsSelectedLines checks the headline path:
// every selected non-blank line gets a comment marker at column zero.
func TestToggleLineComment_CommentsSelectedLines(t *testing.T) {
	tab := commentTestTab("main.go", "package main\nfunc main() {\n\tprintln(\"x\")\n}\n")
	tab.Anchor = Position{Line: 1, Col: 0}
	tab.Cursor = Position{Line: 3, Col: 0}

	changed, ok := tab.ToggleLineComment()

	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	want := "package main\n// func main() {\n// \tprintln(\"x\")\n}\n"
	if got := tab.Buffer.String(); got != want {
		t.Fatalf("buffer:\n%q\nwant:\n%q", got, want)
	}
	if !tab.Dirty || !tab.StyleStale {
		t.Fatal("toggle should dirty the tab and invalidate highlighting")
	}
}

// TestToggleLineComment_UncommentsWhenAllLinesCommented proves the toggle
// flips direction only when every non-blank target line is already commented.
func TestToggleLineComment_UncommentsWhenAllLinesCommented(t *testing.T) {
	tab := commentTestTab("main.go", "// one\n// \ttwo\n")
	tab.Anchor = Position{Line: 0, Col: 0}
	tab.Cursor = Position{Line: 2, Col: 0}

	changed, ok := tab.ToggleLineComment()

	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	want := "one\n\ttwo\n"
	if got := tab.Buffer.String(); got != want {
		t.Fatalf("buffer:\n%q\nwant:\n%q", got, want)
	}
}

// TestToggleLineComment_UncommentsIndentedExistingComments keeps the toggle
// tolerant of comments that already sit after indentation.
func TestToggleLineComment_UncommentsIndentedExistingComments(t *testing.T) {
	tab := commentTestTab("main.go", "\t// one\n  // two\n")
	tab.Anchor = Position{Line: 0, Col: 0}
	tab.Cursor = Position{Line: 2, Col: 0}

	changed, ok := tab.ToggleLineComment()

	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	want := "\tone\n  two\n"
	if got := tab.Buffer.String(); got != want {
		t.Fatalf("buffer:\n%q\nwant:\n%q", got, want)
	}
}

// TestToggleLineComment_MixedSelectionCommentsAllLines locks in the common
// editor rule: a mixed selection comments every non-blank line.
func TestToggleLineComment_MixedSelectionCommentsAllLines(t *testing.T) {
	tab := commentTestTab("main.go", "// one\n\n  two")
	tab.SelectAll()

	changed, ok := tab.ToggleLineComment()

	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	want := "// // one\n\n//   two"
	if got := tab.Buffer.String(); got != want {
		t.Fatalf("buffer:\n%q\nwant:\n%q", got, want)
	}
}

// TestToggleLineComment_SelectionEndingAtColumnZeroExcludesThatLine keeps
// whole-line selections from unexpectedly changing the first untouched line.
func TestToggleLineComment_SelectionEndingAtColumnZeroExcludesThatLine(t *testing.T) {
	tab := commentTestTab("main.go", "one\ntwo\nthree")
	tab.Anchor = Position{Line: 0, Col: 0}
	tab.Cursor = Position{Line: 2, Col: 0}

	changed, ok := tab.ToggleLineComment()

	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	want := "// one\n// two\nthree"
	if got := tab.Buffer.String(); got != want {
		t.Fatalf("buffer:\n%q\nwant:\n%q", got, want)
	}
}

// TestToggleLineComment_NoSelectionUsesCursorLine makes the menu item useful
// even when the user has not highlighted text first.
func TestToggleLineComment_NoSelectionUsesCursorLine(t *testing.T) {
	tab := commentTestTab("main.go", "one\ntwo\nthree")
	tab.Cursor = Position{Line: 1, Col: 1}
	tab.Anchor = tab.Cursor

	changed, ok := tab.ToggleLineComment()

	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	want := "one\n// two\nthree"
	if got := tab.Buffer.String(); got != want {
		t.Fatalf("buffer:\n%q\nwant:\n%q", got, want)
	}
}

// TestToggleLineComment_BlankSelectionIsNoop avoids adding comment markers
// to whitespace-only lines just because they were inside the selection.
func TestToggleLineComment_BlankSelectionIsNoop(t *testing.T) {
	tab := commentTestTab("main.go", "  \n\t")
	tab.SelectAll()

	changed, ok := tab.ToggleLineComment()

	if !ok {
		t.Fatal("blank Go selection should still have a known comment syntax")
	}
	if changed {
		t.Fatal("blank-only selection should not change the buffer")
	}
	if tab.Dirty || tab.CanUndo() {
		t.Fatal("blank-only selection should not dirty the tab or push undo")
	}
}

// TestToggleLineComment_UnsupportedFileTypeIsNoop protects formats like HTML
// where a line-comment marker would be wrong.
func TestToggleLineComment_UnsupportedFileTypeIsNoop(t *testing.T) {
	tab := commentTestTab("index.html", "<main></main>")

	changed, ok := tab.ToggleLineComment()

	if ok || changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want unsupported noop", changed, ok)
	}
	if got := tab.Buffer.String(); got != "<main></main>" {
		t.Fatalf("buffer changed for unsupported type: %q", got)
	}
}

// TestToggleLineComment_UndoRestoresSelectionAndText confirms the action is
// one structural undo step, including the cursor and active selection.
func TestToggleLineComment_UndoRestoresSelectionAndText(t *testing.T) {
	tab := commentTestTab("main.go", "one\ntwo")
	tab.Anchor = Position{Line: 0, Col: 1}
	tab.Cursor = Position{Line: 1, Col: 2}

	changed, ok := tab.ToggleLineComment()
	if !ok || !changed {
		t.Fatalf("ToggleLineComment() = %v, %v; want changed and ok", changed, ok)
	}
	if !tab.Undo() {
		t.Fatal("Undo should restore the pre-toggle snapshot")
	}
	if got := tab.Buffer.String(); got != "one\ntwo" {
		t.Fatalf("undo buffer = %q, want original", got)
	}
	if tab.Anchor != (Position{Line: 0, Col: 1}) || tab.Cursor != (Position{Line: 1, Col: 2}) {
		t.Fatalf("undo selection = anchor %+v cursor %+v", tab.Anchor, tab.Cursor)
	}
}

// commentTestTab constructs a text tab with undo initialized, without touching
// the filesystem.
func commentTestTab(path, text string) *Tab {
	t := &Tab{
		Path:       path,
		Buffer:     NewBuffer(text),
		StyleStale: false,
	}
	t.initUndo()
	return t
}
