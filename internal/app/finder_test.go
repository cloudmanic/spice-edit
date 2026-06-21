// =============================================================================
// File: internal/app/finder_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/gdamore/tcell/v2"
)

// waitForFinderReady spins until the App's finder reports
// StateReady or the timeout expires. Pulled out so each test
// can read as the scenario it's pinning down rather than the
// goroutine-sync boilerplate.
func waitForFinderReady(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.finder != nil && a.finder.State() == finder.StateReady {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("finder did not reach StateReady within 2s")
}

// withFinder wires up an App + an indexed Finder rooted at a
// tempdir we seed with a few files. Tests use it as the entry
// point so they don't repeat the setup chain.
func withFinder(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"main.go",
		"internal/app/app.go",
		"internal/app/tab.go",
		"internal/finder/score.go",
		"README.md",
	}
	for _, f := range files {
		abs := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	a := newTestApp(t, dir)
	a.finder = finder.New(a.rootDir)
	a.finder.Rebuild(nil)
	waitForFinderReady(t, a)
	return a, dir
}

// TestOpenFinder_PopulatesResults pins the central wiring: opening
// the finder with a warm index immediately fills finderResults so
// the first frame shows real paths, not "Indexing…". Without this
// the user would see an empty list and worry the feature broke.
func TestOpenFinder_PopulatesResults(t *testing.T) {
	a, _ := withFinder(t)
	a.openFinder()

	if !a.finderOpen {
		t.Fatal("finderOpen should be true after openFinder")
	}
	if len(a.finderResults) == 0 {
		t.Fatal("expected initial result list (empty query → alphabetical)")
	}
}

// TestFinderKey_TypingFiltersResults walks the keystroke loop:
// typing "tab" narrows the list to paths matching that query, and
// the highlighted match should bubble tab.go to the top.
func TestFinderKey_TypingFiltersResults(t *testing.T) {
	a, _ := withFinder(t)
	a.openFinder()
	for _, r := range "tab" {
		a.handleFinderKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}

	if len(a.finderResults) == 0 {
		t.Fatal("expected results after typing query")
	}
	if !endsWith(a.finderResults[0].Path, "tab.go") {
		t.Fatalf("top result: got %q, want ends-with tab.go", a.finderResults[0].Path)
	}
}

// TestFinderKey_BackspaceShrinksQuery checks the inverse: deleting
// characters re-broadens the result set. Catches a regression
// where backspace edits the query but forgets to rerun the search.
func TestFinderKey_BackspaceShrinksQuery(t *testing.T) {
	a, _ := withFinder(t)
	a.openFinder()
	for _, r := range "score" {
		a.handleFinderKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	narrow := len(a.finderResults)

	// Backspace twice — the query becomes "sco", broader match.
	a.handleFinderKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	a.handleFinderKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))

	if len(a.finderResults) < narrow {
		t.Fatalf("backspace should not shrink results: was %d, now %d", narrow, len(a.finderResults))
	}
	if got := string(a.finderQuery); got != "sco" {
		t.Fatalf("query: got %q, want %q", got, "sco")
	}
}

// TestFinderKey_ArrowsMoveSelection pins the navigation contract:
// ↓ moves to the next row, ↑ moves back, neither runs off the end.
// A regression here would let Enter open the wrong file.
func TestFinderKey_ArrowsMoveSelection(t *testing.T) {
	a, _ := withFinder(t)
	a.openFinder()

	a.handleFinderKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if a.finderSelected != 1 {
		t.Fatalf("selected after ↓: got %d, want 1", a.finderSelected)
	}
	a.handleFinderKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if a.finderSelected != 0 {
		t.Fatalf("selected after ↑: got %d, want 0", a.finderSelected)
	}
	// ↑ at the top stays put.
	a.handleFinderKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if a.finderSelected != 0 {
		t.Fatalf("selected at top after ↑: got %d, want 0", a.finderSelected)
	}
}

// TestFinderKey_EnterOpensFile is the headline outcome: pressing
// Enter on a result actually opens that file as a tab and
// dismisses the modal. Without this the whole feature is nothing
// but a viewer.
func TestFinderKey_EnterOpensFile(t *testing.T) {
	a, dir := withFinder(t)
	a.openFinder()
	for _, r := range "score" {
		a.handleFinderKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	if len(a.finderResults) == 0 {
		t.Fatal("expected score results")
	}
	want := filepath.Join(dir, a.finderResults[0].Path)

	a.handleFinderKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if a.finderOpen {
		t.Fatal("modal should close after Enter")
	}
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected an active tab after opening result")
	}
	if tab.Path != want {
		t.Fatalf("opened path: got %q, want %q", tab.Path, want)
	}
}

// TestFinderKey_EscClosesModal pins the cancel path: Esc must
// dismiss without opening anything, and clear transient state so
// reopening starts fresh (no stale query, no stale selection).
func TestFinderKey_EscClosesModal(t *testing.T) {
	a, _ := withFinder(t)
	a.openFinder()
	a.handleFinderKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	a.handleFinderKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))

	if a.finderOpen {
		t.Fatal("Esc should close the finder")
	}
	if len(a.finderQuery) != 0 {
		t.Fatalf("query should be cleared after close, got %q", string(a.finderQuery))
	}
}

// TestFinderMouse_ClickOpensRow walks the click path: clicking on
// a result row both selects it and opens the file. We use the
// modal rect from the same helper the renderer uses so the row
// math stays in sync if either side changes.
func TestFinderMouse_ClickOpensRow(t *testing.T) {
	a, dir := withFinder(t)
	a.openFinder()
	if len(a.finderResults) < 2 {
		t.Fatalf("need at least 2 results for click test, got %d", len(a.finderResults))
	}
	mx, my, _, _ := a.finderModalRect()
	target := filepath.Join(dir, a.finderResults[1].Path)

	a.handleFinderMouse(mx+5, my+4+1, tcell.Button1)

	if a.finderOpen {
		t.Fatal("modal should close after click-open")
	}
	if got := a.activeTabPtr().Path; got != target {
		t.Fatalf("opened path: got %q, want %q", got, target)
	}
}

// TestFinderMouse_ClickOutsideCloses guards the dismiss path: a
// click that lands outside the modal should close it without
// opening anything. Otherwise a stray click could leave a tab
// open the user didn't ask for.
func TestFinderMouse_ClickOutsideCloses(t *testing.T) {
	a, _ := withFinder(t)
	a.openFinder()
	tabsBefore := len(a.tabs)

	a.handleFinderMouse(0, 0, tcell.Button1)

	if a.finderOpen {
		t.Fatal("modal should close on outside click")
	}
	if len(a.tabs) != tabsBefore {
		t.Fatalf("tab count changed unexpectedly: %d → %d", tabsBefore, len(a.tabs))
	}
}

// TestLeader_PFiresFinder pins the Esc-p binding so a future
// refactor of leader.go can't quietly drop it.
func TestLeader_PFiresFinder(t *testing.T) {
	a, _ := withFinder(t)
	if action := leaderActionFor('p'); action == nil {
		t.Fatal("Esc-p has no leader binding")
	} else {
		action(a)
	}
	if !a.finderOpen {
		t.Fatal("Esc-p should open the finder")
	}
}

// endsWith is a tiny string suffix check pulled in so the result-
// path assertions in this file read as the rule they're enforcing.
func endsWith(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
