// =============================================================================
// File: internal/app/sidebarsearch.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-12
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/finder"
	"github.com/cloudmanic/spice-edit/internal/theme"
)

// Sidebar "Find in files" panel plus the Files/Find header tab strip.
//
// This is a second, persistent surface for the same grep the centered
// "Find in files" modal (searchfiles.go) runs. The sidebar keeps its own
// query/result state so opening the Esc-F modal doesn't clobber it, and
// vice-versa. Results are grouped into per-file accordions: a file header
// toggles its match list, and each match row jumps to that line on click.

// sidebarSearchGroup is one file's worth of matches, in path order.
type sidebarSearchGroup struct {
	Path      string
	Matches   []finder.ContentMatch
	Collapsed bool
}

// sidebarSearchRow is one rendered row in the results area, used for click
// hit-testing (same idea as filetree's visible-row list).
type sidebarSearchRow struct {
	kind       string // "header" | "match"
	path       string
	count      int // matches in this file, for header rows
	collapsed  bool
	match      finder.ContentMatch
	matchIndex int // index into sidebarSearchVisibleMatches, for match rows
}

// sidebarSearchResultsEvent is posted by the background grep goroutine when
// a sidebar query finishes. gen drops stale results, mirroring the modal.
type sidebarSearchResultsEvent struct {
	when    time.Time
	gen     int
	results []finder.ContentMatch
}

// When satisfies the tcell.Event interface.
func (e *sidebarSearchResultsEvent) When() time.Time { return e.when }

// switchSidebarTab moves between the Files and Find-in-files sidebar tabs.
// Single-file mode has no tree, so the sidebar (and thus the tab) can't
// exist; the call is a no-op there.
func (a *App) switchSidebarTab(tab string) {
	if a.tree == nil {
		return
	}
	a.sidebarTab = tab
	if tab == "search" {
		a.activateSidebarSearch()
	} else {
		a.sidebarSearchFocused = false
	}
}

// activateSidebarSearch focuses the input and makes sure the finder index
// is being built so an immediate keystroke doesn't sit on "indexing…".
func (a *App) activateSidebarSearch() {
	a.sidebarSearchFocused = true
	a.sidebarSearchCursor = len(a.sidebarSearchQuery)
	a.sidebarSearchScroll = 0
	scr := a.screen
	if a.finder != nil && a.finder.State() != finder.StateReady {
		a.finder.Rebuild(func() {
			_ = scr.PostEvent(&finderRebuiltEvent{when: time.Now()})
		})
	}
}

// drawSidebarTabs paints the two-tab header at the top of the sidebar. It
// is drawn over the file tree's own " EXPLORER" title row, so the title is
// hidden without touching the filetree package.
func (a *App) drawSidebarTabs() {
	sx, sy, sw, _ := a.sidebarRect()
	half := sw / 2
	for i := 0; i < sw; i++ {
		active := (i < half && a.sidebarTab != "search") || (i >= half && a.sidebarTab == "search")
		bg := a.theme.SidebarBG
		fg := a.theme.Muted
		if active {
			bg = a.theme.BG
			fg = a.theme.Accent
		}
		st := tcell.StyleDefault.Background(bg).Foreground(fg)
		if active {
			st = st.Bold(true)
		}
		a.screen.SetContent(sx+i, sy, ' ', nil, st)
	}
	drawCenteredTab(a.screen, sx, sy, half, "Files", a.theme, a.sidebarTab != "search")
	drawCenteredTab(a.screen, sx+half, sy, sw-half, "Find in files", a.theme, a.sidebarTab == "search")
}

// drawCenteredTab draws one header-tab label, centered and truncated.
func drawCenteredTab(scr tcell.Screen, x, y, w int, label string, th theme.Theme, active bool) {
	runes := []rune(label)
	if len(runes) > w {
		runes = runes[:w]
	}
	pad := (w - len(runes)) / 2
	bg := th.SidebarBG
	fg := th.Muted
	if active {
		bg = th.BG
		fg = th.Accent
	}
	st := tcell.StyleDefault.Background(bg).Foreground(fg)
	if active {
		st = st.Bold(true)
	}
	for i, r := range runes {
		scr.SetContent(x+pad+i, y, r, nil, st)
	}
}

// drawSidebarSearch fills the sidebar body (rows 1..h) with the input row
// and the accordion results. Row 0 is the tab strip, drawn separately.
func (a *App) drawSidebarSearch() {
	sx, sy, sw, sh := a.sidebarRect()
	bg := a.theme.SidebarBG
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	for cy := sy + 1; cy < sy+sh; cy++ {
		for cx := sx; cx < sx+sw; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}
	a.drawSidebarSearchInput(sx, sy+1, sw)
	a.drawSidebarSearchResults(sx, sy+2, sw, sh-2)
}

// drawSidebarSearchInput renders the single-line query field with a caret
// and a status tail (count / searching / indexing / no results).
func (a *App) drawSidebarSearchInput(x, y, w int) {
	inputStyle := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(a.theme.Text)
	a.screen.SetContent(x, y, '›', nil, inputStyle.Foreground(a.theme.Accent))

	fieldStart := x + 1
	fieldEnd := x + w - 1
	fieldWidth := fieldEnd - fieldStart
	a.adjustSidebarSearchScroll(fieldWidth)
	for i := 0; i < fieldWidth; i++ {
		idx := a.sidebarSearchScroll + i
		if idx >= len(a.sidebarSearchQuery) {
			break
		}
		a.screen.SetContent(fieldStart+i, y, a.sidebarSearchQuery[idx], nil, inputStyle)
	}

	caret := fieldStart + (a.sidebarSearchCursor - a.sidebarSearchScroll)
	if a.sidebarSearchFocused && caret >= fieldStart && caret <= fieldEnd {
		a.screen.ShowCursor(caret, y)
	}

	tail := a.sidebarSearchTail()
	if tail != "" && runeLen(tail) < w-1 {
		drawAt(a.screen, x+w-runeLen(tail)-1, y, tail, inputStyle.Foreground(a.theme.Muted))
	}
}

// sidebarSearchTail returns the status string for the input row's right edge.
func (a *App) sidebarSearchTail() string {
	state := finder.StateIdle
	if a.finder != nil {
		state = a.finder.State()
	}
	switch state {
	case finder.StateBuilding, finder.StateIdle:
		return "indexing…"
	case finder.StateErrored:
		return "index err"
	}
	if len(a.sidebarSearchQuery) == 0 {
		return ""
	}
	if !a.sidebarSearchDone {
		return "searching…"
	}
	if len(a.sidebarSearchResults) == 0 {
		return "no results"
	}
	n := len(a.sidebarSearchResults)
	if n >= searchLimit {
		return itoa(n) + "+"
	}
	return itoa(n)
}

// adjustSidebarSearchScroll keeps the caret visible inside the input field.
func (a *App) adjustSidebarSearchScroll(width int) {
	if width <= 0 {
		a.sidebarSearchScroll = 0
		return
	}
	if a.sidebarSearchCursor < a.sidebarSearchScroll {
		a.sidebarSearchScroll = a.sidebarSearchCursor
	}
	if a.sidebarSearchCursor-a.sidebarSearchScroll >= width {
		a.sidebarSearchScroll = a.sidebarSearchCursor - width + 1
	}
	if a.sidebarSearchScroll < 0 {
		a.sidebarSearchScroll = 0
	}
}

// sidebarSearchGroups groups the flat (path,line)-ordered results into
// per-file accordion groups, folding in the per-file collapse state.
func (a *App) sidebarSearchGroups() []sidebarSearchGroup {
	var groups []sidebarSearchGroup
	var cur *sidebarSearchGroup
	for _, m := range a.sidebarSearchResults {
		if cur == nil || cur.Path != m.Path {
			groups = append(groups, sidebarSearchGroup{Path: m.Path, Collapsed: a.sidebarSearchCollapsed[m.Path]})
			cur = &groups[len(groups)-1]
		}
		cur.Matches = append(cur.Matches, m)
	}
	return groups
}

// drawSidebarSearchResults renders the accordion. It also rebuilds the
// transient row/match lists used by click hit-testing and keyboard nav.
func (a *App) drawSidebarSearchResults(x, y, w, h int) {
	groups := a.sidebarSearchGroups()
	a.sidebarSearchRows = a.sidebarSearchRows[:0]
	a.sidebarSearchVisibleMatches = a.sidebarSearchVisibleMatches[:0]
	for _, g := range groups {
		a.sidebarSearchRows = append(a.sidebarSearchRows, sidebarSearchRow{
			kind:      "header",
			path:      g.Path,
			count:     len(g.Matches),
			collapsed: g.Collapsed,
		})
		if g.Collapsed {
			continue
		}
		for _, m := range g.Matches {
			a.sidebarSearchRows = append(a.sidebarSearchRows, sidebarSearchRow{
				kind:       "match",
				path:       g.Path,
				match:      m,
				matchIndex: len(a.sidebarSearchVisibleMatches),
			})
			a.sidebarSearchVisibleMatches = append(a.sidebarSearchVisibleMatches, m)
		}
	}

	// Clamp vertical scroll to the current row count.
	if a.sidebarSearchViewTop < 0 {
		a.sidebarSearchViewTop = 0
	}
	total := len(a.sidebarSearchRows)
	if total > h && a.sidebarSearchViewTop > total-h {
		a.sidebarSearchViewTop = total - h
	}
	if total <= h {
		a.sidebarSearchViewTop = 0
	}

	bg := a.theme.SidebarBG
	for r := 0; r < h; r++ {
		idx := a.sidebarSearchViewTop + r
		ry := y + r
		if idx >= total {
			fillRect(a.screen, x, ry, w, 1, tcell.StyleDefault.Background(bg))
			continue
		}
		row := a.sidebarSearchRows[idx]
		if row.kind == "header" {
			a.drawSidebarSearchHeader(x, ry, w, row)
		} else {
			a.drawSidebarSearchMatchRow(x, ry, w, row.match, row.matchIndex == a.sidebarSearchSelected)
		}
	}
}

// drawSidebarSearchHeader paints one file header with a chevron and count.
func (a *App) drawSidebarSearchHeader(x, y, w int, row sidebarSearchRow) {
	st := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(a.theme.FolderColor).Bold(true)
	fillRect(a.screen, x, y, w, 1, st)
	chev := "▾"
	if row.collapsed {
		chev = "▸"
	}
	label := chev + " " + filepath.Base(row.path) + " (" + itoa(row.count) + ")"
	drawAt(a.screen, x+1, y, trimRunes(label, w-1), st)
}

// drawSidebarSearchMatchRow paints one match: a line number then the source
// line with the matched run highlighted. Selected rows invert to editor BG.
func (a *App) drawSidebarSearchMatchRow(x, y, w int, m finder.ContentMatch, selected bool) {
	bg := a.theme.SidebarBG
	if selected {
		bg = a.theme.BG
	}
	rowStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	fillRect(a.screen, x, y, w, 1, rowStyle)

	lineStr := itoa(m.Line + 1)
	for len(lineStr) < 4 {
		lineStr = " " + lineStr
	}
	col := 1
	for _, r := range lineStr {
		if col >= w-1 {
			return
		}
		a.screen.SetContent(x+col, y, r, nil, rowStyle.Foreground(a.theme.Muted))
		col++
	}
	if col >= w-1 {
		return
	}
	a.screen.SetContent(x+col, y, ' ', nil, rowStyle)
	col++

	preview := []rune(m.Preview)
	trimmed := 0
	for trimmed < len(preview) && (preview[trimmed] == ' ' || preview[trimmed] == '\t') {
		trimmed++
	}
	preview = preview[trimmed:]
	hitStart := m.Col - trimmed
	hitEnd := hitStart + m.Width
	hitStyle := rowStyle.Foreground(a.theme.FindCurrent).Bold(true)

	for i := 0; i < len(preview) && col < w-1; i++ {
		st := rowStyle
		if i >= hitStart && i < hitEnd {
			st = hitStyle
		}
		a.screen.SetContent(x+col, y, preview[i], nil, st)
		col++
	}
}

// sidebarSearchClick routes a click inside the sidebar body while the
// search tab is active: input row focuses the field, header rows toggle
// collapse, match rows jump to the match.
func (a *App) sidebarSearchClick(x, y int) {
	sx, sy, _, _ := a.sidebarRect()
	if y == sy+1 {
		a.sidebarSearchFocused = true
		pos := x - (sx + 1)
		if pos < 0 {
			pos = 0
		}
		if pos > len(a.sidebarSearchQuery) {
			pos = len(a.sidebarSearchQuery)
		}
		a.sidebarSearchCursor = pos
		return
	}
	if y >= sy+2 {
		rel := y - (sy + 2)
		idx := a.sidebarSearchViewTop + rel
		if idx < 0 || idx >= len(a.sidebarSearchRows) {
			return
		}
		row := a.sidebarSearchRows[idx]
		if row.kind == "header" {
			a.toggleSidebarSearchCollapse(row.path)
			return
		}
		a.sidebarSearchSelected = row.matchIndex
		a.openSidebarSearchMatch(row.match)
	}
}

// toggleSidebarSearchCollapse flips a file's accordion state and resets the
// selected match (its flat index is no longer meaningful after reflow).
func (a *App) toggleSidebarSearchCollapse(path string) {
	if a.sidebarSearchCollapsed == nil {
		a.sidebarSearchCollapsed = map[string]bool{}
	}
	a.sidebarSearchCollapsed[path] = !a.sidebarSearchCollapsed[path]
	a.sidebarSearchSelected = 0
}

// sidebarRunSearch launches a background grep for the current query.
func (a *App) sidebarRunSearch() {
	a.sidebarSearchGen++
	gen := a.sidebarSearchGen
	query := string(a.sidebarSearchQuery)
	a.sidebarSearchSelected = 0
	a.sidebarSearchViewTop = 0
	if query == "" || a.finder == nil {
		a.sidebarSearchResults = nil
		a.sidebarSearchDone = false
		a.sidebarSearchCollapsed = nil
		return
	}
	a.sidebarSearchDone = false
	scr := a.screen
	f := a.finder
	go func() {
		results := f.SearchContent(query, searchLimit)
		_ = scr.PostEvent(&sidebarSearchResultsEvent{when: time.Now(), gen: gen, results: results})
	}()
}

// applySidebarSearchResults installs the payload when it still matches the
// live query generation and the search tab is still active.
func (a *App) applySidebarSearchResults(e *sidebarSearchResultsEvent) {
	if a.sidebarTab != "search" || e.gen != a.sidebarSearchGen {
		return
	}
	a.sidebarSearchResults = e.results
	a.sidebarSearchDone = true
	a.sidebarSearchSelected = 0
	a.sidebarSearchViewTop = 0
	a.sidebarSearchCollapsed = nil
}

// handleSidebarSearchKey routes keyboard input while the search input is
// focused. Esc returns to the Files tab; everything else mirrors the modal.
func (a *App) handleSidebarSearchKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.switchSidebarTab("files")
	case tcell.KeyEnter:
		a.openSidebarSearchSelected()
	case tcell.KeyUp:
		if a.sidebarSearchSelected > 0 {
			a.sidebarSearchSelected--
			a.adjustSidebarSearchView()
		}
	case tcell.KeyDown:
		if a.sidebarSearchSelected < len(a.sidebarSearchVisibleMatches)-1 {
			a.sidebarSearchSelected++
			a.adjustSidebarSearchView()
		}
	case tcell.KeyLeft:
		if a.sidebarSearchCursor > 0 {
			a.sidebarSearchCursor--
		}
	case tcell.KeyRight:
		if a.sidebarSearchCursor < len(a.sidebarSearchQuery) {
			a.sidebarSearchCursor++
		}
	case tcell.KeyHome:
		a.sidebarSearchCursor = 0
	case tcell.KeyEnd:
		a.sidebarSearchCursor = len(a.sidebarSearchQuery)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.sidebarSearchCursor > 0 {
			a.sidebarSearchQuery = append(a.sidebarSearchQuery[:a.sidebarSearchCursor-1], a.sidebarSearchQuery[a.sidebarSearchCursor:]...)
			a.sidebarSearchCursor--
			a.sidebarRunSearch()
		}
	case tcell.KeyDelete:
		if a.sidebarSearchCursor < len(a.sidebarSearchQuery) {
			a.sidebarSearchQuery = append(a.sidebarSearchQuery[:a.sidebarSearchCursor], a.sidebarSearchQuery[a.sidebarSearchCursor+1:]...)
			a.sidebarRunSearch()
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(a.sidebarSearchQuery)+1)
		next = append(next, a.sidebarSearchQuery[:a.sidebarSearchCursor]...)
		next = append(next, r)
		next = append(next, a.sidebarSearchQuery[a.sidebarSearchCursor:]...)
		a.sidebarSearchQuery = next
		a.sidebarSearchCursor++
		a.sidebarRunSearch()
	}
}

// openSidebarSearchSelected jumps to the selected visible match.
func (a *App) openSidebarSearchSelected() {
	if a.sidebarSearchSelected < 0 || a.sidebarSearchSelected >= len(a.sidebarSearchVisibleMatches) {
		return
	}
	a.openSidebarSearchMatch(a.sidebarSearchVisibleMatches[a.sidebarSearchSelected])
}

// openSidebarSearchMatch opens the file, drops the cursor on the match, and
// returns focus to the editor while leaving the search tab visible.
func (a *App) openSidebarSearchMatch(m finder.ContentMatch) {
	abs := filepath.Join(a.rootDir, filepath.FromSlash(m.Path))
	a.openFile(abs)
	tab := a.activeTabPtr()
	if tab != nil {
		tab.MoveCursorTo(editor.Position{Line: m.Line, Col: m.Col}, false)
		_, _, ew, eh := a.editorRect()
		tab.EnsureVisible(ew, eh)
	}
	a.sidebarSearchFocused = false
}

// adjustSidebarSearchView slides the vertical scroll so the selected match
// stays visible in the results area.
func (a *App) adjustSidebarSearchView() {
	_, _, _, sh := a.sidebarRect()
	viewH := sh - 2
	if viewH <= 0 {
		a.sidebarSearchViewTop = 0
		return
	}
	row := a.sidebarSearchMatchRowIndex(a.sidebarSearchSelected)
	if row < 0 {
		return
	}
	if row < a.sidebarSearchViewTop {
		a.sidebarSearchViewTop = row
	}
	if row >= a.sidebarSearchViewTop+viewH {
		a.sidebarSearchViewTop = row - viewH + 1
	}
	if a.sidebarSearchViewTop < 0 {
		a.sidebarSearchViewTop = 0
	}
}

// sidebarSearchMatchRowIndex returns the rendered row index of a visible
// match, or -1 when the match isn't present.
func (a *App) sidebarSearchMatchRowIndex(matchIndex int) int {
	for i, r := range a.sidebarSearchRows {
		if r.kind == "match" && r.matchIndex == matchIndex {
			return i
		}
	}
	return -1
}
