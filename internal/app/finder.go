// =============================================================================
// File: internal/app/finder.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

// Project-wide file finder UI — a centered modal with a search
// input on top and ~10 result rows below. Type to filter, ↑/↓ to
// navigate, Enter to open, Esc to dismiss. Mouse hover highlights;
// click opens.
//
// All the index/scoring lifting lives in internal/finder; this
// file is just the wiring between user input and the buffer/tree
// that already exist.

import (
	"path/filepath"
	"time"

	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/gdamore/tcell/v2"
)

const (
	// finderModalMaxWidth caps the modal so very wide terminals
	// don't get a full-width strip that's awkward to scan. 80 is
	// the comfortable reading width every IDE settles on.
	finderModalMaxWidth = 80
	// finderResultsVisible is how many result rows we render at
	// once. 10 is the floor for "feels useful" without dominating
	// the screen on small terminals.
	finderResultsVisible = 10
	// finderSearchLimit is how deep we look — we ask for one full
	// "page" extra so users can scroll past the initial chunk.
	finderSearchLimit = 50
)

// finderRebuiltEvent is posted by the background indexer goroutine
// when it finishes a rebuild. The main loop reacts by re-running
// the current query so the visible results refresh — the user
// gets to see "Indexing…" replaced with real matches without
// having to type or wait for the next keystroke.
type finderRebuiltEvent struct {
	when time.Time
}

// When satisfies the tcell.Event interface.
func (e *finderRebuiltEvent) When() time.Time { return e.when }

// openFinder shows the project-wide file finder. Triggers a
// background rebuild on every open so external file changes are
// reflected even if the periodic invalidation tick hasn't fired.
// (Building when the index is already StateReady is a no-op
// inside the orchestrator's coalesce gate.)
func (a *App) openFinder() {
	a.closeAllModals()
	a.finderOpen = true
	a.finderQuery = nil
	a.finderCursor = 0
	a.finderSelected = 0
	a.finderResults = nil
	scr := a.screen
	if a.finder != nil {
		// Rebuild only when the cache is genuinely stale. A re-open
		// during a still-warm session shouldn't pay for a refresh.
		if a.finder.State() != finder.StateReady {
			a.finder.Rebuild(func() {
				_ = scr.PostEvent(&finderRebuiltEvent{when: time.Now()})
			})
		}
	}
	a.refreshFinderResults()
}

// closeFinder dismisses the finder modal and clears its transient
// state. The cached index on a.finder stays warm — a future open
// reuses it.
func (a *App) closeFinder() {
	a.finderOpen = false
	a.finderQuery = nil
	a.finderCursor = 0
	a.finderResults = nil
	a.finderSelected = 0
}

// menuFindFile is the ≡ menu entry that opens the finder. Lives
// alongside menuFind (which is the in-file find bar) — they share
// vocabulary but search different scopes.
func (a *App) menuFindFile() {
	a.closeMenu()
	a.openFinder()
}

// hasFinder is the menu predicate. Always true once the finder
// has been wired in App.New — the row stays enabled even before
// the first index lands so the user can pop the modal and watch
// "Indexing…" tick over.
func (a *App) hasFinder() bool {
	return a.finder != nil
}

// refreshFinderResults re-runs the cached query against the
// current index. Called on every keystroke and on the rebuilt
// event so a slow first index doesn't leave the modal showing
// stale "Indexing…" forever.
func (a *App) refreshFinderResults() {
	if a.finder == nil {
		a.finderResults = nil
		return
	}
	a.finderResults = a.finder.Search(string(a.finderQuery), finderSearchLimit)
	if a.finderSelected >= len(a.finderResults) {
		a.finderSelected = len(a.finderResults) - 1
	}
	if a.finderSelected < 0 {
		a.finderSelected = 0
	}
}

// invalidateFinder marks the index stale and kicks off a rebuild.
// Called by the file-tree refresh tick and by every fileops
// mutation (create / rename / delete) so the finder doesn't show
// ghost paths or miss freshly-created files.
func (a *App) invalidateFinder() {
	if a.finder == nil {
		return
	}
	a.finder.Invalidate()
	scr := a.screen
	a.finder.Rebuild(func() {
		_ = scr.PostEvent(&finderRebuiltEvent{when: time.Now()})
	})
}

// handleFinderKey routes keyboard input while the finder modal
// is open. Most of it is text editing (mirrors handlePromptKey);
// the finder-specific bits are arrow-key navigation through
// results and Enter-to-open.
func (a *App) handleFinderKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeFinder()
	case tcell.KeyEnter:
		a.openSelectedFinderResult()
	case tcell.KeyUp:
		if a.finderSelected > 0 {
			a.finderSelected--
		}
	case tcell.KeyDown:
		if a.finderSelected < len(a.finderResults)-1 {
			a.finderSelected++
		}
	case tcell.KeyLeft:
		if a.finderCursor > 0 {
			a.finderCursor--
		}
	case tcell.KeyRight:
		if a.finderCursor < len(a.finderQuery) {
			a.finderCursor++
		}
	case tcell.KeyHome:
		a.finderCursor = 0
	case tcell.KeyEnd:
		a.finderCursor = len(a.finderQuery)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.finderCursor > 0 {
			a.finderQuery = append(a.finderQuery[:a.finderCursor-1], a.finderQuery[a.finderCursor:]...)
			a.finderCursor--
			a.finderSelected = 0
			a.refreshFinderResults()
		}
	case tcell.KeyDelete:
		if a.finderCursor < len(a.finderQuery) {
			a.finderQuery = append(a.finderQuery[:a.finderCursor], a.finderQuery[a.finderCursor+1:]...)
			a.finderSelected = 0
			a.refreshFinderResults()
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(a.finderQuery)+1)
		next = append(next, a.finderQuery[:a.finderCursor]...)
		next = append(next, r)
		next = append(next, a.finderQuery[a.finderCursor:]...)
		a.finderQuery = next
		a.finderCursor++
		a.finderSelected = 0
		a.refreshFinderResults()
	}
}

// handleFinderMouse handles mouse input while the modal is open.
// Hover highlights the row under the cursor; click opens it.
// Click outside the modal dismisses.
func (a *App) handleFinderMouse(x, y int, btn tcell.ButtonMask) {
	mx, my, mw, mh := a.finderModalRect()
	rowsStart := my + 4
	row := y - rowsStart
	if row >= 0 && row < len(a.finderResults) && x >= mx && x < mx+mw {
		// Hover highlight always tracks the mouse — same behaviour
		// the action menu uses, so users can scrub through results
		// without clicking.
		a.finderSelected = row
	}
	if btn&tcell.Button1 == 0 {
		return
	}
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.closeFinder()
		return
	}
	if row >= 0 && row < len(a.finderResults) {
		a.finderSelected = row
		a.openSelectedFinderResult()
	}
}

// openSelectedFinderResult opens the result at finderSelected.
// Closes the modal, then resolves the relative path against the
// project root and opens the file. Silent no-op when the result
// list is empty (e.g. the user mashed Enter on a no-match query).
func (a *App) openSelectedFinderResult() {
	if a.finderSelected < 0 || a.finderSelected >= len(a.finderResults) {
		return
	}
	rel := a.finderResults[a.finderSelected].Path
	a.closeFinder()
	abs := filepath.Join(a.rootDir, rel)
	a.openFile(abs)
}

// finderModalRect returns the on-screen rectangle of the finder
// modal. Sized to fit a healthy result list while leaving margins
// on small terminals.
func (a *App) finderModalRect() (x, y, w, h int) {
	w = finderModalMaxWidth
	if w > a.width-4 {
		w = a.width - 4
	}
	if w < 30 {
		w = 30
	}
	// Layout: 1 border + 1 title + 1 divider + 1 input + N results
	// + 1 status + 1 border = N+6 rows.
	h = finderResultsVisible + 6
	if h > a.height-2 {
		h = a.height - 2
	}
	x = (a.width - w) / 2
	y = (a.height - h) / 3 // anchor in upper third — matches VS Code's feel
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}

// drawFinder paints the modal: title + Esc hint, input field with
// match-count tail, then either a "Indexing…" line or the result
// rows with highlighted match characters.
//
// Layout (relY):
//
//	0     top border
//	1     title — "Find file in project    esc"
//	2     divider
//	3     input          [ query…           42/12345 ]
//	4..N  result rows
//	N+1   bottom border
func (a *App) drawFinder() {
	mx, my, mw, mh := a.finderModalRect()
	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	hitStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.FindCurrent).Bold(true)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	drawAt(a.screen, mx+1, my+1, " Find file", titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	// Input row.
	inputBg := a.theme.BG
	inputStyle := tcell.StyleDefault.Background(inputBg).Foreground(a.theme.Text)
	fieldStart := mx + 3
	fieldEnd := mx + mw - 12 // leave room for the count on the right
	fieldWidth := fieldEnd - fieldStart
	a.adjustFinderScroll(fieldWidth)
	for cx := fieldStart - 1; cx <= fieldEnd; cx++ {
		a.screen.SetContent(cx, my+3, ' ', nil, inputStyle)
	}
	for i := 0; i < fieldWidth; i++ {
		idx := a.finderScroll + i
		if idx >= len(a.finderQuery) {
			break
		}
		a.screen.SetContent(fieldStart+i, my+3, a.finderQuery[idx], nil, inputStyle)
	}
	caret := fieldStart + (a.finderCursor - a.finderScroll)
	if caret >= fieldStart && caret <= fieldEnd {
		a.screen.ShowCursor(caret, my+3)
	}

	// Match count tail.
	state, total, viaGit := finder.StateIdle, 0, false
	if a.finder != nil {
		state, total, viaGit = a.finder.Stats()
	}
	_ = viaGit // reserved for a future "via git" badge — keeping the
	// triple-return so callers don't have to plumb a new method later.
	tail := ""
	switch state {
	case finder.StateBuilding, finder.StateIdle:
		tail = "indexing… "
	case finder.StateErrored:
		tail = "index err "
	case finder.StateReady:
		tail = countLabel(len(a.finderResults), total) + " "
	}
	drawAt(a.screen, mx+mw-1-runeLen(tail), my+3, tail, mutedStyle)

	// Result rows. We paint the visible window (no scrolling support
	// in v1 — we just cap at finderResultsVisible). When the query
	// has more matches than fit, the user can refine.
	rowsStart := my + 4
	rowsCap := mh - 5 // borders + title + divider + input
	if rowsCap > finderResultsVisible {
		rowsCap = finderResultsVisible
	}
	visible := a.finderResults
	if len(visible) > rowsCap {
		visible = visible[:rowsCap]
	}
	for i := 0; i < rowsCap; i++ {
		ry := rowsStart + i
		if i >= len(visible) {
			// Clear unused rows so a previous query's tail doesn't
			// linger when results shrink.
			for cx := mx + 1; cx < mx+mw-1; cx++ {
				a.screen.SetContent(cx, ry, ' ', nil, bgStyle)
			}
			continue
		}
		a.drawFinderRow(mx, ry, mw, visible[i], i == a.finderSelected, hitStyle, mutedStyle, bg)
	}
}

// drawFinderRow paints one result line: the path, with matched
// runes highlighted via hitStyle, and the row background flipped
// to LineHL when it's the selected row. dirname is dimmed so the
// basename pops — same trick the editor's tab bar uses to make
// the file name the visual anchor.
func (a *App) drawFinderRow(mx, ry, mw int, r finder.Result, selected bool, hitStyle, mutedStyle tcell.Style, modalBG tcell.Color) {
	rowBG := modalBG
	rowFG := a.theme.Text
	if selected {
		rowBG = a.theme.BG
		rowFG = a.theme.Text
	}
	rowStyle := tcell.StyleDefault.Background(rowBG).Foreground(rowFG)
	hitOnRow := hitStyle.Background(rowBG)
	mutedOnRow := mutedStyle.Background(rowBG)

	// Background fill so the selected row reads as a single block.
	for cx := mx + 1; cx < mx+mw-1; cx++ {
		a.screen.SetContent(cx, ry, ' ', nil, rowStyle)
	}

	pathRunes := []rune(r.Path)
	// Find the basename split so we can dim the directory part.
	sepIdx := -1
	for i := len(pathRunes) - 1; i >= 0; i-- {
		if pathRunes[i] == '/' {
			sepIdx = i
			break
		}
	}
	matchSet := map[int]bool{}
	for _, m := range r.MatchedIndexes {
		matchSet[m] = true
	}

	startCol := mx + 2
	maxCols := mw - 4
	for i, ch := range pathRunes {
		if i >= maxCols {
			break
		}
		st := rowStyle
		if i <= sepIdx {
			st = mutedOnRow
		}
		if matchSet[i] {
			st = hitOnRow
		}
		a.screen.SetContent(startCol+i, ry, ch, nil, st)
	}
}

// adjustFinderScroll keeps the input cursor visible by sliding
// finderScroll left or right within the input field. Same shape
// as adjustPromptScroll — pulled into its own method so the two
// can evolve independently if either modal grows new behaviours.
func (a *App) adjustFinderScroll(width int) {
	if width <= 0 {
		a.finderScroll = 0
		return
	}
	if a.finderCursor < a.finderScroll {
		a.finderScroll = a.finderCursor
	}
	if a.finderCursor-a.finderScroll >= width {
		a.finderScroll = a.finderCursor - width + 1
	}
	if a.finderScroll < 0 {
		a.finderScroll = 0
	}
}

// countLabel formats the "shown/total" tail. Pulled into its own
// helper so future tweaks (commas, abbreviation past 1k) live in
// one spot. We don't bother showing "shown" when it equals total
// — the count would be redundant and the eye should focus on the
// total.
func countLabel(shown, total int) string {
	if total == 0 {
		return "0"
	}
	if shown == total {
		return itoa(total)
	}
	return itoa(shown) + "/" + itoa(total)
}

// itoa is a tiny non-allocating-ish int→string for the count
// tail. strconv would work fine; keeping a local helper avoids
// pulling strconv into a UI file that otherwise doesn't need it.
func itoa(n int) string {
	if n < 0 {
		return "0"
	}
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
