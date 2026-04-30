// =============================================================================
// File: internal/app/modals_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the secondary-modal logic in modals.go: prompt, confirm, and
// the right-click context menu. We don't render anything; we drive the
// modals directly and assert on the App fields they touch. Mutual
// exclusivity (closeAllModals) is the linchpin so most tests double-check
// no other modal flag is left on.

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/filetree"
)

// TestCloseAllModals_ClearsEverything proves the helper turns off every
// modal flag and clears the side-state (drag, auto-scroll dir, callbacks).
func TestCloseAllModals_ClearsEverything(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuOpen = true
	a.promptOpen = true
	a.confirmOpen = true
	a.contextOpen = true
	a.hoveredMenuRow = 3
	a.contextNode = a.tree.Root
	a.contextItems = []contextItem{{label: "x"}}
	a.promptCallback = func(*App, string) {}
	a.confirmCallback = func(*App) {}
	a.dragMode = "editor"
	a.autoScrollDir = 1

	a.closeAllModals()

	if a.menuOpen || a.promptOpen || a.confirmOpen || a.contextOpen {
		t.Fatal("expected all modal flags off")
	}
	if a.hoveredMenuRow != -1 {
		t.Fatalf("hoveredMenuRow not cleared: %d", a.hoveredMenuRow)
	}
	if a.contextNode != nil || a.contextItems != nil {
		t.Fatal("context state not cleared")
	}
	if a.promptCallback != nil || a.confirmCallback != nil {
		t.Fatal("callbacks not cleared")
	}
	if a.dragMode != "" {
		t.Fatalf("dragMode not cleared: %q", a.dragMode)
	}
	if a.autoScrollDir != 0 {
		t.Fatalf("autoScrollDir not reset: %d", a.autoScrollDir)
	}
}

// TestAnyModalOpen returns true for any one flag and false for none.
func TestAnyModalOpen(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if a.anyModalOpen() {
		t.Fatal("none open")
	}
	a.menuOpen = true
	if !a.anyModalOpen() {
		t.Fatal("expected true with menu open")
	}
	a.menuOpen = false
	a.promptOpen = true
	if !a.anyModalOpen() {
		t.Fatal("expected true with prompt open")
	}
	a.promptOpen = false
	a.confirmOpen = true
	if !a.anyModalOpen() {
		t.Fatal("expected true with confirm open")
	}
	a.confirmOpen = false
	a.contextOpen = true
	if !a.anyModalOpen() {
		t.Fatal("expected true with context open")
	}
}

// TestOpenPrompt_Submit runs the callback with a trimmed value and closes
// the modal.
func TestOpenPrompt_Submit(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	got := ""
	a.openPrompt("Title", "Hint", "  hello  ", func(_ *App, v string) { got = v })
	if !a.promptOpen {
		t.Fatal("openPrompt should set promptOpen")
	}
	if a.promptCursor != len([]rune("  hello  ")) {
		t.Fatalf("cursor at end of initial: got %d", a.promptCursor)
	}
	a.promptSubmit()
	if got != "hello" {
		t.Fatalf("callback got %q, want trimmed 'hello'", got)
	}
	if a.promptOpen {
		t.Fatal("promptSubmit should close the modal")
	}
}

// TestPromptSubmit_EmptyIsNoop keeps the modal open and skips the callback.
func TestPromptSubmit_EmptyIsNoop(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openPrompt("T", "H", "   ", func(*App, string) { called = true })
	a.promptSubmit()
	if called {
		t.Fatal("empty submit should not run callback")
	}
	if !a.promptOpen {
		t.Fatal("empty submit should keep modal open")
	}
}

// TestPromptCancel skips the callback.
func TestPromptCancel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openPrompt("T", "H", "x", func(*App, string) { called = true })
	a.promptCancel()
	if called {
		t.Fatal("cancel should not run callback")
	}
	if a.promptOpen {
		t.Fatal("cancel should close")
	}
}

// TestPromptSubmit_NoopWhenClosed guards against double-submits triggered
// by stale events.
func TestPromptSubmit_NoopWhenClosed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.promptSubmit() // does nothing
}

// keyEv builds a synthetic tcell key event for tests.
func keyEv(k tcell.Key, r rune) *tcell.EventKey {
	return tcell.NewEventKey(k, r, tcell.ModNone)
}

// TestHandlePromptKey_Editing exercises every editing branch of the prompt
// keyboard handler.
func TestHandlePromptKey_Editing(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openPrompt("T", "H", "", nil)

	// Insert "abc".
	for _, r := range "abc" {
		a.handlePromptKey(keyEv(tcell.KeyRune, r))
	}
	if string(a.promptValue) != "abc" || a.promptCursor != 3 {
		t.Fatalf("after insert: %q cur=%d", string(a.promptValue), a.promptCursor)
	}

	// Control-rune is ignored.
	a.handlePromptKey(keyEv(tcell.KeyRune, 0x01))
	if string(a.promptValue) != "abc" {
		t.Fatalf("control rune should be ignored: %q", string(a.promptValue))
	}

	// Home / End.
	a.handlePromptKey(keyEv(tcell.KeyHome, 0))
	if a.promptCursor != 0 {
		t.Fatalf("Home: got %d", a.promptCursor)
	}
	a.handlePromptKey(keyEv(tcell.KeyEnd, 0))
	if a.promptCursor != 3 {
		t.Fatalf("End: got %d", a.promptCursor)
	}

	// Left / Right.
	a.handlePromptKey(keyEv(tcell.KeyLeft, 0))
	if a.promptCursor != 2 {
		t.Fatalf("Left: got %d", a.promptCursor)
	}
	a.handlePromptKey(keyEv(tcell.KeyRight, 0))
	if a.promptCursor != 3 {
		t.Fatalf("Right: got %d", a.promptCursor)
	}
	// Right past end is clamped.
	a.handlePromptKey(keyEv(tcell.KeyRight, 0))
	if a.promptCursor != 3 {
		t.Fatalf("Right past end: got %d", a.promptCursor)
	}
	// Left past start is clamped.
	a.promptCursor = 0
	a.handlePromptKey(keyEv(tcell.KeyLeft, 0))
	if a.promptCursor != 0 {
		t.Fatalf("Left past start: got %d", a.promptCursor)
	}

	// Backspace: cursor between 'a' and 'b' → removes 'a'.
	a.promptCursor = 1
	a.handlePromptKey(keyEv(tcell.KeyBackspace, 0))
	if string(a.promptValue) != "bc" || a.promptCursor != 0 {
		t.Fatalf("Backspace: %q cur=%d", string(a.promptValue), a.promptCursor)
	}
	// Backspace at column 0 is a no-op.
	a.handlePromptKey(keyEv(tcell.KeyBackspace2, 0))
	if string(a.promptValue) != "bc" {
		t.Fatalf("Backspace at 0 should be no-op: %q", string(a.promptValue))
	}

	// Delete forward: removes 'b' at cursor=0.
	a.handlePromptKey(keyEv(tcell.KeyDelete, 0))
	if string(a.promptValue) != "c" {
		t.Fatalf("Delete: %q", string(a.promptValue))
	}
	// Delete at end is no-op.
	a.promptCursor = 1
	a.handlePromptKey(keyEv(tcell.KeyDelete, 0))
	if string(a.promptValue) != "c" {
		t.Fatalf("Delete at end should be no-op: %q", string(a.promptValue))
	}
}

// TestHandlePromptKey_Esc cancels the modal.
func TestHandlePromptKey_Esc(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openPrompt("T", "H", "x", func(*App, string) { called = true })
	a.handlePromptKey(keyEv(tcell.KeyEsc, 0))
	if a.promptOpen {
		t.Fatal("Esc should close")
	}
	if called {
		t.Fatal("Esc should not run callback")
	}
}

// TestHandlePromptKey_Enter submits.
func TestHandlePromptKey_Enter(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	got := ""
	a.openPrompt("T", "H", "ok", func(_ *App, v string) { got = v })
	a.handlePromptKey(keyEv(tcell.KeyEnter, 0))
	if got != "ok" {
		t.Fatalf("Enter callback: got %q", got)
	}
}

// TestAdjustPromptScroll keeps the cursor inside the visible window.
func TestAdjustPromptScroll(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.promptValue = []rune("abcdefghijklmnop")
	a.promptCursor = len(a.promptValue) // 16
	a.promptScroll = 0
	a.adjustPromptScroll(5)
	if a.promptScroll != a.promptCursor-5+1 {
		t.Fatalf("scroll-to-end: got %d, want %d", a.promptScroll, a.promptCursor-5+1)
	}

	a.promptCursor = 0
	a.adjustPromptScroll(5)
	if a.promptScroll != 0 {
		t.Fatalf("scroll-to-start: got %d", a.promptScroll)
	}

	// Zero-width clamps to 0.
	a.promptScroll = 7
	a.adjustPromptScroll(0)
	if a.promptScroll != 0 {
		t.Fatalf("zero-width: got %d", a.promptScroll)
	}
}

// TestOpenConfirm_DefaultsToNo lands on the safe button so accidental
// Enter never does anything destructive.
func TestOpenConfirm_DefaultsToNo(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openConfirm("Delete", "Sure?", func(*App) {})
	if !a.confirmOpen {
		t.Fatal("confirm should open")
	}
	if a.confirmHover != 0 {
		t.Fatalf("default focus should be No (0); got %d", a.confirmHover)
	}
}

// TestConfirmYes runs the callback.
func TestConfirmYes(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openConfirm("T", "M", func(*App) { called = true })
	a.confirmYes()
	if !called {
		t.Fatal("confirmYes should run callback")
	}
	if a.confirmOpen {
		t.Fatal("confirmYes should close")
	}
}

// TestConfirmYes_NoopWhenClosed protects against stale events.
func TestConfirmYes_NoopWhenClosed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.confirmCallback = func(*App) { called = true }
	a.confirmYes()
	if called {
		t.Fatal("confirmYes should be a no-op when closed")
	}
}

// TestConfirmCancel skips the callback.
func TestConfirmCancel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openConfirm("T", "M", func(*App) { called = true })
	a.confirmCancel()
	if called {
		t.Fatal("cancel should skip callback")
	}
}

// TestHandleConfirmKey_AllBranches walks Tab, Left, Right, Enter, Esc.
func TestHandleConfirmKey_AllBranches(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openConfirm("T", "M", func(*App) { called = true })

	// Tab cycles 0 ↔ 1.
	a.handleConfirmKey(keyEv(tcell.KeyTab, 0))
	if a.confirmHover != 1 {
		t.Fatalf("Tab: got %d, want 1", a.confirmHover)
	}
	a.handleConfirmKey(keyEv(tcell.KeyTab, 0))
	if a.confirmHover != 0 {
		t.Fatalf("Tab back: got %d", a.confirmHover)
	}

	// Left snaps to No (0).
	a.confirmHover = 1
	a.handleConfirmKey(keyEv(tcell.KeyLeft, 0))
	if a.confirmHover != 0 {
		t.Fatalf("Left: got %d", a.confirmHover)
	}

	// Right snaps to Yes (1).
	a.handleConfirmKey(keyEv(tcell.KeyRight, 0))
	if a.confirmHover != 1 {
		t.Fatalf("Right: got %d", a.confirmHover)
	}

	// Enter on Yes runs the callback.
	a.handleConfirmKey(keyEv(tcell.KeyEnter, 0))
	if !called {
		t.Fatal("Enter on Yes should fire callback")
	}

	// Re-open and verify Enter on No cancels.
	called = false
	a.openConfirm("T", "M", func(*App) { called = true })
	a.confirmHover = 0
	a.handleConfirmKey(keyEv(tcell.KeyEnter, 0))
	if called {
		t.Fatal("Enter on No should cancel without firing callback")
	}

	// Esc cancels.
	a.openConfirm("T", "M", func(*App) {})
	a.handleConfirmKey(keyEv(tcell.KeyEsc, 0))
	if a.confirmOpen {
		t.Fatal("Esc should close confirm")
	}
}

// TestOpenTreeContext_Folder offers New File + Rename + Delete plus the
// two clipboard rows.
func TestOpenTreeContext_Folder(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	if err := mkdir(sub); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	// Find the child node in the tree.
	var node *filetree.Node
	for _, c := range a.tree.Root.Children {
		if c.Name == "child" {
			node = c
			break
		}
	}
	if node == nil {
		t.Fatal("child node not in tree")
	}
	a.openTreeContext(node, 5, 5)
	if !a.contextOpen {
		t.Fatal("context should open")
	}
	wantLabels := []string{"New File", "Rename", "Delete", "Copy rel path", "Copy abs path"}
	if len(a.contextItems) != len(wantLabels) {
		t.Fatalf("folder context should have %d items, got %d", len(wantLabels), len(a.contextItems))
	}
	for i, w := range wantLabels {
		if a.contextItems[i].label != w {
			t.Fatalf("item %d label: got %q, want %q", i, a.contextItems[i].label, w)
		}
	}
}

// TestOpenTreeContext_File offers Rename + Delete plus the two clipboard
// rows. New File is folder-only.
func TestOpenTreeContext_File(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := writeFile(target, "x"); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	var node *filetree.Node
	for _, c := range a.tree.Root.Children {
		if c.Name == "f.txt" {
			node = c
			break
		}
	}
	if node == nil {
		t.Fatal("file node not in tree")
	}
	a.openTreeContext(node, 5, 5)
	wantLabels := []string{"Rename", "Delete", "Copy rel path", "Copy abs path"}
	if len(a.contextItems) != len(wantLabels) {
		t.Fatalf("file context should have %d items, got %d", len(wantLabels), len(a.contextItems))
	}
	for i, w := range wantLabels {
		if a.contextItems[i].label != w {
			t.Fatalf("item %d label: got %q, want %q", i, a.contextItems[i].label, w)
		}
	}
}

// TestOpenTreeContext_Root offers New File and the two clipboard rows —
// Rename / Delete on the project root would be a footgun.
func TestOpenTreeContext_Root(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openTreeContext(a.tree.Root, 5, 5)
	wantLabels := []string{"New File", "Copy rel path", "Copy abs path"}
	if len(a.contextItems) != len(wantLabels) {
		t.Fatalf("root context should have %d items, got %d", len(wantLabels), len(a.contextItems))
	}
	for i, w := range wantLabels {
		if a.contextItems[i].label != w {
			t.Fatalf("item %d label: got %q, want %q", i, a.contextItems[i].label, w)
		}
	}
}

// TestPlaceContext_FlipsLeftAndUp tests that the popup flips when it would
// otherwise overflow the window edges, and clamps at 0.
func TestPlaceContext_FlipsLeftAndUp(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.width, a.height = 30, 20

	// Click far to the right → expect cx to flip left so the popup ends
	// near the click x.
	cx, cy := a.placeContext(28, 5, 3)
	if cx >= 28 {
		t.Fatalf("expected flip left when near right edge: cx=%d", cx)
	}
	if cy != 5 {
		t.Fatalf("y unchanged at top half: cy=%d", cy)
	}

	// Click near the bottom → expect cy to flip up.
	cx, cy = a.placeContext(5, 19, 5)
	if cy >= 19 {
		t.Fatalf("expected flip up when near bottom: cy=%d", cy)
	}
	_ = cx

	// Click out at -10,-10 → expect clamp to (0,0).
	cx, cy = a.placeContext(-10, -10, 3)
	if cx != 0 || cy != 0 {
		t.Fatalf("expected clamp (0,0); got (%d,%d)", cx, cy)
	}
}

// TestContextActivate runs the highlighted item's action against the node
// the menu was opened for, and closes all modals first.
func TestContextActivate(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := 0
	var seenNode *filetree.Node
	a.contextOpen = true
	a.contextNode = a.tree.Root
	a.contextItems = []contextItem{
		{label: "x", action: func(_ *App, n *filetree.Node) {
			called++
			seenNode = n
		}},
	}
	a.contextHover = 0
	a.contextActivate()
	if called != 1 {
		t.Fatalf("expected action to fire once, got %d", called)
	}
	if seenNode != a.tree.Root {
		t.Fatal("action did not receive the contextNode")
	}
	if a.contextOpen {
		t.Fatal("contextActivate should close modals")
	}
}

// TestContextActivate_OutOfRangeIsNoop guards against stale events.
func TestContextActivate_OutOfRangeIsNoop(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.contextHover = 5
	a.contextActivate() // no panic, no state change
	a.contextHover = -1
	a.contextActivate()
}

// TestRuneLen counts visible cells one-per-rune.
func TestRuneLen(t *testing.T) {
	cases := map[string]int{
		"":      0,
		"abc":   3,
		"héllo": 5, // five runes, one cell each by this helper's contract
		"日本":   2,
	}
	for s, want := range cases {
		if got := runeLen(s); got != want {
			t.Errorf("runeLen(%q) = %d, want %d", s, got, want)
		}
	}
}

// TestTrimSpace strips ASCII whitespace from both ends.
func TestTrimSpace(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"   ":         "",
		"abc":         "abc",
		"  abc  ":     "abc",
		"\t\nabc\r\n": "abc",
		"a b c":       "a b c", // interior whitespace untouched
	}
	for in, want := range cases {
		if got := trimSpace(in); got != want {
			t.Errorf("trimSpace(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPromptModalRect centers the prompt and clamps a tiny window.
func TestPromptModalRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.promptModalRect()
	if w != promptModalWidth || h != promptModalHeight {
		t.Fatalf("size: (%d,%d)", w, h)
	}
	if x != (a.width-w)/2 || y != (a.height-h)/2 {
		t.Fatalf("origin: (%d,%d)", x, y)
	}
	a.width, a.height = 4, 4
	x, y, _, _ = a.promptModalRect()
	if x != 0 || y != 0 {
		t.Fatalf("clamp: (%d,%d)", x, y)
	}
}

// TestConfirmModalRect centers the confirm modal and clamps a tiny window.
func TestConfirmModalRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.confirmModalRect()
	if w != confirmModalWidth || h != confirmModalHeight {
		t.Fatalf("size: (%d,%d)", w, h)
	}
	_ = x
	_ = y
	a.width, a.height = 4, 4
	x, y, _, _ = a.confirmModalRect()
	if x != 0 || y != 0 {
		t.Fatalf("clamp: (%d,%d)", x, y)
	}
}

// TestContextRect returns origin + width + count-derived height.
func TestContextRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.contextX, a.contextY = 10, 5
	a.contextItems = []contextItem{{label: "a"}, {label: "b"}}
	x, y, w, h := a.contextRect()
	if x != 10 || y != 5 || w != contextMenuWidth || h != 4 {
		t.Fatalf("contextRect: (%d,%d,%d,%d)", x, y, w, h)
	}
}

// TestHandlePromptMouse_OutsideCancels closes the prompt when clicked
// outside its rect.
func TestHandlePromptMouse_OutsideCancels(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openPrompt("T", "H", "x", nil)
	a.handlePromptMouse(0, 0, tcell.Button1)
	if a.promptOpen {
		t.Fatal("outside click should cancel")
	}
}

// TestHandlePromptMouse_OK clicks the OK button area and submits.
func TestHandlePromptMouse_OK(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	got := ""
	a.openPrompt("T", "H", "ok", func(_ *App, v string) { got = v })
	mx, my, _, _ := a.promptModalRect()
	a.handlePromptMouse(mx+32, my+6, tcell.Button1)
	if got != "ok" {
		t.Fatalf("OK button: callback got %q", got)
	}
}

// TestHandlePromptMouse_Cancel clicks the Cancel button area.
func TestHandlePromptMouse_Cancel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openPrompt("T", "H", "ok", func(*App, string) { called = true })
	mx, my, _, _ := a.promptModalRect()
	a.handlePromptMouse(mx+18, my+6, tcell.Button1)
	if called {
		t.Fatal("cancel button should skip callback")
	}
}

// TestHandlePromptMouse_FieldClickMovesCursor places the cursor at the
// clicked rune.
func TestHandlePromptMouse_FieldClickMovesCursor(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openPrompt("T", "H", "abcdef", nil)
	mx, my, _, _ := a.promptModalRect()
	// Click 2 cells into the field.
	a.handlePromptMouse(mx+3+2, my+4, tcell.Button1)
	if a.promptCursor != 2 {
		t.Fatalf("field click: got %d, want 2", a.promptCursor)
	}
}

// TestHandlePromptMouse_NoButton is a no-op for motion events.
func TestHandlePromptMouse_NoButton(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openPrompt("T", "H", "x", nil)
	a.handlePromptMouse(0, 0, 0)
	if !a.promptOpen {
		t.Fatal("motion event should not close prompt")
	}
}

// TestHandleConfirmMouse_OutsideCancels closes when clicked outside.
func TestHandleConfirmMouse_OutsideCancels(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openConfirm("T", "M", nil)
	a.handleConfirmMouse(0, 0, tcell.Button1)
	if a.confirmOpen {
		t.Fatal("outside click should cancel")
	}
}

// TestHandleConfirmMouse_HoverAndClick covers hover (no button) and click
// branches over both buttons.
func TestHandleConfirmMouse_HoverAndClick(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openConfirm("T", "M", func(*App) { called = true })
	mx, my, _, _ := a.confirmModalRect()

	// Hover over Yes (28..38) — sets confirmHover to 1.
	a.handleConfirmMouse(mx+30, my+5, 0)
	if a.confirmHover != 1 {
		t.Fatalf("hover Yes: got %d", a.confirmHover)
	}
	// Hover back over No (14..22).
	a.handleConfirmMouse(mx+16, my+5, 0)
	if a.confirmHover != 0 {
		t.Fatalf("hover No: got %d", a.confirmHover)
	}

	// Click Yes — fires callback.
	a.handleConfirmMouse(mx+30, my+5, tcell.Button1)
	if !called {
		t.Fatal("click Yes should fire callback")
	}

	// Re-open and click No.
	called = false
	a.openConfirm("T", "M", func(*App) { called = true })
	a.handleConfirmMouse(mx+16, my+5, tcell.Button1)
	if called {
		t.Fatal("click No should not fire callback")
	}
}

// TestHandleContextKey covers Up/Down clamps, Esc cancels, Enter activates.
func TestHandleContextKey(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := 0
	a.contextOpen = true
	a.contextNode = a.tree.Root
	a.contextItems = []contextItem{
		{label: "one", action: func(*App, *filetree.Node) { called++ }},
		{label: "two", action: func(*App, *filetree.Node) { called += 10 }},
	}
	a.contextHover = 0

	// Up at top — clamp.
	a.handleContextKey(keyEv(tcell.KeyUp, 0))
	if a.contextHover != 0 {
		t.Fatalf("Up clamp: got %d", a.contextHover)
	}
	// Down advances.
	a.handleContextKey(keyEv(tcell.KeyDown, 0))
	if a.contextHover != 1 {
		t.Fatalf("Down: got %d", a.contextHover)
	}
	// Down at bottom — clamp.
	a.handleContextKey(keyEv(tcell.KeyDown, 0))
	if a.contextHover != 1 {
		t.Fatalf("Down clamp: got %d", a.contextHover)
	}
	// Enter activates the second item.
	a.handleContextKey(keyEv(tcell.KeyEnter, 0))
	if called != 10 {
		t.Fatalf("Enter: called=%d", called)
	}
	// Re-open and Esc.
	a.contextOpen = true
	a.handleContextKey(keyEv(tcell.KeyEsc, 0))
	if a.contextOpen {
		t.Fatal("Esc should close")
	}
}

// TestHandleContextMouse_HoverAndClick verifies hover updates and click
// activates; outside click dismisses.
func TestHandleContextMouse_HoverAndClick(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := 0
	a.contextOpen = true
	a.contextX, a.contextY = 5, 5
	a.contextNode = a.tree.Root
	a.contextItems = []contextItem{
		{label: "one", action: func(*App, *filetree.Node) { called++ }},
		{label: "two", action: func(*App, *filetree.Node) { called += 10 }},
	}
	a.contextHover = 0

	// Hover row index 1 (relY=2 inside box at y=5 → screen y=7).
	a.handleContextMouse(7, 7, 0)
	if a.contextHover != 1 {
		t.Fatalf("hover: got %d", a.contextHover)
	}
	// Click row 0.
	a.contextOpen = true
	a.contextX, a.contextY = 5, 5
	a.contextItems = []contextItem{
		{label: "one", action: func(*App, *filetree.Node) { called++ }},
	}
	a.handleContextMouse(7, 6, tcell.Button1)
	if called == 0 {
		t.Fatal("click on row should activate")
	}

	// Outside click closes.
	a.contextOpen = true
	a.contextItems = []contextItem{{label: "x"}}
	a.handleContextMouse(0, 0, tcell.Button1)
	if a.contextOpen {
		t.Fatal("outside click should close")
	}
}

// -- small filesystem helpers used by the context-menu tests above ----------

// mkdir is a thin wrapper around os.Mkdir so the test bodies stay readable.
func mkdir(path string) error { return os.Mkdir(path, 0755) }

// writeFile writes payload to path. Wraps os.WriteFile to keep call sites
// terse.
func writeFile(path, payload string) error {
	return os.WriteFile(path, []byte(payload), 0644)
}

// -----------------------------------------------------------------------------
// Save / Discard / Cancel modal (unsaved-changes prompt)
// -----------------------------------------------------------------------------

// seedDirtyApp opens a tab and immediately marks it dirty so each test
// can drive the close / quit flow without re-typing the same fixture.
func seedDirtyApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString(" edit") // dirty
	return a
}

// TestOpenDirtyClose_DefaultsToCancel pins down the "an accidental
// Enter never loses work" property: focus lands on Cancel (idx 0) when
// the modal opens, so a stray Enter just dismisses.
func TestOpenDirtyClose_DefaultsToCancel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openDirtyClose("T", "M", func(*App) {}, func(*App) {})
	if !a.dirtyOpen {
		t.Fatal("modal should open")
	}
	if a.dirtyHover != 0 {
		t.Fatalf("default focus should be Cancel (0), got %d", a.dirtyHover)
	}
}

// TestRequestCloseTab_DirtySaveClosesTab walks the Save path: opening
// the modal, picking Save should both write the file (dirty flag clears)
// and close the tab.
func TestRequestCloseTab_DirtySaveClosesTab(t *testing.T) {
	a := seedDirtyApp(t)
	target := a.tabs[0].Path

	a.requestCloseTab(0)
	if !a.dirtyOpen {
		t.Fatal("dirty close should open the modal")
	}
	a.dirtyHover = 2 // Save
	a.dirtyActivate()

	if len(a.tabs) != 0 {
		t.Fatalf("Save should also close the tab; %d tabs left", len(a.tabs))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after save: %v", err)
	}
	if string(got) != " editseed" {
		t.Fatalf("unexpected file contents after save: %q", got)
	}
}

// TestRequestCloseTab_DirtyDiscardClosesWithoutSaving is the
// counterpart: Discard should drop the tab without touching disk.
func TestRequestCloseTab_DirtyDiscardClosesWithoutSaving(t *testing.T) {
	a := seedDirtyApp(t)
	target := a.tabs[0].Path

	a.requestCloseTab(0)
	a.dirtyHover = 1 // Discard
	a.dirtyActivate()

	if len(a.tabs) != 0 {
		t.Fatal("Discard should close the tab")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "seed" {
		t.Fatalf("Discard should not touch disk; got %q", got)
	}
}

// TestRequestCloseTab_DirtyCancelKeepsTab proves Cancel leaves
// everything alone — the tab stays open, the buffer stays dirty.
func TestRequestCloseTab_DirtyCancelKeepsTab(t *testing.T) {
	a := seedDirtyApp(t)

	a.requestCloseTab(0)
	a.dirtyHover = 0 // Cancel
	a.dirtyActivate()

	if len(a.tabs) != 1 {
		t.Fatalf("Cancel should keep the tab; got %d", len(a.tabs))
	}
	if !a.activeTabPtr().Dirty {
		t.Fatal("Cancel should not flip the dirty flag")
	}
	if a.dirtyOpen {
		t.Fatal("Cancel should dismiss the modal")
	}
}

// TestMenuQuit_NoDirtyTabsExitsImmediately keeps the fast path: when
// nothing is dirty, the modal must NOT open and a.quit flips on the
// spot.
func TestMenuQuit_NoDirtyTabsExitsImmediately(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.menuQuit()
	if a.dirtyOpen {
		t.Fatal("clean state should skip the modal")
	}
	if !a.quit {
		t.Fatal("clean menuQuit should set quit")
	}
}

// TestMenuQuit_DirtyOpensModal proves a dirty tab blocks the immediate
// exit and routes through the Save / Discard / Cancel modal.
func TestMenuQuit_DirtyOpensModal(t *testing.T) {
	a := seedDirtyApp(t)
	a.menuQuit()
	if a.quit {
		t.Fatal("dirty quit should not exit until the user picks an action")
	}
	if !a.dirtyOpen {
		t.Fatal("dirty quit should open the modal")
	}
}

// TestMenuQuit_DirtySaveSavesAllAndQuits drives the Save path on quit:
// every dirty tab is written and a.quit flips. This is the test that
// would catch a regression where Save quits without writing — losing
// the user's edits.
func TestMenuQuit_DirtySaveSavesAllAndQuits(t *testing.T) {
	a := seedDirtyApp(t)
	target := a.tabs[0].Path

	a.menuQuit()
	a.dirtyHover = 2 // Save
	a.dirtyActivate()

	if !a.quit {
		t.Fatal("Save in quit modal should set quit")
	}
	got, _ := os.ReadFile(target)
	if string(got) != " editseed" {
		t.Fatalf("Save in quit modal should have written; got %q", got)
	}
}

// TestMenuQuit_DirtyDiscardQuitsWithoutSaving proves Discard skips the
// save and exits anyway.
func TestMenuQuit_DirtyDiscardQuitsWithoutSaving(t *testing.T) {
	a := seedDirtyApp(t)
	target := a.tabs[0].Path

	a.menuQuit()
	a.dirtyHover = 1 // Discard
	a.dirtyActivate()

	if !a.quit {
		t.Fatal("Discard should still quit")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "seed" {
		t.Fatalf("Discard must not touch disk; got %q", got)
	}
}

// TestHandleDirtyKey_AllBranches walks Left / Right / Tab / Enter / Esc.
// Each branch is small; pinning all of them keeps the navigation
// behaviour stable across refactors.
func TestHandleDirtyKey_AllBranches(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	saveCalled, discardCalled := false, false
	a.openDirtyClose("T", "M",
		func(*App) { saveCalled = true },
		func(*App) { discardCalled = true })

	// Right walks Cancel -> Discard -> Save and stops at Save.
	a.handleDirtyKey(keyEv(tcell.KeyRight, 0))
	if a.dirtyHover != 1 {
		t.Fatalf("Right(0->1) got %d", a.dirtyHover)
	}
	a.handleDirtyKey(keyEv(tcell.KeyRight, 0))
	if a.dirtyHover != 2 {
		t.Fatalf("Right(1->2) got %d", a.dirtyHover)
	}
	a.handleDirtyKey(keyEv(tcell.KeyRight, 0))
	if a.dirtyHover != 2 {
		t.Fatalf("Right(2->2) should clamp, got %d", a.dirtyHover)
	}

	// Left walks back, also clamps at 0.
	a.handleDirtyKey(keyEv(tcell.KeyLeft, 0))
	a.handleDirtyKey(keyEv(tcell.KeyLeft, 0))
	a.handleDirtyKey(keyEv(tcell.KeyLeft, 0))
	if a.dirtyHover != 0 {
		t.Fatalf("Left should clamp at 0, got %d", a.dirtyHover)
	}

	// Tab cycles all the way around.
	a.handleDirtyKey(keyEv(tcell.KeyTab, 0))
	a.handleDirtyKey(keyEv(tcell.KeyTab, 0))
	a.handleDirtyKey(keyEv(tcell.KeyTab, 0))
	if a.dirtyHover != 0 {
		t.Fatalf("Tab cycle should land back on 0, got %d", a.dirtyHover)
	}

	// Enter on Cancel (default) — runs neither callback.
	a.handleDirtyKey(keyEv(tcell.KeyEnter, 0))
	if saveCalled || discardCalled {
		t.Fatalf("Enter on Cancel should run neither cb (save=%v discard=%v)",
			saveCalled, discardCalled)
	}

	// Re-open and Enter on Discard.
	a.openDirtyClose("T", "M",
		func(*App) { saveCalled = true },
		func(*App) { discardCalled = true })
	a.dirtyHover = 1
	a.handleDirtyKey(keyEv(tcell.KeyEnter, 0))
	if !discardCalled {
		t.Fatal("Enter on Discard should fire discard callback")
	}

	// Re-open and Enter on Save.
	saveCalled = false
	a.openDirtyClose("T", "M",
		func(*App) { saveCalled = true },
		func(*App) {})
	a.dirtyHover = 2
	a.handleDirtyKey(keyEv(tcell.KeyEnter, 0))
	if !saveCalled {
		t.Fatal("Enter on Save should fire save callback")
	}

	// Esc dismisses without firing anything.
	a.openDirtyClose("T", "M", func(*App) { t.Fatal("save fired on Esc") },
		func(*App) { t.Fatal("discard fired on Esc") })
	a.handleDirtyKey(keyEv(tcell.KeyEsc, 0))
	if a.dirtyOpen {
		t.Fatal("Esc should close the modal")
	}
}

// TestSaveAllDirty_SavesEveryTab covers the multi-tab quit path: every
// dirty tab is written; a clean tab is left alone.
func TestSaveAllDirty_SavesEveryTab(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte("seed"), 0644); err != nil {
			t.Fatal(err)
		}
		a.openFile(full)
	}
	// Dirty tabs 0 and 2; leave 1 clean.
	a.tabs[0].Buffer.InsertString(a.tabs[0].Cursor, "x")
	a.tabs[0].Dirty = true
	a.tabs[2].Buffer.InsertString(a.tabs[2].Cursor, "y")
	a.tabs[2].Dirty = true

	if !a.saveAllDirty() {
		t.Fatal("expected saveAllDirty to succeed")
	}
	if a.tabs[0].Dirty || a.tabs[2].Dirty {
		t.Fatal("dirty flags should clear after save")
	}
}

// TestDirtyTabCount counts only tabs whose Dirty flag is set.
func TestDirtyTabCount(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	for _, name := range []string{"a.txt", "b.txt"} {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte("seed"), 0644); err != nil {
			t.Fatal(err)
		}
		a.openFile(full)
	}
	if got := a.dirtyTabCount(); got != 0 {
		t.Fatalf("expected 0 dirty, got %d", got)
	}
	a.tabs[0].Dirty = true
	if got := a.dirtyTabCount(); got != 1 {
		t.Fatalf("expected 1 dirty, got %d", got)
	}
}

// TestDirtyButtonAtRelX_HitsAndMisses pins the geometry helper so the
// click rect math stays in sync with the draw layout.
func TestDirtyButtonAtRelX_HitsAndMisses(t *testing.T) {
	cases := []struct {
		rx   int
		want int
	}{
		{dirtyBtnCancelX + 1, 0},
		{dirtyBtnDiscardX + 1, 1},
		{dirtyBtnSaveX + 1, 2},
		{0, -1},
		{dirtyBtnSaveX + dirtyBtnSaveW + 5, -1},
	}
	for _, c := range cases {
		if got := dirtyButtonAtRelX(c.rx); got != c.want {
			t.Errorf("rx=%d: got %d, want %d", c.rx, got, c.want)
		}
	}
}
