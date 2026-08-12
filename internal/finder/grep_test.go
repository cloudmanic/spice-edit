// =============================================================================
// File: internal/finder/grep_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-06-21
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package finder

import (
	"os"
	"path/filepath"
	"testing"
)

// buildReady returns a Finder whose index has been built synchronously so
// SearchContent has a stable path list to grep.
func buildReady(t *testing.T, root string) *Finder {
	t.Helper()
	f := New(root)
	done := make(chan struct{})
	f.Rebuild(func() { close(done) })
	<-done
	if f.State() != StateReady {
		t.Fatalf("index state = %v, want StateReady", f.State())
	}
	return f
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchContentFindsMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package main\nfunc Hello() {}\n")
	writeFile(t, dir, "sub/b.txt", "hello world\nHELLO again\n")

	f := buildReady(t, dir)

	got := f.SearchContent("hello", 100)
	if len(got) != 3 {
		t.Fatalf("SearchContent(hello) = %d matches, want 3: %+v", len(got), got)
	}

	// Case-insensitive: the ALL-CAPS "HELLO" line must match too.
	var sawCaps bool
	for _, m := range got {
		if m.Path == "sub/b.txt" && m.Line == 1 {
			sawCaps = true
			if m.Col != 0 {
				t.Errorf("HELLO match col = %d, want 0", m.Col)
			}
		}
	}
	if !sawCaps {
		t.Errorf("expected a case-insensitive match on the HELLO line")
	}
}

func TestSearchContentEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "anything")
	f := buildReady(t, dir)
	if got := f.SearchContent("", 100); got != nil {
		t.Fatalf("empty query should return nil, got %+v", got)
	}
}

func TestSearchContentSkipsBinary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bin.dat", "match\x00match\n")
	writeFile(t, dir, "text.txt", "match here\n")
	f := buildReady(t, dir)

	got := f.SearchContent("match", 100)
	for _, m := range got {
		if m.Path == "bin.dat" {
			t.Fatalf("binary file should be skipped, got match %+v", m)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want 1 match (text only), got %d: %+v", len(got), got)
	}
}

func TestSearchContentLimit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "many.txt", "x\nx\nx\nx\nx\n")
	f := buildReady(t, dir)

	got := f.SearchContent("x", 3)
	if len(got) != 3 {
		t.Fatalf("limit=3 should cap results, got %d", len(got))
	}
}

func TestSearchContentPreviewAndCol(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "c.txt", "  indented needle here\n")
	f := buildReady(t, dir)

	got := f.SearchContent("needle", 10)
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d", len(got))
	}
	m := got[0]
	if m.Preview != "  indented needle here" {
		t.Errorf("preview = %q, want full untrimmed line", m.Preview)
	}
	// "  indented " is 11 runes before "needle".
	if m.Col != 11 {
		t.Errorf("col = %d, want 11", m.Col)
	}
	if m.Width != 6 {
		t.Errorf("width = %d, want 6", m.Width)
	}
}
