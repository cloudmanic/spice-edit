// =============================================================================
// File: internal/app/app.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package app is the editor's top-level glue: it owns the tcell screen,
// the file tree, the open tabs, and the event loop. The drawing is split
// into four panels (sidebar / tab bar / editor body / status bar) and the
// mouse dispatcher routes presses, drags, and wheel events to whichever
// panel the cursor is over.
//
// The editor is mouse-first by design — there are no Ctrl-keyed shortcuts
// because they collide with terminal flow control (Ctrl-S/Q) and tmux/zellij
// prefixes. Instead, every action lives behind a click on the ≡ icon at
// the top-left of the tab bar, which opens a centered modal of actions.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/clipboard"
	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/theme"
	"github.com/cloudmanic/spice-edit/internal/version"
)

// Layout, behavior, and feel constants. Constants instead of config —
// the editor is opinionated by design.
const (
	defaultSidebarWidth = 30
	minSidebarWidth     = 18
	minEditorAfterDrag  = 40
	minWidth            = 50
	minHeight           = 19
	statusFlashFor = 3 * time.Second
	doubleClickMs  = 500 * time.Millisecond
	doubleEscMs    = 500 * time.Millisecond
	wheelLines     = 3
	tabIndent      = "    " // 4 spaces — opinionated.

	// treeRefreshInterval is how often the background goroutine kicks off
	// a file-tree reload so the sidebar stays in sync with on-disk changes
	// made by other tools (git, mv, another tmux pane, etc.). 10s feels
	// "fresh enough" while costing only a handful of ReadDir syscalls.
	treeRefreshInterval = 10 * time.Second

	// menuButtonWidth is how many cells the ≡ icon occupies at the top-left
	// of the tab bar. Tabs render starting just after it.
	menuButtonWidth = 4

	// Modal geometry. Width comfortably fits the longest action label
	// ("Save & close tab") plus chevron and padding; height is fixed by
	// the layout below.
	modalWidth  = 38
	modalHeight = 18

	// autoScrollTick is how often the auto-scroll goroutine emits a tick
	// while the user is drag-selecting with the cursor parked outside the
	// editor's vertical edges. ~16 ticks/sec feels responsive without
	// overshooting on small files.
	autoScrollTick = 60 * time.Millisecond
)

// autoScrollEvent is the custom tcell event our auto-scroll goroutine
// posts at autoScrollTick intervals while the user is drag-selecting past
// the top or bottom edge of the editor pane.
type autoScrollEvent struct {
	when time.Time
}

// When satisfies the tcell.Event interface.
func (e *autoScrollEvent) When() time.Time { return e.when }

// treeRefreshEvent is the custom tcell event the background tree-refresh
// goroutine posts every treeRefreshInterval. The main loop reacts by
// asking the file tree to re-read every loaded directory.
type treeRefreshEvent struct {
	when time.Time
}

// When satisfies the tcell.Event interface.
func (e *treeRefreshEvent) When() time.Time { return e.when }

// tabRect remembers where each tab was drawn so click handling can hit-test
// against the actual rendered geometry rather than re-deriving it.
type tabRect struct {
	Index    int
	X, Width int
	CloseX   int // Cell column of the × close button.
}

// clickRecord tracks the last mouse-press location and time so we can
// detect double-clicks (and select the word under the cursor).
type clickRecord struct {
	x, y int
	when time.Time
}

// menuItemDef describes one row in the action modal: the label shown to
// the user, the y-offset it lives at inside the modal, the action it runs
// when clicked, and a predicate that returns true when the action is
// applicable in the current context (so we can dim non-applicable rows).
//
// labelFor is an optional dynamic-label hook: when non-nil, drawMenu calls
// it instead of using the static label string. Used by toggle-style rows
// whose label depends on app state ("Show / Hide file explorer").
type menuItemDef struct {
	label    string
	relY     int
	action   func(*App)
	enabled  func(*App) bool
	labelFor func(*App) string
}

// menuItems is the full list of menu actions, in display order. Their relY
// fields are absolute row offsets inside the modal — see drawMenu for the
// surrounding box drawing.
//
// Layout (mirrored by the divider list in drawMenu):
//
//	  3,4,5     Tab actions   — Save, Save & close, Close
//	  7,8       File actions  — Rename, Delete (target the active tab's file)
//	  10,11,12  Clipboard     — Copy, Cut, Paste
//	  14        View toggle   — Show / Hide file explorer
//	  16        Quit
var menuItems = []menuItemDef{
	{label: "Save", relY: 3, action: (*App).menuSave, enabled: (*App).hasSavableTab},
	{label: "Save & close tab", relY: 4, action: (*App).menuSaveAndClose, enabled: (*App).hasSavableTab},
	{label: "Close tab", relY: 5, action: (*App).menuClose, enabled: (*App).hasTab},
	{label: "Rename file", relY: 7, action: (*App).menuRename, enabled: (*App).hasSavableTab},
	{label: "Delete file", relY: 8, action: (*App).menuDelete, enabled: (*App).hasSavableTab},
	{label: "Copy selection", relY: 10, action: (*App).menuCopy, enabled: (*App).hasSelection},
	{label: "Cut selection", relY: 11, action: (*App).menuCut, enabled: (*App).hasSelection},
	{label: "Paste", relY: 12, action: (*App).menuPaste, enabled: (*App).hasClipboard},
	{relY: 14, action: (*App).menuToggleSidebar, enabled: alwaysTrue, labelFor: (*App).sidebarToggleLabel},
	// Manual tree refresh — disabled in favour of the 10s background poller,
	// but menuRefreshTree below stays so we can re-add the row later by
	// picking a free relY (and adjusting modalHeight + dividers if you
	// want it in its own group).
	// {label: "Refresh tree", relY: ?, action: (*App).menuRefreshTree, enabled: alwaysTrue},
	{label: "Quit editor", relY: 16, action: (*App).menuQuit, enabled: alwaysTrue},
}

// alwaysTrue is the default predicate for actions that are always applicable
// (currently just Quit — which has no preconditions).
func alwaysTrue(*App) bool { return true }

// App is the editor's top-level state holder and event-loop owner.
type App struct {
	screen tcell.Screen
	theme  theme.Theme

	rootDir   string
	tree      *filetree.Tree
	tabs      []*editor.Tab
	activeTab int

	width, height int

	// sidebarShown controls whether the file explorer panel is visible.
	// When false the editor and tab bar fill the whole window.
	sidebarShown bool

	// sidebarWidth is the live width of the file-explorer block (file tree
	// + 1-cell splitter on its right edge), in screen cells. The user can
	// drag the splitter to change it within [minSidebarWidth, width-minEditorAfterDrag].
	sidebarWidth int

	clipBuf      string
	statusMsg    string
	statusUntil  time.Time
	pendingClose int
	dragMode     string // "editor" while a drag-select is active.
	lastClick    clickRecord
	lastTabRects []tabRect

	menuOpen        bool
	hoveredMenuRow  int       // index into menuItems of the row under the mouse, or -1.
	lastEscape      time.Time // timestamp of the previous Esc press, for double-tap detection.

	// Prompt modal — single-line text input with OK / Cancel. Used by
	// Rename and New File. See modals.go for render + event handling.
	promptOpen     bool
	promptTitle    string
	promptHint     string
	promptValue    []rune
	promptCursor   int
	promptScroll   int
	promptCallback func(*App, string)

	// Confirm modal — Yes / No, used by Delete. confirmHover is 0 for No
	// (the safe default) or 1 for Yes.
	confirmOpen     bool
	confirmTitle    string
	confirmMessage  string
	confirmHover    int
	confirmCallback func(*App)

	// Right-click context menu over the file tree.
	contextOpen  bool
	contextX     int
	contextY     int
	contextNode  *filetree.Node
	contextItems []contextItem
	contextHover int

	// Auto-scroll while drag-selecting past the editor's top/bottom edge.
	// lastDragX/Y is the most recent mouse position so the auto-scroll
	// tick can extend the selection at the user's column even though the
	// mouse hasn't moved.
	autoScrollStop chan struct{}
	autoScrollDir  int // -1 up, 0 idle, +1 down
	lastDragX      int
	lastDragY      int

	// treeRefreshStop signals the background tree-refresh goroutine to exit.
	treeRefreshStop chan struct{}

	quit bool
}

// New initialises the screen and mouse, builds the file tree at rootDir,
// and returns an App ready to Run.
func New(rootDir string) (*App, error) {
	scr, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := scr.Init(); err != nil {
		return nil, err
	}
	scr.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents | tcell.MouseMotionEvents)

	th := theme.Default()
	scr.SetStyle(tcell.StyleDefault.Background(th.BG).Foreground(th.Text))
	scr.Clear()

	tree, err := filetree.New(rootDir)
	if err != nil {
		scr.Fini()
		return nil, err
	}

	a := &App{
		screen:         scr,
		theme:          th,
		rootDir:        rootDir,
		tree:           tree,
		pendingClose:   -1,
		hoveredMenuRow: -1,
		sidebarShown:   true,
		sidebarWidth:   defaultSidebarWidth,
	}
	a.flash("Welcome — click a file to open · click  ≡  for the menu")
	a.startTreeRefresh()
	return a, nil
}

// startTreeRefresh launches a goroutine that posts a treeRefreshEvent every
// treeRefreshInterval. The main event loop reacts by calling tree.Refresh,
// which keeps the sidebar in sync with on-disk changes from outside the
// editor (git, mv, another tmux pane, etc.).
func (a *App) startTreeRefresh() {
	a.treeRefreshStop = make(chan struct{})
	stop := a.treeRefreshStop
	scr := a.screen
	go func() {
		ticker := time.NewTicker(treeRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case t := <-ticker.C:
				_ = scr.PostEvent(&treeRefreshEvent{when: t})
			}
		}
	}()
}

// stopTreeRefresh signals the background tree-refresh goroutine to exit.
// Safe to call multiple times.
func (a *App) stopTreeRefresh() {
	if a.treeRefreshStop != nil {
		close(a.treeRefreshStop)
		a.treeRefreshStop = nil
	}
}

// Close releases the terminal back to the user. Always call this before exit.
func (a *App) Close() {
	a.stopTreeRefresh()
	a.stopAutoScroll()
	if a.screen != nil {
		a.screen.Fini()
	}
}

// Run is the editor's main event loop. It blocks on PollEvent, dispatches
// each event, redraws, and exits when a.quit is set.
func (a *App) Run() error {
	a.width, a.height = a.screen.Size()
	a.draw()
	a.screen.Show()

	for !a.quit {
		ev := a.screen.PollEvent()
		if ev == nil {
			break
		}
		a.handleEvent(ev)
		a.draw()
		a.screen.Show()
	}
	return nil
}

// handleEvent routes a tcell event to its specific handler.
func (a *App) handleEvent(ev tcell.Event) {
	switch e := ev.(type) {
	case *tcell.EventResize:
		a.width, a.height = a.screen.Size()
		a.screen.Sync()
	case *tcell.EventKey:
		a.handleKey(e)
	case *tcell.EventMouse:
		a.handleMouse(e)
	case *autoScrollEvent:
		a.handleAutoScroll()
	case *treeRefreshEvent:
		a.tree.Refresh()
		a.reconcileOpenTabsWithDisk()
	}
}

// reconcileOpenTabsWithDisk runs once per background tick. For every
// open tab with a real path it stats the file, compares the on-disk
// mtime to what the tab last knew, and reacts:
//
//   - File missing  → flash once, mark the tab dirty so the user knows.
//   - Disk newer, tab clean → reload the buffer silently, flash success.
//   - Disk newer, tab dirty → leave the buffer alone, flash a warning
//     that saving will overwrite.
//
// Untitled tabs (Path == "") are skipped because there's no disk file to
// reconcile against.
func (a *App) reconcileOpenTabsWithDisk() {
	for _, tab := range a.tabs {
		if tab.Path == "" {
			continue
		}
		info, err := os.Stat(tab.Path)
		if os.IsNotExist(err) {
			if !tab.DiskGone {
				tab.DiskGone = true
				tab.Dirty = true
				a.flash(fmt.Sprintf("%s deleted on disk", filepath.Base(tab.Path)))
			}
			continue
		}
		if err != nil {
			// Permission denied or some other transient stat error — leave
			// the tab as-is rather than spamming the user with a flash.
			continue
		}
		if tab.DiskGone {
			// File reappeared. Force the mtime check below to fire so we
			// either reload or warn about a dirty conflict.
			tab.DiskGone = false
			tab.Mtime = time.Time{}
		}
		if !info.ModTime().After(tab.Mtime) {
			continue // unchanged on disk.
		}
		if tab.Dirty {
			a.flash(fmt.Sprintf("%s changed on disk — your edits will overwrite on save",
				filepath.Base(tab.Path)))
			// Update Mtime so we don't re-flash every tick for the same change.
			tab.Mtime = info.ModTime()
			continue
		}
		if err := tab.Reload(); err != nil {
			a.flash(fmt.Sprintf("%s reload failed: %v", filepath.Base(tab.Path), err))
			continue
		}
		a.flash(fmt.Sprintf("%s reloaded from disk", filepath.Base(tab.Path)))
	}
}

// -----------------------------------------------------------------------------
// Layout helpers
// -----------------------------------------------------------------------------

// sidebarW is the effective width of the sidebar block (file tree +
// splitter): zero when hidden, a.sidebarWidth otherwise. Every layout
// helper and click router goes through this so toggling/resizing the
// panel reshapes the entire UI in one place.
func (a *App) sidebarW() int {
	if !a.sidebarShown {
		return 0
	}
	return a.sidebarWidth
}

// sidebarRect returns the file tree's render rectangle (one column
// narrower than the sidebar block — the rightmost column belongs to the
// resize splitter). Zero width when the sidebar is hidden.
func (a *App) sidebarRect() (x, y, w, h int) {
	sw := a.sidebarW()
	if sw <= 0 {
		return 0, 0, 0, 0
	}
	return 0, 0, sw - 1, a.height - 1
}

// splitterX returns the x coordinate of the resize splitter column, or -1
// when the sidebar is hidden (no splitter to draw or click).
func (a *App) splitterX() int {
	if !a.sidebarShown {
		return -1
	}
	return a.sidebarWidth - 1
}

// resizeSidebar applies the user's desired sidebar width while clamping it
// to a sensible range — the file tree stays wide enough to read names and
// the editor keeps at least minEditorAfterDrag columns. Tiny windows that
// can't satisfy both fall back to the minimum and let the editor shrink.
func (a *App) resizeSidebar(target int) {
	if target < minSidebarWidth {
		target = minSidebarWidth
	}
	max := a.width - minEditorAfterDrag
	if max < minSidebarWidth {
		max = minSidebarWidth
	}
	if target > max {
		target = max
	}
	a.sidebarWidth = target
}

// tabBarRect returns the tab bar's screen rectangle (one row tall).
func (a *App) tabBarRect() (x, y, w, h int) {
	sw := a.sidebarW()
	return sw, 0, a.width - sw, 1
}

// editorRect returns the editor body's screen rectangle (everything to the
// right of the sidebar, between the tab bar and the status bar).
func (a *App) editorRect() (x, y, w, h int) {
	sw := a.sidebarW()
	return sw, 1, a.width - sw, a.height - 2
}

// statusRect returns the status bar's screen rectangle (full-width bottom row).
func (a *App) statusRect() (x, y, w, h int) {
	return 0, a.height - 1, a.width, 1
}

// editorSize returns just the (width, height) of the editor body. Used by
// keyboard handlers that need to compute page-up / page-down deltas.
func (a *App) editorSize() (int, int) {
	_, _, w, h := a.editorRect()
	return w, h
}

// menuButtonRect returns the on-screen rectangle of the ≡ icon in the tab
// bar. Click hit-tests in tabBarClick consult this directly. When the
// sidebar is hidden the icon shifts left to fill the corner.
func (a *App) menuButtonRect() (x, y, w, h int) {
	return a.sidebarW(), 0, menuButtonWidth, 1
}

// menuModalRect returns the on-screen rectangle of the action modal,
// centered in the window. Used both for drawing and for hit-testing.
func (a *App) menuModalRect() (x, y, w, h int) {
	w = modalWidth
	h = modalHeight
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

// -----------------------------------------------------------------------------
// Keyboard
// -----------------------------------------------------------------------------

// handleKey responds to keyboard events. There are intentionally no Ctrl-
// based shortcuts: every action lives behind the ≡ menu so the editor never
// fights the terminal (Ctrl-S/Q flow control) or a tmux/zellij prefix. The
// only "command" key is Esc, which closes the menu.
func (a *App) handleKey(ev *tcell.EventKey) {
	// Secondary modals own the keyboard while they're up. Each handler
	// understands Esc (cancel), Enter (submit / activate), and the keys
	// relevant to its layout (text editing for the prompt, arrow keys for
	// the context menu, etc.).
	if a.promptOpen {
		a.handlePromptKey(ev)
		return
	}
	if a.confirmOpen {
		a.handleConfirmKey(ev)
		return
	}
	if a.contextOpen {
		a.handleContextKey(ev)
		return
	}

	if ev.Key() == tcell.KeyEsc {
		// Esc is the editor's only command key. Behavior:
		//   • menu open  → close it
		//   • menu shut  → open it on the SECOND Esc within doubleEscMs.
		// Single Esc with the menu shut is intentionally a no-op so the
		// key feels harmless to mash.
		if a.menuOpen {
			a.closeMenu()
			a.lastEscape = time.Time{}
			return
		}
		now := time.Now()
		if !a.lastEscape.IsZero() && now.Sub(a.lastEscape) < doubleEscMs {
			a.openMenu()
			a.lastEscape = time.Time{}
			return
		}
		a.lastEscape = now
		return
	}
	// Any other key cancels a pending Esc so a stale half-tap doesn't
	// surprise the user later.
	a.lastEscape = time.Time{}

	// While the menu is open, only the navigation keys do anything —
	// editing keys are blocked, but Down/Up move the highlight and Enter
	// activates the highlighted row.
	if a.menuOpen {
		switch ev.Key() {
		case tcell.KeyDown:
			a.menuMoveSelection(1)
		case tcell.KeyUp:
			a.menuMoveSelection(-1)
		case tcell.KeyEnter:
			a.menuActivate()
		}
		return
	}

	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	extend := ev.Modifiers()&tcell.ModShift != 0

	switch ev.Key() {
	case tcell.KeyUp:
		tab.MoveCursor(-1, 0, extend)
	case tcell.KeyDown:
		tab.MoveCursor(1, 0, extend)
	case tcell.KeyLeft:
		tab.MoveCursor(0, -1, extend)
	case tcell.KeyRight:
		tab.MoveCursor(0, 1, extend)
	case tcell.KeyHome:
		tab.MoveLineHome(extend)
	case tcell.KeyEnd:
		tab.MoveLineEnd(extend)
	case tcell.KeyPgUp:
		_, h := a.editorSize()
		tab.MoveCursor(-h, 0, extend)
	case tcell.KeyPgDn:
		_, h := a.editorSize()
		tab.MoveCursor(h, 0, extend)
	case tcell.KeyEnter:
		tab.InsertString("\n")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		tab.Backspace()
	case tcell.KeyDelete:
		tab.Delete()
	case tcell.KeyTab:
		tab.InsertString(tabIndent)
	case tcell.KeyRune:
		tab.InsertRune(ev.Rune())
	}
}

// -----------------------------------------------------------------------------
// Mouse
// -----------------------------------------------------------------------------

// handleMouse routes a mouse event to whichever panel the cursor is over,
// tracking drag state so a click-drag inside the editor extends the
// selection. When the action menu is open it absorbs all mouse events:
// clicks inside trigger an action, clicks outside dismiss the menu.
func (a *App) handleMouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	btn := ev.Buttons()

	// Secondary modals absorb all mouse input. The order here matches
	// keyboard routing so behavior stays predictable.
	if a.promptOpen {
		a.handlePromptMouse(x, y, btn)
		return
	}
	if a.confirmOpen {
		a.handleConfirmMouse(x, y, btn)
		return
	}
	if a.contextOpen {
		a.handleContextMouse(x, y, btn)
		return
	}

	if a.menuOpen {
		a.updateMenuHover(x, y)
		a.handleMenuMouse(x, y, btn)
		return
	}

	// Right-click handling. Over a file-tree row it opens a small context
	// menu with file-management actions for that node; everywhere else
	// it falls through to the main action menu so users have a redundant
	// mouse-only path to it.
	if btn&tcell.Button3 != 0 {
		if a.tryTreeContextClick(x, y) {
			return
		}
		a.openMenu()
		return
	}

	// Wheel events take priority — they fire even with no button held.
	if btn&tcell.WheelUp != 0 {
		a.scrollAt(x, y, -wheelLines)
		return
	}
	if btn&tcell.WheelDown != 0 {
		a.scrollAt(x, y, wheelLines)
		return
	}

	leftDown := btn&tcell.Button1 != 0

	// Drag continuation: while we're mid-drag in the editor, every event
	// with the button held extends the selection — even if the cursor has
	// wandered out of the editor pane.
	if leftDown && a.dragMode == "editor" {
		a.editorDrag(x, y)
		return
	}

	// Sidebar resize drag: keep the splitter glued to the mouse x so the
	// panel reshapes live as the user drags.
	if leftDown && a.dragMode == "sidebar" {
		a.resizeSidebar(x + 1)
		return
	}

	// Initial press dispatch.
	if leftDown && a.dragMode == "" {
		sw := a.sidebarW()
		splitX := a.splitterX()
		switch {
		case splitX >= 0 && x == splitX:
			a.dragMode = "sidebar"
		case sw > 0 && x < splitX:
			a.sidebarClick(x, y)
		case y == 0:
			a.tabBarClick(x, y)
		case y > 0 && y < a.height-1:
			a.editorPress(x, y)
			a.dragMode = "editor"
		}
		return
	}

	// Button released — exit any drag mode we were in.
	a.dragMode = ""
	a.stopAutoScroll()
}

// handleMenuMouse processes mouse events while the action menu is open.
// Left-click outside the modal closes it; left-click on a row runs that
// row's action (if it is currently enabled).
func (a *App) handleMenuMouse(x, y int, btn tcell.ButtonMask) {
	if btn&tcell.Button1 == 0 {
		return
	}
	mx, my, mw, mh := a.menuModalRect()
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		a.closeMenu()
		return
	}
	relY := y - my
	for _, item := range menuItems {
		if item.relY != relY {
			continue
		}
		if item.enabled(a) {
			item.action(a)
		}
		return
	}
}

// scrollAt scrolls whichever panel the (x, y) cursor is over.
func (a *App) scrollAt(x, y, delta int) {
	if sw := a.sidebarW(); sw > 0 && x < sw {
		a.tree.Scroll(delta)
		return
	}
	if y > 0 && y < a.height-1 {
		if t := a.activeTabPtr(); t != nil {
			t.Scroll(delta)
		}
	}
}

// tryTreeContextClick opens the right-click context menu when (x, y) lands
// on a tree row. Returns true if it consumed the event so the caller knows
// not to fall back to the main action menu.
func (a *App) tryTreeContextClick(x, y int) bool {
	sw := a.sidebarW()
	if sw <= 0 {
		return false
	}
	splitX := a.splitterX()
	if x >= splitX {
		return false
	}
	sx, sy, _, _ := a.sidebarRect()
	n, ok := a.tree.HitTest(x-sx, y-sy)
	if !ok {
		return false
	}
	a.openTreeContext(n, x, y)
	return true
}

// sidebarClick toggles a directory or opens a file when the user clicks a
// row in the file tree.
func (a *App) sidebarClick(x, y int) {
	sx, sy, _, _ := a.sidebarRect()
	n, ok := a.tree.HitTest(x-sx, y-sy)
	if !ok {
		return
	}
	if n.IsDir {
		a.tree.Toggle(n)
		return
	}
	a.openFile(n.Path)
}

// tabBarClick dispatches clicks in the tab bar: the leftmost menuButtonWidth
// cells open the action menu; remaining cells switch or close tabs based on
// where the click landed within their rendered geometry.
func (a *App) tabBarClick(x, _ int) {
	sw := a.sidebarW()
	if x >= sw && x < sw+menuButtonWidth {
		a.openMenu()
		return
	}
	for _, r := range a.lastTabRects {
		if x >= r.X && x < r.X+r.Width {
			if x == r.CloseX {
				a.requestCloseTab(r.Index)
				return
			}
			a.activeTab = r.Index
			a.pendingClose = -1
			return
		}
	}
}

// editorPress handles the initial mouse press inside the editor — placing
// the caret, optionally selecting a word on double-click.
func (a *App) editorPress(x, y int) {
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	ex, ey, ew, eh := a.editorRect()
	pos, ok := tab.HitTest(x-ex, y-ey, ew, eh)
	if !ok {
		return
	}

	now := time.Now()
	if a.lastClick.x == x && a.lastClick.y == y && now.Sub(a.lastClick.when) < doubleClickMs {
		a.selectWordAt(tab, pos)
		a.lastClick = clickRecord{} // prevent triple-click from selecting nothing.
		return
	}
	a.lastClick = clickRecord{x: x, y: y, when: now}
	tab.MoveCursorTo(pos, false)
}

// editorDrag extends the selection during a click-drag inside the editor.
// (x, y) is clamped to the editor rect so dragging into another pane still
// extends the selection sensibly. When the mouse passes above or below the
// editor we engage auto-scroll so the user can select content that's not
// yet on screen — same feel as VS Code or any GUI text editor.
func (a *App) editorDrag(x, y int) {
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	ex, ey, ew, eh := a.editorRect()

	// Remember where the mouse is so the auto-scroll tick can extend the
	// selection at this column even while the mouse stops moving.
	a.lastDragX = x
	a.lastDragY = y

	// Edge detection: outside the editor's vertical bounds turns on
	// auto-scroll; back inside turns it off.
	switch {
	case y < ey:
		a.startAutoScroll(-1)
	case y >= ey+eh:
		a.startAutoScroll(1)
	default:
		a.stopAutoScroll()
	}

	// Clamp the mouse into the editor and extend the selection there.
	localX := x - ex
	localY := y - ey
	if localX < 0 {
		localX = 0
	}
	if localY < 0 {
		localY = 0
	}
	if localX >= ew {
		localX = ew - 1
	}
	if localY >= eh {
		localY = eh - 1
	}
	pos, ok := tab.HitTest(localX, localY, ew, eh)
	if !ok {
		return
	}
	tab.MoveCursorTo(pos, true)
}

// startAutoScroll begins a timer goroutine that posts autoScrollEvents at
// autoScrollTick intervals so the editor keeps scrolling while the user
// holds the mouse past an edge. dir is -1 (up) or +1 (down). Calling with
// the same direction is a no-op so we don't restart the timer on every
// drag motion event.
func (a *App) startAutoScroll(dir int) {
	if a.autoScrollDir == dir {
		return
	}
	a.stopAutoScroll()
	a.autoScrollDir = dir
	a.autoScrollStop = make(chan struct{})
	stop := a.autoScrollStop
	scr := a.screen
	go func() {
		ticker := time.NewTicker(autoScrollTick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case t := <-ticker.C:
				_ = scr.PostEvent(&autoScrollEvent{when: t})
			}
		}
	}()
}

// stopAutoScroll signals the auto-scroll goroutine to exit (idempotent).
func (a *App) stopAutoScroll() {
	if a.autoScrollStop != nil {
		close(a.autoScrollStop)
		a.autoScrollStop = nil
	}
	a.autoScrollDir = 0
}

// handleAutoScroll runs once per autoScrollEvent: nudge the viewport in the
// armed direction and extend the selection to the edge row at the user's
// last known mouse column. Bails out (and stops the timer) if anything
// suggests the user is no longer drag-selecting (button released, menu
// opened, no active tab).
func (a *App) handleAutoScroll() {
	if a.autoScrollDir == 0 || a.dragMode != "editor" || a.anyModalOpen() {
		a.stopAutoScroll()
		return
	}
	tab := a.activeTabPtr()
	if tab == nil {
		a.stopAutoScroll()
		return
	}
	tab.Scroll(a.autoScrollDir)

	ex, _, ew, eh := a.editorRect()
	localX := a.lastDragX - ex
	if localX < 0 {
		localX = 0
	}
	if localX >= ew {
		localX = ew - 1
	}
	localY := eh - 1
	if a.autoScrollDir < 0 {
		localY = 0
	}
	pos, ok := tab.HitTest(localX, localY, ew, eh)
	if !ok {
		return
	}
	tab.MoveCursorTo(pos, true)
}

// selectWordAt selects the word under the buffer position p (or does
// nothing if p sits in whitespace / punctuation).
func (a *App) selectWordAt(tab *editor.Tab, p editor.Position) {
	line := tab.Buffer.LineRunes(p.Line)
	if len(line) == 0 {
		return
	}
	start := p.Col
	if start > len(line) {
		start = len(line)
	}
	for start > 0 && isWordChar(line[start-1]) {
		start--
	}
	end := p.Col
	for end < len(line) && isWordChar(line[end]) {
		end++
	}
	if start == end {
		return
	}
	tab.Anchor = editor.Position{Line: p.Line, Col: start}
	tab.Cursor = editor.Position{Line: p.Line, Col: end}
}

// isWordChar reports whether r is part of a "word" for double-click select.
// Intentionally simple ASCII-ish definition; covers the common cases.
func isWordChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// -----------------------------------------------------------------------------
// Tab + clipboard actions
// -----------------------------------------------------------------------------

// activeTabPtr returns the currently active *editor.Tab, or nil when there
// are no tabs open.
func (a *App) activeTabPtr() *editor.Tab {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return nil
	}
	return a.tabs[a.activeTab]
}

// flash sets a transient status message that displays for statusFlashFor
// before the status bar reverts to the active file's info.
func (a *App) flash(msg string) {
	a.statusMsg = msg
	a.statusUntil = time.Now().Add(statusFlashFor)
}

// openFile opens the file at path in a new tab — or switches to it if it is
// already open in another tab. Errors are surfaced as a flash message.
func (a *App) openFile(path string) {
	for i, t := range a.tabs {
		if t.Path == path {
			a.activeTab = i
			return
		}
	}
	t, err := editor.NewTab(path)
	if err != nil {
		a.flash(fmt.Sprintf("Error: %v", err))
		return
	}
	a.tabs = append(a.tabs, t)
	a.activeTab = len(a.tabs) - 1
	a.flash(fmt.Sprintf("Opened %s", filepath.Base(path)))
}

// saveActiveTab writes the active tab's buffer to disk.
func (a *App) saveActiveTab() {
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	if tab.Path == "" {
		a.flash("Saving untitled tabs is not supported yet")
		return
	}
	if err := tab.Save(); err != nil {
		a.flash(fmt.Sprintf("Save failed: %v", err))
		return
	}
	a.flash(fmt.Sprintf("Saved %s", filepath.Base(tab.Path)))
}

// requestCloseTab closes the tab at idx, or arms a pendingClose state when
// the tab is dirty so that the next close-attempt actually closes it. This
// is the editor's mouse-friendly substitute for a save-or-discard modal.
func (a *App) requestCloseTab(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	if a.tabs[idx].Dirty && a.pendingClose != idx {
		a.pendingClose = idx
		a.flash("Unsaved changes — click × again to discard, or use Save & close")
		return
	}
	a.closeTab(idx)
	a.pendingClose = -1
}

// closeTab removes the tab at idx without any dirty-check.
func (a *App) closeTab(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
	if a.activeTab >= len(a.tabs) {
		a.activeTab = len(a.tabs) - 1
	}
	if a.activeTab < 0 {
		a.activeTab = 0
	}
}

// copySelection puts the active tab's selection on the system clipboard
// (via OSC 52) and into the editor's internal clipboard.
func (a *App) copySelection() {
	tab := a.activeTabPtr()
	if tab == nil || !tab.HasSelection() {
		return
	}
	txt := tab.SelectionText()
	a.clipBuf = txt
	if err := clipboard.CopyToSystem(txt); err != nil {
		a.flash("Copied (system clipboard unavailable)")
		return
	}
	a.flash("Copied")
}

// cutSelection copies the selection then deletes it.
func (a *App) cutSelection() {
	tab := a.activeTabPtr()
	if tab == nil || !tab.HasSelection() {
		return
	}
	a.copySelection()
	tab.DeleteSelection()
}

// pasteClipboard inserts the editor's internal clipboard at the cursor.
// We can't read the system clipboard from a TUI, so external pastes have
// to come in through the user's terminal paste (Cmd-V / right-click paste).
func (a *App) pasteClipboard() {
	tab := a.activeTabPtr()
	if tab == nil {
		return
	}
	if a.clipBuf == "" {
		a.flash("Internal clipboard empty — paste from your terminal (Cmd-V)")
		return
	}
	tab.InsertString(a.clipBuf)
}

// -----------------------------------------------------------------------------
// Action menu
// -----------------------------------------------------------------------------

// openMenu shows the action modal. While it is up, the editor doesn't
// receive typed keys, and clicks outside the modal dismiss it. We pre-
// select the first enabled row so Down/Up/Enter keyboard navigation has
// somewhere sensible to start.
func (a *App) openMenu() {
	a.closeAllModals()
	a.menuOpen = true
	a.menuMoveSelection(1)
}

// menuMoveSelection advances hoveredMenuRow to the next (dir=+1) or
// previous (dir=-1) enabled menu item, wrapping around at the ends so the
// list feels continuous. Disabled items and dividers are skipped. If no
// item is currently enabled hoveredMenuRow stays -1.
func (a *App) menuMoveSelection(dir int) {
	n := len(menuItems)
	if n == 0 {
		return
	}
	start := a.hoveredMenuRow
	if start < 0 {
		// No current selection — start one step before the first row (for
		// Down) or one past the last (for Up) so the loop lands on the
		// first/last enabled item.
		if dir > 0 {
			start = -1
		} else {
			start = n
		}
	}
	for i := 1; i <= n; i++ {
		idx := ((start+dir*i)%n + n) % n
		if menuItems[idx].enabled(a) {
			a.hoveredMenuRow = idx
			return
		}
	}
	a.hoveredMenuRow = -1
}

// menuActivate runs the currently-highlighted menu item, if any. It's the
// keyboard-Enter equivalent of clicking a row.
func (a *App) menuActivate() {
	if a.hoveredMenuRow < 0 || a.hoveredMenuRow >= len(menuItems) {
		return
	}
	item := menuItems[a.hoveredMenuRow]
	if !item.enabled(a) {
		return
	}
	item.action(a)
}

// closeMenu hides the action modal without running any action.
func (a *App) closeMenu() {
	a.menuOpen = false
	a.hoveredMenuRow = -1
}

// updateMenuHover sets hoveredMenuRow to the index of the enabled menu row
// at (x, y), or to -1 when the mouse is over a disabled row, a divider, the
// title, or anywhere outside the modal.
func (a *App) updateMenuHover(x, y int) {
	a.hoveredMenuRow = -1
	mx, my, mw, mh := a.menuModalRect()
	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		return
	}
	relY := y - my
	for i, item := range menuItems {
		if item.relY == relY && item.enabled(a) {
			a.hoveredMenuRow = i
			return
		}
	}
}

// hasTab reports whether there is an active tab to act on.
func (a *App) hasTab() bool { return a.activeTabPtr() != nil }

// hasSavableTab reports whether the active tab is one we can persist —
// it must exist and have a path on disk (untitled tabs aren't yet supported).
func (a *App) hasSavableTab() bool {
	t := a.activeTabPtr()
	return t != nil && t.Path != ""
}

// hasSelection reports whether the active tab has a non-empty selection.
func (a *App) hasSelection() bool {
	t := a.activeTabPtr()
	return t != nil && t.HasSelection()
}

// hasClipboard reports whether the editor's internal clipboard has content
// to paste.
func (a *App) hasClipboard() bool { return a.clipBuf != "" }

// menuSave runs the Save action and dismisses the menu.
func (a *App) menuSave() {
	a.closeMenu()
	a.saveActiveTab()
}

// menuSaveAndClose saves the active tab and then closes it. If the save
// fails the close is aborted so we don't lose the user's edits.
func (a *App) menuSaveAndClose() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return
	}
	if err := tab.Save(); err != nil {
		a.flash(fmt.Sprintf("Save failed: %v", err))
		return
	}
	a.flash(fmt.Sprintf("Saved %s — closed", filepath.Base(tab.Path)))
	a.closeTab(a.activeTab)
}

// menuClose closes the active tab via the same dirty-tab confirmation flow
// used by clicking the × on the tab.
func (a *App) menuClose() {
	a.closeMenu()
	a.requestCloseTab(a.activeTab)
}

// menuCopy copies the current selection.
func (a *App) menuCopy() {
	a.closeMenu()
	a.copySelection()
}

// menuCut cuts the current selection.
func (a *App) menuCut() {
	a.closeMenu()
	a.cutSelection()
}

// menuPaste pastes the editor's internal clipboard at the cursor.
func (a *App) menuPaste() {
	a.closeMenu()
	a.pasteClipboard()
}

// menuRefreshTree forces an immediate sidebar reload. Currently unwired
// from the menu — the 10s background poller covers the common case — but
// the method is kept so re-adding the menu row (see menuItems) only
// requires uncommenting one line.
func (a *App) menuRefreshTree() {
	a.closeMenu()
	a.tree.Refresh()
	a.flash("File tree refreshed")
}

// menuToggleSidebar shows or hides the file explorer panel. The editor and
// tab bar reflow to fill the freed cells when the panel is hidden, and
// snap back when it returns.
func (a *App) menuToggleSidebar() {
	a.closeMenu()
	a.sidebarShown = !a.sidebarShown
}

// sidebarToggleLabel returns the label the toggle row should display given
// the current sidebar state. Drawn dynamically by drawMenu.
func (a *App) sidebarToggleLabel() string {
	if a.sidebarShown {
		return "Hide file explorer"
	}
	return "Show file explorer"
}

// menuQuit exits the editor.
func (a *App) menuQuit() {
	a.closeMenu()
	a.quit = true
}

// -----------------------------------------------------------------------------
// Drawing
// -----------------------------------------------------------------------------

// draw paints the entire screen. Called once per event in the main loop.
// The action modal — if open — is drawn last so it sits on top of everything.
func (a *App) draw() {
	a.screen.Clear()

	if a.width < minWidth || a.height < minHeight {
		a.drawTooSmall()
		return
	}

	if a.sidebarShown {
		sx, sy, sw, sh := a.sidebarRect()
		a.tree.Render(a.screen, a.theme, sx, sy, sw, sh)
		a.drawSplitter()
	}

	a.drawTabBar()

	if tab := a.activeTabPtr(); tab != nil {
		ex, ey, ew, eh := a.editorRect()
		tab.Render(a.screen, a.theme, ex, ey, ew, eh)
	} else {
		a.drawEmptyEditor()
	}

	a.drawStatusBar()

	// Modal layering, bottom-up. Only one of these is open at a time
	// (closeAllModals enforces it), but the order still matters so a
	// future contributor can't accidentally double-open them.
	if a.menuOpen {
		a.drawMenu()
	}
	if a.contextOpen {
		a.drawContext()
	}
	if a.promptOpen {
		a.drawPrompt()
	}
	if a.confirmOpen {
		a.drawConfirm()
	}
}

// layoutTabs computes the tabRect geometry for every tab. Tabs are rendered
// to the right of the menu button, in the format:
//
//	" <dirty><name> × " — a single space pad, two-cell dirty slot
//	(dot+space, or two spaces), the file name, a separator space, the close
//	×, and a trailing space.
func (a *App) layoutTabs() []tabRect {
	out := make([]tabRect, 0, len(a.tabs))
	cursor := a.sidebarW() + menuButtonWidth
	for i, t := range a.tabs {
		nameLen := len([]rune(t.DisplayName()))
		w := 1 + 2 + nameLen + 1 + 1 + 1 // pad+dirty+name+space+×+pad
		out = append(out, tabRect{
			Index:  i,
			X:      cursor,
			Width:  w,
			CloseX: cursor + 1 + 2 + nameLen + 1,
		})
		cursor += w
	}
	return out
}

// drawTabBar paints the tab bar across the top of the editor area: first
// the menu button (≡), then any open tabs.
func (a *App) drawTabBar() {
	tx, ty, tw, _ := a.tabBarRect()
	barStyle := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(a.theme.Muted)
	for cx := tx; cx < tx+tw; cx++ {
		a.screen.SetContent(cx, ty, ' ', nil, barStyle)
	}

	a.drawMenuButton()

	rects := a.layoutTabs()
	a.lastTabRects = rects
	for _, r := range rects {
		active := r.Index == a.activeTab
		bg := a.theme.SidebarBG
		fg := a.theme.Muted
		if active {
			bg = a.theme.BG
			fg = a.theme.Text
		}
		st := tcell.StyleDefault.Background(bg).Foreground(fg)
		if active {
			st = st.Bold(true)
		}
		// Background.
		for cx := r.X; cx < r.X+r.Width; cx++ {
			if cx >= tx+tw {
				break
			}
			a.screen.SetContent(cx, ty, ' ', nil, st)
		}
		tab := a.tabs[r.Index]
		col := r.X + 1
		if tab.Dirty {
			a.screen.SetContent(col, ty, '●', nil, st.Foreground(a.theme.Modified))
		}
		col += 2 // skip dirty slot.
		for _, ru := range tab.DisplayName() {
			if col >= tx+tw {
				break
			}
			a.screen.SetContent(col, ty, ru, nil, st)
			col++
		}
		col++ // separator space before ×
		if col < tx+tw {
			closeStyle := st.Foreground(a.theme.Muted)
			if active {
				closeStyle = st.Foreground(a.theme.Subtle)
			}
			a.screen.SetContent(col, ty, '×', nil, closeStyle)
		}
	}
}

// drawSplitter paints a 1-column vertical line at the right edge of the
// sidebar. Idle it sits in Subtle grey; while the user is dragging it
// brightens to Accent so the active grab handle is unmistakable.
func (a *App) drawSplitter() {
	x := a.splitterX()
	if x < 0 {
		return
	}
	fg := a.theme.Subtle
	if a.dragMode == "sidebar" {
		fg = a.theme.Accent
	}
	style := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(fg)
	for y := 0; y < a.height-1; y++ {
		a.screen.SetContent(x, y, '│', nil, style)
	}
}

// drawMenuButton paints the ≡ icon in the leftmost cells of the tab bar.
// It's deliberately big and accent-coloured so it reads as a button.
func (a *App) drawMenuButton() {
	mx, my, mw, _ := a.menuButtonRect()
	bg := a.theme.SidebarBG
	fg := a.theme.Accent
	if a.menuOpen {
		// Visually press the button while the menu is up.
		bg = a.theme.Accent
		fg = a.theme.BG
	}
	style := tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true)
	for cx := mx; cx < mx+mw; cx++ {
		a.screen.SetContent(cx, my, ' ', nil, style)
	}
	// Center the ≡ glyph in the button's mw cells.
	a.screen.SetContent(mx+mw/2, my, '≡', nil, style)
}

// drawEmptyEditor paints the placeholder shown when no tabs are open.
func (a *App) drawEmptyEditor() {
	ex, ey, ew, eh := a.editorRect()
	bg := a.theme.BG
	muted := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	bold := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text).Bold(true)
	for cy := ey; cy < ey+eh; cy++ {
		for cx := ex; cx < ex+ew; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, muted)
		}
	}
	cy := ey + eh/2
	msg1 := "No file open"
	msg2 := "Click a file in the tree, or  ≡  for the menu"
	cx1 := ex + (ew-len([]rune(msg1)))/2
	for i, r := range msg1 {
		a.screen.SetContent(cx1+i, cy-1, r, nil, bold)
	}
	cx2 := ex + (ew-len([]rune(msg2)))/2
	for i, r := range msg2 {
		a.screen.SetContent(cx2+i, cy+1, r, nil, muted)
	}
	a.screen.HideCursor()
}

// drawStatusBar paints the bottom status bar.
func (a *App) drawStatusBar() {
	sx, sy, sw, _ := a.statusRect()
	bg := a.theme.StatusBG
	fg := a.theme.BG
	style := tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true)
	for cx := sx; cx < sx+sw; cx++ {
		a.screen.SetContent(cx, sy, ' ', nil, style)
	}

	// Left-side text: status flash, file info, or root dir.
	var left string
	if time.Now().Before(a.statusUntil) && a.statusMsg != "" {
		left = " " + a.statusMsg
	} else if tab := a.activeTabPtr(); tab != nil {
		lang := detectLangLabel(tab.Path)
		dirty := ""
		if tab.Dirty {
			dirty = " · ●"
		}
		left = fmt.Sprintf(" %s · Ln %d, Col %d · %d lines%s",
			lang, tab.Cursor.Line+1, tab.Cursor.Col+1, tab.Buffer.LineCount(), dirty)
	} else {
		left = " " + filepath.Base(a.rootDir)
	}
	drawStatusText(a.screen, sx, sy, sw, left, style)
}

// drawTooSmall paints a centred error message when the terminal window is
// smaller than the editor's minimum supported size.
func (a *App) drawTooSmall() {
	style := tcell.StyleDefault.Background(a.theme.BG).Foreground(a.theme.Error).Bold(true)
	for cy := 0; cy < a.height; cy++ {
		for cx := 0; cx < a.width; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil,
				tcell.StyleDefault.Background(a.theme.BG))
		}
	}
	msg := "Window too small — please resize"
	cy := a.height / 2
	cx := (a.width - len([]rune(msg))) / 2
	if cx < 0 {
		cx = 0
	}
	for i, r := range msg {
		if cx+i >= a.width {
			break
		}
		a.screen.SetContent(cx+i, cy, r, nil, style)
	}
	a.screen.HideCursor()
}

// drawMenu renders the action modal centered in the window. Each row in
// menuItems is drawn at its declared relY; horizontal dividers separate
// the action groups (file ops / clipboard ops / quit).
func (a *App) drawMenu() {
	mx, my, mw, mh := a.menuModalRect()

	bg := a.theme.LineHL
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	borderStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Subtle)
	titleStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	chevronStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.AccentSoft)

	// Fill the entire modal rect with the modal bg.
	for cy := my; cy < my+mh; cy++ {
		for cx := mx; cx < mx+mw; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	// Outer border.
	a.screen.SetContent(mx, my, '┌', nil, borderStyle)
	a.screen.SetContent(mx+mw-1, my, '┐', nil, borderStyle)
	a.screen.SetContent(mx, my+mh-1, '└', nil, borderStyle)
	a.screen.SetContent(mx+mw-1, my+mh-1, '┘', nil, borderStyle)
	for cx := mx + 1; cx < mx+mw-1; cx++ {
		a.screen.SetContent(cx, my, '─', nil, borderStyle)
		a.screen.SetContent(cx, my+mh-1, '─', nil, borderStyle)
	}
	for cy := my + 1; cy < my+mh-1; cy++ {
		a.screen.SetContent(mx, cy, '│', nil, borderStyle)
		a.screen.SetContent(mx+mw-1, cy, '│', nil, borderStyle)
	}

	// Horizontal dividers between action groups. See menuItems above for
	// the matching row layout.
	for _, dy := range []int{2, 6, 9, 13, 15} {
		cy := my + dy
		a.screen.SetContent(mx, cy, '├', nil, borderStyle)
		a.screen.SetContent(mx+mw-1, cy, '┤', nil, borderStyle)
		for cx := mx + 1; cx < mx+mw-1; cx++ {
			a.screen.SetContent(cx, cy, '─', nil, borderStyle)
		}
	}

	// Title row: " Menu" on the left, "esc " on the right.
	drawAt(a.screen, mx+1, my+1, " Menu", titleStyle)
	hint := "esc "
	drawAt(a.screen, mx+mw-1-len([]rune(hint)), my+1, hint, mutedStyle)

	// Version stamp baked into the bottom border, right-aligned. A small
	// pad of dashes is left between the version text and the corner so it
	// reads as part of the frame rather than a label awkwardly butted up
	// against the border.
	verLabel := " v" + version.Version + " "
	verLen := len([]rune(verLabel))
	verX := mx + mw - 2 - verLen
	if verX > mx+1 {
		drawAt(a.screen, verX, my+mh-1, verLabel, mutedStyle)
	}

	// Action rows. Hovered (enabled) rows get a tinted full-width
	// background so they read like a hovered button in a GUI menu.
	hoverBg := a.theme.Selection
	hoverStyle := tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.Text).Bold(true)
	hoverChevStyle := tcell.StyleDefault.Background(hoverBg).Foreground(a.theme.AccentSoft).Bold(true)
	for i, item := range menuItems {
		cy := my + item.relY
		enabled := item.enabled(a)
		hovered := enabled && i == a.hoveredMenuRow

		var labelStyle, chevStyle tcell.Style
		switch {
		case hovered:
			// Paint the row's interior with the hover background first.
			for cx := mx + 1; cx < mx+mw-1; cx++ {
				a.screen.SetContent(cx, cy, ' ', nil, hoverStyle)
			}
			labelStyle = hoverStyle
			chevStyle = hoverChevStyle
		case enabled:
			labelStyle = bgStyle
			chevStyle = chevronStyle
		default:
			labelStyle = mutedStyle
			chevStyle = mutedStyle
		}
		// Dynamic label (e.g. the file-explorer toggle row) takes precedence
		// over the static one when present.
		label := item.label
		if item.labelFor != nil {
			label = item.labelFor(a)
		}
		drawAt(a.screen, mx+2, cy, "▸", chevStyle)
		drawAt(a.screen, mx+4, cy, label, labelStyle)
	}

	a.screen.HideCursor()
}

// drawStatusText writes s left-aligned into the status bar at (x, y) with a
// max width of maxW cells. Truncates rather than wraps.
func drawStatusText(scr tcell.Screen, x, y, maxW int, s string, st tcell.Style) {
	col := 0
	for _, r := range s {
		if col >= maxW {
			return
		}
		scr.SetContent(x+col, y, r, nil, st)
		col++
	}
}

// drawAt writes s starting at (x, y) without bounds checking. Callers are
// expected to keep the string within the rectangle they're drawing into.
func drawAt(scr tcell.Screen, x, y int, s string, st tcell.Style) {
	col := 0
	for _, r := range s {
		scr.SetContent(x+col, y, r, nil, st)
		col++
	}
}

// detectLangLabel returns a short label for the active file's language —
// just the file extension, or "text" when there is no path or extension.
func detectLangLabel(path string) string {
	if path == "" {
		return "text"
	}
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "" {
		return "text"
	}
	return ext
}
