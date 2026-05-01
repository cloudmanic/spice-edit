// =============================================================================
// File: internal/app/formmodal.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// formmodal.go renders the third secondary-modal type — a vertical form
// with one row per prompt. Custom actions opt in by listing prompts in
// their config; before the action's shell command runs, the editor opens
// this modal, lets the user fill in each field, and only on Submit does
// the action actually execute. The submitted values are exported to the
// shell as env vars named after the prompt keys.
//
// Layout intentionally mirrors the prompt modal it lives next to — same
// border, divider, "esc" hint, button row — so the two read as siblings.
// What's different: a flexible-height field stack and per-row focus
// state, both keyed on the prompt slice the caller passed to openForm.

package app

import (
	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/customactions"
)

// Layout constants for the form modal. The width matches the prompt
// modal so the visual rhythm of the editor's secondary surfaces stays
// consistent, and the height grows with the prompt count rather than
// being pinned — see formModalRect for the math.
const (
	formModalWidth = 60

	// formRowHeight is two lines per prompt: one for the label, one
	// for the input. Compact enough that 3-4 fields stay on a normal
	// terminal without scrolling.
	formRowHeight = 2

	// formChromeHeight is the cells the modal spends on borders,
	// title row, divider, button row, and trailing border — added on
	// top of the prompt rows. Pulled out so the height calc reads
	// "rows * row-height + chrome" rather than a magic number.
	formChromeHeight = 7
)

// openForm displays the form modal for a custom action. The caller
// passes the prompt list straight from customactions.Action — we
// expand each Default through the editor-state vars here so the
// modal sees only fully-resolved strings. callback receives the
// per-key value map only on Submit; Cancel discards everything.
//
// All slice/map state is rebuilt from prompts so a previous form
// can't leak data into the next one — that's a quiet but real
// problem the dirty/confirm flows have had to fix the hard way.
func (a *App) openForm(title string, prompts []customactions.Prompt, callback func(*App, map[string]string)) {
	if len(prompts) == 0 {
		return
	}
	a.closeAllModals()

	vars := a.captureActionVars()

	a.formOpen = true
	a.formTitle = title
	a.formPrompts = append([]customactions.Prompt(nil), prompts...)
	a.formValues = make(map[string]string, len(prompts))
	a.formText = make([][]rune, len(prompts))
	a.formCursor = make([]int, len(prompts))
	a.formScroll = make([]int, len(prompts))
	a.formFocus = 0
	a.formCallback = callback

	for i, p := range prompts {
		switch p.Type {
		case customactions.PromptText:
			val := vars.expand(p.Default)
			a.formText[i] = []rune(val)
			a.formCursor[i] = len(a.formText[i])
			a.formValues[p.Key] = val
		case customactions.PromptSelect:
			// Select state is the option index. Default matches by
			// equality against an option string when present;
			// otherwise we land on the first option so the user
			// always sees a valid choice on open.
			idx := 0
			expanded := vars.expand(p.Default)
			for j, opt := range p.Options {
				if opt == expanded {
					idx = j
					break
				}
			}
			a.formCursor[i] = idx
			a.formValues[p.Key] = p.Options[idx]
		}
	}
}

// formSubmit hands the collected (Key, Value) map back to the caller
// and closes the modal. Empty fields are still passed through — the
// shell command author chooses whether to treat empty as "skip" or as
// an error. Forcing non-empty here would block legitimate cases like
// "REMOTE_OPTS defaults to nothing."
func (a *App) formSubmit() {
	if !a.formOpen {
		return
	}
	cb := a.formCallback
	values := a.formValues
	a.closeAllModals()
	if cb != nil {
		cb(a, values)
	}
}

// formCancel dismisses the form without invoking the callback.
func (a *App) formCancel() {
	a.closeAllModals()
}

// formModalRect computes the on-screen rectangle. Height grows with
// the prompt count — 4 prompts is 4*2 + 7 = 15 rows — so a tall form
// in a small terminal still gets clamped to (0,0) instead of falling
// off the top.
func (a *App) formModalRect() (x, y, w, h int) {
	w = formModalWidth
	h = formChromeHeight + formRowHeight*max1(len(a.formPrompts))
	x = (a.width - w) / 2
	y = (a.height - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}

// max1 keeps the height calc at least one row tall even if a buggy
// caller managed to open the form with no prompts. Without it the
// chrome rows would still draw against a 0-prompt height, which
// looks like a broken modal.
func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// formButtonRects returns the (Submit, Cancel) click rectangles in
// absolute screen coordinates. Pulled out so handleFormMouse and
// drawForm can't drift on layout — both compute hit-tests / draws
// against the same numbers.
func (a *App) formButtonRects() (cancelX, submitX, btnY, cancelW, submitW int) {
	mx, my, mw, mh := a.formModalRect()
	cancelW = 10 // [ Cancel ]
	submitW = 10 // [ Submit ]
	cancelX = mx + 4
	submitX = mx + mw - submitW - 4
	btnY = my + mh - 3
	_ = mh
	return
}

// drawForm renders the modal: title, divider, one (label, input)
// pair per prompt, and a [ Cancel ] / [ Submit ] button row. The
// focused row's input is highlighted; selects show the current
// option with chevrons, text rows show their value with a caret.
func (a *App) drawForm() {
	mx, my, mw, mh := a.formModalRect()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	drawAt(a.screen, mx+1, my+1, " "+a.formTitle, titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	a.screen.HideCursor()
	for i, p := range a.formPrompts {
		rowY := my + 3 + i*formRowHeight
		labelRow := rowY
		inputRow := rowY + 1

		labelStyle := mutedStyle
		if i == a.formFocus {
			labelStyle = titleStyle
		}
		drawAt(a.screen, mx+2, labelRow, p.Label, labelStyle)

		fieldStart := mx + 3
		fieldEnd := mx + mw - 3
		fieldWidth := fieldEnd - fieldStart

		inputBg := a.theme.BG
		if i == a.formFocus {
			inputBg = a.theme.Subtle
		}
		inputStyle := tcell.StyleDefault.Background(inputBg).Foreground(a.theme.Text)
		for cx := fieldStart - 1; cx <= fieldEnd; cx++ {
			a.screen.SetContent(cx, inputRow, ' ', nil, inputStyle)
		}

		switch p.Type {
		case customactions.PromptText:
			a.adjustFormScroll(i, fieldWidth)
			text := a.formText[i]
			cursor := a.formCursor[i]
			scroll := a.formScroll[i]
			for j := 0; j < fieldWidth; j++ {
				idx := scroll + j
				if idx >= len(text) {
					break
				}
				a.screen.SetContent(fieldStart+j, inputRow, text[idx], nil, inputStyle)
			}
			if i == a.formFocus {
				caret := fieldStart + (cursor - scroll)
				if caret >= fieldStart && caret <= fieldEnd {
					a.screen.ShowCursor(caret, inputRow)
				}
			}
		case customactions.PromptSelect:
			// Render as "‹  option  ›" centered-ish. The chevrons
			// double as click targets via handleFormMouse; we offset
			// them inward from the field edges so they don't sit
			// on top of the input border.
			drawAt(a.screen, fieldStart, inputRow, "<", inputStyle)
			drawAt(a.screen, fieldEnd-1, inputRow, ">", inputStyle)
			opt := ""
			if idx := a.formCursor[i]; idx >= 0 && idx < len(p.Options) {
				opt = p.Options[idx]
			}
			startX := fieldStart + (fieldWidth-runeLen(opt))/2
			drawAt(a.screen, startX, inputRow, opt, inputStyle)
		}
	}

	cancelX, submitX, btnY, _, _ := a.formButtonRects()
	drawButton(a.screen, cancelX, btnY, "[ Cancel ]", bg, a.theme.Text, false)
	drawButton(a.screen, submitX, btnY, "[ Submit ]", bg, a.theme.Accent, true)
}

// adjustFormScroll keeps the caret within the visible window of a
// text row's input field. Mirror of adjustPromptScroll; lives here
// because the form's per-row scroll state is its own slice.
func (a *App) adjustFormScroll(i, width int) {
	if width <= 0 {
		a.formScroll[i] = 0
		return
	}
	cursor := a.formCursor[i]
	scroll := a.formScroll[i]
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+width {
		scroll = cursor - width + 1
	}
	if scroll < 0 {
		scroll = 0
	}
	a.formScroll[i] = scroll
}

// handleFormKey routes keystrokes while the form is open. Tab moves
// focus forward, Shift+Tab back. On a text row, printable runes /
// arrow keys / Backspace edit the buffer. On a select row, Left /
// Right cycle options. Enter on the last row submits; Enter on any
// other row advances focus (so a user racing through the form can
// hold Tab/Enter to fill it out).
func (a *App) handleFormKey(ev *tcell.EventKey) {
	if !a.formOpen || len(a.formPrompts) == 0 {
		return
	}
	switch ev.Key() {
	case tcell.KeyEsc:
		a.formCancel()
		return
	case tcell.KeyTab:
		a.formMoveFocus(+1)
		return
	case tcell.KeyBacktab:
		a.formMoveFocus(-1)
		return
	}

	i := a.formFocus
	p := a.formPrompts[i]

	switch p.Type {
	case customactions.PromptSelect:
		a.handleFormSelectKey(ev, i, p)
	case customactions.PromptText:
		a.handleFormTextKey(ev, i, p)
	}
}

// handleFormSelectKey advances or retreats the select's option index
// on Left/Right; Enter advances focus or submits on the last row.
// Pulled out because text-row handling is meaningfully different
// (rune insertion, caret moves) and inlining both would make the
// switch in handleFormKey hard to scan.
func (a *App) handleFormSelectKey(ev *tcell.EventKey, i int, p customactions.Prompt) {
	switch ev.Key() {
	case tcell.KeyLeft, tcell.KeyUp:
		idx := a.formCursor[i] - 1
		if idx < 0 {
			idx = len(p.Options) - 1
		}
		a.formCursor[i] = idx
		a.formValues[p.Key] = p.Options[idx]
	case tcell.KeyRight, tcell.KeyDown:
		idx := (a.formCursor[i] + 1) % len(p.Options)
		a.formCursor[i] = idx
		a.formValues[p.Key] = p.Options[idx]
	case tcell.KeyEnter:
		if i == len(a.formPrompts)-1 {
			a.formSubmit()
			return
		}
		a.formMoveFocus(+1)
	}
}

// handleFormTextKey runs the editor primitives for the focused text
// row: rune insert, Backspace/Delete, Home/End/Left/Right caret
// motion, and Enter as either "advance focus" or "submit on last
// row." formValues is updated on every edit so the modal-side state
// is the single source of truth for both render and submit.
func (a *App) handleFormTextKey(ev *tcell.EventKey, i int, p customactions.Prompt) {
	text := a.formText[i]
	cursor := a.formCursor[i]
	switch ev.Key() {
	case tcell.KeyEnter:
		if i == len(a.formPrompts)-1 {
			a.formSubmit()
			return
		}
		a.formMoveFocus(+1)
	case tcell.KeyLeft:
		if cursor > 0 {
			a.formCursor[i] = cursor - 1
		}
	case tcell.KeyRight:
		if cursor < len(text) {
			a.formCursor[i] = cursor + 1
		}
	case tcell.KeyHome:
		a.formCursor[i] = 0
	case tcell.KeyEnd:
		a.formCursor[i] = len(text)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if cursor > 0 {
			text = append(text[:cursor-1], text[cursor:]...)
			a.formText[i] = text
			a.formCursor[i] = cursor - 1
			a.formValues[p.Key] = string(text)
		}
	case tcell.KeyDelete:
		if cursor < len(text) {
			text = append(text[:cursor], text[cursor+1:]...)
			a.formText[i] = text
			a.formValues[p.Key] = string(text)
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(text)+1)
		next = append(next, text[:cursor]...)
		next = append(next, r)
		next = append(next, text[cursor:]...)
		a.formText[i] = next
		a.formCursor[i] = cursor + 1
		a.formValues[p.Key] = string(next)
	}
}

// formMoveFocus shifts the focused row by delta and wraps. Wrapping
// matches every list-with-keyboard surface in the editor (action
// menu, finder, this modal) so users never hit a "stuck at the top"
// surprise.
func (a *App) formMoveFocus(delta int) {
	n := len(a.formPrompts)
	if n == 0 {
		return
	}
	i := (a.formFocus + delta) % n
	if i < 0 {
		i += n
	}
	a.formFocus = i
}

// handleFormMouse routes mouse clicks: a click on a prompt row's
// label or input area moves focus there; clicks on a select's < or
// > chevrons cycle the option; clicks on Submit / Cancel resolve
// the modal; clicks outside dismiss it.
func (a *App) handleFormMouse(x, y int, btn tcell.ButtonMask) {
	if btn&tcell.Button1 == 0 {
		return
	}
	mx, my, mw, mh := a.formModalRect()
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.formCancel()
		return
	}

	cancelX, submitX, btnY, cancelW, submitW := a.formButtonRects()
	if y == btnY {
		switch {
		case x >= cancelX && x < cancelX+cancelW:
			a.formCancel()
			return
		case x >= submitX && x < submitX+submitW:
			a.formSubmit()
			return
		}
	}

	for i, p := range a.formPrompts {
		rowY := my + 3 + i*formRowHeight
		inputRow := rowY + 1
		if y == rowY || y == inputRow {
			a.formFocus = i

			if y == inputRow && p.Type == customactions.PromptSelect {
				fieldStart := mx + 3
				fieldEnd := mx + mw - 3
				switch x {
				case fieldStart:
					idx := a.formCursor[i] - 1
					if idx < 0 {
						idx = len(p.Options) - 1
					}
					a.formCursor[i] = idx
					a.formValues[p.Key] = p.Options[idx]
					return
				case fieldEnd - 1:
					idx := (a.formCursor[i] + 1) % len(p.Options)
					a.formCursor[i] = idx
					a.formValues[p.Key] = p.Options[idx]
					return
				}
			}

			if y == inputRow && p.Type == customactions.PromptText {
				fieldStart := mx + 3
				fieldEnd := mx + mw - 3
				if x >= fieldStart && x < fieldEnd {
					localCol := x - fieldStart
					target := a.formScroll[i] + localCol
					if target < 0 {
						target = 0
					}
					if target > len(a.formText[i]) {
						target = len(a.formText[i])
					}
					a.formCursor[i] = target
				}
			}
			return
		}
	}
}
