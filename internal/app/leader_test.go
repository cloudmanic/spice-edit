// =============================================================================
// File: internal/app/leader_test.go
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

	"github.com/gdamore/tcell/v2"
)

// TestLeaderActionFor_AllBindingsResolve walks the binding table and
// verifies every entry returns a non-nil action. Catches accidentally
// dropping a method reference when the table is reshuffled.
func TestLeaderActionFor_AllBindingsResolve(t *testing.T) {
	for _, b := range leaderBindings() {
		if leaderActionFor(b.key) == nil {
			t.Errorf("binding %q resolved to nil", b.key)
		}
	}
}

// TestLeaderActionFor_UnboundReturnsNil pins down the contract that
// leaderActionFor reports a miss with nil so handleKey can distinguish
// "leader fired" from "key was unbound — fall through".
func TestLeaderActionFor_UnboundReturnsNil(t *testing.T) {
	if leaderActionFor('z') != nil {
		t.Fatal("'z' should not be a leader binding (no editor action mapped)")
	}
}

// TestHandleKey_LeaderSave saves the active tab via Esc, s. The buffer
// is dirtied before the leader fires so the assertion is meaningful:
// a successful save flips the dirty flag back to false.
func TestHandleKey_LeaderSave(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.handleKey(keyEv(tcell.KeyRune, 'x')) // dirty the buffer
	if !a.activeTabPtr().Dirty {
		t.Fatal("expected dirty buffer before save")
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 's'))

	if a.activeTabPtr().Dirty {
		t.Fatal("Esc-s should have saved the buffer (dirty still true)")
	}
}

// TestHandleKey_LeaderUndoRedo round-trips an edit through Esc-u and
// Esc-r. Pins down both bindings at once and the fact that the leader
// state resets between sequences (we re-arm Esc each time).
func TestHandleKey_LeaderUndoRedo(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.handleKey(keyEv(tcell.KeyRune, 'a'))

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'u'))
	if a.activeTabPtr().Buffer.Lines[0] != "" {
		t.Fatalf("Esc-u should have undone the insert, got %q", a.activeTabPtr().Buffer.Lines[0])
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'r'))
	if a.activeTabPtr().Buffer.Lines[0] != "a" {
		t.Fatalf("Esc-r should have redone the insert, got %q", a.activeTabPtr().Buffer.Lines[0])
	}
}

// TestHandleKey_LeaderToggleSidebar flips sidebarShown via Esc-t. The
// toggle is the simplest leader action with no preconditions, so it's
// the most stable smoke test that the dispatch wiring is intact.
func TestHandleKey_LeaderToggleSidebar(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	before := a.sidebarShown
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 't'))
	if a.sidebarShown == before {
		t.Fatalf("Esc-t should toggle sidebar (still %v)", a.sidebarShown)
	}
}

// TestHandleKey_LeaderQuit sets a.quit via Esc-q. We test this directly
// rather than through Run() so we don't have to drive the event loop —
// the quit flag is what Run() polls each tick.
func TestHandleKey_LeaderQuit(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'q'))
	if !a.quit {
		t.Fatal("Esc-q should set a.quit = true")
	}
}

// TestHandleKey_LeaderUnboundFallsThrough is the regression test for the
// "stray Esc shouldn't swallow the next keystroke" property: pressing
// Esc and then an unbound letter must still deliver that letter to the
// active tab. Without the fall-through, an accidental Esc tap would
// silently eat the user's next character.
func TestHandleKey_LeaderUnboundFallsThrough(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'z'))

	if got := a.activeTabPtr().Buffer.Lines[0]; got != "z" {
		t.Fatalf("unbound key after Esc should reach the editor, got %q", got)
	}
}

// TestHandleKey_LeaderTimesOut verifies the leader window expires:
// after doubleEscMs has passed since the last Esc, a bound letter must
// reach the editor as a normal keystroke instead of firing the action.
func TestHandleKey_LeaderTimesOut(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	// Backdate the Esc timestamp past the leader window so the next 's'
	// is treated as a plain keystroke rather than Save.
	a.lastEscape = time.Now().Add(-2 * doubleEscMs)
	a.handleKey(keyEv(tcell.KeyRune, 's'))

	if got := a.activeTabPtr().Buffer.Lines[0]; got != "s" {
		t.Fatalf("expired leader window: 's' should insert literally, got %q", got)
	}
}

// TestHandleKey_EscDoubleTapStillOpensMenu makes sure adding the leader
// table didn't break the existing double-Esc-opens-menu gesture. The
// second Esc inside the leader window must still be interpreted as
// "open the menu," not as an unbound leader keystroke.
func TestHandleKey_EscDoubleTapStillOpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if !a.menuOpen {
		t.Fatal("double-Esc should still open the menu after leader was added")
	}
}
