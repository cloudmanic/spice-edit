// =============================================================================
// File: internal/app/diffviewer_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for diffviewer.go. The pure helpers (firstDiffNewLine,
// diffKindGlyphTheme, buildDiffEntries) are exercised with synthetic
// input so they don't fork git. The shell-out path (loadGitFileDiff,
// showDiffForSelected end-to-end) runs against a real `git init`'d repo
// and skips when git isn't on PATH.

package app

import (
	"path/filepath"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/theme"
	"github.com/gdamore/tcell/v2"
)

// TestHasDiffViewer_DefaultOff pins the menu predicate on a fresh App:
// no gitStatus snapshot yet → the "Git changes" row stays disabled.
func TestHasDiffViewer_DefaultOff(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if a.hasDiffViewer() {
		t.Fatal("hasDiffViewer should be false with no gitStatus snapshot")
	}
}

// TestMenuLayout_GitChangesRow ensures the row is present in the menu
// (disabled by default) so the gesture is discoverable even before the
// first refresh tick populates gitStatus.
func TestMenuLayout_GitChangesRow(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	item := menuItemByLabel(t, a, "Git changes")
	if item.action == nil {
		t.Fatal("Git changes row has no action")
	}
	if item.enabled == nil {
		t.Fatal("Git changes row has no enabled predicate")
	}
	if item.enabled(a) {
		t.Fatal("Git changes should be disabled without a repo / dirty files")
	}
}

// TestOpenDiffViewer_NotARepo guards the no-repo early return: the modal
// never opens and the cached empty gitStatus is reported via flash.
func TestOpenDiffViewer_NotARepo(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openDiffViewer()
	if a.diffOpen {
		t.Fatal("modal should not open for a non-repo")
	}
}

// TestOpenDiffViewer_RepoNoChanges guards the no-dirty-files return.
func TestOpenDiffViewer_RepoNoChanges(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.gitStatus = gitStatus{IsRepo: true, Root: a.rootDir, DirtyFiles: map[string]filetree.GitChangeKind{}}
	a.openDiffViewer()
	if a.diffOpen {
		t.Fatal("modal should not open with no dirty files")
	}
}

// TestOpenDiffViewer_BuildsList seeds a synthetic gitStatus and confirms
// openDiffViewer flips the modal on and builds a sorted entry list with
// the list view active (diffViewFile empty).
func TestOpenDiffViewer_BuildsList(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	root := a.rootDir
	a.gitStatus = gitStatus{
		IsRepo: true,
		Root:   root,
		DirtyFiles: map[string]filetree.GitChangeKind{
			filepath.Join(root, "zeta.go"):  filetree.GitChangeModified,
			filepath.Join(root, "alpha.go"): filetree.GitChangeAdded,
			filepath.Join(root, "mid.txt"):  filetree.GitChangeDeleted,
		},
	}
	a.openDiffViewer()
	if !a.diffOpen {
		t.Fatal("modal should be open")
	}
	if a.diffViewFile != "" {
		t.Fatalf("expected list view, diffViewFile=%q", a.diffViewFile)
	}
	want := []string{"alpha.go", "mid.txt", "zeta.go"}
	if len(a.diffEntries) != len(want) {
		t.Fatalf("entry count = %d, want %d (%v)", len(a.diffEntries), len(want), a.diffEntries)
	}
	for i, w := range want {
		if a.diffEntries[i].rel != w {
			t.Errorf("entries[%d].rel = %q, want %q", i, a.diffEntries[i].rel, w)
		}
	}
	if a.diffSelected != 0 {
		t.Errorf("diffSelected = %d, want 0", a.diffSelected)
	}
}

// TestBuildDiffEntries_RelativePaths confirms rel paths are computed
// against the repo root, not the tree root, so renames across the root
// boundary still display cleanly.
func TestBuildDiffEntries_RelativePaths(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	root := a.rootDir
	a.gitStatus = gitStatus{
		IsRepo: true,
		Root:   root,
		DirtyFiles: map[string]filetree.GitChangeKind{
			filepath.Join(root, "pkg", "inner.go"): filetree.GitChangeRenamed,
		},
	}
	entries := a.buildDiffEntries()
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	want := filepath.Join("pkg", "inner.go")
	if entries[0].rel != want {
		t.Errorf("rel = %q, want %q", entries[0].rel, want)
	}
	if entries[0].kind != filetree.GitChangeRenamed {
		t.Errorf("kind = %v, want Renamed", entries[0].kind)
	}
}

// TestFirstDiffNewLine pins the hunk-header parser used to land the
// cursor on the first visible change. Zero-based return: a hunk
// "+10,3" points at new-file line 10 → index 9.
func TestFirstDiffNewLine(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  int
	}{
		{"first hunk", []string{"diff --git a/x b/x", "@@ -1,3 +10,3 @@", "+foo", "-bar"}, 9},
		{"no hunks", []string{"diff --git a/x b/x"}, 0},
		{"malformed header", []string{"@@ junk @@"}, 0}, // malformed: parser skips
		{"empty", []string{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstDiffNewLine(c.lines); got != c.want {
				t.Errorf("firstDiffNewLine = %d, want %d", got, c.want)
			}
		})
	}
}

// TestFirstDiffNewLine_RealHunk uses a realistic unified-diff header.
func TestFirstDiffNewLine_RealHunk(t *testing.T) {
	lines := []string{
		"diff --git a/main.go b/main.go",
		"index 1234567..89abcde 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -20,7 +20,9 @@ func main() {",
		" old",
		"+new",
	}
	if got := firstDiffNewLine(lines); got != 20 {
		t.Fatalf("firstDiffNewLine = %d, want 20 (first + line after context)", got)
	}
}

// TestDiffKindGlyphTheme pins the kind → (glyph, colour) mapping so a
// refactor of the filetree kinds doesn't silently recolour the list.
func TestDiffKindGlyphTheme(t *testing.T) {
	th := theme.Default()
	cases := []struct {
		kind    filetree.GitChangeKind
		wantG   rune
		wantCol tcell.Color
	}{
		{filetree.GitChangeAdded, 'A', th.GitAdded},
		{filetree.GitChangeDeleted, 'D', th.GitDeleted},
		{filetree.GitChangeRenamed, 'R', th.AccentSoft},
		{filetree.GitChangeModified, 'M', th.GitModified},
		{filetree.GitChangeMixed, 'M', th.GitModified},
		{filetree.GitChangeNone, 'M', th.GitModified},
	}
	for _, c := range cases {
		g, col := diffKindGlyphTheme(c.kind, th)
		if g != c.wantG {
			t.Errorf("kind %v glyph = %q, want %q", c.kind, g, c.wantG)
		}
		if col != c.wantCol {
			t.Errorf("kind %v colour mismatch", c.kind)
		}
	}
}

// TestCloseAllModals_ClearsDiff ensures closeAllModals tears down diff
// state so a stale diffViewFile can't leak into a later modal.
func TestCloseAllModals_ClearsDiff(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.diffOpen = true
	a.diffEntries = []diffEntry{{abs: "/x", rel: "x", kind: filetree.GitChangeModified}}
	a.diffViewFile = "/x"
	a.diffLines = []string{"@@ -1 +1 @@"}
	a.closeAllModals()
	if a.diffOpen {
		t.Fatal("diffOpen should be false after closeAllModals")
	}
	if a.diffViewFile != "" {
		t.Fatalf("diffViewFile = %q, want cleared", a.diffViewFile)
	}
	if len(a.diffLines) != 0 {
		t.Fatal("diffLines should be cleared")
	}
}

// TestAnyModalOpen_IncludesDiff confirms the event-router guard sees
// the diff modal as an open overlay.
func TestAnyModalOpen_IncludesDiff(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if a.anyModalOpen() {
		t.Fatal("no modal open initially")
	}
	a.diffOpen = true
	if !a.anyModalOpen() {
		t.Fatal("anyModalOpen should be true while diff modal is up")
	}
}

// TestOpenDiffViewer_EndToEnd runs the whole pipeline against a real git
// repo: commit a file, modify it, refresh gitStatus, open the modal,
// drill into the diff, then open the file and confirm the cursor lands
// on the changed line. Skipped when git isn't installed.
func TestOpenDiffViewer_EndToEnd(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	target := filepath.Join(repo, "main.go")
	writeFileT(t, target, "package main\n\nfunc a() {}\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "init")

	// Modify: insert a line so the first hunk's new-start is line 3.
	writeFileT(t, target, "package main\n\nfunc a() {}\nfunc b() {}\n")

	a := newTestApp(t, repo)
	a.refreshGitStatus()
	if !a.hasDiffViewer() {
		t.Fatalf("hasDiffViewer should be true after refresh, gitStatus=%+v", a.gitStatus)
	}
	a.openDiffViewer()
	if !a.diffOpen {
		t.Fatal("modal should be open")
	}
	if len(a.diffEntries) != 1 {
		t.Fatalf("expected 1 dirty entry, got %d (%+v)", len(a.diffEntries), a.diffEntries)
	}

	// Drill into the diff view.
	a.showDiffForSelected()
	if a.diffViewFile == "" {
		t.Fatal("expected diff view after showDiffForSelected")
	}
	if len(a.diffLines) == 0 {
		t.Fatal("diffLines should be populated from git diff")
	}
	// The diff body must contain at least one + line (the added func b).
	found := false
	for _, l := range a.diffLines {
		if len(l) > 0 && l[0] == '+' && l != "+++" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diffLines contained no added line: %v", a.diffLines)
	}

	// Jump: open the file and confirm cursor lands on a line >= 0 inside
	// the changed region.
	a.openDiffFileAtChange()
	if a.diffOpen {
		t.Fatal("modal should close after openDiffFileAtChange")
	}
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected an open tab after jump")
	}
	if tab.Path != target {
		t.Errorf("tab.Path = %q, want %q", tab.Path, target)
	}
	// Hunk is "@@ -3 +3,2 @@" → new-start line 3 → zero-based 2.
	if tab.Cursor.Line < 2 {
		t.Errorf("cursor line = %d, want >= 2 (first changed line)", tab.Cursor.Line)
	}
}

// TestDrawDiff_ListView renders the modal and asserts the title and a
// seeded filename land on screen. Locks the draw path so a future layout
// refactor can't silently blank the modal.
func TestDrawDiff_ListView(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	root := a.rootDir
	a.gitStatus = gitStatus{
		IsRepo: true,
		Root:   root,
		DirtyFiles: map[string]filetree.GitChangeKind{
			filepath.Join(root, "alpha.go"): filetree.GitChangeAdded,
		},
	}
	a.openDiffViewer()
	a.draw()
	scr := a.screen.(tcell.SimulationScreen)
	scr.Show()

	_, my, _, _ := a.diffModalRect()
	title := screenLine(scr, my+1)
	if !contains(title, "Git changes") {
		t.Errorf("title row = %q, want to contain 'Git changes'", trimSpace(title))
	}
	row := screenLine(scr, my+3)
	if !contains(row, "alpha.go") {
		t.Errorf("list row = %q, want to contain 'alpha.go'", trimSpace(row))
	}
}

// TestDrawDiff_DiffView drills into a synthetic diff body and confirms
// the title switches to "Git diff · <base>".
func TestDrawDiff_DiffView(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	root := a.rootDir
	target := filepath.Join(root, "main.go")
	a.gitStatus = gitStatus{
		IsRepo: true,
		Root:   root,
		DirtyFiles: map[string]filetree.GitChangeKind{
			target: filetree.GitChangeModified,
		},
	}
	a.openDiffViewer()
	// Inject a synthetic diff body instead of shelling to git.
	a.diffViewFile = target
	a.diffLines = []string{"@@ -1 +1,2 @@", " ctx", "+added"}
	a.draw()
	scr := a.screen.(tcell.SimulationScreen)
	scr.Show()

	_, my, _, _ := a.diffModalRect()
	title := screenLine(scr, my+1)
	if !contains(title, "Git diff") || !contains(title, "main.go") {
		t.Errorf("title row = %q, want 'Git diff · main.go'", trimSpace(title))
	}
	body := screenLine(scr, my+3)
	if !contains(body, "@@") {
		t.Errorf("diff body row = %q, want to contain '@@'", trimSpace(body))
	}
}

// contains is a minimal strings.Contains stand-in kept local so the test
// file doesn't grow a strings import just for two assertions.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
