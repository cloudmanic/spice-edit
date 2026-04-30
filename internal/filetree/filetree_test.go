// =============================================================================
// File: internal/filetree/filetree_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the filetree package — the lazy file explorer that powers the
// editor's left sidebar. These pin down disk-merge behavior (refresh keeps
// expanded folders open), the small visibility/hide rules, the flatten +
// hit-test math, and a handful of render assertions made via tcell's
// SimulationScreen so we can verify chevrons, the bold active row, etc.

package filetree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// mkTree is a tiny helper that builds a small directory layout under t.TempDir
// and returns the absolute root path. Several tests use the same shape so
// pulling it into a helper keeps each test focused on the behavior it cares
// about rather than scaffolding.
func mkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustMkdir(t, filepath.Join(root, "Beta"))
	mustMkdir(t, filepath.Join(root, ".git")) // hidden — should be filtered
	mustMkdir(t, filepath.Join(root, "node_modules"))
	mustWrite(t, filepath.Join(root, "zeta.txt"), "z")
	mustWrite(t, filepath.Join(root, "Apple.md"), "a")
	mustWrite(t, filepath.Join(root, ".DS_Store"), "junk")
	mustWrite(t, filepath.Join(root, "alpha", "inner.go"), "package x")
	return root
}

// mustMkdir is a fail-on-error mkdir helper for test setup.
func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

// mustWrite is a fail-on-error file-write helper for test setup.
func mustWrite(t *testing.T, p, contents string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// findChild walks a node's children for an entry named name. Returns nil
// when not present so tests can assert absence as well as presence.
func findChild(n *Node, name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestNew_NonExistentRoot verifies that pointing the tree at a path that
// doesn't exist surfaces an error rather than panicking or producing an
// empty tree (which would silently mislead the user).
func TestNew_NonExistentRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := New(missing); err == nil {
		t.Fatal("expected error for non-existent root")
	}
}

// TestNew_RootIsFile guards the "user passed a filename, not a folder" case.
// The constructor should reject it instead of trying to read children.
func TestNew_RootIsFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	mustWrite(t, f, "hi")
	if _, err := New(f); err == nil {
		t.Fatal("expected error when root is a regular file")
	}
}

// TestNew_LoadsAndHides confirms a successful build returns a tree whose
// root is expanded, has its children loaded, and excludes the well-known
// noise entries (.git, node_modules, .DS_Store).
func TestNew_LoadsAndHides(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !tr.Root.IsDir || !tr.Root.Expanded || !tr.Root.Loaded {
		t.Fatalf("root flags wrong: %+v", tr.Root)
	}
	for _, hidden := range []string{".git", ".DS_Store", "node_modules"} {
		if findChild(tr.Root, hidden) != nil {
			t.Fatalf("hidden entry %s should have been filtered", hidden)
		}
	}
	// Sanity: visible names ARE present.
	for _, want := range []string{"alpha", "Beta", "zeta.txt", "Apple.md"} {
		if findChild(tr.Root, want) == nil {
			t.Fatalf("expected child %s to be present", want)
		}
	}
}

// TestLoadChildren_SortOrder asserts directories sort before files and that
// each group is case-insensitive alphabetical — what users expect from a
// VSCode-style sidebar.
func TestLoadChildren_SortOrder(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	names := make([]string, 0, len(tr.Root.Children))
	for _, c := range tr.Root.Children {
		names = append(names, c.Name)
	}
	// Expected: alpha, Beta (dirs alpha-by-lower), then Apple.md, zeta.txt.
	want := []string{"alpha", "Beta", "Apple.md", "zeta.txt"}
	if len(names) != len(want) {
		t.Fatalf("child count mismatch: got %v want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("sort mismatch at %d: got %q want %q (full=%v)", i, names[i], n, names)
		}
	}
}

// TestRefresh_PreservesExpandedState verifies that refreshing the tree
// after files appear or vanish on disk keeps the *Node pointers (and
// their Expanded flag) intact for entries that still exist — important
// because the 10-second auto-refresh would otherwise collapse every
// folder the user had opened.
func TestRefresh_PreservesExpandedState(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alpha := findChild(tr.Root, "alpha")
	if alpha == nil {
		t.Fatal("alpha missing")
	}
	tr.Toggle(alpha) // expand + load
	if !alpha.Expanded || !alpha.Loaded {
		t.Fatalf("alpha state after toggle wrong: %+v", alpha)
	}

	// Mutate disk: add a new sibling, remove zeta.txt.
	mustWrite(t, filepath.Join(root, "Newcomer.txt"), "n")
	if err := os.Remove(filepath.Join(root, "zeta.txt")); err != nil {
		t.Fatalf("remove zeta: %v", err)
	}

	tr.Refresh()

	// Pointer identity preserved for survivors.
	alphaAfter := findChild(tr.Root, "alpha")
	if alphaAfter != alpha {
		t.Fatal("alpha pointer changed across refresh")
	}
	if !alphaAfter.Expanded {
		t.Fatal("alpha.Expanded was lost across refresh")
	}
	// New file appears.
	if findChild(tr.Root, "Newcomer.txt") == nil {
		t.Fatal("Newcomer.txt should have been picked up")
	}
	// Deleted file vanished.
	if findChild(tr.Root, "zeta.txt") != nil {
		t.Fatal("zeta.txt should have been removed from the tree")
	}
}

// TestShouldHide is an exhaustive table for the small hide list — keeps
// future edits to that list honest by showing exactly what's in/out.
func TestShouldHide(t *testing.T) {
	cases := []struct {
		name string
		hide bool
	}{
		{".git", true},
		{".svn", true},
		{".hg", true},
		{".DS_Store", true},
		{"node_modules", true},
		{".idea", true},
		{".vscode", true},
		{"main.go", false},
		{"README.md", false},
		{".env", false}, // dotfiles are intentionally NOT hidden
		{"git", false},
		{"node_modules2", false},
	}
	for _, tc := range cases {
		if got := shouldHide(tc.name); got != tc.hide {
			t.Errorf("shouldHide(%q) = %v, want %v", tc.name, got, tc.hide)
		}
	}
}

// TestFlattenInto_Collapsed ensures a non-expanded directory contributes
// only itself to the flat list — its children stay hidden until the user
// expands it.
func TestFlattenInto_Collapsed(t *testing.T) {
	dir := &Node{Name: "d", IsDir: true, Expanded: false, Children: []*Node{
		{Name: "c1"}, {Name: "c2"},
	}}
	var out []flatNode
	flattenInto(dir, 0, &out)
	if len(out) != 1 {
		t.Fatalf("expected 1 row for collapsed dir, got %d", len(out))
	}
	if out[0].Depth != 0 {
		t.Fatalf("depth wrong: %d", out[0].Depth)
	}
}

// TestFlattenInto_Expanded checks the recursive case: an expanded directory
// flattens itself plus children at depth+1, and nested expansion compounds.
func TestFlattenInto_Expanded(t *testing.T) {
	leaf := &Node{Name: "leaf"}
	inner := &Node{Name: "inner", IsDir: true, Expanded: true, Children: []*Node{leaf}}
	root := &Node{Name: "root", IsDir: true, Expanded: true, Children: []*Node{inner}}

	var out []flatNode
	flattenInto(root, 0, &out)

	if len(out) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(out))
	}
	if out[0].Node.Name != "root" || out[0].Depth != 0 {
		t.Fatalf("row 0 wrong: %+v", out[0])
	}
	if out[1].Node.Name != "inner" || out[1].Depth != 1 {
		t.Fatalf("row 1 wrong: %+v", out[1])
	}
	if out[2].Node.Name != "leaf" || out[2].Depth != 2 {
		t.Fatalf("row 2 wrong: %+v", out[2])
	}
}

// TestFlattenInto_NilSafe documents that a nil *Node is a tolerated input
// (defensive: avoids requiring callers to nil-check before recursing).
func TestFlattenInto_NilSafe(t *testing.T) {
	var out []flatNode
	flattenInto(nil, 0, &out)
	if len(out) != 0 {
		t.Fatalf("nil node should produce no rows, got %d", len(out))
	}
}

// TestToggle_LoadsThenFlips verifies the two-step contract for Toggle:
// the first call on a never-loaded directory loads its children AND flips
// Expanded; subsequent calls just flip Expanded without re-reading disk.
func TestToggle_LoadsThenFlips(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alpha := findChild(tr.Root, "alpha")
	if alpha.Loaded || alpha.Expanded {
		t.Fatalf("alpha should start unloaded+collapsed: %+v", alpha)
	}

	tr.Toggle(alpha)
	if !alpha.Expanded || !alpha.Loaded {
		t.Fatalf("after first toggle alpha should be expanded+loaded: %+v", alpha)
	}
	if len(alpha.Children) == 0 {
		t.Fatal("expected alpha's children to be loaded")
	}

	tr.Toggle(alpha)
	if alpha.Expanded {
		t.Fatal("second toggle should collapse")
	}
	tr.Toggle(alpha)
	if !alpha.Expanded {
		t.Fatal("third toggle should re-expand")
	}
}

// TestToggle_FileIsNoop ensures Toggle on a file doesn't mutate state —
// only directories have an open/closed concept.
func TestToggle_FileIsNoop(t *testing.T) {
	tr := &Tree{Root: &Node{Name: "r", IsDir: true}}
	f := &Node{Name: "x.txt"}
	tr.Toggle(f)
	if f.Expanded || f.Loaded {
		t.Fatalf("file node should not be mutated: %+v", f)
	}
}

// TestScroll_ClampsAtZero exercises Tree.Scroll's lower bound: scrolling
// past the top should pin at 0 rather than going negative.
func TestScroll_ClampsAtZero(t *testing.T) {
	tr := &Tree{Root: &Node{IsDir: true}}
	tr.Scroll(-5)
	if tr.ScrollY != 0 {
		t.Fatalf("ScrollY should clamp to 0, got %d", tr.ScrollY)
	}
	tr.Scroll(3)
	if tr.ScrollY != 3 {
		t.Fatalf("expected ScrollY=3, got %d", tr.ScrollY)
	}
	tr.Scroll(-10)
	if tr.ScrollY != 0 {
		t.Fatalf("ScrollY should clamp to 0 after big up-scroll, got %d", tr.ScrollY)
	}
}

// TestClampScroll_AllCases tabulates clampScroll's three regimes: list
// fits entirely (=> 0), overflow with valid scroll (=> unchanged), and
// scroll past max (=> pinned to total-viewH).
func TestClampScroll_AllCases(t *testing.T) {
	cases := []struct {
		label  string
		start  int
		total  int
		viewH  int
		expect int
	}{
		{"fits entirely", 4, 5, 10, 0},
		{"in range", 3, 20, 10, 3},
		{"past max", 50, 20, 10, 10},
		{"negative", -5, 20, 10, 0},
	}
	for _, c := range cases {
		tr := &Tree{ScrollY: c.start}
		tr.clampScroll(c.total, c.viewH)
		if tr.ScrollY != c.expect {
			t.Errorf("%s: ScrollY=%d want %d", c.label, tr.ScrollY, c.expect)
		}
	}
}

// TestHitTest_HeaderRows confirms clicks on the two header rows return
// ok=false — those rows are the explorer label and project name, not
// targets.
func TestHitTest_HeaderRows(t *testing.T) {
	tr := &Tree{visible: []*Node{{Name: "a"}}}
	for _, y := range []int{0, 1} {
		if n, ok := tr.HitTest(0, y); ok || n != nil {
			t.Fatalf("y=%d should miss, got ok=%v node=%v", y, ok, n)
		}
	}
}

// TestHitTest_ValidRow checks the happy path: a click on a real row maps
// back to the same Node we recorded during the last Render.
func TestHitTest_ValidRow(t *testing.T) {
	target := &Node{Name: "x"}
	tr := &Tree{visible: []*Node{target, nil}}
	n, ok := tr.HitTest(5, 2) // first list row
	if !ok || n != target {
		t.Fatalf("expected hit on target, got ok=%v n=%v", ok, n)
	}
	// nil entry (blank padding row) should miss.
	if n, ok := tr.HitTest(5, 3); ok || n != nil {
		t.Fatalf("blank row should miss, got ok=%v n=%v", ok, n)
	}
}

// TestHitTest_OutOfRange covers clicks below the last visible row — the
// renderer pads with nil but the hit test should still cleanly miss.
func TestHitTest_OutOfRange(t *testing.T) {
	tr := &Tree{visible: []*Node{{Name: "a"}}}
	if n, ok := tr.HitTest(0, 99); ok || n != nil {
		t.Fatalf("out-of-range should miss, got ok=%v n=%v", ok, n)
	}
}

// renderAndCollect is a small helper that builds a SimulationScreen, runs
// Tree.Render, and returns the cell buffer + width so individual tests
// can inspect both runes and styles.
func renderAndCollect(t *testing.T, tr *Tree, w, h int) ([]tcell.SimCell, int) {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("scr.Init: %v", err)
	}
	t.Cleanup(scr.Fini)
	scr.SetSize(w, h)
	tr.Render(scr, theme.Default(), 0, 0, w, h)
	scr.Show() // flush back buffer to front so GetContents sees it
	cells, cw, _ := scr.GetContents()
	return cells, cw
}

// rowText reconstructs the visible text of a single screen row, which is
// far more readable in test failures than dumping the raw cell array.
func rowText(cells []tcell.SimCell, w, y int) string {
	row := make([]rune, 0, w)
	for x := 0; x < w; x++ {
		c := cells[y*w+x]
		if len(c.Runes) == 0 {
			row = append(row, ' ')
			continue
		}
		row = append(row, c.Runes[0])
	}
	return string(row)
}

// TestRender_ProjectNameAndChevrons asserts that the explorer header shows
// the project (root) name on row 1 and that an expanded directory renders
// with a '▾' while a collapsed sibling renders with a '▸'.
func TestRender_ProjectNameAndChevrons(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// alpha will appear collapsed (default), Beta the same. Force alpha
	// expanded so we can see both chevrons in one render.
	alpha := findChild(tr.Root, "alpha")
	tr.Toggle(alpha) // expand alpha

	cells, w := renderAndCollect(t, tr, 40, 20)

	// Row 1 should contain the project (root) folder name.
	rootName := filepath.Base(root)
	if got := rowText(cells, w, 1); !containsRune(got, rootName) {
		t.Fatalf("row 1 missing project name %q: got %q", rootName, got)
	}

	// Find the row containing alpha; verify '▾' present.
	if !findRowWithBoth(cells, w, 20, "alpha", '▾') {
		t.Fatal("expected an expanded-row showing alpha with '▾'")
	}
	// Beta is collapsed — verify '▸' present.
	if !findRowWithBoth(cells, w, 20, "Beta", '▸') {
		t.Fatal("expected a collapsed-row showing Beta with '▸'")
	}
}

// TestRender_ActiveFolderIsBold sets ActiveFolder to alpha's path and
// checks that alpha's row carries the AttrBold style — the visual cue
// the user uses to confirm where "New file" will land.
func TestRender_ActiveFolderIsBold(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alpha := findChild(tr.Root, "alpha")
	tr.ActiveFolder = alpha.Path

	cells, w := renderAndCollect(t, tr, 40, 20)

	// Find any cell on the alpha row; assert the foreground style has Bold.
	rowY := -1
	for y := 2; y < 20; y++ {
		if containsRune(rowText(cells, w, y), "alpha") {
			rowY = y
			break
		}
	}
	if rowY < 0 {
		t.Fatal("could not find alpha row in render output")
	}
	// Scan the row for any cell with AttrBold set.
	bold := false
	for x := 0; x < w; x++ {
		_, _, attr := cells[rowY*w+x].Style.Decompose()
		if attr&tcell.AttrBold != 0 {
			bold = true
			break
		}
	}
	if !bold {
		t.Fatal("expected alpha row to be rendered bold (active folder)")
	}
}

// TestRender_TinyHeightDoesNotPanic guards against an off-by-one when the
// caller hands Render a height smaller than the 2-row header — listH goes
// to zero and we shouldn't blow up dividing or indexing.
func TestRender_TinyHeightDoesNotPanic(t *testing.T) {
	root := mkTree(t)
	tr, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(20, 1)
	tr.Render(scr, theme.Default(), 0, 0, 20, 1) // listH would be -1 -> clamped to 0
	// no panic = pass; also visible must be empty.
	if len(tr.visible) != 0 {
		t.Fatalf("expected empty visible slice, got len=%d", len(tr.visible))
	}
}

// containsRune is a tiny "string contains substring" wrapper that keeps
// the imports of this test file lean.
func containsRune(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// findRowWithBoth scans the simulation buffer for any row that contains
// both the given name substring and the given chevron rune — used to
// assert "Beta is shown collapsed" / "alpha is shown expanded".
func findRowWithBoth(cells []tcell.SimCell, w, h int, name string, chev rune) bool {
	for y := 0; y < h; y++ {
		text := rowText(cells, w, y)
		if !containsRune(text, name) {
			continue
		}
		for _, r := range text {
			if r == chev {
				return true
			}
		}
	}
	return false
}
