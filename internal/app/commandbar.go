// =============================================================================
// File: internal/app/commandbar.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-13
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// commandbar.go owns the ":" command line — a 1-row input strip above the
// status bar, opened with the Esc-: leader or the ≡ menu. Commands run
// against a small built-in registry; the first command is cd, which
// re-roots the editor (file tree, project index, git status) at the given
// directory the way :cd does in nvim. The input gets bash-style Tab
// completion for directory paths, with the suggestion list popping up
// above the bar.

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/finder"
)

// commandSuggestRows caps how many completion candidates render above the
// input row. Five is plenty for a quick scan and keeps the popup from
// covering most of a short terminal.
const commandSuggestRows = 5

// openCommandBar shows the command line with an empty input. Called from
// the Esc-: leader and the ≡ menu; both paths funnel through closeAllModals
// first so the bar can never stack on another modal.
func (a *App) openCommandBar() {
	a.closeAllModals()
	a.commandOpen = true
	a.commandValue = nil
	a.commandCursor = 0
	a.commandScroll = 0
	a.commandSelected = -1 // no suggestion adopted yet; Down/Up pick the first
	a.commandCycling = false
	a.commandRefresh()
}

// menuCommandBar is the ≡ menu entry point for the command bar.
func (a *App) menuCommandBar() {
	a.closeMenu()
	a.openCommandBar()
}

// closeCommandBar dismisses the bar and clears its transient state so a
// future open starts from a clean slate (same convention as the find bar).
func (a *App) closeCommandBar() {
	a.commandOpen = false
	a.commandValue = nil
	a.commandCursor = 0
	a.commandScroll = 0
	a.commandSuggestion = nil
	a.commandSelected = -1
	a.commandCycling = false
	a.commandHint = ""
}

// handleCommandKey processes keyboard input while the command bar is open:
// printable runes edit the line, Tab / arrows drive completion, Enter runs
// the command, Esc dismisses.
func (a *App) handleCommandKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEsc:
		a.closeCommandBar()
	case tcell.KeyEnter:
		a.commandSubmit()
	case tcell.KeyTab:
		a.commandComplete()
	case tcell.KeyUp:
		a.commandCycle(-1)
	case tcell.KeyDown:
		a.commandCycle(1)
	case tcell.KeyLeft:
		if a.commandCursor > 0 {
			a.commandCursor--
		}
	case tcell.KeyRight:
		if a.commandCursor < len(a.commandValue) {
			a.commandCursor++
		}
	case tcell.KeyHome:
		a.commandCursor = 0
	case tcell.KeyEnd:
		a.commandCursor = len(a.commandValue)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.commandCursor > 0 {
			a.commandValue = append(a.commandValue[:a.commandCursor-1], a.commandValue[a.commandCursor:]...)
			a.commandCursor--
			a.commandCycling = false
			a.commandRefresh()
		}
	case tcell.KeyDelete:
		if a.commandCursor < len(a.commandValue) {
			a.commandValue = append(a.commandValue[:a.commandCursor], a.commandValue[a.commandCursor+1:]...)
			a.commandCycling = false
			a.commandRefresh()
		}
	case tcell.KeyRune:
		r := ev.Rune()
		if r < 0x20 {
			return
		}
		next := make([]rune, 0, len(a.commandValue)+1)
		next = append(next, a.commandValue[:a.commandCursor]...)
		next = append(next, r)
		next = append(next, a.commandValue[a.commandCursor:]...)
		a.commandValue = next
		a.commandCursor++
		a.commandCycling = false
		a.commandRefresh()
	}
}

// handleCommandMouse routes clicks while the command bar is open: a click on
// a suggestion row adopts it as the last token, a click on the input row
// moves the caret, and a click anywhere else dismisses the bar.
func (a *App) handleCommandMouse(x, y int, btn tcell.ButtonMask) {
	if btn&tcell.Button1 == 0 {
		return
	}
	bx, by, bw, _ := a.commandBarRect()
	if y < by && y >= by-commandSuggestRows && x >= bx && x < bx+bw {
		// Suggestion row: index 0 is the row directly above the input.
		idx := by - 1 - y
		if idx >= 0 && idx < len(a.commandSuggestion) {
			a.commandCycling = true
			a.replaceLastToken(a.commandSuggestion[idx])
			a.commandRefresh()
		}
		return
	}
	if y == by {
		col := x - (bx + runeLen(" : ")) + a.commandScroll
		if col < 0 {
			col = 0
		}
		if col > len(a.commandValue) {
			col = len(a.commandValue)
		}
		a.commandCursor = col
		return
	}
	a.closeCommandBar()
}

// commandBarRect returns the on-screen rectangle of the command bar's input
// row: the row directly above the status bar. The suggestion popup grows
// upward from there and drawCommandBar clips it against the window top.
func (a *App) commandBarRect() (x, y, w, h int) {
	sw := a.sidebarW()
	return sw, a.height - 2, a.width - sw, 1
}

// drawCommandBar renders the command bar: a " : " input row above the
// status bar with up to commandSuggestRows completion candidates stacked
// above it. The selected candidate is highlighted; the hint (match count,
// error text) sits right-aligned on the input row and is dropped first on
// narrow windows.
func (a *App) drawCommandBar() {
	if !a.commandOpen {
		return
	}
	bx, by, bw, _ := a.commandBarRect()

	bg := a.theme.LineHL
	barStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Text)
	labelStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Accent).Bold(true)
	mutedStyle := tcell.StyleDefault.Background(bg).Foreground(a.theme.Muted)
	selStyle := tcell.StyleDefault.Background(a.theme.Selection).Foreground(a.theme.Text).Bold(true)

	// Suggestion rows, nearest to the input first.
	for i, s := range a.commandSuggestion {
		if i >= commandSuggestRows {
			break
		}
		cy := by - 1 - i
		if cy < 0 {
			break
		}
		for cx := bx; cx < bx+bw; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, barStyle)
		}
		st := mutedStyle
		marker := " "
		if i == a.commandSelected%len(a.commandSuggestion) {
			st = selStyle
			marker = "▸"
		}
		drawAt(a.screen, bx, cy, marker, st)
		label := s
		if runeLen(label) > bw-2 {
			label = string([]rune(label)[:bw-2])
		}
		drawAt(a.screen, bx+1, cy, label, st)
	}

	// Input row.
	for cx := bx; cx < bx+bw; cx++ {
		a.screen.SetContent(cx, by, ' ', nil, barStyle)
	}
	label := " : "
	drawAt(a.screen, bx, by, label, labelStyle)
	inputStart := bx + runeLen(label)

	hint := ""
	if a.commandHint != "" {
		hint = " " + a.commandHint + "  "
	}
	rightStart := bx + bw
	if bw > runeLen(label)+runeLen(hint)+10 {
		rightStart -= runeLen(hint)
		drawAt(a.screen, rightStart, by, hint, mutedStyle)
	}

	inputEnd := rightStart - 1
	if inputEnd <= inputStart {
		inputEnd = bx + bw - 1
	}
	width := inputEnd - inputStart
	if width < 1 {
		width = 1
	}
	a.adjustCommandScroll(width)
	for i := 0; i < width; i++ {
		idx := a.commandScroll + i
		if idx >= len(a.commandValue) {
			break
		}
		a.screen.SetContent(inputStart+i, by, a.commandValue[idx], nil, barStyle)
	}
	caret := inputStart + (a.commandCursor - a.commandScroll)
	if caret >= inputStart && caret <= inputEnd {
		a.screen.ShowCursor(caret, by)
	}
}

// adjustCommandScroll keeps the caret within the visible window of the
// command input by sliding commandScroll left or right as needed.
func (a *App) adjustCommandScroll(width int) {
	if width <= 0 {
		a.commandScroll = 0
		return
	}
	if a.commandCursor < a.commandScroll {
		a.commandScroll = a.commandCursor
	}
	if a.commandCursor >= a.commandScroll+width {
		a.commandScroll = a.commandCursor - width + 1
	}
	if a.commandScroll < 0 {
		a.commandScroll = 0
	}
}

// commandRefresh recomputes the completion candidates and hint for the
// current line. Called on every keystroke; the directory listing behind it
// is cheap (one ReadDir of the deepest prefix directory). While the user
// is cycling with Tab / arrows the candidate list stays frozen — bash
// keeps the list stable between adoptions, only a manual edit narrows it.
func (a *App) commandRefresh() {
	line := string(a.commandValue)
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "cd" {
		a.commandSuggestion = nil
		a.commandHint = ""
		return
	}
	prefix := ""
	if len(fields) > 1 {
		prefix = fields[len(fields)-1]
	} else if !strings.HasSuffix(line, " ") {
		// Caret is still on the command word itself — nothing to
		// complete until the user adds the argument separator.
		a.commandSuggestion = nil
		a.commandHint = ""
		return
	}
	if !a.commandCycling {
		a.commandSuggestion = a.completeDirPath(prefix)
	}
	switch {
	case len(a.commandSuggestion) == 0:
		a.commandHint = "no matching directories"
	case len(a.commandSuggestion) == 1:
		a.commandHint = "1 match · Tab to complete"
	default:
		a.commandHint = fmt.Sprintf("%d matches · Tab cycles", len(a.commandSuggestion))
	}
	if a.commandSelected >= len(a.commandSuggestion) {
		a.commandSelected = 0
	}
}

// commandComplete implements bash-style Tab completion on the last token of
// the line:
//
//   - no candidates: nothing happens
//   - one candidate: the token is replaced with it plus a trailing space
//   - several candidates: the token extends to their common prefix; a
//     second Tab cycles through the list (wrapping)
func (a *App) commandComplete() {
	cands := a.commandSuggestion
	if len(cands) == 0 {
		return
	}
	line := string(a.commandValue)
	fields := strings.Fields(line)
	token := ""
	if len(fields) > 1 {
		token = fields[len(fields)-1]
	} else if len(fields) == 1 && !strings.HasSuffix(line, " ") {
		return // caret is on the command word itself
	}
	if len(cands) == 1 {
		a.commandCycling = true
		a.replaceLastToken(cands[0] + " ")
		a.commandRefresh()
		return
	}
	common := commonPrefix(cands)
	if len(common) > len(token) {
		a.commandCycling = true
		a.replaceLastToken(common)
		a.commandSelected = 0 // the common prefix is the first candidate's
		a.commandRefresh()
		return
	}
	// Second Tab: advance to the next candidate and adopt it, wrapping.
	a.commandCycling = true
	if a.commandSelected < 0 {
		a.commandSelected = 0
	} else {
		a.commandSelected = (a.commandSelected + 1) % len(cands)
	}
	a.replaceLastToken(cands[a.commandSelected])
	a.commandRefresh()
}

// selIndex returns a safe suggestion index for callers that need one even
// when nothing has been adopted yet (-1): falls back to the first
// candidate.
func (a *App) selIndex() int {
	if a.commandSelected < 0 {
		return 0
	}
	return a.commandSelected % len(a.commandSuggestion)
}

// commandCycle adopts the next / previous suggestion as the last token,
// wrapping around the list. Up and Down in the bar, bash's menu-complete
// gesture. A -1 selection (nothing adopted yet) starts at the list ends
// rather than skipping the first candidate.
func (a *App) commandCycle(delta int) {
	cands := a.commandSuggestion
	if len(cands) == 0 {
		return
	}
	if a.commandSelected < 0 {
		if delta > 0 {
			a.commandSelected = 0
		} else {
			a.commandSelected = len(cands) - 1
		}
	} else {
		a.commandSelected = (a.commandSelected + delta + len(cands)) % len(cands)
	}
	a.commandCycling = true
	a.replaceLastToken(cands[a.commandSelected])
	a.commandRefresh()
}

// replaceLastToken swaps the final whitespace-delimited token of the
// command line for repl and parks the caret at the end of the replacement.
// When the line ends in whitespace the token region is empty and the
// replacement appends after the separator — "cd " + Tab yields "cd alpha",
// not "cdalpha".
func (a *App) replaceLastToken(repl string) {
	runes := a.commandValue
	start := len(runes)
	for start > 0 && !isSpaceRune(runes[start-1]) {
		start--
	}
	replR := []rune(repl)
	next := make([]rune, 0, start+len(replR))
	next = append(next, runes[:start]...)
	next = append(next, replR...)
	a.commandValue = next
	a.commandCursor = len(next)
	a.commandScroll = 0
}

// isSpaceRune reports whether r is a command-line field separator.
func isSpaceRune(r rune) bool {
	return r == ' ' || r == '\t'
}

// commonPrefix returns the longest string every candidate starts with, or
// "" when they share nothing. Rune-safe — it slices on rune boundaries so
// multi-byte names never produce half a character.
func commonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	p := []rune(strs[0])
	for _, s := range strs[1:] {
		rs := []rune(s)
		for len(p) > 0 && (len(rs) < len(p) || string(rs[:len(p)]) != string(p)) {
			p = p[:len(p)-1]
		}
		if len(p) == 0 {
			return ""
		}
	}
	return string(p)
}

// completeDirPath returns full replacement strings for a cd completion
// prefix: every directory under the resolved search path whose name starts
// with the prefix's basename. Candidates replace the token wholesale, so a
// nested prefix like "a/ba" yields "a/bar" rather than just "bar".
func (a *App) completeDirPath(prefix string) []string {
	search, base, outPrefix := a.commandSearchBase(prefix)
	var out []string
	for _, name := range listDirCandidates(search, base) {
		out = append(out, outPrefix+name)
	}
	return out
}

// commandSearchBase resolves a cd completion prefix into the directory to
// list, the basename filter, and the token text to keep when splicing
// candidates back in:
//
//   - ""         → list the project root
//   - "foo"      → list the root, filter "foo", splice "foo…"
//   - "a/ba"     → list root/a, filter "ba", splice "a/ba…"
//   - "a/"       → list root/a, no filter, splice "a/…"
//   - "/abs"     → absolute: list the filesystem, no splice prefix
//   - "~", "~/x" → home-directory forms keep their "~" spelling
func (a *App) commandSearchBase(prefix string) (search, base, outPrefix string) {
	if prefix == "" {
		return a.rootDir, "", ""
	}
	home, homeErr := os.UserHomeDir()
	if prefix == "~" {
		if homeErr != nil {
			return "", "", ""
		}
		return home, "", "~/"
	}
	if strings.HasPrefix(prefix, "~/") {
		if homeErr != nil {
			return "", "", ""
		}
		rest := prefix[2:]
		if strings.HasSuffix(rest, "/") {
			return filepath.Join(home, rest), "", prefix
		}
		d := filepath.Dir(rest)
		out := "~/"
		if d != "." {
			out += d + "/"
		}
		return filepath.Join(home, d), filepath.Base(rest), out
	}
	if filepath.IsAbs(prefix) {
		if strings.HasSuffix(prefix, "/") {
			return prefix, "", prefix
		}
		return filepath.Dir(prefix), filepath.Base(prefix), ""
	}
	if strings.HasSuffix(prefix, "/") {
		return filepath.Join(a.rootDir, prefix), "", prefix
	}
	if d := filepath.Dir(prefix); d != "." {
		return filepath.Join(a.rootDir, d), filepath.Base(prefix), d + "/"
	}
	return a.rootDir, prefix, ""
}

// listDirCandidates returns the sorted names of directories under dir whose
// names start with base (or every directory when base is ""). The project's
// own .git is skipped — cd'ing into it is a mistake, not a destination.
func listDirCandidates(dir, base string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if base != "" && !strings.HasPrefix(name, base) {
			continue
		}
		if name == ".git" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// commandSubmit runs the typed command. cd is the first built-in; unknown
// commands flash a hint and keep the bar open so the typo can be fixed
// without retyping the whole line.
func (a *App) commandSubmit() {
	line := trimSpace(string(a.commandValue))
	if line == "" {
		a.closeCommandBar()
		return
	}
	fields := strings.Fields(line)
	cmd, args := fields[0], ""
	if len(fields) > 1 {
		args = strings.Join(fields[1:], " ")
	}
	switch cmd {
	case "cd":
		a.cmdCd(args)
	default:
		a.commandHint = "unknown command: " + cmd
		a.commandSelected = 0
	}
}

// cmdCd implements the cd command: expand and validate the target, then
// re-root the editor so the file tree, project index and git status all
// point at the new directory. Errors keep the bar open with the reason as
// the hint — nvim's E344 behavior.
func (a *App) cmdCd(args string) {
	if a.tree == nil {
		a.commandHint = "cd: not available in single-file mode"
		return
	}
	target := strings.TrimSpace(args)
	if target == "" {
		a.commandHint = "cd: missing directory"
		return
	}
	if target == "~" || strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			a.commandHint = "cd: cannot expand ~"
			return
		}
		target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
	}
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(a.rootDir, abs)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		a.commandHint = "cd: no such directory: " + target
		a.commandSelected = 0
		return
	}
	a.closeCommandBar()
	a.reroot(abs)
	a.flash("cd → " + abs)
}

// reroot points the editor's project state at path: the sidebar file tree,
// the finder's file index, git status and every search surface. Open tabs
// are left alone — like nvim, changing directory doesn't close buffers.
func (a *App) reroot(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		a.flash("cd: bad path " + path)
		return
	}
	tree, err := filetree.New(abs)
	if err != nil {
		a.flash("cd: cannot open " + abs)
		return
	}
	icons := a.tree.IconsEnabled
	a.tree = tree
	a.tree.IconsEnabled = icons
	a.rootDir = abs
	a.setActiveFolder(abs)
	a.refreshGitStatus()
	// Re-root the project index and drop every root-relative result list
	// so stale paths from the old tree can't be jumped to.
	a.finder = finder.New(abs)
	scr := a.screen
	a.finder.Rebuild(func() {
		_ = scr.PostEvent(&finderRebuiltEvent{when: time.Now()})
	})
	a.finderResults = nil
	a.searchResults = nil
	a.sidebarSearchResults = nil
	a.sidebarSearchVisibleMatches = nil
	a.sidebarSearchQuery = nil
	a.sidebarSearchCollapsed = map[string]bool{}
}
