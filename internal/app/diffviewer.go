// =============================================================================
// File: internal/app/diffviewer.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// diffviewer.go is the "Git changes" modal — a mouse-first browser for
// the repo's uncommitted work. It reuses plumbing that already exists:
//
//   - the dirty-file set comes from the cached gitStatus snapshot that
//     refreshGitStatus stamps onto the App (same source the file tree's
//     dirty highlight reads), so there's no extra `git status` fork per
//     open.
//   - the per-file diff body is `git diff --unified=3 HEAD -- <path>`
//     (loadGitFileDiff), rendered with confirmInfoLineStyle — the same
//     colouring the per-line hunk preview already uses.
//
// The modal has two views, switched by diffViewFile:
//
//   - list view (diffViewFile == ""): the dirty files, one per row, with
//     a status glyph (M/A/D/R) and a path relative to the repo root.
//     ↑/↓ move, Enter (or click) drills into a file's diff.
//   - diff view (diffViewFile set): the file's unified diff, scrollable.
//     Enter opens the file in a tab and drops the cursor on the first
//     changed line; Backspace / Esc returns to the list view.
//
// Esc on the list view closes the modal outright — same convention as
// the finder and search modals.

package app

import (
	"path/filepath"
	"sort"

	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/theme"
	"github.com/gdamore/tcell/v2"
)

const (
	// diffModalMaxWidth caps the modal so very wide terminals don't get
	// a sprawling strip. Matches the file finder's comfortable width.
	diffModalMaxWidth = 80
	// diffRowsVisible is how many list / diff rows render at once. Same
	// floor as the finder — "feels useful" without dominating small
	// terminals.
	diffRowsVisible = 14
)

// diffEntry is one row in the list view: a dirty file's absolute path,
// its path relative to the repo root (for display), and the git change
// kind reported by porcelain.
type diffEntry struct {
	abs  string
	rel  string
	kind filetree.GitChangeKind
}

// openDiffViewer builds the list view from the cached gitStatus snapshot
// and shows the modal. If the project isn't a git repo, or git reported
// no changes, we flash a status message and bail instead of popping an
// empty dialog — the menu predicate (hasDiffViewer) keeps the row dimmed
// in that case too, but the leader-key path can still reach here.
func (a *App) openDiffViewer() {
	if a.gitStatus.Root == "" || !a.gitStatus.IsRepo {
		a.flash("Not a git repository")
		return
	}
	entries := a.buildDiffEntries()
	if len(entries) == 0 {
		a.flash("No uncommitted changes")
		return
	}
	a.closeAllModals()
	a.diffOpen = true
	a.diffEntries = entries
	a.diffSelected = 0
	a.diffViewTop = 0
	a.diffViewFile = ""
	a.diffLines = nil
	a.diffScroll = 0
}

// closeDiffViewer tears down the modal's transient state. The cached
// gitStatus is left alone — it's owned by the tree-refresh tick.
func (a *App) closeDiffViewer() {
	a.diffOpen = false
	a.diffEntries = nil
	a.diffSelected = 0
	a.diffViewTop = 0
	a.diffViewFile = ""
	a.diffLines = nil
	a.diffScroll = 0
}

// menuDiffViewer is the ≡ menu entry point.
func (a *App) menuDiffViewer() {
	a.closeMenu()
	a.openDiffViewer()
}

// hasDiffViewer is the menu predicate: the row is enabled when we're in a
// git repo that currently reports at least one changed file. Using the
// cached snapshot means the menu greys out the instant the user commits
// the last change (on the next refresh tick) without forking git on every
// draw.
func (a *App) hasDiffViewer() bool {
	return a.gitStatus.IsRepo && len(a.gitStatus.DirtyFiles) > 0
}

// buildDiffEntries flattens the cached dirty-file map into a sorted slice
// of display rows. Sorting gives a stable order across the random map
// iteration and a predictable ↑/↓ walk for the user.
func (a *App) buildDiffEntries() []diffEntry {
	root := a.gitStatus.Root
	entries := make([]diffEntry, 0, len(a.gitStatus.DirtyFiles))
	for abs, kind := range a.gitStatus.DirtyFiles {
		rel, ok := relFromRoot(abs, root)
		if !ok {
			rel = filepath.Base(abs)
		}
		entries = append(entries, diffEntry{abs: abs, rel: rel, kind: kind})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	return entries
}

// handleDiffKey routes keyboard input for the modal. In diff view,
// Backspace / Esc pop back to the list rather than closing outright —
// Esc-closes-everything would make it impossible to browse several files
// without re-opening the modal each time. Enter in diff view opens the
// file at the first change.
func (a *App) handleDiffKey(ev *tcell.EventKey) {
	if a.diffViewFile != "" {
		switch ev.Key() {
		case tcell.KeyEsc, tcell.KeyBackspace, tcell.KeyBackspace2:
			a.diffViewFile = ""
			a.diffLines = nil
			a.diffScroll = 0
		case tcell.KeyEnter:
			a.openDiffFileAtChange()
		case tcell.KeyUp:
			a.scrollDiff(-1)
		case tcell.KeyDown:
			a.scrollDiff(1)
		case tcell.KeyPgUp:
			a.scrollDiff(-a.diffVisibleRows())
		case tcell.KeyPgDn:
			a.scrollDiff(a.diffVisibleRows())
		}
		return
	}
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeDiffViewer()
	case tcell.KeyUp:
		a.moveDiffSelection(-1)
	case tcell.KeyDown:
		a.moveDiffSelection(1)
	case tcell.KeyPgUp:
		a.moveDiffSelection(-a.diffVisibleRows())
	case tcell.KeyPgDn:
		a.moveDiffSelection(a.diffVisibleRows())
	case tcell.KeyEnter:
		a.showDiffForSelected()
	}
}

// handleDiffMouse routes mouse input for the modal. Hovering a list row
// selects it; clicking a row drills into its diff (or, in diff view,
// opens the file). Wheel scrolls the visible pane. Clicks outside the
// modal dismiss it — same convention as every other modal.
func (a *App) handleDiffMouse(x, y int, btn tcell.ButtonMask) {
	mx, my, mw, mh := a.diffModalRect()
	if btn&tcell.Button4 != 0 {
		a.scrollDiff(-3)
		return
	}
	if btn&tcell.Button5 != 0 {
		a.scrollDiff(3)
		return
	}
	if btn&tcell.Button1 == 0 {
		// Motion with no button: update hover selection in list view.
		if a.diffViewFile == "" && x >= mx && x < mx+mw && y >= my && y < my+mh {
			row := y - (my + 3)
			if row >= 0 && row < a.diffVisibleRows() {
				idx := a.diffViewTop + row
				if idx < len(a.diffEntries) {
					a.diffSelected = idx
					a.adjustDiffView()
				}
			}
		}
		return
	}
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.closeDiffViewer()
		return
	}
	if a.diffViewFile != "" {
		// Any click inside the diff body opens the file.
		a.openDiffFileAtChange()
		return
	}
	row := y - (my + 3)
	if row >= 0 && row < a.diffVisibleRows() {
		idx := a.diffViewTop + row
		if idx < len(a.diffEntries) {
			a.diffSelected = idx
			a.showDiffForSelected()
		}
	}
}

// moveDiffSelection moves the list cursor by dir rows, clamped to the
// entry count, then keeps the selection inside the visible window.
func (a *App) moveDiffSelection(dir int) {
	n := len(a.diffEntries)
	if n == 0 {
		return
	}
	a.diffSelected += dir
	if a.diffSelected < 0 {
		a.diffSelected = 0
	}
	if a.diffSelected >= n {
		a.diffSelected = n - 1
	}
	a.adjustDiffView()
}

// showDiffForSelected loads the unified diff for the selected file and
// flips the modal into diff view. Empty diff output (e.g. a fully-staged
// file with no worktree delta) is surfaced as a single placeholder line
// so the view never looks blank.
func (a *App) showDiffForSelected() {
	if a.diffSelected < 0 || a.diffSelected >= len(a.diffEntries) {
		return
	}
	e := a.diffEntries[a.diffSelected]
	lines := loadGitFileDiff(a.rootDir, e.abs)
	if len(lines) == 0 {
		lines = []string{"(no worktree changes vs HEAD)"}
	}
	a.diffViewFile = e.abs
	a.diffLines = lines
	a.diffScroll = 0
}

// openDiffFileAtChange opens the file shown in diff view and drops the
// cursor on the first line the diff reports as changed (the new-file
// start of the first hunk). Falls back to line 0 when no hunk header is
// parseable — opening at the top beats not opening at all.
func (a *App) openDiffFileAtChange() {
	path := a.diffViewFile
	if path == "" {
		return
	}
	// Capture the target line before closeDiffViewer wipes diffLines.
	line := firstDiffNewLine(a.diffLines)
	a.closeDiffViewer()
	a.openFile(path)
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	tab.MoveCursorTo(editor.Position{Line: line, Col: 0}, false)
	_, _, ew, eh := a.editorRect()
	tab.EnsureVisible(ew, eh)
}

// scrollDiff advances the diff-view scroll offset by delta, clamped to
// the valid range. A no-op delta still clamps, which is how the draw
// path guarantees the offset is sane before painting.
func (a *App) scrollDiff(delta int) {
	if a.diffViewFile == "" {
		return
	}
	maxScroll := len(a.diffLines) - a.diffVisibleRows()
	if maxScroll < 0 {
		maxScroll = 0
	}
	a.diffScroll += delta
	if a.diffScroll < 0 {
		a.diffScroll = 0
	}
	if a.diffScroll > maxScroll {
		a.diffScroll = maxScroll
	}
}

// adjustDiffView slides the list-view top offset so the selection stays
// on screen. Mirrors the search modal's adjustSearchView.
func (a *App) adjustDiffView() {
	rows := a.diffVisibleRows()
	if rows <= 0 {
		a.diffViewTop = 0
		return
	}
	if a.diffSelected < a.diffViewTop {
		a.diffViewTop = a.diffSelected
	}
	if a.diffSelected >= a.diffViewTop+rows {
		a.diffViewTop = a.diffSelected - rows + 1
	}
	if a.diffViewTop < 0 {
		a.diffViewTop = 0
	}
}

// diffVisibleRows returns how many body rows the modal can show given
// the current terminal height, capped at diffRowsVisible.
func (a *App) diffVisibleRows() int {
	_, _, _, mh := a.diffModalRect()
	rows := mh - 4 // borders + title + divider
	if rows > diffRowsVisible {
		rows = diffRowsVisible
	}
	if rows < 0 {
		rows = 0
	}
	return rows
}

// diffModalRect returns the on-screen rectangle of the modal, centered.
// Same layout budget as the search modal: 1 border + 1 title + 1 divider
// + N body rows + 1 border = N+4 rows.
func (a *App) diffModalRect() (x, y, w, h int) {
	w = diffModalMaxWidth
	if w > a.width-4 {
		w = a.width - 4
	}
	if w < 30 {
		w = 30
	}
	h = diffRowsVisible + 4
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

// drawDiff paints the modal. The title and hint change with the view;
// the body is either the file list or the scrollable diff, both using
// confirmInfoLineStyle so colours match the existing git-diff preview.
func (a *App) drawDiff() {
	mx, my, mw, mh := a.diffModalRect()
	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	title := " Git changes"
	hint := "esc "
	if a.diffViewFile != "" {
		title = " Git diff · " + filepath.Base(a.diffViewFile)
		hint = "⌫ list "
	}
	drawAt(a.screen, mx+1, my+1, title, titleStyle)
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	bodyStart := my + 3
	rowsCap := a.diffVisibleRows()

	if a.diffViewFile == "" {
		// List view.
		for i := 0; i < rowsCap; i++ {
			ry := bodyStart + i
			idx := a.diffViewTop + i
			if idx >= len(a.diffEntries) {
				for cx := mx + 1; cx < mx+mw-1; cx++ {
					a.screen.SetContent(cx, ry, ' ', nil, bgStyle)
				}
				continue
			}
			a.drawDiffListRow(mx, ry, mw, a.diffEntries[idx], idx == a.diffSelected, bg)
		}
		a.screen.HideCursor()
		return
	}

	// Diff view.
	a.scrollDiff(0)
	end := a.diffScroll + rowsCap
	if end > len(a.diffLines) {
		end = len(a.diffLines)
	}
	for i, line := range a.diffLines[a.diffScroll:end] {
		ry := bodyStart + i
		if runeLen(line) > mw-4 {
			line = string([]rune(line)[:mw-4])
		}
		drawAt(a.screen, mx+2, ry, line, confirmInfoLineStyle(a.theme, bg, line))
	}
	// Blank any trailing rows so old content can't bleed through when the
	// diff is shorter than the viewport.
	for ry := bodyStart + (end - a.diffScroll); ry < bodyStart+rowsCap; ry++ {
		for cx := mx + 1; cx < mx+mw-1; cx++ {
			a.screen.SetContent(cx, ry, ' ', nil, bgStyle)
		}
	}
	a.screen.HideCursor()
}

// drawDiffListRow paints one list-view row: a coloured status glyph, a
// gutter space, then the relative path. The selected row's background
// flips to the editor BG so it reads as focused — same vocabulary as the
// search/finder rows.
func (a *App) drawDiffListRow(mx, ry, mw int, e diffEntry, selected bool, modalBG tcell.Color) {
	glyph, fg := diffKindGlyphTheme(e.kind, a.theme)
	rowBG := modalBG
	if selected {
		rowBG = a.theme.BG
	}
	rowStyle := tcell.StyleDefault.Background(rowBG).Foreground(a.theme.Text)
	for cx := mx + 1; cx < mx+mw-1; cx++ {
		a.screen.SetContent(cx, ry, ' ', nil, rowStyle)
	}
	glyphStyle := tcell.StyleDefault.Background(rowBG).Foreground(fg).Bold(true)
	a.screen.SetContent(mx+2, ry, glyph, nil, glyphStyle)
	label := e.rel
	if runeLen(label) > mw-6 {
		label = string([]rune(label)[:mw-6])
	}
	drawAt(a.screen, mx+4, ry, label, rowStyle)
}

// diffKindGlyphTheme maps a git change kind to a status character and
// colour. Mixed (a folder with conflicting kinds) surfaces as 'M' since a
// file row only ever carries one kind; the switch keeps the function total.
func diffKindGlyphTheme(k filetree.GitChangeKind, th theme.Theme) (rune, tcell.Color) {
	switch k {
	case filetree.GitChangeAdded:
		return 'A', th.GitAdded
	case filetree.GitChangeDeleted:
		return 'D', th.GitDeleted
	case filetree.GitChangeRenamed:
		return 'R', th.AccentSoft
	default:
		return 'M', th.GitModified
	}
}

// firstDiffNewLine returns the zero-based new-file line of the first
// added/changed line in the diff, or 0 when no hunk parses. Used to land
// the cursor ON the first change (not the hunk's context top) when
// opening a file from diff view. It walks the first hunk tracking the
// new-file line counter: context and "+" lines advance it, "-" lines
// don't.
func firstDiffNewLine(lines []string) int {
	inHunk := false
	newLine := 0
	for _, l := range lines {
		if len(l) >= 3 && l[:3] == "@@ " {
			_, _, newStart, _, ok := parseHunkHeader(l)
			if !ok || newStart < 1 {
				return 0
			}
			newLine = newStart - 1
			inHunk = true
			continue
		}
		if !inHunk || len(l) == 0 {
			continue
		}
		// Skip the file-path header lines inside the hunk body.
		if len(l) >= 3 && (l[:3] == "+++" || l[:3] == "---") {
			continue
		}
		switch l[0] {
		case '+':
			return newLine // first added line — land here.
		case '-', '\\':
			// deleted line / "\ No newline" — no new-file advance.
			continue
		}
		// Context line (" ") advances the new-file counter.
		newLine++
	}
	return 0
}
