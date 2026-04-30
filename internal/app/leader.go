// =============================================================================
// File: internal/app/leader.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// leader.go defines the editor's Esc-leader hotkey table. Esc-Esc still opens
// the action menu (handled in handleKey); the bindings here handle the
// "Esc, then a single letter within doubleEscMs" sequences for common
// actions. We deliberately avoid Ctrl-key shortcuts because they fight
// tmux/zellij prefixes and the terminal's own bindings — Esc is the only
// modifier we trust over SSH.

package app

// leaderBinding is one Esc-leader entry: the trigger rune (lowercase ASCII)
// and the App method that fires when the user presses Esc, <rune> in quick
// succession. Each method already handles its own preconditions — calling
// menuUndo with no active tab, for example, is a safe no-op — so the leader
// dispatch doesn't need to re-check enable predicates.
type leaderBinding struct {
	key    rune
	action func(*App)
}

// leaderBindings is the editor's full Esc-leader table. The order is purely
// presentational: tests iterate it to assert every binding fires, and a
// future help screen can render the table directly. Letters are chosen to
// be mnemonic and avoid collisions — capital letters are reserved in case
// we ever want a "shift-flavored" variant for destructive twins.
//
// Intentionally not bound:
//   - c / x / v (clipboard) — the host terminal's Cmd+C/V already covers
//     that path; adding a third channel just adds confusion.
//   - rename / delete / revert — destructive enough that we want the
//     menu's confirm dialog to gate the action as a deliberate gesture.
func leaderBindings() []leaderBinding {
	return []leaderBinding{
		{'s', (*App).menuSave},
		{'u', (*App).menuUndo},
		{'r', (*App).menuRedo},
		{'w', (*App).menuClose},
		{'q', (*App).menuQuit},
		{'n', (*App).menuNewFile},
		{'t', (*App).menuToggleSidebar},
		{'f', (*App).openFind},
		{'g', (*App).findAgain},
	}
}

// leaderActionFor looks up the App method bound to r in the leader table,
// or returns nil when r isn't bound. Returning nil rather than a no-op
// lets the caller distinguish "leader fired" from "key was unbound — fall
// through to normal handling", which matters for typing flow: pressing
// Esc then a non-leader letter must still let that letter reach the
// editor's normal key handler.
func leaderActionFor(r rune) func(*App) {
	for _, b := range leaderBindings() {
		if b.key == r {
			return b.action
		}
	}
	return nil
}
