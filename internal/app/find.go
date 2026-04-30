// =============================================================================
// File: internal/app/find.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// find.go owns the in-file search UI: the 1-row "Find:" bar that lives
// directly above the status bar, the keystroke dispatch while the bar is
// focused, and the Esc-f / Esc-g leader entry points.
//
// The matching logic itself lives on Tab (see internal/editor/find.go) so
// each tab carries its own query, match list, and current-index. This
// file only handles UI: the input string, cursor, scroll, and rendering.

package app

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

// findBarHeight is the cell height of the find bar. Always 1 — the bar is
// a single row pinned above the status bar. Pulled out as a constant so
// editorRect / find rect math reads as "subtract the find bar's row" at
// every call site.
const findBarHeight = 1

// openFind shows the find bar and seeds the input with the active tab's
// last query (so tapping Esc-f a second time reopens the bar with the
// search the user was running). The cursor lands at the end of the
// pre-fill so the user can keep typing or just press Enter.
//
// We don't compute matches here — the user will type something, and the
// per-keystroke handler will recompute the match list. Pre-filling with
// a stale query is also a no-op visually until the user presses Enter.
func (a *App) openFind() {
	tab := a.activeTabPtr()
	if tab == nil || tab.IsImage() {
		return
	}
	a.closeAllModals() // a modal would otherwise eat our keystrokes
	a.findOpen = true
	a.findValue = []rune(tab.FindQuery)
	a.findCursor = len(a.findValue)
	a.findScroll = 0
	// Re-apply the (possibly empty) query so the highlight bar matches
	// what's in the input field. SetFindQuery handles the empty case
	// itself, so this works regardless of whether the user had a prior
	// search.
	tab.SetFindQuery(string(a.findValue))
}

// closeFind hides the find bar but preserves the tab's query / matches
// so that Esc g (find again) and re-opening with Esc f both pick up
// where the user left off. ClearFind would wipe that state — we don't
// want to.
func (a *App) closeFind() {
	a.findOpen = false
	a.findValue = nil
	a.findCursor = 0
	a.findScroll = 0
}

// findApplyQuery pushes the current input text into the active tab's
// find state and snaps the cursor to the new "current" match (so the
// user can see their result while still typing). Called on every input
// change so the highlights track the query live.
func (a *App) findApplyQuery() {
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	tab.SetFindQuery(string(a.findValue))
	tab.FocusCurrentMatch()
}

// findNext is the Enter-in-the-bar action: jump to the next match (with
// wrap). Also reachable from the Esc-g leader.
func (a *App) findNext() {
	if tab := a.activeTabPtr(); tab != nil {
		tab.FindNext()
	}
}

// findPrev is the Shift-Enter action: jump to the previous match.
func (a *App) findPrev() {
	if tab := a.activeTabPtr(); tab != nil {
		tab.FindPrev()
	}
}

// menuFind is the action menu entry point. Behaves identically to the
// Esc-f leader — opens the bar against the active tab.
func (a *App) menuFind() {
	a.closeMenu()
	a.openFind()
}

// findAgain is the Esc-g leader entry point. If the active tab has a
// prior query, jump to its next match without re-opening the bar; if it
// has no prior query, fall back to opening the bar so the user has
// somewhere to type.
func (a *App) findAgain() {
	tab := a.activeTabPtr()
	if tab == nil || tab.IsImage() {
		return
	}
	if tab.FindQuery == "" {
		a.openFind()
		return
	}
	tab.FindNext()
}

// hasFindable reports whether the active tab is a text tab — used to
// gray out the menu row on image tabs / no-tab states.
func (a *App) hasFindable() bool {
	t := a.activeTabPtr()
	return t != nil && !t.IsImage()
}

// findBarRect returns the on-screen rectangle of the find bar. Always
// the row directly above the status bar; height is findBarHeight.
// Caller is expected to check a.findOpen before drawing.
func (a *App) findBarRect() (x, y, w, h int) {
	sw := a.sidebarW()
	return sw, a.height - 1 - findBarHeight, a.width - sw, findBarHeight
}

// handleFindKey dispatches a keystroke while the find bar is focused.
// Behavior:
//
//	Esc                     close the bar
//	Enter                   jump to the next match
//	Shift+Enter             jump to the previous match
//	Backspace / Delete      edit the input (live re-search)
//	Left / Right / Home/End cursor movement inside the input
//	printable rune          insert into the input (live re-search)
//
// Anything else is dropped on the floor — the find bar owns the keyboard
// while it's open.
func (a *App) handleFindKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeFind()
	case tcell.KeyEnter:
		if ev.Modifiers()&tcell.ModShift != 0 {
			a.findPrev()
		} else {
			a.findNext()
		}
	case tcell.KeyLeft:
		if a.findCursor > 0 {
			a.findCursor--
		}
	case tcell.KeyRight:
		if a.findCursor < len(a.findValue) {
			a.findCursor++
		}
	case tcell.KeyHome:
		a.findCursor = 0
	case tcell.KeyEnd:
		a.findCursor = len(a.findValue)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.findCursor > 0 {
			a.findValue = append(a.findValue[:a.findCursor-1], a.findValue[a.findCursor:]...)
			a.findCursor--
			a.findApplyQuery()
		}
	case tcell.KeyDelete:
		if a.findCursor < len(a.findValue) {
			a.findValue = append(a.findValue[:a.findCursor], a.findValue[a.findCursor+1:]...)
			a.findApplyQuery()
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(a.findValue)+1)
		next = append(next, a.findValue[:a.findCursor]...)
		next = append(next, r)
		next = append(next, a.findValue[a.findCursor:]...)
		a.findValue = next
		a.findCursor++
		a.findApplyQuery()
	}
}

// drawFindBar renders the 1-row find bar at the bottom of the editor
// area. Layout (left to right):
//
//	" Find: <input>                       3 of 12   Enter: next · Esc: close "
//
// The hint on the right is dropped first when the window is too narrow
// to fit it; the match counter is dropped next; the input itself always
// stays visible because that's the whole point of the bar.
func (a *App) drawFindBar() {
	if !a.findOpen {
		return
	}
	bx, by, bw, _ := a.findBarRect()

	bg := a.theme.LineHL
	barStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	labelStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	emptyStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Error).Bold(true)

	// Clear the row.
	for cx := bx; cx < bx+bw; cx++ {
		a.screen.SetContent(cx, by, ' ', nil, barStyle)
	}

	// "Find:" label.
	label := " Find: "
	drawAt(a.screen, bx, by, label, labelStyle)
	inputStart := bx + runeLen(label)

	// Right side: counter + hint, drawn first so we can clip the input
	// against them on a narrow window.
	hint := " Enter: next · Shift+Enter: prev · Esc: close "
	counter := a.findCounterText()
	rightPadding := 1
	rightTextStart := bx + bw

	if bw > runeLen(label)+runeLen(hint)+10 {
		rightTextStart -= runeLen(hint) + rightPadding
		drawAt(a.screen, rightTextStart, by, hint, mutedStyle)
	}
	if counter != "" && bw > runeLen(label)+runeLen(counter)+4 {
		// Only draw the counter when there's room; right-align before
		// the hint (or against the bar's right edge if the hint was
		// dropped).
		rightTextStart -= runeLen(counter) + 2
		// Color the counter red when the query has no matches so the
		// user gets immediate negative feedback without having to read
		// the digits.
		style := mutedStyle
		if a.findHasNoMatches() {
			style = emptyStyle
		}
		drawAt(a.screen, rightTextStart, by, counter, style)
	}

	// Input field — render the value with horizontal scroll so a long
	// query keeps the cursor visible.
	inputEnd := rightTextStart - 1
	if inputEnd <= inputStart {
		inputEnd = bx + bw - 1
	}
	inputWidth := inputEnd - inputStart
	if inputWidth < 1 {
		inputWidth = 1
	}
	a.adjustFindScroll(inputWidth)
	for i := 0; i < inputWidth; i++ {
		idx := a.findScroll + i
		if idx >= len(a.findValue) {
			break
		}
		a.screen.SetContent(inputStart+i, by, a.findValue[idx], nil, barStyle)
	}

	// Place the screen cursor at the input position so the user sees a
	// blinking caret while typing in the bar.
	caret := inputStart + (a.findCursor - a.findScroll)
	if caret >= inputStart && caret <= inputEnd {
		a.screen.ShowCursor(caret, by)
	}
}

// findCounterText renders the "N of M" indicator. Returns "" when there
// is no query so the renderer can skip drawing the field entirely.
func (a *App) findCounterText() string {
	if len(a.findValue) == 0 {
		return ""
	}
	tab := a.activeTabPtr()
	if tab == nil {
		return ""
	}
	if len(tab.FindMatches) == 0 {
		return "no results"
	}
	return fmt.Sprintf("%d of %d", tab.FindIndex+1, len(tab.FindMatches))
}

// findHasNoMatches reports whether the user has typed a query that
// returned zero hits, so the counter can flip color.
func (a *App) findHasNoMatches() bool {
	if len(a.findValue) == 0 {
		return false
	}
	tab := a.activeTabPtr()
	return tab != nil && len(tab.FindMatches) == 0
}

// adjustFindScroll keeps the input cursor inside the visible window of
// the input field, sliding the scroll offset left or right as needed.
// Mirrors the prompt modal's adjustPromptScroll.
func (a *App) adjustFindScroll(width int) {
	if width <= 0 {
		a.findScroll = 0
		return
	}
	if a.findCursor < a.findScroll {
		a.findScroll = a.findCursor
	}
	if a.findCursor >= a.findScroll+width {
		a.findScroll = a.findCursor - width + 1
	}
	if a.findScroll < 0 {
		a.findScroll = 0
	}
}
