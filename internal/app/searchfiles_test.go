// =============================================================================
// File: internal/app/searchfiles_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-06-21
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/gdamore/tcell/v2"
)

// withSearch wires an App + indexed finder rooted at a tempdir seeded with
// files whose *contents* we can grep. Mirrors withFinder but the bodies
// matter here, not just the paths.
func withSearch(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"main.go":              "package main\n\nfunc widget() {}\n",
		"internal/app/app.go":  "package app\n// a widget lives here\n",
		"internal/finder/x.go": "package finder\nfunc unrelated() {}\n",
		"README.md":            "no match on that word\n",
	}
	for f, body := range files {
		abs := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(body), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	a := newTestApp(t, dir)
	a.finder = finder.New(a.rootDir)
	a.finder.Rebuild(nil)
	waitForFinderReady(t, a)
	return a, dir
}

// runSearchSync drives the search synchronously: it greps on the calling
// goroutine and applies the result the same way the event loop would, so
// tests don't have to race the background goroutine.
func runSearchSync(a *App, query string) {
	a.searchQuery = []rune(query)
	a.searchCursor = len(a.searchQuery)
	a.searchGen++
	gen := a.searchGen
	results := a.finder.SearchContent(query, searchLimit)
	a.applySearchResults(&searchResultsEvent{gen: gen, results: results})
}

// TestOpenSearchFiles_State pins the open/close wiring.
func TestOpenSearchFiles_State(t *testing.T) {
	a, _ := withSearch(t)
	a.openSearchFiles()
	if !a.searchOpen {
		t.Fatal("searchOpen should be true after openSearchFiles")
	}
	a.closeSearchFiles()
	if a.searchOpen || a.searchResults != nil {
		t.Fatal("closeSearchFiles should clear modal state")
	}
}

// TestSearch_FindsAcrossFiles greps a word present in two files and
// asserts both matches show up.
func TestSearch_FindsAcrossFiles(t *testing.T) {
	a, _ := withSearch(t)
	a.openSearchFiles()
	runSearchSync(a, "widget")

	if !a.searchDone {
		t.Fatal("searchDone should be set after results applied")
	}
	if len(a.searchResults) != 2 {
		t.Fatalf("want 2 'widget' matches, got %d: %+v", len(a.searchResults), a.searchResults)
	}
}

// TestSearch_EmptyQueryClears verifies deleting back to empty clears the
// result list without hitting the filesystem.
func TestSearch_EmptyQueryClears(t *testing.T) {
	a, _ := withSearch(t)
	a.openSearchFiles()
	runSearchSync(a, "widget")
	if len(a.searchResults) == 0 {
		t.Fatal("precondition: expected matches")
	}
	a.searchQuery = nil
	a.searchCursor = 0
	a.runSearch()
	if a.searchResults != nil {
		t.Fatalf("empty query should clear results, got %+v", a.searchResults)
	}
}

// TestSearch_OpenSelectedJumps opens the selected match and asserts the
// right file is active with the cursor on the match line.
func TestSearch_OpenSelectedJumps(t *testing.T) {
	a, _ := withSearch(t)
	a.openSearchFiles()
	runSearchSync(a, "unrelated")

	if len(a.searchResults) != 1 {
		t.Fatalf("want 1 match, got %d", len(a.searchResults))
	}
	m := a.searchResults[0]
	a.searchSelected = 0
	a.openSelectedSearchResult()

	if a.searchOpen {
		t.Fatal("opening a result should close the modal")
	}
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected an active tab after jumping to a match")
	}
	if filepath.Base(tab.Path) != "x.go" {
		t.Fatalf("active tab = %q, want x.go", tab.Path)
	}
	if tab.Cursor.Line != m.Line {
		t.Fatalf("cursor line = %d, want %d", tab.Cursor.Line, m.Line)
	}
}

// TestSearch_SingleFileModeNoOp confirms the modal refuses to open when
// there's no project tree (single-file invocation).
func TestSearch_SingleFileModeNoOp(t *testing.T) {
	a, _ := withSearch(t)
	a.tree = nil
	a.openSearchFiles()
	if a.searchOpen {
		t.Fatal("search modal must not open in single-file mode")
	}
}

// TestSearch_KeyTypingTriggers ensures typing a rune routes through the
// search key handler and updates the query.
func TestSearch_KeyTypingTriggers(t *testing.T) {
	a, _ := withSearch(t)
	a.openSearchFiles()
	a.handleSearchKey(keyEv(tcell.KeyRune, 'w'))
	if string(a.searchQuery) != "w" {
		t.Fatalf("query = %q, want w", string(a.searchQuery))
	}
	a.handleSearchKey(keyEv(tcell.KeyEsc, 0))
	if a.searchOpen {
		t.Fatal("Esc should close the search modal")
	}
}
