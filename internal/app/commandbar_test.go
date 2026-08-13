// =============================================================================
// File: internal/app/commandbar_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-13
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the command bar: open/close routing, bash-style directory
// completion, the cd command's validation, and the re-root that rewires
// the file tree, finder index and git status at the new directory.

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/gdamore/tcell/v2"
)

// typeIntoCommand drives runes through the command bar's key handler.
func typeIntoCommand(a *App, s string) {
	for _, r := range s {
		a.handleCommandKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

// seedDirs creates a small directory tree for completion tests:
// root/{alpha, alphabeta, beta, sub/{nested/{deep}, other}}, plus a file
// that must never surface as a cd candidate.
func seedDirs(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"alpha", "alphabeta", "beta", "sub/nested/deep", "sub/other", "sub/.git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// A file that must never appear in cd completion.
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestOpenCommandBar opens the bar via the leader path and checks the
// initial state is a clean, empty input.
func TestOpenCommandBar(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openCommandBar()
	if !a.commandOpen {
		t.Fatal("commandOpen should be true after openCommandBar")
	}
	if len(a.commandValue) != 0 {
		t.Errorf("commandValue = %q, want empty", string(a.commandValue))
	}
	a.closeCommandBar()
	if a.commandOpen {
		t.Fatal("commandOpen should be false after closeCommandBar")
	}
}

// TestCommandLeaderRouting verifies the Esc-: leader opens the bar and
// that a bare ':' still types into the editor when no leader is armed.
func TestCommandLeaderRouting(t *testing.T) {
	a := newTestApp(t, t.TempDir())

	// Bare ':' with no pending Esc inserts normally.
	if a.commandOpen {
		t.Fatal("command bar should start closed")
	}
	a.lastEscape = time.Now()
	a.handleKey(tcell.NewEventKey(tcell.KeyRune, ':', tcell.ModNone))
	if !a.commandOpen {
		t.Fatal("Esc-: should open the command bar")
	}

	// Esc alone (stale leader) must not open the bar.
	a.closeCommandBar()
	a.lastEscape = time.Now().Add(-2 * doubleEscMs)
	a.handleKey(tcell.NewEventKey(tcell.KeyRune, ':', tcell.ModNone))
	if a.commandOpen {
		t.Fatal("stale Esc should not open the command bar")
	}
}

// TestCommandCompletionSingle extends one candidate to a full directory
// plus a trailing space, bash-style.
func TestCommandCompletionSingle(t *testing.T) {
	root := t.TempDir()
	seedDirs(t, root)
	a := newTestApp(t, t.TempDir())
	a.rootDir = root
	a.openCommandBar()

	typeIntoCommand(a, "cd bet")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd beta " {
		t.Errorf("value = %q, want %q", got, "cd beta ")
	}
}

// TestCommandCompletionCommonPrefix extends the token only as far as the
// candidates agree, then a second Tab cycles through the list.
func TestCommandCompletionCommonPrefix(t *testing.T) {
	root := t.TempDir()
	seedDirs(t, root)
	a := newTestApp(t, t.TempDir())
	a.rootDir = root
	a.openCommandBar()

	typeIntoCommand(a, "cd alp")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd alpha" {
		t.Errorf("after first Tab = %q, want common prefix %q", got, "cd alpha")
	}
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd alphabeta" {
		t.Errorf("after cycle Tab = %q, want %q", got, "cd alphabeta")
	}
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd alpha" {
		t.Errorf("after wrap Tab = %q, want %q", got, "cd alpha")
	}
}

// TestCommandCompletionNested completes inside a subdirectory and keeps
// the directory prefix in the replacement.
func TestCommandCompletionNested(t *testing.T) {
	root := t.TempDir()
	seedDirs(t, root)
	a := newTestApp(t, t.TempDir())
	a.rootDir = root
	a.openCommandBar()

	typeIntoCommand(a, "cd sub/nes")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd sub/nested " {
		t.Errorf("value = %q, want %q", got, "cd sub/nested ")
	}

	// Trailing slash lists the directory's own children.
	a.closeCommandBar()
	a.openCommandBar()
	typeIntoCommand(a, "cd sub/nested/")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd sub/nested/deep " {
		t.Errorf("value = %q, want %q", got, "cd sub/nested/deep ")
	}
}

// TestCommandCompletionTilde completes under the home directory while
// preserving the ~ spelling.
func TestCommandCompletionTilde(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Redirect os.UserHomeDir by setting HOME — it consults the env var
	// on unix before falling back to passwd.
	t.Setenv("HOME", home)

	a := newTestApp(t, t.TempDir())
	a.openCommandBar()
	typeIntoCommand(a, "cd ~/Doc")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd ~/Documents " {
		t.Errorf("value = %q, want %q", got, "cd ~/Documents ")
	}
}

// TestCommandCompletionIgnoresFilesAndGit asserts files and the project's
// .git directory never surface as cd candidates.
func TestCommandCompletionIgnoresFilesAndGit(t *testing.T) {
	root := t.TempDir()
	seedDirs(t, root)
	a := newTestApp(t, t.TempDir())
	a.rootDir = root
	a.openCommandBar()

	typeIntoCommand(a, "cd ")
	for _, name := range a.commandSuggestion {
		if name == "alpha.txt" || name == ".git" {
			t.Errorf("candidate %q must not appear", name)
		}
	}
	if len(a.commandSuggestion) != 4 { // alpha, alphabeta, beta, sub
		t.Errorf("candidates = %v, want 4 dirs", a.commandSuggestion)
	}
}

// TestCommandCompletionArrows cycles candidates with Up/Down. The prefix
// stays broad ("alp" matches both alpha and alphabeta) so the list doesn't
// narrow under the cursor the way a full candidate would.
func TestCommandCompletionArrows(t *testing.T) {
	root := t.TempDir()
	seedDirs(t, root)
	a := newTestApp(t, t.TempDir())
	a.rootDir = root
	a.openCommandBar()

	typeIntoCommand(a, "cd alp")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd alpha" {
		t.Errorf("after Down = %q, want %q", got, "cd alpha")
	}
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd alphabeta" {
		t.Errorf("after second Down = %q, want %q", got, "cd alphabeta")
	}
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if got := string(a.commandValue); got != "cd alpha" {
		t.Errorf("after Up = %q, want %q", got, "cd alpha")
	}
}

// TestCmdCdReroot runs a valid cd through the bar and verifies every
// project surface — tree root, active folder, rootDir, finder index —
// points at the new directory, while open tabs survive.
func TestCmdCdReroot(t *testing.T) {
	old := t.TempDir()
	sub := filepath.Join(old, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	a := newTestApp(t, old)
	a.openCommandBar()
	typeIntoCommand(a, "cd sub")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if a.commandOpen {
		t.Fatal("command bar should close after a successful cd")
	}
	if a.rootDir != sub {
		t.Errorf("rootDir = %q, want %q", a.rootDir, sub)
	}
	if a.tree == nil || a.tree.Root.Path != sub {
		t.Errorf("tree root = %v, want %q", a.tree, sub)
	}
	if a.activeFolder != sub {
		t.Errorf("activeFolder = %q, want %q", a.activeFolder, sub)
	}
	waitForFinderReady(t, a)
	if a.finder == nil {
		t.Fatal("finder should be re-created after reroot")
	}
	// The new index must contain the new root's file, not the old one's.
	found := false
	for _, r := range a.finder.Search("main.go", 10) {
		if r.Path == "main.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("finder index lacks main.go under the new root")
	}
}

// TestCmdCdInvalidPath keeps the bar open with a hint for a missing
// directory — nvim's E344 behavior.
func TestCmdCdInvalidPath(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openCommandBar()
	typeIntoCommand(a, "cd nope")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if !a.commandOpen {
		t.Fatal("bar should stay open after a failed cd")
	}
	if !strings.Contains(a.commandHint, "no such directory") {
		t.Errorf("hint = %q, want 'no such directory'", a.commandHint)
	}
}

// TestCmdCdMissingArg flashes a hint instead of running.
func TestCmdCdMissingArg(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openCommandBar()
	typeIntoCommand(a, "cd")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !a.commandOpen {
		t.Fatal("bar should stay open")
	}
	if a.commandHint != "cd: missing directory" {
		t.Errorf("hint = %q, want missing-directory", a.commandHint)
	}
}

// TestCommandUnknownCommand reports unknown commands and keeps the bar
// open so the typo can be fixed.
func TestCommandUnknownCommand(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openCommandBar()
	typeIntoCommand(a, "ls -la")
	a.handleCommandKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !a.commandOpen {
		t.Fatal("bar should stay open for unknown commands")
	}
	if a.commandHint != "unknown command: ls" {
		t.Errorf("hint = %q, want unknown-command hint", a.commandHint)
	}
}

// TestRerootClearsSearchResults asserts stale root-relative results from
// every search surface are dropped when the project re-roots.
func TestRerootClearsSearchResults(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.finder = finder.New(a.rootDir)
	a.finderResults = []finder.Result{{Path: "old/file.go"}}
	a.searchResults = []finder.ContentMatch{{Path: "old/file.go"}}
	a.sidebarSearchResults = []finder.ContentMatch{{Path: "old/file.go"}}

	next := t.TempDir()
	a.reroot(next)

	if a.finderResults != nil {
		t.Error("finderResults should be cleared on reroot")
	}
	if a.searchResults != nil {
		t.Error("searchResults should be cleared on reroot")
	}
	if a.sidebarSearchResults != nil {
		t.Error("sidebarSearchResults should be cleared on reroot")
	}
	if a.tree == nil || a.tree.Root.Path != next {
		t.Errorf("tree root = %v, want %q", a.tree, next)
	}
}

// TestCommandBarDraw renders the bar on a simulation screen and checks
// the input row paints with the typed command.
func TestCommandBarDraw(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openCommandBar()
	typeIntoCommand(a, "cd sub")
	a.draw()
	scr := a.screen.(tcell.SimulationScreen)
	scr.Show()

	_, by, _, _ := a.commandBarRect()
	line := screenLine(scr, by)
	if !strings.Contains(line, "cd sub") {
		t.Errorf("command row = %q, want it to contain %q", line, "cd sub")
	}
}
