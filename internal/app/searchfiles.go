// =============================================================================
// File: internal/app/searchfiles.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-06-21
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

// Project-wide content search ("Find in files") — the VS Code
// Ctrl+Shift+F gesture. A centered modal with a search input on top and a
// scrollable list of "path:line  preview" match rows below. Type a word to
// grep every file the project index knows about, ↑/↓ to move through hits,
// Enter to jump straight to the match, Esc to dismiss.
//
// This mirrors finder.go (the file finder) almost beat-for-beat — same
// modal shape, same input editing, same async-index handling — but the
// results are line-level content matches instead of file paths, and the
// grep runs on a background goroutine (it touches the filesystem, so it
// can't block the UI thread on a large repo). A generation counter drops
// stale results when the user keeps typing.

import (
	"path/filepath"
	"time"

	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/gdamore/tcell/v2"
)

const (
	// searchModalMaxWidth caps the modal width. Content matches need more
	// room than bare file paths — the preview snippet eats horizontal
	// space — so this runs wider than the file finder's 80.
	searchModalMaxWidth = 100
	// searchResultsVisible is how many match rows we render at once.
	searchResultsVisible = 14
	// searchLimit caps how many matches the grep collects. Past this the
	// user should refine the query rather than scroll thousands of rows.
	searchLimit = 500
)

// searchResultsEvent is posted by the background grep goroutine when a
// query finishes. gen guards against stale results: the main loop only
// applies the payload when its gen still matches the live query, so a
// slow search for an old query can't clobber a newer one.
type searchResultsEvent struct {
	when    time.Time
	gen     int
	results []finder.ContentMatch
}

// When satisfies the tcell.Event interface.
func (e *searchResultsEvent) When() time.Time { return e.when }

// openSearchFiles shows the project-wide content search modal. Like the
// file finder it's a no-op in single-file mode (no project index) and it
// kicks a background index rebuild so freshly-changed files are searched.
func (a *App) openSearchFiles() {
	if a.tree == nil {
		a.flash("Find in files isn't available in single-file mode")
		return
	}
	a.closeAllModals()
	a.searchOpen = true
	a.searchQuery = nil
	a.searchCursor = 0
	a.searchScroll = 0
	a.searchSelected = 0
	a.searchViewTop = 0
	a.searchResults = nil
	a.searchDone = false
	scr := a.screen
	if a.finder != nil && a.finder.State() != finder.StateReady {
		a.finder.Rebuild(func() {
			_ = scr.PostEvent(&finderRebuiltEvent{when: time.Now()})
		})
	}
}

// closeSearchFiles dismisses the modal and clears its transient state.
// Bumping searchGen guarantees any in-flight grep goroutine's result is
// ignored when it eventually posts.
func (a *App) closeSearchFiles() {
	a.searchOpen = false
	a.searchQuery = nil
	a.searchCursor = 0
	a.searchScroll = 0
	a.searchSelected = 0
	a.searchViewTop = 0
	a.searchResults = nil
	a.searchDone = false
	a.searchGen++
}

// menuSearchFiles is the ≡ menu entry point. Sits next to menuFindFile in
// the Search group — same vocabulary, different scope (contents vs paths).
func (a *App) menuSearchFiles() {
	a.closeMenu()
	a.openSearchFiles()
}

// hasSearchFiles is the menu predicate: available whenever the project
// finder is wired (i.e. not single-file mode).
func (a *App) hasSearchFiles() bool {
	return a.finder != nil
}

// runSearch kicks off a background grep for the current query. It bumps
// the generation counter first so any earlier in-flight search is dropped
// when it returns, then spawns a goroutine that greps and posts the
// results back to the event loop. An empty query clears results without
// touching the filesystem.
func (a *App) runSearch() {
	a.searchGen++
	gen := a.searchGen
	query := string(a.searchQuery)
	a.searchSelected = 0
	a.searchViewTop = 0
	if query == "" || a.finder == nil {
		a.searchResults = nil
		a.searchDone = false
		return
	}
	a.searchDone = false
	scr := a.screen
	f := a.finder
	go func() {
		results := f.SearchContent(query, searchLimit)
		_ = scr.PostEvent(&searchResultsEvent{when: time.Now(), gen: gen, results: results})
	}()
}

// applySearchResults installs the payload of a searchResultsEvent when it
// still matches the live query. Called from the main event loop.
func (a *App) applySearchResults(e *searchResultsEvent) {
	if !a.searchOpen || e.gen != a.searchGen {
		return
	}
	a.searchResults = e.results
	a.searchDone = true
	a.searchSelected = 0
	a.searchViewTop = 0
}

// handleSearchKey routes keyboard input while the modal is open. Text
// editing mirrors the file finder; the search-specific bits are result
// navigation and Enter-to-jump.
func (a *App) handleSearchKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeSearchFiles()
	case tcell.KeyEnter:
		a.openSelectedSearchResult()
	case tcell.KeyUp:
		if a.searchSelected > 0 {
			a.searchSelected--
			a.adjustSearchView()
		}
	case tcell.KeyDown:
		if a.searchSelected < len(a.searchResults)-1 {
			a.searchSelected++
			a.adjustSearchView()
		}
	case tcell.KeyLeft:
		if a.searchCursor > 0 {
			a.searchCursor--
		}
	case tcell.KeyRight:
		if a.searchCursor < len(a.searchQuery) {
			a.searchCursor++
		}
	case tcell.KeyHome:
		a.searchCursor = 0
	case tcell.KeyEnd:
		a.searchCursor = len(a.searchQuery)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.searchCursor > 0 {
			a.searchQuery = append(a.searchQuery[:a.searchCursor-1], a.searchQuery[a.searchCursor:]...)
			a.searchCursor--
			a.runSearch()
		}
	case tcell.KeyDelete:
		if a.searchCursor < len(a.searchQuery) {
			a.searchQuery = append(a.searchQuery[:a.searchCursor], a.searchQuery[a.searchCursor+1:]...)
			a.runSearch()
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(a.searchQuery)+1)
		next = append(next, a.searchQuery[:a.searchCursor]...)
		next = append(next, r)
		next = append(next, a.searchQuery[a.searchCursor:]...)
		a.searchQuery = next
		a.searchCursor++
		a.runSearch()
	}
}

// handleSearchMouse handles mouse input while the modal is open. Hover
// highlights the row under the cursor; click jumps to it; the wheel
// scrolls the result list; a click outside dismisses.
func (a *App) handleSearchMouse(x, y int, btn tcell.ButtonMask) {
	mx, my, mw, mh := a.searchModalRect()
	rowsStart := my + 4
	rowsCap := a.searchVisibleRows()

	if btn&tcell.WheelUp != 0 {
		if a.searchViewTop > 0 {
			a.searchViewTop--
		}
		return
	}
	if btn&tcell.WheelDown != 0 {
		if a.searchViewTop < len(a.searchResults)-rowsCap {
			a.searchViewTop++
		}
		return
	}

	row := y - rowsStart
	if row >= 0 && row < rowsCap && x >= mx && x < mx+mw {
		idx := a.searchViewTop + row
		if idx < len(a.searchResults) {
			a.searchSelected = idx
		}
	}
	if btn&tcell.Button1 == 0 {
		return
	}
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.closeSearchFiles()
		return
	}
	if row >= 0 && row < rowsCap {
		idx := a.searchViewTop + row
		if idx < len(a.searchResults) {
			a.searchSelected = idx
			a.openSelectedSearchResult()
		}
	}
}

// openSelectedSearchResult opens the file for the selected match, drops
// the cursor onto the match, scrolls it into view, and closes the modal.
func (a *App) openSelectedSearchResult() {
	if a.searchSelected < 0 || a.searchSelected >= len(a.searchResults) {
		return
	}
	m := a.searchResults[a.searchSelected]
	a.closeSearchFiles()
	abs := filepath.Join(a.rootDir, filepath.FromSlash(m.Path))
	a.openFile(abs)
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	tab.MoveCursorTo(editor.Position{Line: m.Line, Col: m.Col}, false)
	_, _, ew, eh := a.editorRect()
	tab.EnsureVisible(ew, eh)
}

// searchVisibleRows returns how many result rows the modal can show given
// the current terminal height, capped at searchResultsVisible.
func (a *App) searchVisibleRows() int {
	_, _, _, mh := a.searchModalRect()
	rowsCap := mh - 5 // borders + title + divider + input
	if rowsCap > searchResultsVisible {
		rowsCap = searchResultsVisible
	}
	if rowsCap < 0 {
		rowsCap = 0
	}
	return rowsCap
}

// adjustSearchView slides the vertical scroll offset so the selected row
// stays inside the visible window.
func (a *App) adjustSearchView() {
	rows := a.searchVisibleRows()
	if rows <= 0 {
		a.searchViewTop = 0
		return
	}
	if a.searchSelected < a.searchViewTop {
		a.searchViewTop = a.searchSelected
	}
	if a.searchSelected >= a.searchViewTop+rows {
		a.searchViewTop = a.searchSelected - rows + 1
	}
	if a.searchViewTop < 0 {
		a.searchViewTop = 0
	}
}

// searchModalRect returns the on-screen rectangle of the search modal.
func (a *App) searchModalRect() (x, y, w, h int) {
	w = searchModalMaxWidth
	if w > a.width-4 {
		w = a.width - 4
	}
	if w < 30 {
		w = 30
	}
	// Layout: 1 border + 1 title + 1 divider + 1 input + N results
	// + 1 border = N+5 rows.
	h = searchResultsVisible + 5
	if h > a.height-2 {
		h = a.height - 2
	}
	x = (a.width - w) / 2
	y = (a.height - h) / 3
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}

// drawSearch paints the modal: title + Esc hint, input field with a
// match-count tail, then either an "Indexing…" / "Searching…" line or the
// match rows.
func (a *App) drawSearch() {
	mx, my, mw, mh := a.searchModalRect()
	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	hitStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.FindCurrent).Bold(true)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	drawAt(a.screen, mx+1, my+1, " Find in files", titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	// Input row.
	inputBg := a.theme.BG
	inputStyle := tcell.StyleDefault.Background(inputBg).Foreground(a.theme.Text)
	fieldStart := mx + 3
	fieldEnd := mx + mw - 14 // leave room for the count tail
	fieldWidth := fieldEnd - fieldStart
	a.adjustSearchScroll(fieldWidth)
	for cx := fieldStart - 1; cx <= fieldEnd; cx++ {
		a.screen.SetContent(cx, my+3, ' ', nil, inputStyle)
	}
	for i := 0; i < fieldWidth; i++ {
		idx := a.searchScroll + i
		if idx >= len(a.searchQuery) {
			break
		}
		a.screen.SetContent(fieldStart+i, my+3, a.searchQuery[idx], nil, inputStyle)
	}
	caret := fieldStart + (a.searchCursor - a.searchScroll)
	if caret >= fieldStart && caret <= fieldEnd {
		a.screen.ShowCursor(caret, my+3)
	}

	// Count tail — mirrors the finder's status vocabulary.
	tail := a.searchTail()
	drawAt(a.screen, mx+mw-1-runeLen(tail), my+3, tail, mutedStyle)

	// Result rows.
	rowsStart := my + 4
	rowsCap := a.searchVisibleRows()
	for i := 0; i < rowsCap; i++ {
		ry := rowsStart + i
		idx := a.searchViewTop + i
		if idx >= len(a.searchResults) {
			for cx := mx + 1; cx < mx+mw-1; cx++ {
				a.screen.SetContent(cx, ry, ' ', nil, bgStyle)
			}
			continue
		}
		a.drawSearchRow(mx, ry, mw, a.searchResults[idx], idx == a.searchSelected, hitStyle, mutedStyle, bg)
	}
}

// searchTail returns the status string shown at the right of the input:
// index state, a "searching…" spinner-less placeholder, a match count, or
// "no results" when a finished search came back empty.
func (a *App) searchTail() string {
	state := finder.StateIdle
	if a.finder != nil {
		state = a.finder.State()
	}
	switch state {
	case finder.StateBuilding, finder.StateIdle:
		return "indexing… "
	case finder.StateErrored:
		return "index err "
	}
	if len(a.searchQuery) == 0 {
		return ""
	}
	if !a.searchDone {
		return "searching… "
	}
	if len(a.searchResults) == 0 {
		return "no results "
	}
	n := len(a.searchResults)
	if n >= searchLimit {
		return itoa(n) + "+ "
	}
	return itoa(n) + " "
}

// drawSearchRow paints one match line: a dimmed "path:line" location
// prefix, then the trimmed source line with the matched run highlighted.
// The selected row's background flips to the editor BG so it reads as a
// single block, matching the file finder's selection styling.
func (a *App) drawSearchRow(mx, ry, mw int, m finder.ContentMatch, selected bool, hitStyle, mutedStyle tcell.Style, modalBG tcell.Color) {
	rowBG := modalBG
	if selected {
		rowBG = a.theme.BG
	}
	rowStyle := tcell.StyleDefault.Background(rowBG).Foreground(a.theme.Text)
	hitOnRow := hitStyle.Background(rowBG)
	mutedOnRow := mutedStyle.Background(rowBG)

	// Background fill.
	for cx := mx + 1; cx < mx+mw-1; cx++ {
		a.screen.SetContent(cx, ry, ' ', nil, rowStyle)
	}

	startCol := mx + 2
	maxCols := mw - 4

	// Location prefix: "path:line " (line shown 1-based to match editors).
	loc := m.Path + ":" + itoa(m.Line+1) + " "
	locRunes := []rune(loc)
	col := 0
	for ; col < len(locRunes) && col < maxCols; col++ {
		a.screen.SetContent(startCol+col, ry, locRunes[col], nil, mutedOnRow)
	}

	// Preview: trim leading whitespace so the code content starts right
	// after the location, tracking how many runes we dropped so the
	// highlight offset stays correct.
	preview := []rune(m.Preview)
	trimmed := 0
	for trimmed < len(preview) && (preview[trimmed] == ' ' || preview[trimmed] == '\t') {
		trimmed++
	}
	preview = preview[trimmed:]
	hitStart := m.Col - trimmed
	hitEnd := hitStart + m.Width

	for i := 0; i < len(preview) && col < maxCols; i, col = i+1, col+1 {
		st := rowStyle
		if i >= hitStart && i < hitEnd {
			st = hitOnRow
		}
		a.screen.SetContent(startCol+col, ry, preview[i], nil, st)
	}
}

// adjustSearchScroll keeps the input caret visible by sliding searchScroll
// within the input field. Mirrors adjustFinderScroll.
func (a *App) adjustSearchScroll(width int) {
	if width <= 0 {
		a.searchScroll = 0
		return
	}
	if a.searchCursor < a.searchScroll {
		a.searchScroll = a.searchCursor
	}
	if a.searchCursor-a.searchScroll >= width {
		a.searchScroll = a.searchCursor - width + 1
	}
	if a.searchScroll < 0 {
		a.searchScroll = 0
	}
}
