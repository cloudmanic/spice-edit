// =============================================================================
// File: internal/editor/tab.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// gutterWidth is the cell width reserved for the line-number column inside
// the editor area. Six gives us up to five-digit line numbers plus a one-cell
// pad on the right — comfortable for files of any realistic length.
const gutterWidth = 6

// Tab is a single open file. It owns the on-disk path, the in-memory buffer,
// the per-tab view state (scroll position, cursor, selection anchor), the
// cached syntax-highlight styles, and a dirty flag.
type Tab struct {
	Path       string  // Empty for an unsaved/scratch tab.
	Buffer     *Buffer
	Cursor     Position // Where new typed text appears.
	Anchor     Position // Selection anchor; equals Cursor when nothing is selected.
	ScrollY    int      // Index of the first visible line.
	ScrollX    int      // Index of the first visible column (rune-indexed).
	Dirty      bool
	Styles     [][]tcell.Style
	StyleStale bool

	// Mtime is the file's modification time as of the last successful
	// read or write. The app's periodic disk-reconcile loop compares it
	// against the live mtime to detect external edits.
	Mtime time.Time

	// DiskGone is set when the most recent disk check found the file
	// missing. It exists so we only flash the "deleted on disk" warning
	// once, instead of re-flashing every reconcile tick.
	DiskGone bool

	// cursorMoved is set by every method that changes Cursor; Render
	// consumes it to decide whether to scroll the viewport so the cursor
	// is visible. Without this flag, mouse-wheel scrolling is fought by
	// every redraw — EnsureVisible would snap us back to the cursor.
	cursorMoved bool

	// Undo / redo stacks plus the original on-open snapshot used by
	// RevertFile. See undo.go for the push / coalescing rules and the
	// public Undo / Redo / RevertFile entry points.
	undoStack     []snapshot
	redoStack     []snapshot
	undoOriginal  snapshot
	lastUndoGroup undoGroup
	lastUndoAt    time.Time

	// Mode is "" for a normal text tab and imageMode (= "image") for a
	// read-only image preview. Image tabs reuse the Tab type so the
	// app's tab list, switcher, and modal-routing all just work — the
	// content-mutating methods short-circuit on imageMode and Render
	// delegates to renderImage. See image.go for the render path.
	Mode     string
	Image    image.Image // populated when Mode == imageMode
	ImageFmt string      // "png" / "jpeg" / "gif" — for the status bar

	// Find state — populated when the user opens the find bar and
	// types a query. The UI layer (App) owns the bar geometry and
	// keystroke routing; the tab owns the query, the resolved match
	// list, and the index of the "current" match so the query
	// survives switching tabs and re-opening the bar.
	FindQuery   string
	FindMatches []Match
	FindIndex   int // -1 = no current match; otherwise an index into FindMatches.

	// IndentUnit is the string the editor inserts when the user presses
	// Tab. Detected on file open (DetectIndent) so the editor matches
	// whatever the file already does — a tab-indented Go file gets a
	// real tab; a 2-space-indented file gets two spaces. Mixed-style
	// files take the dominant signal.
	IndentUnit string
}

// NewTab opens path and returns a Tab. If the file does not exist, the tab
// is created with an empty buffer that will be written on first save —
// matching what most editors do when you "open" a brand-new file path.
// When path looks like an image we recognise (PNG / JPEG / GIF), the tab
// is opened in read-only image-preview mode instead of as text.
func NewTab(path string) (*Tab, error) {
	if path != "" && isImageExt(path) {
		return newImageTab(path)
	}
	var data []byte
	var mtime time.Time
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		data = b
		// Record the on-disk mtime so the app can detect external edits
		// later. A missing file leaves mtime as the zero value, which is
		// fine — the reconcile loop handles that case explicitly.
		if info, statErr := os.Stat(path); statErr == nil {
			mtime = info.ModTime()
		}
	}
	t := &Tab{
		Path:       path,
		Buffer:     NewBuffer(string(data)),
		StyleStale: true,
		Mtime:      mtime,
	}
	t.IndentUnit = DetectIndent(t.Buffer.Lines, path)
	// Record the on-open buffer state so RevertFile has somewhere to
	// rewind to even after the user has typed away.
	t.initUndo()
	return t, nil
}

// newImageTab decodes path as an image and returns a Tab in image
// preview mode. The buffer is left empty (image tabs ignore it) but
// allocated so any code that pokes at t.Buffer doesn't have to nil-check.
func newImageTab(path string) (*Tab, error) {
	img, format, err := decodeImageFile(path)
	if err != nil {
		return nil, err
	}
	var mtime time.Time
	if info, statErr := os.Stat(path); statErr == nil {
		mtime = info.ModTime()
	}
	t := &Tab{
		Path:     path,
		Buffer:   NewBuffer(""),
		Mtime:    mtime,
		Mode:     imageMode,
		Image:    img,
		ImageFmt: format,
	}
	// Capture the empty original snapshot so CanRevert / RevertFile
	// behave sensibly even though image tabs are read-only — they'll
	// just always report "nothing to revert".
	t.initUndo()
	return t, nil
}

// IsImage reports whether the tab is an image-preview, not a text editor.
// Callers use this to skip text-only behaviour (cursor placement, key
// dispatch, save, etc.) without having to know about Mode strings.
func (t *Tab) IsImage() bool {
	return t.Mode == imageMode
}

// DisplayName returns the basename of Path, or "untitled" for unsaved tabs.
func (t *Tab) DisplayName() string {
	if t.Path == "" {
		return "untitled"
	}
	return filepath.Base(t.Path)
}

// Save writes the buffer to disk and clears Dirty. It is an error to call
// Save on an untitled tab — callers should prompt for a path first. Mtime
// is refreshed so the disk-reconcile loop doesn't immediately think the
// file we just wrote was changed by someone else. Image tabs return an
// error since the editor only knows how to read those, not re-encode them.
func (t *Tab) Save() error {
	if t.IsImage() {
		return fmt.Errorf("image tabs are read-only")
	}
	if t.Path == "" {
		return fmt.Errorf("no path set for tab")
	}
	if err := os.WriteFile(t.Path, []byte(t.Buffer.String()), 0644); err != nil {
		return err
	}
	t.Dirty = false
	t.DiskGone = false
	if info, err := os.Stat(t.Path); err == nil {
		t.Mtime = info.ModTime()
	}
	// Save is a natural logical-step boundary: the next typing burst is
	// clearly a separate intent, so don't let it merge into whatever was
	// in flight before the save.
	t.breakUndoGroup()
	return nil
}

// Reload re-reads the file from disk into the buffer. Cursor and anchor
// are clamped to the new content (so the user keeps roughly their place
// instead of getting snapped to line 0); ScrollY is left alone and gets
// clamped on the next render. Dirty is cleared and the syntax cache is
// invalidated. Image tabs decode the file again instead of replacing
// the text buffer.
func (t *Tab) Reload() error {
	if t.Path == "" {
		return fmt.Errorf("no path set for tab")
	}
	if t.IsImage() {
		img, format, err := decodeImageFile(t.Path)
		if err != nil {
			return err
		}
		info, err := os.Stat(t.Path)
		if err != nil {
			return err
		}
		t.Image = img
		t.ImageFmt = format
		t.Mtime = info.ModTime()
		t.DiskGone = false
		return nil
	}
	data, err := os.ReadFile(t.Path)
	if err != nil {
		return err
	}
	info, err := os.Stat(t.Path)
	if err != nil {
		return err
	}
	t.Buffer = NewBuffer(string(data))
	t.Cursor = t.Buffer.Clamp(t.Cursor)
	t.Anchor = t.Cursor // drop any selection — line indices may have shifted.
	t.Dirty = false
	t.DiskGone = false
	t.Mtime = info.ModTime()
	t.StyleStale = true
	t.cursorMoved = true
	// Reload re-establishes "what's on disk" as the new baseline. Any
	// prior undo history is meaningless now (the line indices may have
	// shifted, and the user explicitly asked to take the disk version),
	// so reset both stacks and the revert anchor.
	t.initUndo()
	return nil
}

// HasSelection reports whether the tab currently has a non-empty selection.
func (t *Tab) HasSelection() bool {
	return t.Cursor != t.Anchor
}

// SelectionText returns the currently selected text, or "" if nothing is
// selected. The text is always returned in document order.
func (t *Tab) SelectionText() string {
	if !t.HasSelection() {
		return ""
	}
	return t.Buffer.Substring(t.Anchor, t.Cursor)
}

// DeleteSelection removes the selected range and collapses the cursor to the
// start of the selection. A no-op when nothing is selected.
func (t *Tab) DeleteSelection() {
	if t.IsImage() || !t.HasSelection() {
		return
	}
	// Selection deletes are always their own undo step — they can wipe
	// out a lot in one stroke, and merging them into adjacent typing
	// would make the next undo recover content the user thought was
	// just-deleted.
	t.pushUndo(undoGroupStructural)
	pos := t.Buffer.DeleteRange(t.Anchor, t.Cursor)
	t.Cursor = pos
	t.Anchor = pos
	t.Dirty = true
	t.StyleStale = true
	t.cursorMoved = true
}

// InsertString inserts s at the cursor (replacing any selection first) and
// advances the cursor past the inserted text. Always recorded as a
// structural undo step — pasted text or "\n" presses shouldn't merge
// with the surrounding typing burst. No-op on image tabs.
func (t *Tab) InsertString(s string) {
	if t.IsImage() {
		return
	}
	if t.HasSelection() {
		// DeleteSelection records its own structural step. Don't push a
		// second one here or the user would have to undo twice to get
		// back to the pre-paste-with-selection state.
		t.DeleteSelection()
	} else {
		t.pushUndo(undoGroupStructural)
	}
	t.Cursor = t.Buffer.InsertString(t.Cursor, s)
	t.Anchor = t.Cursor
	t.Dirty = true
	t.StyleStale = true
	t.cursorMoved = true
}

// InsertRune inserts a single typed character at the cursor. Coalesces
// with adjacent runes inside the undo window so a typed word collapses
// into a single undo step rather than one entry per keystroke. No-op
// on image tabs.
func (t *Tab) InsertRune(r rune) {
	if t.IsImage() {
		return
	}
	if t.HasSelection() {
		// First-rune-after-selection: let DeleteSelection capture the
		// pre-state, then run the insert without a second push.
		t.DeleteSelection()
	} else {
		t.pushUndo(undoGroupTyping)
	}
	t.Cursor = t.Buffer.InsertString(t.Cursor, string(r))
	t.Anchor = t.Cursor
	t.Dirty = true
	t.StyleStale = true
	t.cursorMoved = true
}

// Backspace deletes the character before the cursor (or the selection if any).
// Coalesces with adjacent backspaces inside the undo window. No-op on
// image tabs.
func (t *Tab) Backspace() {
	if t.IsImage() {
		return
	}
	if t.HasSelection() {
		t.DeleteSelection()
		return
	}
	if t.Cursor.Line == 0 && t.Cursor.Col == 0 {
		return
	}
	t.pushUndo(undoGroupBackspace)
	var prev Position
	if t.Cursor.Col == 0 {
		prev.Line = t.Cursor.Line - 1
		prev.Col = len([]rune(t.Buffer.Lines[prev.Line]))
	} else {
		prev = Position{Line: t.Cursor.Line, Col: t.Cursor.Col - 1}
	}
	t.Cursor = t.Buffer.DeleteRange(prev, t.Cursor)
	t.Anchor = t.Cursor
	t.Dirty = true
	t.StyleStale = true
	t.cursorMoved = true
}

// Delete removes the character after the cursor (or the selection if any).
// Coalesces with adjacent forward-deletes inside the undo window. No-op
// on image tabs.
func (t *Tab) Delete() {
	if t.IsImage() {
		return
	}
	if t.HasSelection() {
		t.DeleteSelection()
		return
	}
	end := t.Buffer.EndPos()
	if t.Cursor == end {
		return
	}
	t.pushUndo(undoGroupDelete)
	var next Position
	line := []rune(t.Buffer.Lines[t.Cursor.Line])
	if t.Cursor.Col >= len(line) {
		next = Position{Line: t.Cursor.Line + 1, Col: 0}
	} else {
		next = Position{Line: t.Cursor.Line, Col: t.Cursor.Col + 1}
	}
	t.Cursor = t.Buffer.DeleteRange(t.Cursor, next)
	t.Anchor = t.Cursor
	t.Dirty = true
	t.StyleStale = true
	t.cursorMoved = true
}

// MoveCursor shifts the cursor by (dLine, dCol). When extend is true the
// anchor is left in place so the user is extending a selection.
func (t *Tab) MoveCursor(dLine, dCol int, extend bool) {
	cur := t.Cursor
	if dLine != 0 {
		cur.Line += dLine
		if cur.Line < 0 {
			cur.Line = 0
		}
		if cur.Line >= t.Buffer.LineCount() {
			cur.Line = t.Buffer.LineCount() - 1
		}
		runes := []rune(t.Buffer.Lines[cur.Line])
		if cur.Col > len(runes) {
			cur.Col = len(runes)
		}
	}
	if dCol != 0 {
		cur.Col += dCol
		if cur.Col < 0 {
			// Wrap to the end of the previous line.
			if cur.Line > 0 {
				cur.Line--
				cur.Col = len([]rune(t.Buffer.Lines[cur.Line]))
			} else {
				cur.Col = 0
			}
		} else {
			runes := []rune(t.Buffer.Lines[cur.Line])
			if cur.Col > len(runes) {
				if cur.Line < t.Buffer.LineCount()-1 {
					cur.Line++
					cur.Col = 0
				} else {
					cur.Col = len(runes)
				}
			}
		}
	}
	t.Cursor = cur
	if !extend {
		t.Anchor = cur
	}
	t.cursorMoved = true
	// Cursor moved on the user's explicit command — close any open
	// coalescing window so the next typing burst is a fresh undo step.
	t.breakUndoGroup()
}

// MoveCursorTo sets the cursor to a specific buffer position. Position is
// clamped within the buffer; extend=true preserves the selection anchor.
func (t *Tab) MoveCursorTo(p Position, extend bool) {
	p = t.Buffer.Clamp(p)
	t.Cursor = p
	if !extend {
		t.Anchor = p
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// MoveLineHome moves the cursor to column 0 of the current line.
func (t *Tab) MoveLineHome(extend bool) {
	t.Cursor.Col = 0
	if !extend {
		t.Anchor = t.Cursor
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// MoveLineEnd moves the cursor to the last column of the current line.
func (t *Tab) MoveLineEnd(extend bool) {
	t.Cursor.Col = len([]rune(t.Buffer.Lines[t.Cursor.Line]))
	if !extend {
		t.Anchor = t.Cursor
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// SelectAll selects the entire buffer (anchor at start, cursor at end).
func (t *Tab) SelectAll() {
	t.Anchor = Position{Line: 0, Col: 0}
	t.Cursor = t.Buffer.EndPos()
	t.cursorMoved = true
	t.breakUndoGroup()
}

// EnsureVisible scrolls the viewport so the cursor is on screen. The
// caller passes the editor area's width and height because the Tab itself
// doesn't know its render rect.
func (t *Tab) EnsureVisible(viewW, viewH int) {
	contentW := viewW - gutterWidth - 1
	if contentW < 1 {
		contentW = 1
	}
	if t.Cursor.Line < t.ScrollY {
		t.ScrollY = t.Cursor.Line
	}
	if t.Cursor.Line >= t.ScrollY+viewH {
		t.ScrollY = t.Cursor.Line - viewH + 1
	}
	if t.Cursor.Col < t.ScrollX {
		t.ScrollX = t.Cursor.Col
	}
	if t.Cursor.Col >= t.ScrollX+contentW {
		t.ScrollX = t.Cursor.Col - contentW + 1
	}
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
	if t.ScrollX < 0 {
		t.ScrollX = 0
	}
}

// Render draws the editor's content (line numbers, code with syntax
// highlighting, selection, cursor) into the rectangle (x, y, w, h).
// Image tabs delegate to renderImage instead of drawing text.
func (t *Tab) Render(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	if t.IsImage() {
		t.renderImage(scr, th, x, y, w, h)
		return
	}
	if t.StyleStale {
		t.Styles = Highlight(t.Path, t.Buffer.String(), th)
		t.StyleStale = false
	}
	// Only re-center on the cursor if the cursor moved this tick. Doing it
	// every render fights the user when they scroll with the wheel.
	if t.cursorMoved {
		t.EnsureVisible(w, h)
		t.cursorMoved = false
	}
	t.clampScroll(h)

	bg := th.BG
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(th.Text)

	// Paint the entire editor rectangle with the base background first so
	// any cells we don't draw (short lines, blank rows) still get themed.
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	selStart, selEnd := PosOrdered(t.Anchor, t.Cursor)
	hasSel := t.HasSelection()

	contentX := x + gutterWidth + 1
	contentW := w - gutterWidth - 1
	if contentW < 1 {
		contentW = 1
	}

	for row := 0; row < h; row++ {
		lineIdx := t.ScrollY + row
		if lineIdx >= t.Buffer.LineCount() {
			break
		}
		cy := y + row
		isCursorLine := lineIdx == t.Cursor.Line

		// Pick the row background — a hair lighter on the cursor's line so
		// the eye can catch where the caret is from across the screen.
		lineBg := bg
		if isCursorLine {
			lineBg = th.LineHL
		}
		lineBgStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Text)

		// Re-paint this row with its (possibly highlighted) bg.
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, lineBgStyle)
		}

		// Gutter / line number, right-aligned with one trailing space.
		numStr := fmt.Sprintf("%*d", gutterWidth-1, lineIdx+1)
		gutterStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Muted)
		if isCursorLine {
			gutterStyle = gutterStyle.Foreground(th.AccentSoft)
		}
		for i, r := range numStr {
			scr.SetContent(x+i, cy, r, nil, gutterStyle)
		}

		// Line content, with syntax styles, selection bg, and line bg.
		// We walk from the start of the line so tab stops anchor to col 0
		// — a tab one cell into the line still expands to the next stop,
		// not the next-stop-from-the-scroll-offset. ScrollX skips runes;
		// the visual column we paint at is rune-walked from there.
		runes := []rune(t.Buffer.Lines[lineIdx])
		var styles []tcell.Style
		if lineIdx < len(t.Styles) {
			styles = t.Styles[lineIdx]
		}
		scrollVisual := LineVisualCol(runes, t.ScrollX)
		visualCol := 0 // visual cell offset from the start of the LINE
		for runeIdx, r := range runes {
			width := RuneVisualWidth(r, visualCol)
			if runeIdx >= t.ScrollX {
				// Once we're past ScrollX, paint each cell of this rune.
				// The rune's first cell shows the actual glyph (or ' '
				// for tabs); padding cells for a multi-cell tab show a
				// space so the trailing tab area still gets the right bg.
				st := bgStyle
				if runeIdx < len(styles) {
					st = styles[runeIdx]
				}
				st = st.Background(lineBg)
				if hasSel {
					p := Position{Line: lineIdx, Col: runeIdx}
					if !PosLess(p, selStart) && PosLess(p, selEnd) {
						st = st.Background(th.Selection)
					}
				}
				if mIdx := t.matchAtRune(lineIdx, runeIdx); mIdx >= 0 {
					if mIdx == t.FindIndex {
						st = st.Background(th.FindCurrent).Foreground(th.BG)
					} else {
						st = st.Background(th.FindMatch)
					}
				}
				glyph := r
				if r == '\t' {
					glyph = ' '
				}
				for cell := 0; cell < width; cell++ {
					sc := visualCol - scrollVisual + cell
					if sc < 0 {
						continue
					}
					if sc >= contentW {
						break
					}
					ch := glyph
					if cell > 0 {
						ch = ' '
					}
					scr.SetContent(contentX+sc, cy, ch, nil, st)
				}
			}
			visualCol += width
		}
	}

	// Position the hardware cursor at its visual column (so a cursor
	// past a tab lands at the tab's *end* cell, not just rune-Col cells
	// to the right of ScrollX).
	cy := y + (t.Cursor.Line - t.ScrollY)
	cursorRunes := t.Buffer.LineRunes(t.Cursor.Line)
	cursorVisual := LineVisualCol(cursorRunes, t.Cursor.Col)
	scrollVisual := LineVisualCol(cursorRunes, t.ScrollX)
	cx := contentX + (cursorVisual - scrollVisual)
	if cy >= y && cy < y+h && cx >= contentX && cx < contentX+contentW {
		scr.ShowCursor(cx, cy)
	} else {
		scr.HideCursor()
	}
}

// HitTest converts screen coordinates within this tab's render area to a
// buffer position. ok=false means the click was outside any line.
func (t *Tab) HitTest(localX, localY, w, h int) (Position, bool) {
	if localY < 0 || localY >= h {
		return Position{}, false
	}
	contentX := gutterWidth + 1
	line := t.ScrollY + localY
	if line < 0 || line >= t.Buffer.LineCount() {
		return Position{}, false
	}
	if localX < contentX {
		// Click in the gutter — treat as click at column 0 of that line.
		return Position{Line: line, Col: 0}, true
	}
	runes := []rune(t.Buffer.Lines[line])
	// Convert the click's screen column back to a rune column. With tabs
	// expanding to multi-cell tab stops we can't just subtract ScrollX
	// from localX — we have to walk the runes counting visual cells.
	scrollVisual := LineVisualCol(runes, t.ScrollX)
	targetVisual := scrollVisual + (localX - contentX)
	col := RuneColAtVisual(runes, targetVisual)
	if col > len(runes) {
		col = len(runes)
	}
	if col < 0 {
		col = 0
	}
	return Position{Line: line, Col: col}, true
}

// Scroll moves the viewport by delta lines (negative = up). Render runs
// clampScroll afterwards so the user never scrolls into pure void; here we
// just adjust the raw value.
func (t *Tab) Scroll(deltaLines int) {
	t.ScrollY += deltaLines
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
}

// clampScroll keeps ScrollY inside a sensible range for the current viewport
// height. The max is "last line still on screen, plus a small overscroll
// pad" so the user can scroll the bottom of the file up to the middle of
// the viewport — which feels much better than abruptly stopping when the
// last line hits the bottom row.
func (t *Tab) clampScroll(viewH int) {
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
	overscroll := viewH / 2
	if overscroll < 3 {
		overscroll = 3
	}
	max := t.Buffer.LineCount() - viewH + overscroll
	if max < 0 {
		max = 0
	}
	if t.ScrollY > max {
		t.ScrollY = max
	}
}
