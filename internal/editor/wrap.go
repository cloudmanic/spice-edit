// =============================================================================
// File: internal/editor/wrap.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-11-27
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"fmt"
	"sort"
	"unicode"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// wrappedRow records the visual cell range of one continuation row.
type wrappedRow struct {
	start, end int
}

// wordSpan records a non-whitespace run in logical-line cell coordinates.
type wordSpan struct {
	start, end int
}

const wrapRightMargin = 2

// wrapContentWidth returns the number of cells available to text.
func (t *Tab) wrapContentWidth(viewW int) int {
	w := viewW - gutterWidthFor(t.Buffer.LineCount()) - 1 - wrapRightMargin
	if w < 1 {
		return 1
	}
	return w
}

// SetWrap changes the view without changing the buffer or its dirty state.
func (t *Tab) SetWrap(enabled bool, viewW int) {
	if t.wrapEnabled == enabled || t.IsImage() {
		return
	}
	line := t.ScrollY
	if t.wrapEnabled && len(t.wrapStarts) > 1 {
		line = t.wrappedLine(t.ScrollY)
	}
	t.wrapEnabled = enabled
	t.wrapStarts = nil
	t.wrapRows = nil
	t.wrapWidth = 0
	t.ScrollX = 0
	t.ScrollY = line
	if enabled {
		t.updateWrapLayout(viewW)
		if line >= 0 && line < len(t.wrapStarts)-1 {
			t.ScrollY = t.wrapStarts[line]
		}
	}
	t.StyleStale = true
}

// updateWrapLayout maps logical lines to visual rows, preserving the top
// logical position when a resize or edit changes the layout.
func (t *Tab) updateWrapLayout(viewW int) {
	width := t.wrapContentWidth(viewW)
	if width == t.wrapWidth && len(t.wrapStarts) == t.Buffer.LineCount()+1 && !t.StyleStale {
		return
	}
	line, offset := 0, 0
	if len(t.wrapStarts) > 1 {
		line = t.wrappedLine(t.ScrollY)
		row := t.ScrollY
		if row >= len(t.wrapRows) {
			row = len(t.wrapRows) - 1
		}
		if row >= 0 {
			offset = t.wrapRows[row].start
		}
	}
	starts := make([]int, t.Buffer.LineCount()+1)
	var rows []wrappedRow
	for i, text := range t.Buffer.Lines {
		rows = appendWrappedRows(rows, text, width)
		starts[i+1] = len(rows)
	}
	t.wrapStarts = starts
	t.wrapRows = rows
	t.wrapWidth = width
	if line >= t.Buffer.LineCount() {
		line = t.Buffer.LineCount() - 1
	}
	if line >= 0 {
		t.ScrollY = t.wrappedRowAt(line, offset)
	}
}

// appendWrappedRows prefers whitespace boundaries and splits oversized words.
func appendWrappedRows(rows []wrappedRow, text string, width int) []wrappedRow {
	var words []wordSpan
	visual, wordStart := 0, -1
	for _, r := range text {
		if unicode.IsSpace(r) {
			if wordStart >= 0 {
				words = append(words, wordSpan{wordStart, visual})
				wordStart = -1
			}
		} else if wordStart < 0 {
			wordStart = visual
		}
		visual += RuneVisualWidth(r, visual)
	}
	if wordStart >= 0 {
		words = append(words, wordSpan{wordStart, visual})
	}

	first := len(rows)
	word := 0
	for start := 0; start < visual; {
		end := start + width
		if end > visual {
			end = visual
		}
		for word < len(words) && words[word].end <= start {
			word++
		}
		for i := word; i < len(words) && words[i].start < end; i++ {
			if words[i].end > end && words[i].start > start {
				end = words[i].start
				break
			}
		}
		rows = append(rows, wrappedRow{start, end})
		start = end
	}
	// Reserve a cell for the caret at the end of a completely full row.
	if len(rows) == first || rows[len(rows)-1].end-rows[len(rows)-1].start == width {
		rows = append(rows, wrappedRow{visual, visual})
	}
	return rows
}

// wrappedLine finds the logical line containing a visual row.
func (t *Tab) wrappedLine(row int) int {
	line := sort.Search(len(t.wrapStarts)-1, func(i int) bool {
		return t.wrapStarts[i+1] > row
	})
	if line >= len(t.wrapStarts)-1 {
		line = len(t.wrapStarts) - 2
	}
	if line < 0 {
		return 0
	}
	return line
}

// wrappedCursor returns the visual row and column of the cursor.
func (t *Tab) wrappedCursor() (int, int) {
	runes := t.Buffer.LineRunes(t.Cursor.Line)
	col := LineVisualCol(runes, t.Cursor.Col)
	row := t.wrappedRowAt(t.Cursor.Line, col)
	return row, col - t.wrapRows[row].start
}

// wrappedRowAt finds the continuation row containing a logical visual column.
func (t *Tab) wrappedRowAt(line, col int) int {
	first, end := t.wrapStarts[line], t.wrapStarts[line+1]
	row := sort.Search(end-first, func(i int) bool {
		return t.wrapRows[first+i].start > col
	})
	if row > 0 {
		row--
	}
	return first + row
}

// wrappedTarget clamps a screen column to a position on its visual row.
func (t *Tab) wrappedTarget(row, col int) int {
	segment := t.wrapRows[row]
	target := segment.start + col
	line := t.wrappedLine(row)
	if row+1 < t.wrapStarts[line+1] && target >= segment.end {
		target = segment.end - 1
	}
	if target < segment.start {
		target = segment.start
	}
	return target
}

// ensureWrappedVisible follows explicit cursor movement in visual rows.
func (t *Tab) ensureWrappedVisible(viewW, viewH int) {
	t.updateWrapLayout(viewW)
	row, _ := t.wrappedCursor()
	if row < t.ScrollY {
		t.ScrollY = row
	}
	if viewH > 0 && row >= t.ScrollY+viewH {
		t.ScrollY = row - viewH + 1
	}
}

// MoveVisual moves vertically through wrapped screen rows.
func (t *Tab) MoveVisual(delta, viewW int, extend bool) {
	if !t.wrapEnabled {
		t.MoveCursor(delta, 0, extend)
		return
	}
	t.updateWrapLayout(viewW)
	row, col := t.wrappedCursor()
	row += delta
	if row < 0 {
		row = 0
	}
	if last := t.wrapStarts[len(t.wrapStarts)-1] - 1; row > last {
		row = last
	}
	line := t.wrappedLine(row)
	target := t.wrappedTarget(row, col)
	runes := t.Buffer.LineRunes(line)
	p := Position{Line: line, Col: RuneColAtVisual(runes, target)}
	if p.Col < len(runes) && LineVisualCol(runes, p.Col) < t.wrapRows[row].start {
		p.Col++
	}
	t.MoveCursorTo(p, extend)
}

// wrappedHitTest maps a screen row and cell back to the buffer.
func (t *Tab) wrappedHitTest(localX, localY, w, h int) (Position, bool) {
	if localY < 0 || localY >= h {
		return Position{}, false
	}
	t.updateWrapLayout(w)
	row := t.ScrollY + localY
	if row < 0 || row >= t.wrapStarts[len(t.wrapStarts)-1] {
		return Position{}, false
	}
	line := t.wrappedLine(row)
	x := localX - gutterWidthFor(t.Buffer.LineCount()) - 1
	if x < 0 {
		x = 0
	}
	if x >= t.wrapWidth {
		x = t.wrapWidth - 1
	}
	target := t.wrappedTarget(row, x)
	return Position{Line: line, Col: RuneColAtVisual(t.Buffer.LineRunes(line), target)}, true
}

// renderWrapped draws logical lines across as many visual rows as needed.
func (t *Tab) renderWrapped(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	t.updateWrapLayout(w)
	if t.cursorMoved {
		t.ensureWrappedVisible(w, h)
		t.cursorMoved = false
	}
	overscroll := h / 2
	if overscroll < 3 {
		overscroll = 3
	}
	maxScroll := t.wrapStarts[len(t.wrapStarts)-1] - h + overscroll
	if maxScroll < 0 {
		maxScroll = 0
	}
	if t.ScrollY > maxScroll {
		t.ScrollY = maxScroll
	}
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
	first := t.wrappedLine(t.ScrollY)
	last := t.wrappedLine(t.ScrollY + h - 1)
	if t.StyleStale || first != t.lastHighlightScrollY || last-first+1 != t.lastHighlightHeight {
		t.Styles = HighlightVisible(t.Path, t.Buffer.Lines, first, last-first+1, th)
		t.StyleStale = false
		t.lastHighlightScrollY = first
		t.lastHighlightHeight = last - first + 1
	}
	gw := gutterWidthFor(t.Buffer.LineCount())
	contentX := x + gw + 1
	selStart, selEnd := PosOrdered(t.Anchor, t.Cursor)
	cursorRow, cursorCol := t.wrappedCursor()
	for row := 0; row < h; row++ {
		visualRow := t.ScrollY + row
		cy := y + row
		lineBg := th.BG
		valid := visualRow < t.wrapStarts[len(t.wrapStarts)-1]
		line := 0
		if valid {
			line = t.wrappedLine(visualRow)
			if line == t.Cursor.Line {
				lineBg = th.LineHL
			}
		}
		bgStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Text)
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, bgStyle)
		}
		if !valid {
			continue
		}
		segment := visualRow - t.wrapStarts[line]
		if segment == 0 {
			gutterStyle := bgStyle.Foreground(th.Muted)
			if line == t.Cursor.Line {
				gutterStyle = gutterStyle.Foreground(th.AccentSoft)
			}
			if marker := t.GitLines[line]; marker != GitLineNone {
				scr.SetContent(x, cy, gitLineMarkerRune(marker), nil, gutterStyle.Foreground(gitLineMarkerColor(th, marker)))
			}
			for i, r := range fmt.Sprintf("%*d", gw-1, line+1) {
				if i != 0 || t.GitLines[line] == GitLineNone {
					scr.SetContent(x+i, cy, r, nil, gutterStyle)
				}
			}
		}
		start := t.wrapRows[visualRow].start
		end := t.wrapRows[visualRow].end
		visual := 0
		for idx, r := range []rune(t.Buffer.Lines[line]) {
			width := RuneVisualWidth(r, visual)
			if visual >= end {
				break
			}
			if visual+width > start {
				st := bgStyle
				if line < len(t.Styles) && idx < len(t.Styles[line]) {
					st = t.Styles[line][idx].Background(lineBg)
				}
				p := Position{Line: line, Col: idx}
				if t.HasSelection() && !PosLess(p, selStart) && PosLess(p, selEnd) {
					st = st.Background(th.Selection)
				}
				if match := t.matchAtRune(line, idx); match >= 0 {
					if match == t.FindIndex {
						st = st.Background(th.FindCurrent).Foreground(th.BG)
					} else {
						st = st.Background(th.FindMatch)
					}
				}
				for cell := 0; cell < width; cell++ {
					col := visual + cell
					if col < start || col >= end {
						continue
					}
					ch := r
					if r == '\t' || cell > 0 {
						ch = ' '
					}
					scr.SetContent(contentX+col-start, cy, ch, nil, st)
				}
			}
			visual += width
		}
	}
	cy := y + cursorRow - t.ScrollY
	if cy >= y && cy < y+h && contentX+cursorCol < x+w {
		scr.ShowCursor(contentX+cursorCol, cy)
	} else {
		scr.HideCursor()
	}
}
