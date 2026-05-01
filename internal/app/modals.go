// =============================================================================
// File: internal/app/modals.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// modals.go houses the editor's secondary modals: a single-line text prompt
// (used for Rename and New File), a Yes/No confirmation (used for Delete),
// and the small right-click context menu that appears over the file tree.
//
// Each modal is mutually exclusive with the main action menu and with each
// other — opening any one calls closeAllModals() first, so we can never end
// up with two on screen at once.

package app

import (
	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/filetree"
)

// Layout constants for the secondary modals. Width is wide enough to hold a
// reasonably long filename or sentence; heights are fixed by the row layout
// in the draw functions below.
const (
	promptModalWidth   = 54
	promptModalHeight  = 9
	confirmModalWidth  = 54
	confirmModalHeight = 9

	// contextMenuWidth is fixed so the popup geometry stays predictable —
	// the labels are short enough to fit comfortably.
	contextMenuWidth = 19
)

// contextItem is one row in the tree's right-click context menu. action runs
// against the node the menu was opened for; enabled gates whether the row is
// clickable.
type contextItem struct {
	label   string
	action  func(*App, *filetree.Node)
	enabled func(*App, *filetree.Node) bool
}

// closeAllModals dismisses every modal in one shot and parks any in-flight
// drag / auto-scroll state. Every "open this modal" helper calls it first
// so the modals stay mutually exclusive and a stale drag from before the
// modal opened can't keep extending a selection underneath it. The find
// bar is closed too — opening a modal should never leave it taking
// keystrokes underneath the modal.
func (a *App) closeAllModals() {
	a.menuOpen = false
	a.promptOpen = false
	a.confirmOpen = false
	a.contextOpen = false
	a.dirtyOpen = false
	a.findOpen = false
	a.finderOpen = false
	a.findValue = nil
	a.findCursor = 0
	a.findScroll = 0
	a.hoveredMenuRow = -1
	a.contextNode = nil
	a.contextItems = nil
	a.promptCallback = nil
	a.confirmCallback = nil
	a.dirtySaveCallback = nil
	a.dirtyDiscardCallback = nil
	// confirmCancelHook is parked here so an unrelated confirm modal
	// opened after a format-trust / format-install prompt can't
	// accidentally inherit the cancel hook. The flows that need a
	// hook set it *after* calling openConfirm precisely so this
	// clear doesn't erase their own arming.
	a.confirmCancelHook = nil
	a.dragMode = ""
	a.stopAutoScroll()
}

// anyModalOpen reports whether any modal is on screen. Used by the main
// event router to short-circuit normal editor input. The find bar is
// included so click-through behaviour matches the modals — a click into
// the editor body while the bar is up still routes to the editor (which
// is what the user wants), but a key/mouse handler can use this to know
// "is the user mid-task in some overlay surface".
func (a *App) anyModalOpen() bool {
	return a.menuOpen || a.promptOpen || a.confirmOpen || a.contextOpen || a.dirtyOpen || a.findOpen || a.finderOpen
}

// -----------------------------------------------------------------------------
// Prompt modal (text input + OK / Cancel)
// -----------------------------------------------------------------------------

// openPrompt shows a single-line text input modal. title is the heading,
// hint is a small subtitle (e.g. "in /path/to/folder"), initial pre-fills
// the input field, and callback runs with the trimmed value when the user
// confirms with Enter or clicks OK. An empty submit is ignored.
func (a *App) openPrompt(title, hint, initial string, callback func(*App, string)) {
	a.closeAllModals()
	a.promptOpen = true
	a.promptTitle = title
	a.promptHint = hint
	a.promptValue = []rune(initial)
	a.promptCursor = len(a.promptValue)
	a.promptScroll = 0
	a.promptCallback = callback
}

// promptSubmit runs the prompt's callback with the current value (trimmed of
// surrounding whitespace) and closes the modal. An empty value is rejected
// silently — the user can still cancel with Esc.
func (a *App) promptSubmit() {
	if !a.promptOpen {
		return
	}
	value := trimSpace(string(a.promptValue))
	if value == "" {
		return
	}
	cb := a.promptCallback
	a.closeAllModals()
	if cb != nil {
		cb(a, value)
	}
}

// promptCancel dismisses the prompt without calling the callback.
func (a *App) promptCancel() {
	a.closeAllModals()
}

// handlePromptKey processes keyboard input while the prompt modal is open:
// printable runes are inserted at the cursor; arrow keys move the cursor;
// Backspace / Delete edit; Enter submits; Esc cancels.
func (a *App) handlePromptKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.promptCancel()
	case tcell.KeyEnter:
		a.promptSubmit()
	case tcell.KeyLeft:
		if a.promptCursor > 0 {
			a.promptCursor--
		}
	case tcell.KeyRight:
		if a.promptCursor < len(a.promptValue) {
			a.promptCursor++
		}
	case tcell.KeyHome:
		a.promptCursor = 0
	case tcell.KeyEnd:
		a.promptCursor = len(a.promptValue)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.promptCursor > 0 {
			a.promptValue = append(a.promptValue[:a.promptCursor-1], a.promptValue[a.promptCursor:]...)
			a.promptCursor--
		}
	case tcell.KeyDelete:
		if a.promptCursor < len(a.promptValue) {
			a.promptValue = append(a.promptValue[:a.promptCursor], a.promptValue[a.promptCursor+1:]...)
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(a.promptValue)+1)
		next = append(next, a.promptValue[:a.promptCursor]...)
		next = append(next, r)
		next = append(next, a.promptValue[a.promptCursor:]...)
		a.promptValue = next
		a.promptCursor++
	}
}

// handlePromptMouse processes mouse input while the prompt modal is open.
// Clicks on OK / Cancel run the corresponding action; clicks outside the
// modal cancel; clicks on the input field reposition the cursor.
func (a *App) handlePromptMouse(x, y int, btn tcell.ButtonMask) {
	if btn&tcell.Button1 == 0 {
		return
	}
	mx, my, mw, mh := a.promptModalRect()
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.promptCancel()
		return
	}
	// Buttons sit on row 6 (relY). [ Cancel ] occupies cells 14..23,
	// [  OK  ] occupies 30..37 — see drawPrompt for the matching layout.
	if y == my+6 {
		relX := x - mx
		switch {
		case relX >= 14 && relX < 24:
			a.promptCancel()
			return
		case relX >= 30 && relX < 38:
			a.promptSubmit()
			return
		}
	}
	// Click in the input field — move the cursor to the clicked rune.
	if y == my+4 {
		fieldStart := mx + 3
		fieldEnd := mx + mw - 3
		if x >= fieldStart && x < fieldEnd {
			localCol := x - fieldStart
			target := a.promptScroll + localCol
			if target < 0 {
				target = 0
			}
			if target > len(a.promptValue) {
				target = len(a.promptValue)
			}
			a.promptCursor = target
		}
	}
}

// promptModalRect returns the on-screen rectangle of the prompt modal,
// centered in the window.
func (a *App) promptModalRect() (x, y, w, h int) {
	w = promptModalWidth
	h = promptModalHeight
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

// drawPrompt renders the prompt modal: a centered box with a title row, a
// single-line text input, and a Cancel / OK button row.
//
// Rows (relY):
//
//	0   top border
//	1   title — "<title>   esc"
//	2   divider
//	3   hint (greyed)
//	4   input field    [ value ]
//	5   blank
//	6   buttons        [ Cancel ]   [  OK  ]
//	7   blank
//	8   bottom border
func (a *App) drawPrompt() {
	mx, my, mw, mh := a.promptModalRect()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)

	// Title divider.
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	drawAt(a.screen, mx+1, my+1, " "+a.promptTitle, titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	if a.promptHint != "" {
		drawAt(a.screen, mx+2, my+3, a.promptHint, mutedStyle)
	}

	// Input field — a faint inset at row 4. We render the value with a
	// horizontal scroll window so a long path still keeps the cursor in
	// view.
	fieldStart := mx + 3
	fieldEnd := mx + mw - 3
	fieldWidth := fieldEnd - fieldStart
	a.adjustPromptScroll(fieldWidth)

	inputBg := a.theme.BG
	inputStyle := tcell.StyleDefault.Background(inputBg).Foreground(a.theme.Text)
	for cx := fieldStart - 1; cx <= fieldEnd; cx++ {
		a.screen.SetContent(cx, my+4, ' ', nil, inputStyle)
	}
	for i := 0; i < fieldWidth; i++ {
		idx := a.promptScroll + i
		if idx >= len(a.promptValue) {
			break
		}
		a.screen.SetContent(fieldStart+i, my+4, a.promptValue[idx], nil, inputStyle)
	}
	// Place the screen cursor at the input position so the user sees a
	// blinking caret like any other text field.
	caret := fieldStart + (a.promptCursor - a.promptScroll)
	if caret >= fieldStart && caret <= fieldEnd {
		a.screen.ShowCursor(caret, my+4)
	}

	// Buttons — drawn at fixed columns matching the click rects in
	// handlePromptMouse.
	drawButton(a.screen, mx+14, my+6, "[ Cancel ]", bg, a.theme.Text, false)
	drawButton(a.screen, mx+30, my+6, "[  OK  ]", bg, a.theme.Accent, true)
}

// adjustPromptScroll keeps the cursor within the visible window of the input
// field by sliding promptScroll left or right as needed.
func (a *App) adjustPromptScroll(width int) {
	if width <= 0 {
		a.promptScroll = 0
		return
	}
	if a.promptCursor < a.promptScroll {
		a.promptScroll = a.promptCursor
	}
	if a.promptCursor >= a.promptScroll+width {
		a.promptScroll = a.promptCursor - width + 1
	}
	if a.promptScroll < 0 {
		a.promptScroll = 0
	}
}

// -----------------------------------------------------------------------------
// Confirm modal (Yes / No)
// -----------------------------------------------------------------------------

// openConfirm shows a Yes/No confirmation modal. message is the body text
// shown to the user; callback runs only when the user picks Yes. The default
// focus lands on No so an accidental Enter is harmless — important for
// destructive actions like Delete.
func (a *App) openConfirm(title, message string, callback func(*App)) {
	a.closeAllModals()
	a.confirmOpen = true
	a.confirmTitle = title
	a.confirmMessage = message
	a.confirmHover = 0 // No
	a.confirmCallback = callback
}

// confirmYes runs the confirm callback and closes the modal.
func (a *App) confirmYes() {
	if !a.confirmOpen {
		return
	}
	cb := a.confirmCallback
	a.closeAllModals()
	if cb != nil {
		cb(a)
	}
}

// confirmCancel dismisses the confirm modal without running the callback.
// If a flow armed confirmCancelHook (today: format-trust deny,
// format-install decline) we run it after closing the modal. The
// hook is captured before close so closeAllModals can clear the
// pointer without losing the handler we're about to fire — same
// capture-then-close pattern dirtySave / dirtyDiscard use.
func (a *App) confirmCancel() {
	hook := a.confirmCancelHook
	a.closeAllModals()
	if hook != nil {
		hook(a)
	}
}

// handleConfirmKey processes keyboard input while the confirm modal is open.
// Left / Right toggle focus between [No] and [Yes]; Enter activates the
// focused button; Esc cancels.
func (a *App) handleConfirmKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.confirmCancel()
	case tcell.KeyEnter:
		if a.confirmHover == 1 {
			a.confirmYes()
		} else {
			a.confirmCancel()
		}
	case tcell.KeyLeft, tcell.KeyTab:
		// Tab cycles between buttons; Left moves to No.
		if ev.Key() == tcell.KeyTab {
			a.confirmHover = 1 - a.confirmHover
		} else {
			a.confirmHover = 0
		}
	case tcell.KeyRight:
		a.confirmHover = 1
	}
}

// handleConfirmMouse processes mouse input for the confirm modal. Hovering
// a button highlights it; clicking it activates. Clicks outside the modal
// cancel.
func (a *App) handleConfirmMouse(x, y int, btn tcell.ButtonMask) {
	mx, my, mw, mh := a.confirmModalRect()
	if x >= mx && x < mx+mw && y == my+5 {
		relX := x - mx
		switch {
		case relX >= 14 && relX < 22:
			a.confirmHover = 0
		case relX >= 28 && relX < 38:
			a.confirmHover = 1
		}
	}
	if btn&tcell.Button1 == 0 {
		return
	}
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.confirmCancel()
		return
	}
	if y == my+5 {
		relX := x - mx
		switch {
		case relX >= 14 && relX < 22:
			a.confirmCancel()
			return
		case relX >= 28 && relX < 38:
			a.confirmYes()
			return
		}
	}
}

// confirmModalRect returns the on-screen rectangle of the confirm modal,
// centered in the window.
func (a *App) confirmModalRect() (x, y, w, h int) {
	w = confirmModalWidth
	h = confirmModalHeight
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

// drawConfirm renders the Yes/No modal.
//
// Rows (relY):
//
//	0   top border
//	1   title — "<title>   esc"
//	2   divider
//	3   blank
//	4   message (centered)
//	5   buttons          [  No  ]    [  Yes  ]
//	6   blank
//	7   blank
//	8   bottom border
func (a *App) drawConfirm() {
	mx, my, mw, mh := a.confirmModalRect()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	bodyStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	drawAt(a.screen, mx+1, my+1, " "+a.confirmTitle, titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	// Message — centered, truncated if too long.
	msg := a.confirmMessage
	if runeLen(msg) > mw-4 {
		msg = msg[:mw-4]
	}
	mxText := mx + (mw-runeLen(msg))/2
	drawAt(a.screen, mxText, my+4, msg, bodyStyle)

	// Buttons. Default focus is No so an accidental Enter is non-destructive.
	drawButton(a.screen, mx+14, my+5, "[  No  ]", bg, a.theme.Text, a.confirmHover == 0)
	drawButton(a.screen, mx+28, my+5, "[ Yes ]", bg, a.theme.Error, a.confirmHover == 1)

	a.screen.HideCursor()
}

// -----------------------------------------------------------------------------
// Save / Discard / Cancel modal (unsaved-changes prompt)
// -----------------------------------------------------------------------------

// dirtyModalWidth and dirtyModalHeight pin the unsaved-changes modal's
// geometry. Wider than the Yes/No confirm so the three buttons sit
// comfortably on one row with breathing space between them.
const (
	dirtyModalWidth  = 60
	dirtyModalHeight = 9
)

// Button column origins for the dirty-close modal, kept in one place so
// the draw function and the click hit-tester agree on geometry. The
// buttons are laid out as: [ Cancel ] [ Discard ] [ Save ]; spacing is
// chosen to center the trio inside the 60-cell modal.
const (
	dirtyBtnCancelX  = 5
	dirtyBtnCancelW  = 10 // "[ Cancel ]"
	dirtyBtnDiscardX = 22
	dirtyBtnDiscardW = 11 // "[ Discard ]"
	dirtyBtnSaveX    = 42
	dirtyBtnSaveW    = 8 // "[ Save ]"
)

// openDirtyClose shows the unsaved-changes modal. saveCB runs when the
// user picks Save (typically: save the tab(s), then proceed); discardCB
// runs when the user picks Discard (skip saving, proceed anyway). Cancel
// just dismisses without running anything. Default focus is Cancel so a
// stray Enter is non-destructive — same safety pattern the delete confirm
// uses.
func (a *App) openDirtyClose(title, message string, saveCB, discardCB func(*App)) {
	a.closeAllModals()
	a.dirtyOpen = true
	a.dirtyTitle = title
	a.dirtyMessage = message
	a.dirtyHover = 0 // Cancel
	a.dirtySaveCallback = saveCB
	a.dirtyDiscardCallback = discardCB
}

// dirtyCancel dismisses the modal without running anything.
func (a *App) dirtyCancel() {
	a.closeAllModals()
}

// dirtyDiscard runs the discard callback and dismisses the modal. The
// callback is captured before the close so closeAllModals can clear the
// pointer without losing the handler we're about to fire.
func (a *App) dirtyDiscard() {
	if !a.dirtyOpen {
		return
	}
	cb := a.dirtyDiscardCallback
	a.closeAllModals()
	if cb != nil {
		cb(a)
	}
}

// dirtySave runs the save callback and dismisses the modal. Same
// capture-then-close pattern as dirtyDiscard.
func (a *App) dirtySave() {
	if !a.dirtyOpen {
		return
	}
	cb := a.dirtySaveCallback
	a.closeAllModals()
	if cb != nil {
		cb(a)
	}
}

// dirtyActivate runs the focused button's action — used by Enter and by
// keyboard-driven activations.
func (a *App) dirtyActivate() {
	switch a.dirtyHover {
	case 0:
		a.dirtyCancel()
	case 1:
		a.dirtyDiscard()
	case 2:
		a.dirtySave()
	}
}

// handleDirtyKey processes keyboard input while the dirty-close modal
// is open. Left/Right and Tab cycle focus across the three buttons;
// Enter activates the focused button; Esc cancels.
func (a *App) handleDirtyKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.dirtyCancel()
	case tcell.KeyEnter:
		a.dirtyActivate()
	case tcell.KeyLeft:
		if a.dirtyHover > 0 {
			a.dirtyHover--
		}
	case tcell.KeyRight:
		if a.dirtyHover < 2 {
			a.dirtyHover++
		}
	case tcell.KeyTab:
		a.dirtyHover = (a.dirtyHover + 1) % 3
	}
}

// handleDirtyMouse processes mouse input for the dirty-close modal.
// Hovering a button highlights it; clicking activates. A click outside
// the modal cancels — same as the confirm modal.
func (a *App) handleDirtyMouse(x, y int, btn tcell.ButtonMask) {
	mx, my, mw, mh := a.dirtyModalRect()
	// Hover tracking — works for any move with a button bit set or not.
	if x >= mx && x < mx+mw && y == my+5 {
		switch idx := dirtyButtonAtRelX(x - mx); idx {
		case 0, 1, 2:
			a.dirtyHover = idx
		}
	}
	if btn&tcell.Button1 == 0 {
		return
	}
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.dirtyCancel()
		return
	}
	if y == my+5 {
		switch dirtyButtonAtRelX(x - mx) {
		case 0:
			a.dirtyCancel()
		case 1:
			a.dirtyDiscard()
		case 2:
			a.dirtySave()
		}
	}
}

// dirtyButtonAtRelX maps an x offset within the modal to a button index
// (0=Cancel, 1=Discard, 2=Save) or -1 when the offset misses every
// button. Pulled out so the keyboard-free hover and the click handler
// share one geometry source.
func dirtyButtonAtRelX(rx int) int {
	switch {
	case rx >= dirtyBtnCancelX && rx < dirtyBtnCancelX+dirtyBtnCancelW:
		return 0
	case rx >= dirtyBtnDiscardX && rx < dirtyBtnDiscardX+dirtyBtnDiscardW:
		return 1
	case rx >= dirtyBtnSaveX && rx < dirtyBtnSaveX+dirtyBtnSaveW:
		return 2
	}
	return -1
}

// dirtyModalRect returns the on-screen rectangle of the dirty-close
// modal, centered in the window.
func (a *App) dirtyModalRect() (x, y, w, h int) {
	w = dirtyModalWidth
	h = dirtyModalHeight
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

// drawDirtyClose renders the Save / Discard / Cancel modal.
//
// Rows (relY):
//
//	0   top border
//	1   title — "<title>   esc"
//	2   divider
//	3   blank
//	4   message (centered)
//	5   buttons    [ Cancel ]    [ Discard ]    [ Save ]
//	6   blank
//	7   blank
//	8   bottom border
func (a *App) drawDirtyClose() {
	mx, my, mw, mh := a.dirtyModalRect()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	bodyStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)
	drawHDivider(a.screen, mx, my+2, mw, borderStyle)

	drawAt(a.screen, mx+1, my+1, " "+a.dirtyTitle, titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-runeLen(hint), my+1, hint, mutedStyle)

	// Message — centered, truncated if too long.
	msg := a.dirtyMessage
	if runeLen(msg) > mw-4 {
		msg = msg[:mw-4]
	}
	mxText := mx + (mw-runeLen(msg))/2
	drawAt(a.screen, mxText, my+4, msg, bodyStyle)

	// Buttons. Cancel is neutral, Discard is red (destructive),
	// Save is the editor's accent so it reads as the productive default.
	drawButton(a.screen, mx+dirtyBtnCancelX, my+5, "[ Cancel ]", bg, a.theme.Text, a.dirtyHover == 0)
	drawButton(a.screen, mx+dirtyBtnDiscardX, my+5, "[ Discard ]", bg, a.theme.Error, a.dirtyHover == 1)
	drawButton(a.screen, mx+dirtyBtnSaveX, my+5, "[ Save ]", bg, a.theme.Accent, a.dirtyHover == 2)

	a.screen.HideCursor()
}

// -----------------------------------------------------------------------------
// Tree right-click context menu
// -----------------------------------------------------------------------------

// openTreeContext opens a small right-click popup over the file tree, anchored
// near (x, y). The items shown depend on whether n is a file or a folder.
// Renaming or deleting the project root is intentionally not allowed.
func (a *App) openTreeContext(n *filetree.Node, x, y int) {
	a.closeAllModals()

	items := []contextItem{}
	if n.IsDir {
		items = append(items, contextItem{label: "New File", action: ctxNewFile})
	}
	if n != a.tree.Root {
		items = append(items, contextItem{label: "Rename", action: ctxRename})
		items = append(items, contextItem{label: "Delete", action: ctxDelete})
	}
	items = append(items, contextItem{label: "Copy rel path", action: ctxCopyRelativePath})
	items = append(items, contextItem{label: "Copy abs path", action: ctxCopyAbsolutePath})

	a.contextNode = n
	a.contextItems = items
	a.contextHover = 0
	a.contextX, a.contextY = a.placeContext(x, y, len(items))
	a.contextOpen = true
}

// placeContext picks an on-screen origin for the context menu. Anchors on
// the click point, but flips left or up when that would put part of the
// popup off-screen.
func (a *App) placeContext(x, y, count int) (int, int) {
	w := contextMenuWidth
	h := count + 2
	cx := x
	cy := y
	if cx+w > a.width {
		cx = x - w + 1
	}
	if cy+h > a.height {
		cy = y - h + 1
	}
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	return cx, cy
}

// contextRect returns the on-screen rectangle of the context menu.
func (a *App) contextRect() (x, y, w, h int) {
	return a.contextX, a.contextY, contextMenuWidth, len(a.contextItems) + 2
}

// handleContextKey processes keyboard input for the context menu.
func (a *App) handleContextKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeAllModals()
	case tcell.KeyDown:
		if a.contextHover < len(a.contextItems)-1 {
			a.contextHover++
		}
	case tcell.KeyUp:
		if a.contextHover > 0 {
			a.contextHover--
		}
	case tcell.KeyEnter:
		a.contextActivate()
	}
}

// handleContextMouse processes mouse input for the context menu. Hovering
// a row highlights it; clicking activates. Any click outside the popup
// dismisses it.
func (a *App) handleContextMouse(x, y int, btn tcell.ButtonMask) {
	mx, my, mw, mh := a.contextRect()
	if x >= mx && x < mx+mw && y > my && y < my+mh-1 {
		a.contextHover = y - my - 1
	}
	if btn&tcell.Button1 == 0 {
		return
	}
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.closeAllModals()
		return
	}
	if y > my && y < my+mh-1 {
		a.contextHover = y - my - 1
		a.contextActivate()
	}
}

// contextActivate runs the currently highlighted context item against the
// node the menu was opened for.
func (a *App) contextActivate() {
	if a.contextHover < 0 || a.contextHover >= len(a.contextItems) {
		return
	}
	item := a.contextItems[a.contextHover]
	node := a.contextNode
	a.closeAllModals()
	if item.action != nil && node != nil {
		item.action(a, node)
	}
}

// drawContext renders the right-click context menu at its anchor.
func (a *App) drawContext() {
	mx, my, mw, mh := a.contextRect()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	hoverBg := a.theme.Selection
	hoverStyle := tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.Text).Bold(true)
	hoverChevStyle := tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.AccentSoft).Bold(true)
	chevStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.AccentSoft)

	fillRect(a.screen, mx, my, mw, mh, bgStyle)
	drawBorder(a.screen, mx, my, mw, mh, borderStyle)

	for i, item := range a.contextItems {
		cy := my + 1 + i
		hovered := i == a.contextHover
		if hovered {
			for cx := mx + 1; cx < mx+mw-1; cx++ {
				a.screen.SetContent(cx, cy, ' ', nil, hoverStyle)
			}
			drawAt(a.screen, mx+2, cy, "▸", hoverChevStyle)
			drawAt(a.screen, mx+4, cy, item.label, hoverStyle)
		} else {
			drawAt(a.screen, mx+2, cy, "▸", chevStyle)
			drawAt(a.screen, mx+4, cy, item.label, bgStyle)
		}
	}

	a.screen.HideCursor()
}

// -----------------------------------------------------------------------------
// Drawing helpers shared across modals.
// -----------------------------------------------------------------------------

// fillRect paints a rectangle of (w x h) cells starting at (x, y) with the
// given style.
func fillRect(scr tcell.Screen, x, y, w, h int, st tcell.Style) {
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, st)
		}
	}
}

// drawBorder draws a single-line box border around the rectangle.
func drawBorder(scr tcell.Screen, x, y, w, h int, st tcell.Style) {
	scr.SetContent(x, y, '┌', nil, st)
	scr.SetContent(x+w-1, y, '┐', nil, st)
	scr.SetContent(x, y+h-1, '└', nil, st)
	scr.SetContent(x+w-1, y+h-1, '┘', nil, st)
	for cx := x + 1; cx < x+w-1; cx++ {
		scr.SetContent(cx, y, '─', nil, st)
		scr.SetContent(cx, y+h-1, '─', nil, st)
	}
	for cy := y + 1; cy < y+h-1; cy++ {
		scr.SetContent(x, cy, '│', nil, st)
		scr.SetContent(x+w-1, cy, '│', nil, st)
	}
}

// drawHDivider draws a horizontal divider with ├ ┤ end caps inside an
// existing border.
func drawHDivider(scr tcell.Screen, x, y, w int, st tcell.Style) {
	scr.SetContent(x, y, '├', nil, st)
	scr.SetContent(x+w-1, y, '┤', nil, st)
	for cx := x + 1; cx < x+w-1; cx++ {
		scr.SetContent(cx, y, '─', nil, st)
	}
}

// drawButton renders a "button" — really just bracketed label — at (x, y).
// Active buttons get a tinted background so they read as the focused option.
func drawButton(scr tcell.Screen, x, y int, label string, modalBG tcell.Color, fg tcell.Color, focused bool) {
	bg := modalBG
	st := tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true)
	if focused {
		// Focused button: invert — the label sits on a tinted block.
		st = tcell.StyleDefault.Background(fg).Foreground(modalBG).Bold(true)
	}
	col := 0
	for _, r := range label {
		scr.SetContent(x+col, y, r, nil, st)
		col++
	}
}

// runeLen returns the visible cell count of s (one cell per rune).
func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// trimSpace strips ASCII whitespace from both ends of s. Tiny dependency-free
// substitute for strings.TrimSpace so this file doesn't grow imports.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
