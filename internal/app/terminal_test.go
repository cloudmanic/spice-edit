// =============================================================================
// File: internal/app/terminal_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/editor"
)

// skipWithoutPTY guards the tests that spawn a real shell.
func skipWithoutPTY(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("terminal tabs are unsupported on Windows")
	}
}

// TestMenuOpenTerminal_AppendsAndFocusesTab verifies the menu action adds
// a terminal tab and makes it active, which is what "open terminal in a
// new tab" means to the user.
func TestMenuOpenTerminal_AppendsAndFocusesTab(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)

	a.menuOpenTerminal()

	if len(a.tabs) != 1 {
		t.Fatalf("tab count = %d, want 1", len(a.tabs))
	}
	if a.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0", a.activeTab)
	}
	tab := a.activeTabPtr()
	if tab == nil || !tab.IsTerminal() {
		t.Fatal("active tab is not a terminal tab")
	}
	if a.menuOpen {
		t.Error("menu should be closed after the action runs")
	}
}

// TestMenuOpenTerminal_OpensAlongsideFileTabs verifies a terminal doesn't
// replace or disturb existing file tabs — it's an additional tab.
func TestMenuOpenTerminal_OpensAlongsideFileTabs(t *testing.T) {
	skipWithoutPTY(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	t.Cleanup(a.closeAllTerminals)

	a.openFile(target)
	a.menuOpenTerminal()

	if len(a.tabs) != 2 {
		t.Fatalf("tab count = %d, want 2", len(a.tabs))
	}
	if !a.tabs[0].IsTerminal() && a.tabs[0].Path != target {
		t.Errorf("first tab should still be the file tab, got %q", a.tabs[0].Path)
	}
	if !a.tabs[1].IsTerminal() {
		t.Error("second tab should be the terminal")
	}
}

// TestMenuOpenTerminal_StartsInActiveFolder pins the cwd choice: the
// shell should start where the user is working, not always at the
// project root, so relative commands land where they expect.
func TestMenuOpenTerminal_StartsInActiveFolder(t *testing.T) {
	skipWithoutPTY(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	a := newTestApp(t, root)
	a.setActiveFolder(sub)

	if got := a.terminalCwd(); got != sub {
		t.Errorf("terminalCwd() = %q, want %q", got, sub)
	}

	// A stale / deleted active folder must fall back to the root rather
	// than handing the shell a directory that no longer exists.
	a.setActiveFolder(filepath.Join(root, "does-not-exist"))
	if got := a.terminalCwd(); got != a.rootDir {
		t.Errorf("terminalCwd() = %q, want root %q", got, a.rootDir)
	}
}

// TestTerminalTab_KeysGoToShell is the integration check that keystrokes
// routed through the app's normal key handler reach the child shell and
// come back as output.
func TestTerminalTab_KeysGoToShell(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	for _, r := range "echo spice_marker" {
		a.handleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(editorPaneText(a), "spice_marker") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("shell never echoed the marker; pane was:\n%s", editorPaneText(a))
}

// editorPaneText draws the app and reads the editor pane back out of the
// simulation screen as plain text, so terminal assertions go through the
// exact render path the user sees.
func editorPaneText(a *App) string {
	a.draw()
	a.screen.Show()
	ex, ey, ew, eh := a.editorRect()
	var sb strings.Builder
	for y := ey; y < ey+eh; y++ {
		for x := ex; x < ex+ew; x++ {
			ch, _, _, _ := a.screen.GetContent(x, y)
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// TestTerminalTab_TypingDoesNotDirtyTheTab guards the quit flow: if
// typing into a terminal marked the tab dirty, quitting would pop the
// unsaved-changes modal for a shell that has nothing to save.
func TestTerminalTab_TypingDoesNotDirtyTheTab(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	for _, r := range "some text" {
		a.handleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleKey(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))

	tab := a.activeTabPtr()
	if tab.Dirty {
		t.Error("terminal tab became dirty from typing")
	}
	if got := tab.Buffer.String(); got != "" {
		t.Errorf("terminal tab buffer = %q, want empty", got)
	}
}

// TestTerminalTab_EscStillOpensMenu is the key contract that keeps the
// editor usable from inside a shell: Esc must never be swallowed by the
// terminal, or the user would have no way back to the action menu.
func TestTerminalTab_EscStillOpensMenu(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))

	if !a.menuOpen {
		t.Fatal("double-Esc did not open the action menu from a terminal tab")
	}
}

// TestTerminalTab_LeaderBindingOpensTerminal verifies the Esc-` leader
// key reaches menuOpenTerminal, matching the shortcut advertised in the
// menu row.
func TestTerminalTab_LeaderBindingOpensTerminal(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)

	action := leaderActionFor('`')
	if action == nil {
		t.Fatal("Esc-` is not bound in the leader table")
	}
	action(a)

	if len(a.tabs) != 1 || !a.tabs[0].IsTerminal() {
		t.Fatal("Esc-` did not open a terminal tab")
	}
}

// TestCloseTab_ShutsDownTheShell verifies closing the tab tears the child
// process down. Without this the editor would leak a shell per terminal
// tab for the rest of the session.
func TestCloseTab_ShutsDownTheShell(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	a.menuOpenTerminal()

	tab := a.activeTabPtr()
	term := tab.Term
	proc := term.Process()
	if proc == nil {
		t.Fatal("terminal has no child process")
	}

	a.closeTab(0)

	if len(a.tabs) != 0 {
		t.Fatalf("tab count = %d, want 0", len(a.tabs))
	}
	// Once the PTY is closed and the process killed, the reader goroutine
	// reaps it and flips Exited.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if exited, _ := term.Exited(); exited {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("child shell was still running after the tab was closed")
}

// TestMenuLayout_TerminalRowPresent pins the menu row itself — label,
// shortcut, and that it's reachable and enabled on this platform.
func TestMenuLayout_TerminalRowPresent(t *testing.T) {
	a := newTestApp(t, t.TempDir())

	item := menuItemByLabel(t, a, "Open terminal in new tab")
	if item.action == nil {
		t.Fatal("terminal menu row has no action")
	}
	if item.shortcut != "Esc `" {
		t.Errorf("shortcut = %q, want %q", item.shortcut, "Esc `")
	}
	if runtime.GOOS != "windows" && !item.enabled(a) {
		t.Error("terminal row should be enabled on a PTY-capable platform")
	}
}

// TestStatusBar_ShowsTerminalState verifies the status bar reports shell
// state rather than trying to print a line/column for a terminal, which
// has neither.
func TestStatusBar_ShowsTerminalState(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	// Clear the "Terminal opened" flash so the tab-derived text renders.
	a.statusMsg = ""
	a.statusUntil = time.Time{}

	a.drawStatusBar()
	a.screen.Show()

	if got := statusBarText(a); !strings.Contains(got, "terminal") {
		t.Errorf("status bar = %q, want it to mention the terminal", got)
	}
}

// statusBarText reads the rendered status bar row back out of the
// simulation screen.
func statusBarText(a *App) string {
	_, sy, sw, _ := a.statusRect()
	var sb strings.Builder
	for cx := 0; cx < sw; cx++ {
		ch, _, _, _ := a.screen.GetContent(cx, sy)
		sb.WriteRune(ch)
	}
	return strings.TrimSpace(sb.String())
}

// TestDrawWithTerminalTab_DoesNotPanic exercises the full draw pipeline
// with a terminal as the active tab, including the degenerate tiny-window
// path where the editor rect can collapse.
func TestDrawWithTerminalTab_DoesNotPanic(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	a.draw()
	a.screen.Show()

	// Shrink to the smallest sane size and redraw — the terminal must
	// clamp rather than index outside its grid.
	a.width, a.height = minWidth, minHeight
	a.draw()
	a.screen.Show()
}

// TestTerminalTab_EscLeaderDoesNotStealShellKeys guards a nasty footgun.
// Esc arms the leader table, so at a shell prompt "Esc" then "q" used to
// run menuQuit — killing the editor and every running shell — instead of
// typing "q". Shell users press Esc constantly (vi keybindings, cancelling
// a completion), so the leader table has to stand down for terminal tabs.
func TestTerminalTab_EscLeaderDoesNotStealShellKeys(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	// Esc, then 'q' well within the leader window.
	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	a.handleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))

	if a.quit {
		t.Fatal("Esc-q quit the editor from a terminal tab; the leader table must not fire there")
	}
	if a.dirtyOpen {
		t.Fatal("Esc-q opened the quit-confirmation modal from a terminal tab")
	}

	// The same sequence in a text tab must still work, so this fix
	// doesn't silently disable the leader table everywhere.
	a.closeTab(0)
	if got := len(a.tabs); got != 0 {
		t.Fatalf("tab count = %d, want 0", got)
	}
	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	a.handleKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	if !a.quit {
		t.Error("Esc-q no longer quits from a non-terminal context")
	}
}

// TestTerminalTab_DoubleEscStillOpensMenuAfterLeaderOptOut makes sure the
// leader opt-out didn't cost terminal tabs their route back to the menu —
// that's the only way to reach editor actions from inside a shell.
func TestTerminalTab_DoubleEscStillOpensMenuAfterLeaderOptOut(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)
	a.menuOpenTerminal()

	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	if !a.menuOpen {
		t.Fatal("double-Esc no longer opens the menu from a terminal tab")
	}

	// And the menu's own rune shortcuts must still work once it's open.
	a.handleKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	if a.menuOpen {
		t.Error("Esc did not close the open menu")
	}
}

// TestClose_ReapsTerminalsOnQuit verifies the app-level teardown path:
// quitting the editor must hang up every shell it started, including
// their background jobs. Exercises App.Close rather than a single tab so
// the concurrent closeAllTerminals path is covered too.
func TestClose_ReapsTerminalsOnQuit(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())

	a.menuOpenTerminal()
	a.menuOpenTerminal()
	if len(a.tabs) != 2 {
		t.Fatalf("tab count = %d, want 2", len(a.tabs))
	}

	terms := []*editor.Terminal{a.tabs[0].Term, a.tabs[1].Term}

	// Closing must not take grace × N — the shells are hung up in
	// parallel, so a serial implementation would stall quit.
	start := time.Now()
	a.closeAllTerminals()
	elapsed := time.Since(start)

	for i, term := range terms {
		deadline := time.Now().Add(5 * time.Second)
		reaped := false
		for time.Now().Before(deadline) {
			if exited, _ := term.Exited(); exited {
				reaped = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !reaped {
			t.Errorf("terminal %d still running after closeAllTerminals", i)
		}
	}

	if elapsed > 3*time.Second {
		t.Errorf("closeAllTerminals took %v; shells should be hung up concurrently", elapsed)
	}
}

// TestTabBarClick_TerminalButton verifies the far-right tab-bar button opens
// and focuses a terminal tab.
func TestTabBarClick_TerminalButton(t *testing.T) {
	skipWithoutPTY(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(a.closeAllTerminals)

	a.drawTabBar() // lays out the far-right button and sets newTabBtnX
	if a.newTabBtnX < 0 {
		t.Fatal("terminal button not laid out")
	}

	a.tabBarClick(a.newTabBtnX+1, 0)

	if len(a.tabs) != 1 {
		t.Fatalf("tab count = %d, want 1", len(a.tabs))
	}
	if a.activeTab != 0 || a.activeTabPtr() == nil || !a.activeTabPtr().IsTerminal() {
		t.Fatal("terminal tab not opened/focused")
	}
}
