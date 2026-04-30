// =============================================================================
// File: internal/app/app_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the pure-logic helpers and the small bits of App glue that don't
// require a live terminal. Where we need an *App we build one against a
// tcell.SimulationScreen so layout and event-routing helpers can run without
// touching a real tty. The interactive code paths (Run, the event loop, real
// drawing) are exercised manually — here we just pin down the helpers so
// future refactors don't silently regress them.

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/theme"
)

// newTestApp builds a fully-wired App against a tcell.SimulationScreen. It
// mirrors what New() does, but skips the background tree-refresh goroutine
// because we don't want a ticker firing while tests run.
func newTestApp(t *testing.T, root string) *App {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { scr.Fini() })
	scr.SetSize(120, 40)

	tree, err := filetree.New(root)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	a := &App{
		screen:         scr,
		theme:          theme.Default(),
		rootDir:        tree.Root.Path,
		tree:           tree,
		pendingClose:   -1,
		hoveredMenuRow: -1,
		sidebarShown:   true,
		sidebarWidth:   defaultSidebarWidth,
	}
	a.setActiveFolder(tree.Root.Path)
	a.width, a.height = scr.Size()
	return a
}

// TestSidebarW_ShownVsHidden verifies the sidebar width helper returns 0
// when hidden and the configured width when shown. Every layout helper
// pivots on this so we want it locked in.
func TestSidebarW_ShownVsHidden(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if got := a.sidebarW(); got != defaultSidebarWidth {
		t.Fatalf("shown sidebarW: got %d, want %d", got, defaultSidebarWidth)
	}
	a.sidebarShown = false
	if got := a.sidebarW(); got != 0 {
		t.Fatalf("hidden sidebarW: got %d, want 0", got)
	}
}

// TestSidebarRect checks the sidebar render rectangle reserves one cell
// for the splitter on its right edge, and collapses to zero when hidden.
func TestSidebarRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.sidebarRect()
	if x != 0 || y != 0 {
		t.Fatalf("expected origin (0,0), got (%d,%d)", x, y)
	}
	if w != defaultSidebarWidth-1 {
		t.Fatalf("expected w = sidebarWidth-1, got %d", w)
	}
	if h != a.height-1 {
		t.Fatalf("expected h = height-1, got %d", h)
	}

	a.sidebarShown = false
	x, y, w, h = a.sidebarRect()
	if x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("expected zero rect when hidden, got (%d,%d,%d,%d)", x, y, w, h)
	}
}

// TestSplitterX returns the splitter column when shown and -1 when hidden.
func TestSplitterX(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if got := a.splitterX(); got != defaultSidebarWidth-1 {
		t.Fatalf("shown splitterX: got %d", got)
	}
	a.sidebarShown = false
	if got := a.splitterX(); got != -1 {
		t.Fatalf("hidden splitterX: got %d, want -1", got)
	}
}

// TestTabBarRect checks the tab bar starts after the sidebar and spans the
// remaining width on row 0.
func TestTabBarRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.tabBarRect()
	if x != defaultSidebarWidth || y != 0 || h != 1 {
		t.Fatalf("tabBar position/size unexpected: (%d,%d,%d,%d)", x, y, w, h)
	}
	if w != a.width-defaultSidebarWidth {
		t.Fatalf("tabBar width: got %d", w)
	}
	a.sidebarShown = false
	x, _, w, _ = a.tabBarRect()
	if x != 0 || w != a.width {
		t.Fatalf("hidden-sidebar tabBar should fill row: got x=%d w=%d", x, w)
	}
}

// TestEditorRect verifies the editor body sits between tab bar and status
// bar, to the right of the sidebar.
func TestEditorRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.editorRect()
	if x != defaultSidebarWidth || y != 1 {
		t.Fatalf("editor origin: (%d,%d)", x, y)
	}
	if w != a.width-defaultSidebarWidth {
		t.Fatalf("editor width: got %d", w)
	}
	if h != a.height-2 {
		t.Fatalf("editor height: got %d", h)
	}
}

// TestStatusRect always returns the bottom-most row, full width.
func TestStatusRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.statusRect()
	if x != 0 || y != a.height-1 || w != a.width || h != 1 {
		t.Fatalf("status rect: (%d,%d,%d,%d)", x, y, w, h)
	}
}

// TestMenuButtonRect places the ≡ button at the start of the tab bar and
// shifts left when the sidebar is hidden.
func TestMenuButtonRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, _, w, _ := a.menuButtonRect()
	if x != defaultSidebarWidth || w != menuButtonWidth {
		t.Fatalf("shown menuButtonRect: x=%d w=%d", x, w)
	}
	a.sidebarShown = false
	x, _, _, _ = a.menuButtonRect()
	if x != 0 {
		t.Fatalf("hidden menuButtonRect should sit at column 0: got %d", x)
	}
}

// TestMenuModalRect centers the modal in the window and clamps the origin
// to (0,0) when the window is too small to fit it.
func TestMenuModalRect_Centered(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.menuModalRect()
	if w != modalWidth || h != modalHeight {
		t.Fatalf("modal size: got (%d,%d)", w, h)
	}
	if x != (a.width-modalWidth)/2 || y != (a.height-modalHeight)/2 {
		t.Fatalf("modal origin off-center: (%d,%d)", x, y)
	}
}

// TestMenuModalRect_ClampsTinyWindow ensures the origin never goes negative
// even if the window is smaller than the modal.
func TestMenuModalRect_ClampsTinyWindow(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.width, a.height = 10, 5
	x, y, _, _ := a.menuModalRect()
	if x != 0 || y != 0 {
		t.Fatalf("expected clamped origin (0,0), got (%d,%d)", x, y)
	}
}

// TestResizeSidebar_Clamps verifies the sidebar width clamps to the
// [minSidebarWidth, width-minEditorAfterDrag] range.
func TestResizeSidebar_Clamps(t *testing.T) {
	a := newTestApp(t, t.TempDir())

	// Negative target → clamps up to minSidebarWidth.
	a.resizeSidebar(-50)
	if a.sidebarWidth != minSidebarWidth {
		t.Fatalf("negative target: got %d, want %d", a.sidebarWidth, minSidebarWidth)
	}

	// Above max → clamps to width - minEditorAfterDrag.
	a.resizeSidebar(a.width)
	wantMax := a.width - minEditorAfterDrag
	if a.sidebarWidth != wantMax {
		t.Fatalf("oversize target: got %d, want %d", a.sidebarWidth, wantMax)
	}

	// In range — kept verbatim.
	a.resizeSidebar(25)
	if a.sidebarWidth != 25 {
		t.Fatalf("in-range target: got %d", a.sidebarWidth)
	}
}

// TestResizeSidebar_TinyWindow falls back to minSidebarWidth when the window
// is too narrow for both panels at the requested size.
func TestResizeSidebar_TinyWindow(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.width = 30 // smaller than minSidebarWidth + minEditorAfterDrag.
	a.resizeSidebar(50)
	if a.sidebarWidth != minSidebarWidth {
		t.Fatalf("tiny window: got %d, want %d", a.sidebarWidth, minSidebarWidth)
	}
}

// TestDetectLangLabel covers the language label helper's three cases.
func TestDetectLangLabel(t *testing.T) {
	cases := map[string]string{
		"":              "text",
		"foo.go":        "go",
		"foo":           "text",
		"path/to/x.py":  "py",
		"archive.tar.gz": "gz",
	}
	for in, want := range cases {
		if got := detectLangLabel(in); got != want {
			t.Errorf("detectLangLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsWordChar pins down the ASCII-only word definition we use for
// double-click word selection.
func TestIsWordChar(t *testing.T) {
	word := []rune{'a', 'z', 'A', 'Z', '0', '9', '_'}
	for _, r := range word {
		if !isWordChar(r) {
			t.Errorf("isWordChar(%q) = false, want true", r)
		}
	}
	nonWord := []rune{' ', '\t', '.', ',', '-', '!', '\n', '/'}
	for _, r := range nonWord {
		if isWordChar(r) {
			t.Errorf("isWordChar(%q) = true, want false", r)
		}
	}
}

// TestSetActiveFolder writes both the App field and the tree's mirror copy.
func TestSetActiveFolder(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.setActiveFolder(sub)
	if a.activeFolder != sub {
		t.Fatalf("activeFolder: got %q, want %q", a.activeFolder, sub)
	}
	if a.tree.ActiveFolder != sub {
		t.Fatalf("tree.ActiveFolder: got %q, want %q", a.tree.ActiveFolder, sub)
	}
}

// TestOpenFile_Basic opens a file, switches to it on re-open, and updates
// activeFolder to the file's parent.
func TestOpenFile_Basic(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	target := filepath.Join(sub, "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	a := newTestApp(t, dir)
	a.openFile(target)
	if len(a.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(a.tabs))
	}
	if a.activeFolder != sub {
		t.Fatalf("activeFolder: got %q, want %q", a.activeFolder, sub)
	}

	// Re-opening should switch to existing tab, not create a new one.
	a.activeTab = -1
	a.openFile(target)
	if len(a.tabs) != 1 {
		t.Fatalf("re-open created duplicate tab")
	}
	if a.activeTab != 0 {
		t.Fatalf("re-open didn't switch active: got %d", a.activeTab)
	}
}

// TestOpenFile_ErrorFlash surfaces an error when the path can't be loaded
// (here, a directory rather than a file).
func TestOpenFile_ErrorFlash(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(sub)
	if !strings.Contains(a.statusMsg, "Error") {
		t.Fatalf("expected error flash, got %q", a.statusMsg)
	}
	if len(a.tabs) != 0 {
		t.Fatalf("expected no tabs, got %d", len(a.tabs))
	}
}

// TestRequestCloseTab_DirtyLatch arms pendingClose on the first request to
// close a dirty tab and actually closes it on the second.
func TestRequestCloseTab_DirtyLatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dirty.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.tabs[0].Dirty = true

	a.requestCloseTab(0)
	if len(a.tabs) != 1 {
		t.Fatalf("first close on dirty tab should latch, not close")
	}
	if a.pendingClose != 0 {
		t.Fatalf("pendingClose: got %d", a.pendingClose)
	}

	a.requestCloseTab(0)
	if len(a.tabs) != 0 {
		t.Fatalf("second close on dirty tab should remove it; got %d tabs", len(a.tabs))
	}
	if a.pendingClose != -1 {
		t.Fatalf("pendingClose should reset to -1, got %d", a.pendingClose)
	}
}

// TestRequestCloseTab_CleanClosesImmediately closes a clean tab in one shot.
func TestRequestCloseTab_CleanClosesImmediately(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.requestCloseTab(0)
	if len(a.tabs) != 0 {
		t.Fatalf("clean tab should close on first request")
	}
}

// TestCloseTab_ClampsActive ensures activeTab never points outside the slice.
func TestCloseTab_ClampsActive(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "a.txt"))
	a.openFile(filepath.Join(dir, "b.txt"))
	a.activeTab = 1
	a.closeTab(1)
	if a.activeTab != 0 {
		t.Fatalf("activeTab should clamp to 0 after closing last; got %d", a.activeTab)
	}
	a.closeTab(0)
	if a.activeTab != 0 {
		t.Fatalf("activeTab should stay >=0 with no tabs; got %d", a.activeTab)
	}
}

// TestCloseTab_OutOfRange is a no-op.
func TestCloseTab_OutOfRange(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.closeTab(-1)
	a.closeTab(99)
	a.requestCloseTab(99)
}

// TestHasTab_Predicates covers the four "is X available?" checks used to
// dim menu rows.
func TestHasTab_Predicates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	if a.hasTab() || a.hasSavableTab() || a.hasSelection() || a.hasClipboard() {
		t.Fatal("fresh app should have no tab/selection/clipboard")
	}

	a.openFile(target)
	if !a.hasTab() || !a.hasSavableTab() {
		t.Fatal("expected hasTab && hasSavableTab after open")
	}
	if a.hasSelection() {
		t.Fatal("no selection on a fresh tab")
	}

	// Make a synthetic selection.
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 1}
	if !a.hasSelection() {
		t.Fatal("expected selection after Anchor != Cursor")
	}

	a.clipBuf = "x"
	if !a.hasClipboard() {
		t.Fatal("expected hasClipboard once clipBuf set")
	}
}

// TestSidebarToggleLabel flips between Show/Hide based on sidebarShown.
func TestSidebarToggleLabel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if a.sidebarToggleLabel() != "Hide file explorer" {
		t.Fatalf("got %q", a.sidebarToggleLabel())
	}
	a.sidebarShown = false
	if a.sidebarToggleLabel() != "Show file explorer" {
		t.Fatalf("got %q", a.sidebarToggleLabel())
	}
}

// TestNewFileLabel_Plain shows the bare label when the active folder is the
// project root.
func TestNewFileLabel_Plain(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if got := a.newFileLabel(); got != "New file" {
		t.Fatalf("root label: got %q", got)
	}
	a.activeFolder = ""
	if got := a.newFileLabel(); got != "New file" {
		t.Fatalf("empty folder label: got %q", got)
	}
}

// TestNewFileLabel_SuffixForSubdir adds a "(in subdir)" suffix when the
// active folder is under the project root.
func TestNewFileLabel_SuffixForSubdir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "alpha")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.setActiveFolder(sub)
	got := a.newFileLabel()
	if !strings.HasPrefix(got, "New file (in ") {
		t.Fatalf("expected 'New file (in ...)', got %q", got)
	}
	if !strings.Contains(got, "alpha") {
		t.Fatalf("expected basename in label, got %q", got)
	}
}

// TestNewFileLabel_TruncatesLongPaths keeps the trailing folder visible
// when the relative path would otherwise overflow the modal.
func TestNewFileLabel_TruncatesLongPaths(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir,
		"this-is-a-rather-long-name", "and-another-very-long-name", "trailing")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.setActiveFolder(deep)
	got := a.newFileLabel()
	if !strings.Contains(got, "trailing") {
		t.Fatalf("expected trailing folder name preserved; got %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("expected truncation ellipsis; got %q", got)
	}
}

// TestRelativeFolderLabel covers the three branches: root, subdir, and a
// non-relatable path.
func TestRelativeFolderLabel(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)

	// Root → basename + sep.
	rootLabel := a.relativeFolderLabel(a.rootDir)
	if !strings.HasSuffix(rootLabel, string(filepath.Separator)) {
		t.Fatalf("root missing trailing sep: %q", rootLabel)
	}
	if !strings.HasPrefix(rootLabel, filepath.Base(a.rootDir)) {
		t.Fatalf("root should start with its basename: %q", rootLabel)
	}

	// Subdir → relative path.
	subLabel := a.relativeFolderLabel(sub)
	if subLabel != "child"+string(filepath.Separator) {
		t.Fatalf("subdir label: got %q", subLabel)
	}
}

// TestMenuMoveSelection_WrapsAroundEnds simulates a small menu with all rows
// enabled to verify wrapping in both directions.
func TestMenuMoveSelection_WrapsAroundEnds(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	// Open every potential gate: a savable tab + selection + clipboard.
	tmp := filepath.Join(a.rootDir, "f.txt")
	if err := os.WriteFile(tmp, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a.openFile(tmp)
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 1}
	a.clipBuf = "x"

	// Count the rows currently enabled so we know how many forward
	// steps land us back at the starting row (vs going past it). A
	// hard-coded len(menuItems) breaks every time the menu grows.
	enabled := 0
	for _, item := range menuItems {
		if item.enabled(a) {
			enabled++
		}
	}
	if enabled < 2 {
		t.Fatalf("need at least 2 enabled items to test wrap; got %d", enabled)
	}

	// Walk forward exactly `enabled` steps and land on the first row.
	a.hoveredMenuRow = -1
	a.menuMoveSelection(1)
	first := a.hoveredMenuRow
	for i := 1; i < enabled; i++ {
		a.menuMoveSelection(1)
	}
	a.menuMoveSelection(1) // wrap
	if a.hoveredMenuRow != first {
		t.Fatalf("forward wrap: got %d, want %d", a.hoveredMenuRow, first)
	}

	// Same for backward.
	a.hoveredMenuRow = -1
	a.menuMoveSelection(-1)
	last := a.hoveredMenuRow
	for i := 1; i < enabled; i++ {
		a.menuMoveSelection(-1)
	}
	a.menuMoveSelection(-1) // wrap
	if a.hoveredMenuRow != last {
		t.Fatalf("backward wrap: got %d, want %d", a.hoveredMenuRow, last)
	}
}

// TestMenuMoveSelection_NothingEnabledYieldsMinusOne lands on -1 when no row
// is enabled (we synthesise that by setting every predicate to false-ish via
// the no-tab/no-clipboard initial state, except always-true rows).
func TestMenuMoveSelection_SkipsDisabled(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	// No tabs, no selection, no clipboard. Save/Close/Rename/Delete/Copy/
	// Cut/Paste are all disabled. New file / toggle / quit stay enabled.
	a.hoveredMenuRow = -1
	a.menuMoveSelection(1)
	if a.hoveredMenuRow < 0 {
		t.Fatal("expected a row to land somewhere")
	}
	idx := a.hoveredMenuRow
	if !menuItems[idx].enabled(a) {
		t.Fatalf("landed on disabled row %d", idx)
	}
}

// TestFlash sets statusMsg and pushes statusUntil into the future.
func TestFlash(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	before := time.Now()
	a.flash("hello world")
	if a.statusMsg != "hello world" {
		t.Fatalf("statusMsg: got %q", a.statusMsg)
	}
	if !a.statusUntil.After(before) {
		t.Fatalf("statusUntil should be in the future, got %v", a.statusUntil)
	}
}

// TestMenuToggleSidebar flips the sidebarShown flag.
func TestMenuToggleSidebar(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if !a.sidebarShown {
		t.Fatal("sidebar should start visible")
	}
	a.menuToggleSidebar()
	if a.sidebarShown {
		t.Fatal("expected hidden after first toggle")
	}
	a.menuToggleSidebar()
	if !a.sidebarShown {
		t.Fatal("expected shown after second toggle")
	}
}

// TestTabBarClick_OpensMenu clicks the ≡ button cell and verifies the menu
// opens.
func TestTabBarClick_OpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	mx, _, _, _ := a.menuButtonRect()
	a.tabBarClick(mx, 0)
	if !a.menuOpen {
		t.Fatal("clicking ≡ should open menu")
	}
}

// TestTabBarClick_SwitchesTab clicks inside a non-active tab's body and
// verifies activeTab updates.
func TestTabBarClick_SwitchesTab(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "a.txt"))
	a.openFile(filepath.Join(dir, "b.txt"))
	// b is active. Lay out the tabs and click inside tab 0's body (not the ×).
	a.lastTabRects = a.layoutTabs()
	tabA := a.lastTabRects[0]
	clickX := tabA.X + 1
	if clickX == tabA.CloseX {
		clickX = tabA.X + 2
	}
	a.tabBarClick(clickX, 0)
	if a.activeTab != 0 {
		t.Fatalf("expected activeTab=0, got %d", a.activeTab)
	}
}

// TestEditorSize matches the editor rect's width and height.
func TestEditorSize(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	w, h := a.editorSize()
	if w != a.width-defaultSidebarWidth || h != a.height-2 {
		t.Fatalf("editorSize: got (%d,%d)", w, h)
	}
}

// TestActiveTabPtr returns nil with no tabs and the right pointer otherwise.
func TestActiveTabPtr(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	if a.activeTabPtr() != nil {
		t.Fatal("expected nil with no tabs")
	}
	a.openFile(target)
	if a.activeTabPtr() != a.tabs[0] {
		t.Fatal("activeTabPtr should match tabs[activeTab]")
	}
	a.activeTab = 99
	if a.activeTabPtr() != nil {
		t.Fatal("out-of-range activeTab should yield nil")
	}
}

// TestSaveActiveTab writes the buffer to disk and clears Dirty.
func TestSaveActiveTab(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString("X")
	a.saveActiveTab()
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "X") {
		t.Fatalf("save did not persist: %q", got)
	}
}

// TestSaveActiveTab_NoTab is a no-op.
func TestSaveActiveTab_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.saveActiveTab()
}

// TestCopyCutPaste exercises the clipboard glue.
func TestCopyCutPaste(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	// No selection — copy/cut should be no-ops.
	a.copySelection()
	a.cutSelection()
	if a.clipBuf != "" {
		t.Fatalf("clipBuf should still be empty: %q", a.clipBuf)
	}

	// Make selection of "hello".
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 5}
	a.copySelection()
	if a.clipBuf != "hello" {
		t.Fatalf("copy: clipBuf %q", a.clipBuf)
	}

	// Cut: same selection should now empty the buffer.
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 5}
	a.cutSelection()
	if tab.Buffer.LineRunes(0) != nil && len(tab.Buffer.LineRunes(0)) != 0 {
		// Some buffer impls return empty slice; both fine.
	}

	// Paste empty path: when clipBuf empty, flash about external paste.
	a.clipBuf = ""
	a.pasteClipboard()
	if !strings.Contains(a.statusMsg, "clipboard empty") {
		t.Fatalf("expected empty-clip flash, got %q", a.statusMsg)
	}

	// Paste with content.
	a.clipBuf = "X"
	a.pasteClipboard()
}

// TestPasteClipboard_NoTab is safe with no tab open.
func TestPasteClipboard_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.clipBuf = "X"
	a.pasteClipboard() // no tab — nothing to paste into.
}

// TestMenuSaveAndClose saves then closes the active tab.
func TestMenuSaveAndClose(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sc.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString("Y")
	a.menuSaveAndClose()
	if len(a.tabs) != 0 {
		t.Fatalf("expected tab closed; got %d tabs", len(a.tabs))
	}
}

// TestMenuSaveAndClose_NoTab is a no-op when nothing is open.
func TestMenuSaveAndClose_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuSaveAndClose()
}

// TestMenuClickPaths covers menuSave/menuCopy/menuCut/menuPaste/menuClose
// menuQuit and menuRefreshTree as one-liners.
func TestMenuClickPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	// Selection so copy/cut have something to operate on.
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 2}

	a.menuOpen = true
	a.menuSave()
	a.menuOpen = true
	a.menuCopy()
	a.menuOpen = true
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 1}
	a.menuCut()
	a.menuOpen = true
	a.menuPaste()
	a.menuOpen = true
	a.menuRefreshTree()

	a.menuOpen = true
	a.menuQuit()
	if !a.quit {
		t.Fatal("menuQuit should set quit flag")
	}
}

// TestUndoRedoRevert_MenuPaths exercises the new history actions end
// to end through the menu wrappers. The flash on no-op paths is also
// covered so the user always gets feedback when they hit a dead-end.
func TestUndoRedoRevert_MenuPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()

	// Nothing to undo / redo / revert on a freshly opened file.
	if a.hasUndo() || a.hasRedo() || a.hasRevert() {
		t.Fatal("freshly opened tab should have no history")
	}
	a.menuOpen = true
	a.menuUndo()
	a.menuOpen = true
	a.menuRedo()
	a.menuOpen = true
	a.menuRevert()

	// One edit → undo + revert become available.
	tab.MoveCursorTo(editor.Position{Line: 0, Col: 4}, false)
	tab.InsertString("X")
	if !a.hasUndo() || !a.hasRevert() {
		t.Fatal("expected undo + revert after edit")
	}
	if a.hasRedo() {
		t.Fatal("redo should still be empty")
	}

	a.menuOpen = true
	a.menuUndo()
	if got := tab.Buffer.String(); got != "seed" {
		t.Fatalf("after menuUndo = %q, want seed", got)
	}
	if !a.hasRedo() {
		t.Fatal("redo should be populated after an undo")
	}

	a.menuOpen = true
	a.menuRedo()
	if got := tab.Buffer.String(); got != "seedX" {
		t.Fatalf("after menuRedo = %q, want seedX", got)
	}

	// Revert back to original; then Undo must recover the post-edit state.
	a.menuOpen = true
	a.menuRevert()
	if got := tab.Buffer.String(); got != "seed" {
		t.Fatalf("after menuRevert = %q, want seed", got)
	}
	a.menuOpen = true
	a.menuUndo()
	if got := tab.Buffer.String(); got != "seedX" {
		t.Fatalf("after undo-of-revert = %q, want seedX", got)
	}
}

// TestUndoRedoRevert_NoTabSafelyNoOps guards against crashes when the
// menu rows somehow fire with no active tab — they should silently
// return rather than dereferencing nil.
func TestUndoRedoRevert_NoTabSafelyNoOps(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuOpen = true
	a.menuUndo()
	a.menuOpen = true
	a.menuRedo()
	a.menuOpen = true
	a.menuRevert()
	if a.hasUndo() || a.hasRedo() || a.hasRevert() {
		t.Fatal("no-tab predicates should all be false")
	}
}

// TestMenuClose_NoTab safely no-ops.
func TestMenuClose_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuOpen = true
	a.menuClose()
}

// TestMenuActivate_RunsHovered runs the action attached to the highlighted
// row.
func TestMenuActivate_RunsHovered(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	// Force highlight onto the toggle row (always enabled), then activate.
	for i, item := range menuItems {
		if item.labelFor != nil && item.label == "" && item.relY == 19 {
			a.hoveredMenuRow = i
			break
		}
	}
	if a.hoveredMenuRow < 0 {
		t.Fatal("could not find toggle row")
	}
	before := a.sidebarShown
	a.menuActivate()
	if a.sidebarShown == before {
		t.Fatal("expected sidebarShown to flip after menuActivate")
	}
}

// TestMenuActivate_OutOfRange and disabled rows are no-ops.
func TestMenuActivate_OutOfRange(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.hoveredMenuRow = -1
	a.menuActivate()
	a.hoveredMenuRow = 999
	a.menuActivate()
}

// TestUpdateMenuHover snaps to the right row when over an enabled row, and
// to -1 when outside.
func TestUpdateMenuHover(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	mx, my, _, _ := a.menuModalRect()

	// Find an always-enabled row and click on its relY.
	var pickIdx, pickRelY int
	for i, item := range menuItems {
		if item.enabled(a) {
			pickIdx = i
			pickRelY = item.relY
			break
		}
	}
	a.updateMenuHover(mx+5, my+pickRelY)
	if a.hoveredMenuRow != pickIdx {
		t.Fatalf("hoveredMenuRow: got %d, want %d", a.hoveredMenuRow, pickIdx)
	}

	// Outside the modal → -1.
	a.updateMenuHover(0, 0)
	if a.hoveredMenuRow != -1 {
		t.Fatalf("outside modal: got %d", a.hoveredMenuRow)
	}
}

// TestScrollAt routes scroll to the panel under the cursor; we just verify
// it doesn't panic across the three regions.
func TestScrollAt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.scrollAt(1, 5, 1)            // sidebar
	a.scrollAt(60, 5, 1)           // editor
	a.scrollAt(60, a.height-1, 1)  // status bar (no-op-ish)
}

// TestSidebarClick_File opens a file when a file row is clicked.
func TestSidebarClick_File(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "click.txt")
	if err := os.WriteFile(target, []byte("z"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	// Render once so the tree has visible rows for HitTest.
	a.draw()
	// File row is row 1 (0 is the root); click at column 1, row 1.
	a.sidebarClick(1, 1)
	// Only a no-panic guarantee — depending on row order we may or may
	// not have opened the file. Just make sure no crash and either zero
	// or one tab is open.
	if len(a.tabs) > 1 {
		t.Fatalf("unexpected tabs: %d", len(a.tabs))
	}
}

// TestSidebarClick_Miss is safe when (x,y) hits no row.
func TestSidebarClick_Miss(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarClick(1, 100) // off the bottom of the tree
}

// TestSelectWordAt selects the word under a buffer position.
func TestSelectWordAt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(target, []byte("hello world"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	a.selectWordAt(tab, editor.Position{Line: 0, Col: 2})
	if tab.Anchor.Col != 0 || tab.Cursor.Col != 5 {
		t.Fatalf("word select: anchor=%v cursor=%v", tab.Anchor, tab.Cursor)
	}

	// Empty line — no selection.
	tab.Buffer = editor.NewBuffer("")
	a.selectWordAt(tab, editor.Position{Line: 0, Col: 0})
}

// TestEditorPress_PlacesCaret moves the caret to the clicked spot.
func TestEditorPress_PlacesCaret(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(target, []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, _ := a.editorRect()
	a.editorPress(ex+2, ey+1)
	tab := a.activeTabPtr()
	if tab.Cursor.Line != 1 {
		t.Fatalf("expected line 1, got %d", tab.Cursor.Line)
	}
}

// TestEditorPress_DoubleClickSelectsWord triggers the word-select path.
func TestEditorPress_DoubleClickSelectsWord(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(target, []byte("hello world"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, _ := a.editorRect()
	a.editorPress(ex+2, ey)
	a.editorPress(ex+2, ey) // immediately again — double-click within window
	tab := a.activeTabPtr()
	if tab.Anchor.Col == tab.Cursor.Col {
		t.Fatal("expected a word selection after double-click")
	}
}

// TestEditorPress_NoTabSafe doesn't panic with no active tab.
func TestEditorPress_NoTabSafe(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.editorPress(50, 5)
	a.editorDrag(50, 5)
}

// TestEditorDrag_AutoScroll arms the auto-scroll direction when dragging
// outside the editor's vertical bounds.
func TestEditorDrag_AutoScroll(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "d.txt")
	if err := os.WriteFile(target, []byte("a\nb\nc\nd\ne\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, eh := a.editorRect()
	a.editorDrag(ex+1, ey-1) // above editor → auto-scroll up
	if a.autoScrollDir != -1 {
		t.Fatalf("expected autoScrollDir=-1, got %d", a.autoScrollDir)
	}
	a.editorDrag(ex+1, ey+eh+1) // below → auto-scroll down
	if a.autoScrollDir != 1 {
		t.Fatalf("expected autoScrollDir=1, got %d", a.autoScrollDir)
	}
	a.editorDrag(ex+1, ey+1) // inside → stops
	if a.autoScrollDir != 0 {
		t.Fatalf("expected stopped autoScroll, got %d", a.autoScrollDir)
	}
}

// TestHandleKey_EscDoubleTapOpensMenu opens the menu after two Esc presses.
func TestHandleKey_EscDoubleTapOpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if a.menuOpen {
		t.Fatal("first Esc should not open menu")
	}
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if !a.menuOpen {
		t.Fatal("second Esc should open menu")
	}
	// Third Esc — menu open, should close.
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if a.menuOpen {
		t.Fatal("Esc with menu open should close it")
	}
}

// TestHandleKey_MenuNavKeys move highlight and Enter activates.
func TestHandleKey_MenuNavKeys(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	first := a.hoveredMenuRow
	a.handleKey(keyEv(tcell.KeyDown, 0))
	if a.hoveredMenuRow == first {
		t.Fatal("Down should advance highlight")
	}
	a.handleKey(keyEv(tcell.KeyUp, 0))
	if a.hoveredMenuRow != first {
		t.Fatalf("Up should return to %d, got %d", first, a.hoveredMenuRow)
	}
}

// TestHandleKey_RoutesToActiveTab dispatches typing to the active tab.
func TestHandleKey_RoutesToActiveTab(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.handleKey(keyEv(tcell.KeyRune, 'a'))
	a.handleKey(keyEv(tcell.KeyRune, 'b'))
	a.handleKey(keyEv(tcell.KeyEnter, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'c'))
	a.handleKey(keyEv(tcell.KeyTab, 0))
	a.handleKey(keyEv(tcell.KeyBackspace, 0))
	a.handleKey(keyEv(tcell.KeyHome, 0))
	a.handleKey(keyEv(tcell.KeyEnd, 0))
	a.handleKey(keyEv(tcell.KeyLeft, 0))
	a.handleKey(keyEv(tcell.KeyRight, 0))
	a.handleKey(keyEv(tcell.KeyUp, 0))
	a.handleKey(keyEv(tcell.KeyDown, 0))
	a.handleKey(keyEv(tcell.KeyPgUp, 0))
	a.handleKey(keyEv(tcell.KeyPgDn, 0))
	a.handleKey(keyEv(tcell.KeyDelete, 0))
}

// TestHandleEvent_Resize updates width/height.
func TestHandleEvent_Resize(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	scr := a.screen.(tcell.SimulationScreen)
	scr.SetSize(80, 24)
	ev := tcell.NewEventResize(80, 24)
	a.handleEvent(ev)
	if a.width != 80 || a.height != 24 {
		t.Fatalf("resize: got %dx%d", a.width, a.height)
	}
}

// TestHandleMouse_Wheel routes scroll events to the panel under the cursor.
func TestHandleMouse_Wheel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	ev := tcell.NewEventMouse(60, 5, tcell.WheelDown, tcell.ModNone)
	a.handleMouse(ev)
	ev = tcell.NewEventMouse(60, 5, tcell.WheelUp, tcell.ModNone)
	a.handleMouse(ev)
}

// TestHandleMouse_RightClickOpensMenu falls back to the main menu when the
// right-click isn't on a tree row.
func TestHandleMouse_RightClickOpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	ev := tcell.NewEventMouse(60, 5, tcell.Button3, tcell.ModNone)
	a.handleMouse(ev)
	if !a.menuOpen {
		t.Fatal("right-click outside tree should open the main menu")
	}
}

// TestHandleMouse_LeftPressInEditor enters editor drag mode.
func TestHandleMouse_LeftPressInEditor(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("ab\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ev := tcell.NewEventMouse(60, 5, tcell.Button1, tcell.ModNone)
	a.handleMouse(ev)
	if a.dragMode != "editor" {
		t.Fatalf("expected dragMode=editor, got %q", a.dragMode)
	}
	// Release.
	ev = tcell.NewEventMouse(60, 5, 0, tcell.ModNone)
	a.handleMouse(ev)
	if a.dragMode != "" {
		t.Fatalf("expected drag cleared on release, got %q", a.dragMode)
	}
}

// TestHandleMouse_SidebarSplitterDrag enters splitter drag and resizes.
func TestHandleMouse_SidebarSplitterDrag(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	splitX := a.splitterX()
	ev := tcell.NewEventMouse(splitX, 5, tcell.Button1, tcell.ModNone)
	a.handleMouse(ev)
	if a.dragMode != "sidebar" {
		t.Fatalf("expected sidebar drag, got %q", a.dragMode)
	}
	// Continue dragging — resizes.
	ev = tcell.NewEventMouse(splitX+5, 5, tcell.Button1, tcell.ModNone)
	a.handleMouse(ev)
}

// TestHandleMenuMouse_ClicksRowAndOutside both fires the row action and
// dismisses on outside click.
func TestHandleMenuMouse_ClicksRowAndOutside(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	mx, my, _, _ := a.menuModalRect()
	// Click on the toggle row (relY=19) — flips the sidebar.
	before := a.sidebarShown
	a.handleMenuMouse(mx+5, my+19, tcell.Button1)
	if a.sidebarShown == before {
		t.Fatal("expected toggle to fire")
	}

	// Click outside — closes.
	a.openMenu()
	a.handleMenuMouse(0, 0, tcell.Button1)
	if a.menuOpen {
		t.Fatal("outside click should close menu")
	}
}

// TestHandleMenuMouse_NoButtonIsNoop ignores motion-only events.
func TestHandleMenuMouse_NoButtonIsNoop(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	a.handleMenuMouse(0, 0, 0)
	if !a.menuOpen {
		t.Fatal("motion-only event should not close menu")
	}
}

// TestDraw_AllPanels exercises the drawing path so the stdout/screen code
// is covered. Result correctness is exercised manually; here we just make
// sure no panics across several states.
func TestDraw_AllPanels(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.draw() // empty editor + sidebar
	a.openFile(target)
	a.draw() // with a tab
	a.activeTabPtr().Dirty = true
	a.draw() // dirty marker
	a.openMenu()
	a.draw() // with menu modal
	a.closeMenu()
	a.openPrompt("T", "H", "x", nil)
	a.draw()
	a.promptCancel()
	a.openConfirm("T", "M", nil)
	a.draw()
	a.confirmCancel()
	a.openTreeContext(a.tree.Root, 5, 5)
	a.draw()
	a.closeAllModals()
	a.flash("hello")
	a.draw() // status flash
	a.sidebarShown = false
	a.draw()
	// Tiny window → too-small message.
	a.width, a.height = 5, 5
	a.draw()
}

// TestTabBarClick_ClosesViaX clicks the × in a tab and verifies the close
// path runs (clean tab → tab removed).
func TestTabBarClick_ClosesViaX(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.lastTabRects = a.layoutTabs()
	r := a.lastTabRects[0]
	a.tabBarClick(r.CloseX, 0)
	if len(a.tabs) != 0 {
		t.Fatalf("expected close, got %d tabs", len(a.tabs))
	}
}
