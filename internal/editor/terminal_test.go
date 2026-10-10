// =============================================================================
// File: internal/editor/terminal_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hinshun/vt10x"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// newTestTerminal starts a real shell on a PTY for tests that need one,
// skipping on platforms where PTYs aren't available. The tab is closed
// via t.Cleanup so a failing assertion can't leak a shell process.
func newTestTerminal(t *testing.T, cols, rows int, notify func()) *Tab {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("terminal tabs are unsupported on Windows")
	}
	tab, err := NewTerminalTab(t.TempDir(), cols, rows, notify)
	if err != nil {
		t.Fatalf("NewTerminalTab: %v", err)
	}
	t.Cleanup(tab.CloseTerminal)
	return tab
}

// waitFor polls cond until it holds or the deadline passes. Terminal
// output arrives asynchronously from the PTY reader goroutine, so tests
// can't assert immediately after writing — but they also mustn't sleep a
// fixed duration and hope.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestNewTerminalTab_ModePredicates verifies a terminal tab reports the
// right mode predicates, since every guard in the app pivots on these.
func TestNewTerminalTab_ModePredicates(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	if !tab.IsTerminal() {
		t.Error("IsTerminal() = false, want true")
	}
	if tab.IsImage() {
		t.Error("IsImage() = true, want false")
	}
	if tab.IsTextual() {
		t.Error("IsTextual() = true, want false for a terminal tab")
	}
	if tab.Term == nil {
		t.Fatal("Term is nil")
	}
	if tab.Buffer == nil {
		t.Error("Buffer should be allocated so buffer-poking code needn't nil-check")
	}
}

// TestTerminalTab_DisplayNameAndNoPath pins the tab-bar label and the
// fact that a terminal has no file path — the latter is what keeps it
// out of Save / Rename / git-status code paths.
func TestTerminalTab_DisplayNameAndNoPath(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	if got := tab.DisplayName(); got != "terminal" {
		t.Errorf("DisplayName() = %q, want %q", got, "terminal")
	}
	if tab.Path != "" {
		t.Errorf("Path = %q, want empty", tab.Path)
	}
}

// TestTerminalTab_MutatorsAreNoOps verifies the text-editing entry points
// refuse to touch a terminal tab. A stray InsertRune here would corrupt
// the unused buffer and, worse, mark the tab dirty and block quit.
func TestTerminalTab_MutatorsAreNoOps(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	tab.InsertRune('x')
	tab.InsertString("hello")
	tab.Backspace()
	tab.Delete()
	tab.DeleteSelection()

	if got := tab.Buffer.String(); got != "" {
		t.Errorf("buffer = %q, want empty after mutator calls", got)
	}
	if tab.Dirty {
		t.Error("Dirty = true; a terminal tab must never look unsaved")
	}
	if changed, ok := tab.ToggleLineComment(); changed || ok {
		t.Errorf("ToggleLineComment() = (%v, %v), want (false, false)", changed, ok)
	}
}

// TestTerminalTab_SaveAndReloadError verifies the file-oriented
// operations report a clear error rather than silently doing nothing or
// panicking on the empty Path.
func TestTerminalTab_SaveAndReloadError(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	if err := tab.Save(); err == nil {
		t.Error("Save() = nil, want an error for a terminal tab")
	}
	if err := tab.Reload(); err == nil {
		t.Error("Reload() = nil, want an error for a terminal tab")
	}
}

// TestTerminal_EchoRoundTrip is the end-to-end check: write a command to
// the shell and assert it shows up in the emulator's grid. This is what
// proves the PTY, the reader goroutine, and the vt10x parse path are all
// actually wired together.
func TestTerminal_EchoRoundTrip(t *testing.T) {
	notified := make(chan struct{}, 64)
	tab := newTestTerminal(t, 60, 12, func() {
		select {
		case notified <- struct{}{}:
		default:
		}
	})

	tab.Term.Write([]byte("echo spice_marker\r"))

	found := waitFor(t, 5*time.Second, func() bool {
		return strings.Contains(terminalText(tab, 60, 12), "spice_marker")
	})
	if !found {
		t.Fatalf("shell output never contained the marker; grid was:\n%s",
			terminalText(tab, 60, 12))
	}

	select {
	case <-notified:
	default:
		t.Error("notify callback was never invoked for shell output")
	}
}

// terminalText dumps the emulator grid as plain text so assertions can
// search it without caring about styling or exact cursor placement.
func terminalText(tab *Tab, cols, rows int) string {
	var sb strings.Builder
	tab.Term.vt.Lock()
	defer tab.Term.vt.Unlock()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			ch := tab.Term.vt.Cell(x, y).Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// TestTerminal_ResizeIsIdempotent verifies a repeat Resize to the same
// dimensions is a no-op. Render calls Resize every frame, and resizing
// for real each time would fire SIGWINCH at the shell continuously.
func TestTerminal_ResizeIsIdempotent(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	tab.Term.Resize(50, 20)
	cols, rows := tab.Term.vt.Size()
	if cols != 50 || rows != 20 {
		t.Fatalf("emulator size = %dx%d, want 50x20", cols, rows)
	}

	// Second identical call must leave the recorded size untouched.
	tab.Term.Resize(50, 20)
	tab.Term.mu.Lock()
	gotCols, gotRows := tab.Term.cols, tab.Term.rows
	tab.Term.mu.Unlock()
	if gotCols != 50 || gotRows != 20 {
		t.Errorf("tracked size = %dx%d, want 50x20", gotCols, gotRows)
	}
}

// TestTerminal_ResizeClampsToMinimum guards the degenerate rects the app
// hands us mid-layout: a zero or negative winsize makes shells and
// full-screen TUIs misbehave.
func TestTerminal_ResizeClampsToMinimum(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	tab.Term.Resize(0, 0)
	cols, rows := tab.Term.vt.Size()
	if cols < termMinCols || rows < termMinRows {
		t.Errorf("size = %dx%d, want at least %dx%d", cols, rows, termMinCols, termMinRows)
	}
}

// TestTerminal_CloseIsIdempotent verifies a double close (tab closed
// after the shell already exited, then again at app shutdown) doesn't
// panic on a second file close or process kill.
func TestTerminal_CloseIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("terminal tabs are unsupported on Windows")
	}
	tab, err := NewTerminalTab(t.TempDir(), 40, 10, nil)
	if err != nil {
		t.Fatalf("NewTerminalTab: %v", err)
	}
	tab.CloseTerminal()
	tab.CloseTerminal() // must not panic
}

// TestTerminal_CloseReapsBackgroundJobs is a regression test for a real
// leak: closing the terminal used to SIGKILL the shell outright, which
// meant bash never ran its exit path and never hung up its own jobs — so
// every `foo &` the user started outlived the editor. Close now SIGHUPs
// the process group first, which is what makes the shell clean up.
func TestTerminal_CloseReapsBackgroundJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("terminal tabs are unsupported on Windows")
	}
	// A distinctive sleep duration so we can find this exact process
	// without matching other tests' or the machine's sleeps.
	const marker = "4243"

	tab, err := NewTerminalTab(t.TempDir(), 60, 12, nil)
	if err != nil {
		t.Fatalf("NewTerminalTab: %v", err)
	}
	t.Cleanup(tab.CloseTerminal)

	tab.Term.Write([]byte("sleep " + marker + " &\r"))

	// Wait for the job to actually exist before closing, otherwise we'd
	// be asserting on a race we already won.
	if !waitFor(t, 5*time.Second, func() bool { return sleepJobAlive(marker) }) {
		t.Skip("shell never started the background job; can't test teardown")
	}

	tab.CloseTerminal()

	if !waitFor(t, 5*time.Second, func() bool { return !sleepJobAlive(marker) }) {
		// Don't leave the orphan behind for the next test run.
		exec.Command("pkill", "-f", "sleep "+marker).Run()
		t.Fatal("background job survived CloseTerminal — the shell was killed without hanging up its jobs")
	}
}

// sleepJobAlive reports whether a `sleep <marker>` process is running, by
// reading /proc rather than shelling out to pgrep so the check itself
// can't match its own command line.
func sleepJobAlive(marker string) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a pid directory
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if len(args) == 2 && filepath.Base(args[0]) == "sleep" && args[1] == marker {
			return true
		}
	}
	return false
}

// TestTerminal_ExitedAfterShellExits verifies the reader goroutine
// records the child's exit so the status bar can say so instead of
// looking like a frozen editor.
func TestTerminal_ExitedAfterShellExits(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	tab.Term.Write([]byte("exit\r"))

	if !waitFor(t, 5*time.Second, func() bool {
		exited, _ := tab.Term.Exited()
		return exited
	}) {
		t.Fatal("terminal never reported the shell as exited")
	}

	exited, msg := tab.Term.Exited()
	if !exited || msg == "" {
		t.Errorf("Exited() = (%v, %q), want (true, non-empty)", exited, msg)
	}

	// Writing to a dead shell must be a silent no-op, not a panic or a
	// surfaced error the user can't act on.
	tab.Term.Write([]byte("echo after-exit\r"))
}

// TestRenderTerminal_DrawsGridAndCursor renders a terminal tab into a
// simulation screen and asserts the emulator contents land in the right
// cells, offset by the pane origin.
func TestRenderTerminal_DrawsGridAndCursor(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(60, 20)

	tab := newTestTerminal(t, 40, 8, nil)

	// Feed the emulator directly so the assertion doesn't depend on the
	// user's shell prompt or startup files.
	tab.Term.vt.Write([]byte("AB"))

	const originX, originY = 5, 3
	tab.Render(scr, theme.Default(), originX, originY, 40, 8)
	scr.Show()

	if got, _, _, _ := scr.GetContent(originX, originY); got != 'A' {
		t.Errorf("cell at pane origin = %q, want 'A'", got)
	}
	if got, _, _, _ := scr.GetContent(originX+1, originY); got != 'B' {
		t.Errorf("cell at origin+1 = %q, want 'B'", got)
	}
}

// TestRenderTerminal_IgnoresZeroSizedRects guards the pathological rects
// the app can produce during a tiny window or right after a resize.
func TestRenderTerminal_IgnoresZeroSizedRects(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(20, 10)

	tab := newTestTerminal(t, 20, 5, nil)

	tab.Render(scr, theme.Default(), 0, 0, 0, 0)
	tab.Render(scr, theme.Default(), 0, 0, -4, -2)
}

// TestTerminalKeyBytes covers the key-to-PTY translation table. These
// sequences are what every shell and readline implementation expects, so
// a regression here silently breaks arrow-key history or Ctrl-C.
func TestTerminalKeyBytes(t *testing.T) {
	cases := []struct {
		name string
		ev   *tcell.EventKey
		want string
	}{
		{"rune", tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone), "a"},
		{"enter sends CR", tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), "\r"},
		{"tab", tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), "\t"},
		{"backspace sends DEL", tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone), "\x7f"},
		{"up", tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone), "\x1b[A"},
		{"down", tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), "\x1b[B"},
		{"right", tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), "\x1b[C"},
		{"left", tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), "\x1b[D"},
		{"home", tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone), "\x1b[H"},
		{"end", tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone), "\x1b[F"},
		{"delete", tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone), "\x1b[3~"},
		{"pgup", tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone), "\x1b[5~"},
		{"pgdn", tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone), "\x1b[6~"},
		// Real terminal input encodes control keys as KeyCtrlSpace+byte.
		{"ctrl-c (tty encoding)", tcell.NewEventKey(tcell.KeyCtrlC, 'c', tcell.ModCtrl), "\x03"},
		{"ctrl-d (tty encoding)", tcell.NewEventKey(tcell.KeyCtrlD, 'd', tcell.ModCtrl), "\x04"},
		{"ctrl-z (tty encoding)", tcell.NewEventKey(tcell.KeyCtrlZ, 'z', tcell.ModCtrl), "\x1a"},
		// NewEventKey / the simulation screen post the raw control byte.
		{"ctrl-c (raw byte encoding)", tcell.NewEventKey(tcell.KeyETX, 0, tcell.ModCtrl), "\x03"},
		{"ctrl-d (raw byte encoding)", tcell.NewEventKey(tcell.KeyEOT, 0, tcell.ModCtrl), "\x04"},
		{"alt-rune is ESC prefixed", tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModAlt), "\x1bb"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(TerminalKeyBytes(tc.ev)); got != tc.want {
				t.Errorf("TerminalKeyBytes() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTerminalKeyBytes_EscapeIsNotForwarded pins the deliberate
// exception: Esc belongs to the editor's action menu, so the terminal
// key encoder must not claim it.
func TestTerminalKeyBytes_EscapeIsNotForwarded(t *testing.T) {
	if got := TerminalKeyBytes(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)); got != nil {
		t.Errorf("TerminalKeyBytes(Esc) = %q, want nil", got)
	}
}

// TestTermColor verifies the emulator-to-tcell colour mapping, including
// the "default" sentinels that must fall back to the editor theme so an
// unstyled shell blends into the surrounding UI.
func TestTermColor(t *testing.T) {
	th := theme.Default()

	// The sentinels resolve by meaning, not by slot — this is what keeps
	// reverse video (which swaps FG and BG) from collapsing back to the
	// normal colour pair.
	if got := termColor(vt10x.DefaultFG, th); got != th.Text {
		t.Errorf("DefaultFG mapped to %v, want theme Text %v", got, th.Text)
	}
	if got := termColor(vt10x.DefaultBG, th); got != th.BG {
		t.Errorf("DefaultBG mapped to %v, want theme BG %v", got, th.BG)
	}
	if got, want := termColor(1, th), tcell.PaletteColor(1); got != want {
		t.Errorf("palette colour 1 mapped to %v, want %v", got, want)
	}
	// Truecolor is packed as 0xRRGGBB.
	if got, want := termColor(0x0080ff, th), tcell.NewRGBColor(0, 0x80, 0xff); got != want {
		t.Errorf("truecolor mapped to %v, want %v", got, want)
	}
}

// TestGlyphStyle_ReverseIsNotDoubleApplied pins a subtle rendering bug.
// vt10x bakes reverse video into the stored cell (it swaps FG/BG in
// setChar) while ALSO leaving the reverse bit set in Glyph.Mode. If
// glyphStyle honoured that bit, tcell would swap a second time and the
// highlight would vanish — silently breaking less's status line, git
// add -p, fzf selections, and vim's visual selection.
func TestGlyphStyle_ReverseIsNotDoubleApplied(t *testing.T) {
	th := theme.Default()
	vt := vt10x.New(vt10x.WithSize(20, 3))

	// SGR 7 = reverse video.
	if _, err := vt.Write([]byte("\x1b[7mR")); err != nil {
		t.Fatalf("write: %v", err)
	}
	vt.Lock()
	g := vt.Cell(0, 0)
	vt.Unlock()

	if g.Mode&termAttrReverse == 0 {
		t.Skip("vt10x no longer reports the reverse bit; mapping assumption changed")
	}

	fg, bg, attrs := glyphStyle(g, th).Decompose()
	if attrs&tcell.AttrReverse != 0 {
		t.Error("style sets AttrReverse; vt10x already swapped the colours, so this double-swaps and cancels the highlight")
	}
	// The swap vt10x performed must survive into the rendered style:
	// foreground should now be the theme background and vice versa.
	if fg != th.BG || bg != th.Text {
		t.Errorf("reverse cell rendered fg=%v bg=%v, want fg=%v bg=%v (colours swapped)", fg, bg, th.BG, th.Text)
	}
}

// TestClose_DoesNotSignalAfterReap guards against signalling a PID the
// kernel may have recycled. Once readLoop has reaped the child, its PID
// is fair game for reuse, so Close must not fire SIGHUP/SIGKILL at it.
func TestClose_DoesNotSignalAfterReap(t *testing.T) {
	tab := newTestTerminal(t, 40, 10, nil)

	tab.Term.Write([]byte("exit\r"))
	if !waitFor(t, 5*time.Second, func() bool {
		exited, _ := tab.Term.Exited()
		return exited
	}) {
		t.Fatal("shell never exited")
	}

	// Must return promptly and without signalling anything. If it tried,
	// it would also burn the full terminalCloseGrace polling for an exit
	// that already happened.
	start := time.Now()
	tab.CloseTerminal()
	if elapsed := time.Since(start); elapsed >= terminalCloseGrace {
		t.Errorf("Close took %v on an already-exited shell; it should short-circuit", elapsed)
	}
}

// TestRenderTerminal_SurvivesGridSmallerThanPane renders with a pane
// larger than the emulator's grid. Indexing vt10x out of range panics,
// so the render must derive its bounds from the grid, not the pane.
func TestRenderTerminal_SurvivesGridSmallerThanPane(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(80, 30)

	tab := newTestTerminal(t, 20, 5, nil)

	// Shrink the emulator behind the renderer's back, then draw into a
	// much larger pane. Without grid-derived bounds this panics.
	tab.Term.vt.Resize(4, 2)
	tab.renderTerminal(scr, theme.Default(), 0, 0, 60, 20)
	scr.Show()
}
