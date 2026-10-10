// =============================================================================
// File: internal/editor/terminal.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// terminal.go gives Tab a third mode: a live shell running on a pseudo
// terminal, rendered inside the editor pane like any other tab. The
// motivation is the project's core workflow — you're already SSH'd into a
// box inside tmux; needing a second pane just to run `go test` breaks the
// "one window, mouse-first" feel the editor is going for.
//
// Design, and why:
//
//   - We spawn the user's $SHELL on a PTY (creack/pty) and feed its output
//     into a virtual terminal emulator (hinshun/vt10x) which maintains a
//     cell grid. Render then blits that grid into the tcell screen. We are
//     NOT passing the child's escape codes through to the host terminal —
//     that would fight the editor for cursor position and scroll region.
//     Owning a real emulator is what lets the shell live in a sub-rectangle
//     of our layout.
//
//   - Both dependencies are pure Go with no CGO, which the project
//     requires. On Windows creack/pty compiles but returns
//     pty.ErrUnsupported at runtime, so NewTerminalTab surfaces a clean
//     error there instead of failing the build.
//
//   - The PTY read loop runs in a goroutine, but it does NOT touch UI
//     state. It writes into vt10x (which is internally mutex-guarded) and
//     then notifies the app via a callback so the app can post a tcell
//     event and redraw on the main loop. This follows the existing
//     "custom tcell events for goroutine → main-loop messaging" pattern.

package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// terminalMode is the value Tab.Mode takes when the tab hosts a shell
// rather than a file. Defined here, next to the behaviour it unlocks,
// mirroring how imageMode lives in image.go.
const terminalMode = "terminal"

// termMinCols / termMinRows are the floor we report to the child process.
// A zero or negative winsize makes many shells (and most full-screen TUIs)
// misbehave, and Render can legitimately be handed a 0-width rect while
// the layout is settling or the sidebar is mid-drag.
const (
	termMinCols = 2
	termMinRows = 1
)

// terminalCloseGrace is how long Close waits after SIGHUP for the shell
// to hang up its jobs and exit on its own before escalating to SIGKILL.
// Long enough for bash/zsh to run their exit path, short enough that
// quitting the editor still feels instant.
const terminalCloseGrace = 300 * time.Millisecond

// vt10x keeps its glyph attribute bits unexported, so we mirror them here.
// These are the bit positions from vt10x's state.go (attrReverse first,
// then underline, bold, gfx, italic, blink) and are part of the on-wire
// meaning of Glyph.Mode, so they're stable.
const (
	termAttrReverse = 1 << iota
	termAttrUnderline
	termAttrBold
	termAttrGfx
	termAttrItalic
	termAttrBlink
)

// Terminal owns one child shell: the PTY master, the process handle, the
// vt10x emulator holding the screen grid, and the lifecycle flags the UI
// reads. It is created and owned by a Tab in terminalMode.
//
// Concurrency: the emulator has its own lock (Lock/Unlock) and is safe to
// write from the reader goroutine while the main loop renders from it.
// The plain fields below are guarded by mu because the reader goroutine
// sets exited/exitMsg when the shell dies.
type Terminal struct {
	vt   vt10x.Terminal
	ptmx *os.File
	cmd  *exec.Cmd

	mu      sync.Mutex
	exited  bool
	exitMsg string

	// cols / rows track the size we last told the child about, so
	// Resize can skip redundant ioctls on every single redraw.
	cols, rows int

	// notify is called (from the reader goroutine) whenever new output
	// has been parsed, so the app can wake its event loop and redraw.
	// It must be safe to call from a non-main goroutine — the app
	// passes a closure that only does screen.PostEvent.
	notify func()

	// closeOnce guards Close so a double close (user closes the tab of
	// an already-exited shell) can't panic on a second file close.
	closeOnce sync.Once
}

// shellCommand picks the shell to launch. $SHELL is the user's explicit
// choice and wins; otherwise fall back to sh, which exists on every unix
// the editor targets. We deliberately start it as an interactive login-ish
// shell ("-i") so the user's aliases and prompt show up — a bare
// non-interactive sh gives a jarring, promptless black box.
func shellCommand() (string, []string) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return sh, []string{"-i"}
}

// NewTerminalTab starts a shell on a PTY rooted at dir and returns a Tab
// that renders it. cols / rows are the initial viewport; they get
// corrected on the first Render once the real editor rect is known.
//
// notify is invoked from the PTY reader goroutine each time output
// arrives; the caller should use it to post a custom tcell event (never
// to mutate UI state directly).
//
// The returned Tab has an empty Buffer allocated so the mass of existing
// code that pokes at t.Buffer doesn't need a nil check, exactly like
// image tabs.
func NewTerminalTab(dir string, cols, rows int, notify func()) (*Tab, error) {
	if runtime.GOOS == "windows" {
		// creack/pty compiles on Windows but every entry point returns
		// ErrUnsupported. Say so plainly rather than letting the user
		// stare at an empty tab.
		return nil, fmt.Errorf("terminal tabs are not supported on Windows")
	}
	if cols < termMinCols {
		cols = termMinCols
	}
	if rows < termMinRows {
		rows = termMinRows
	}

	name, args := shellCommand()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// TERM: vt10x implements a vt100-family emulator, so advertise
	// xterm-256color to get colour without the child assuming
	// capabilities (sixel, kitty graphics) we can't honour.
	//
	// We also strip any inherited COLUMNS / LINES: those would override
	// the winsize we just set and leave the child laying out to the host
	// terminal's width instead of our pane's.
	cmd.Env = append(filteredEnv(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, fmt.Errorf("start terminal: %w", err)
	}

	term := &Terminal{
		vt:     vt10x.New(vt10x.WithWriter(ptmx), vt10x.WithSize(cols, rows)),
		ptmx:   ptmx,
		cmd:    cmd,
		cols:   cols,
		rows:   rows,
		notify: notify,
	}
	go term.readLoop()

	t := &Tab{
		Buffer: NewBuffer(""),
		Mode:   terminalMode,
		Term:   term,
	}
	// Give undo/revert a snapshot to look at so CanRevert and friends
	// answer "nothing to revert" instead of reading a zero value.
	t.initUndo()
	return t, nil
}

// filteredEnv returns the parent environment minus the variables that
// would confuse a child laid out for our pane: COLUMNS / LINES describe
// the *host* terminal, and a stale TERM would be overridden anyway.
func filteredEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		switch {
		case strings.HasPrefix(kv, "COLUMNS="),
			strings.HasPrefix(kv, "LINES="),
			strings.HasPrefix(kv, "TERM="):
			continue
		}
		out = append(out, kv)
	}
	return out
}

// readLoop pumps PTY output into the emulator until the shell exits or
// the PTY is closed. It never touches UI state — it parses into vt10x
// (which locks internally) and then pings notify so the main loop
// redraws. On exit it records the child's status for the status bar.
func (tm *Terminal) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := tm.ptmx.Read(buf)
		if n > 0 {
			// vt10x.Write locks the state for the duration of the
			// parse, so this is safe against a concurrent Render.
			_, _ = tm.vt.Write(buf[:n])
			if tm.notify != nil {
				tm.notify()
			}
		}
		if err != nil {
			// Read fails with EIO on Linux when the child exits and
			// the slave side closes — that's the normal path, not an
			// error worth showing. Wait for the real exit status.
			break
		}
	}

	msg := "shell exited"
	if err := tm.cmd.Wait(); err != nil {
		msg = fmt.Sprintf("shell exited: %v", err)
	}
	tm.mu.Lock()
	tm.exited = true
	tm.exitMsg = msg
	tm.mu.Unlock()
	if tm.notify != nil {
		tm.notify()
	}
}

// Exited reports whether the child shell has terminated, along with a
// short human-readable status for the status bar.
func (tm *Terminal) Exited() (bool, string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.exited, tm.exitMsg
}

// Process returns the child shell's process handle, or nil if it never
// started. Exposed so callers (and tests) can check on the child without
// reaching into the command.
func (tm *Terminal) Process() *os.Process {
	if tm.cmd == nil {
		return nil
	}
	return tm.cmd.Process
}

// Write forwards user input to the child shell. It's a no-op once the
// shell has exited so stray keystrokes on a dead terminal don't raise
// write errors the user can't act on.
func (tm *Terminal) Write(p []byte) {
	if exited, _ := tm.Exited(); exited {
		return
	}
	_, _ = tm.ptmx.Write(p)
}

// Resize tells both the emulator and the child process about a new
// viewport size. Skipped when nothing changed, since Render calls this
// on every frame and a TIOCSWINSZ ioctl per redraw would spam SIGWINCH
// at the shell (which redraws its prompt every time it gets one).
func (tm *Terminal) Resize(cols, rows int) {
	if cols < termMinCols {
		cols = termMinCols
	}
	if rows < termMinRows {
		rows = termMinRows
	}
	tm.mu.Lock()
	unchanged := tm.cols == cols && tm.rows == rows
	tm.mu.Unlock()
	if unchanged {
		return
	}

	tm.vt.Resize(cols, rows)
	if err := pty.Setsize(tm.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		// Leave tm.cols/rows alone so the next Render retries. Recording
		// the new size here would make the "unchanged" fast path above
		// skip every future attempt, leaving the child wedged at a stale
		// winsize for the rest of the session.
		return
	}
	tm.mu.Lock()
	tm.cols, tm.rows = cols, rows
	tm.mu.Unlock()
}

// Close tears the terminal down. Getting this right matters more than it
// looks: the naive "close the PTY and SIGKILL the shell" leaks every
// backgrounded job the user started, because a SIGKILLed bash never runs
// its exit path and so never hangs up its own children.
//
// So we do what a terminal emulator does when its window closes:
//
//  1. Close the PTY master. The child's next read/write gets EIO / SIGHUP.
//  2. Send SIGHUP to the child's *process group* — pty.StartWithSize sets
//     Setsid, making the shell a session leader whose PGID equals its PID,
//     so -pid reaches the shell and every job it spawned. SIGHUP is the
//     signal shells actually handle by hanging up their jobs.
//  3. Give it a short grace period to die on its own.
//  4. If it's still there, SIGKILL the group as a last resort.
//
// Safe to call twice (closing an already-exited tab, then again at app
// shutdown).
func (tm *Terminal) Close() {
	tm.closeOnce.Do(func() {
		if tm.ptmx != nil {
			_ = tm.ptmx.Close()
		}
		proc := tm.Process()
		if proc == nil {
			return
		}
		// If readLoop already reaped the child, its PID is free for the
		// kernel to reuse — signalling it now could hit an unrelated
		// process group. There's nothing left to clean up anyway.
		if exited, _ := tm.Exited(); exited {
			return
		}

		tm.hangupGroup(proc.Pid)

		// Poll rather than Wait — readLoop owns cmd.Wait() and calling
		// it from two goroutines is undefined.
		deadline := time.Now().Add(terminalCloseGrace)
		for time.Now().Before(deadline) {
			if exited, _ := tm.Exited(); exited {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}

		if exited, _ := tm.Exited(); !exited {
			tm.killGroup(proc.Pid)
		}
	})
}

// IsTerminal reports whether the tab hosts a shell rather than a file.
// Callers use this (like IsImage) to skip file-oriented behaviour
// without knowing about Mode strings.
func (t *Tab) IsTerminal() bool {
	return t.Mode == terminalMode
}

// IsTextual reports whether the tab is an ordinary editable text buffer.
// Both alternate modes (image preview, terminal) answer false.
//
// This exists because nearly every guard in the app means "is this a
// normal text tab", not "is this specifically not an image" — before
// terminal tabs there was only one alternate mode, so `!IsImage()` was
// an accidentally-correct spelling of it. New non-text modes should be
// added here rather than bolting another negation onto every call site.
func (t *Tab) IsTextual() bool {
	return t.Mode == ""
}

// CloseTerminal shuts down the tab's child shell if it has one. Called
// when the tab is closed and when the app exits, so the shell doesn't
// outlive the editor.
func (t *Tab) CloseTerminal() {
	if t.Term != nil {
		t.Term.Close()
	}
}

// renderTerminal blits the emulator's cell grid into the editor pane and
// places the hardware cursor where the shell put it. The grid is sized to
// the pane by Resize first, so this is a straight cell-for-cell copy —
// no scrolling or clamping of our own, because the shell (and any
// full-screen program inside it) owns that entirely.
func (t *Tab) renderTerminal(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	tm := t.Term
	if tm == nil {
		return
	}

	tm.Resize(w, h)

	tm.vt.Lock()
	defer tm.vt.Unlock()

	// Iterate the intersection of the pane and the emulator's real grid
	// rather than trusting them to agree. vt10x.Cell panics on an
	// out-of-range index, and the two sizes are tracked in different
	// places (our tm.cols/rows vs. the emulator's own), so deriving the
	// bound from the grid itself is what keeps a future desync from
	// turning into a crash mid-render.
	gridCols, gridRows := tm.vt.Size()
	rows := min(h, gridRows)
	cols := min(w, gridCols)

	// Any pane cells beyond the grid get the editor background so a
	// transient size mismatch reads as empty space, not stale pixels.
	blank := tcell.StyleDefault.Background(th.BG)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			if row < rows && col < cols {
				g := tm.vt.Cell(col, row)
				ch := g.Char
				if ch == 0 {
					ch = ' '
				}
				scr.SetContent(x+col, y+row, ch, nil, glyphStyle(g, th))
				continue
			}
			scr.SetContent(x+col, y+row, ' ', nil, blank)
		}
	}

	cur := tm.vt.Cursor()
	if tm.vt.CursorVisible() && cur.X >= 0 && cur.X < cols && cur.Y >= 0 && cur.Y < rows {
		scr.ShowCursor(x+cur.X, y+cur.Y)
	} else {
		scr.HideCursor()
	}
}

// glyphStyle converts a vt10x glyph's colours and attributes into a tcell
// style, mapping the emulator's "default" colours onto the editor theme so
// an unstyled shell blends into the surrounding UI instead of rendering on
// pure black.
func glyphStyle(g vt10x.Glyph, th theme.Theme) tcell.Style {
	fg := termColor(g.FG, th)
	bg := termColor(g.BG, th)

	st := tcell.StyleDefault.Foreground(fg).Background(bg)
	// Deliberately no st.Reverse(): vt10x already swapped FG/BG into the
	// stored cell (see setChar in its state.go) while *also* leaving the
	// reverse bit set in Mode. Honouring the bit here would swap a second
	// time and cancel the effect out, making every reverse-video construct
	// — less's status line, git add -p, fzf selections, vim's visual
	// selection — render as plain text.
	if g.Mode&termAttrUnderline != 0 {
		st = st.Underline(true)
	}
	if g.Mode&termAttrBold != 0 {
		st = st.Bold(true)
	}
	if g.Mode&termAttrItalic != 0 {
		st = st.Italic(true)
	}
	return st
}

// termColor maps a vt10x colour to a tcell colour. vt10x encodes the 16
// ANSI colours and the 256-colour palette as small integers, truecolor as
// a packed 0xRRGGBB, and its three "default" colours as sentinels above
// 1<<24.
//
// The sentinels are resolved by *meaning*, not by which slot they were
// found in: DefaultFG always becomes the theme's text colour and
// DefaultBG always the theme's background. That distinction is what makes
// reverse video work. vt10x implements reverse by swapping a cell's FG and
// BG, so a reversed default cell arrives with FG=DefaultBG and
// BG=DefaultFG — mapping each sentinel to a positional fallback would
// collapse both back to the normal pair and silently undo the swap.
func termColor(c vt10x.Color, th theme.Theme) tcell.Color {
	switch c {
	case vt10x.DefaultFG:
		return th.Text
	case vt10x.DefaultBG:
		return th.BG
	case vt10x.DefaultCursor:
		return th.Text
	}
	if c < 256 {
		// Palette index — tcell's first 256 colours are the same
		// xterm palette vt10x is indexing into.
		return tcell.PaletteColor(int(c))
	}
	if c < 1<<24 {
		return tcell.NewRGBColor(int32(c>>16&0xff), int32(c>>8&0xff), int32(c&0xff))
	}
	return th.Text
}

// TerminalKeyBytes translates a tcell key event into the byte sequence a
// PTY-attached shell expects. Returns nil when the key carries no meaning
// for a terminal, so the caller can drop it.
//
// The escape sequences are the standard xterm ones; vt10x's own parser and
// every shell/readline implementation agree on these. Note Esc itself is
// intentionally NOT translated here — the app reserves Esc for its action
// menu, so the terminal gets it only via the explicit Esc-leader path.
func TerminalKeyBytes(ev *tcell.EventKey) []byte {
	switch ev.Key() {
	case tcell.KeyRune:
		r := ev.Rune()
		// Alt+<rune> is sent as ESC-prefixed, which is how xterm
		// encodes Meta and how readline expects Alt-b / Alt-f.
		if ev.Modifiers()&tcell.ModAlt != 0 {
			return append([]byte{0x1b}, []byte(string(r))...)
		}
		return []byte(string(r))
	case tcell.KeyEnter:
		return []byte{'\r'}
	case tcell.KeyTab:
		return []byte{'\t'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// DEL (0x7f), not BS — this is what readline and every modern
		// shell treat as "erase previous character".
		return []byte{0x7f}
	case tcell.KeyUp:
		return []byte("\x1b[A")
	case tcell.KeyDown:
		return []byte("\x1b[B")
	case tcell.KeyRight:
		return []byte("\x1b[C")
	case tcell.KeyLeft:
		return []byte("\x1b[D")
	case tcell.KeyHome:
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		return []byte("\x1b[F")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	}

	// Control keys reach us in one of two encodings, and we have to
	// honour both:
	//
	//   • Real terminal input (input.go) posts KeyCtrlSpace+<byte>, so
	//     Ctrl-C arrives as KeyCtrlC == 67, not as byte 3.
	//   • NewEventKey and the simulation screen post the raw control
	//     byte as the Key, so Ctrl-C arrives as KeyETX == 3.
	//
	// This is the one place the editor *does* want Ctrl keys: they're
	// going to the shell as signals, not to an editor action.
	k := ev.Key()
	switch {
	case k >= tcell.KeyCtrlSpace && k <= tcell.KeyCtrlUnderscore:
		return []byte{byte(k - tcell.KeyCtrlSpace)}
	case k > 0 && k <= 0x1f && k != tcell.KeyEsc:
		// Raw control byte. Esc is excluded on purpose — the editor
		// reserves it for the action menu and the leader table, so
		// forwarding it here would make Esc ambiguous.
		return []byte{byte(k)}
	}
	return nil
}
