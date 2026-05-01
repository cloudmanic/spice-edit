// =============================================================================
// File: internal/app/format.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

// Format-on-save wiring. The pure logic (config parsing, trust file)
// lives in internal/format; this file is the bridge into the editor's
// event loop and modals. The flow on every successful save:
//
//  1. Load <root>/.spiceedit/format.json. Missing → done.
//  2. Look up an argv for the file's extension. None → done.
//  3. Check the trust store. Allowed → run; Denied → done; Unknown
//     → open the trust prompt and re-enter the run on Allow.
//  4. exec.Command in a goroutine; post a formatDoneEvent on
//     completion so the main loop can reload the buffer (when the
//     user hasn't typed in the meantime) and flash a status.
//
// Keeping everything except the goroutine on the main loop means the
// usual rule still holds: tcell state is mutated only from the event
// dispatch.

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cloudmanic/spice-edit/internal/format"
	"github.com/gdamore/tcell/v2"
)

// formatDoneEvent is posted by runFormatter when the goroutine
// finishes. tabPath is how we re-find the right tab on the main
// loop — not the index, because tabs may have been reordered or
// closed in the meantime.
type formatDoneEvent struct {
	when    time.Time
	tabPath string
	label   string // command name (just argv[0]) for status messages
	err     error
}

// When satisfies the tcell.Event interface.
func (e *formatDoneEvent) When() time.Time { return e.when }

// runFormatOnSave is called by saveTabAt after a successful disk
// write. It's a no-op when the project has no .spiceedit/format.json,
// when the file's extension isn't configured, when the binary isn't
// on $PATH, or when the user has previously denied trust for the
// current config. Trust-unknown opens the prompt and re-enters this
// function on approval.
func (a *App) runFormatOnSave(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	tab := a.tabs[idx]
	if tab.Path == "" {
		return
	}

	cfg, err := format.Load(a.rootDir)
	if err != nil {
		a.flash("format: " + err.Error())
		return
	}
	if cfg == nil {
		return
	}
	argv := cfg.CommandFor(tab.Path)
	if argv == nil {
		return
	}

	trust, err := format.LoadTrust(format.DefaultTrustPath())
	if err != nil {
		a.flash("format trust: " + err.Error())
		return
	}
	switch trust.CheckTrust(a.rootDir, cfg.Hash()) {
	case format.TrustDenied:
		return
	case format.TrustUnknown:
		a.openFormatTrustPrompt(idx, cfg, argv)
		return
	}

	a.execFormatter(tab.Path, argv)
}

// openFormatTrustPrompt asks the user whether to allow this project's
// format.json to run commands on save. Yes records trust + runs the
// formatter on the file we just saved; No records denial and skips.
// Cancel (Esc) leaves the trust file alone so the next save will
// prompt again — the safest non-decision.
//
// We capture the loaded *format.Config (not a fresh re-Load) so the
// hash we trust is the exact one we evaluated, not whatever the file
// looks like after an external edit between the prompt and the
// answer. Same defense as the (path, hash) trust key itself.
func (a *App) openFormatTrustPrompt(idx int, cfg *format.Config, argv []string) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	tab := a.tabs[idx]
	tabPath := tab.Path

	// Two prompts back to back is jarring, so we use the existing
	// confirm modal — Yes/No — and treat Esc as "ask again later."
	// The trust file is updated only on an explicit Yes or No.
	msg := fmt.Sprintf("Allow %s to run formatters on save?", filepath.Join(format.ConfigDir, format.ConfigFile))
	a.openConfirm("Trust this project's formatter?", msg, func(app *App) {
		// Yes — record allow, persist, and run.
		tf, _ := format.LoadTrust(format.DefaultTrustPath())
		if tf == nil {
			tf = &format.TrustFile{Projects: map[string]format.TrustEntry{}}
		}
		tf.SetTrust(app.rootDir, cfg.Hash(), true)
		if err := format.SaveTrust(format.DefaultTrustPath(), tf); err != nil {
			app.flash("format trust: " + err.Error())
			// Best-effort: still run this once even if persistence failed;
			// the user already approved, and the alternative is silently
			// dropping their click.
		}
		app.execFormatter(tabPath, argv)
	})
	// We piggy-back the deny path on confirmCancel to avoid bolting a
	// third "no" callback onto the existing modal. The wrapper here
	// records denial only; the modal still dismisses normally on Esc.
	a.formatDenyArmed = formatDenyContext{rootDir: a.rootDir, hash: cfg.Hash(), armed: true}
}

// formatDenyContext is a small holder that lets the confirm modal's
// "No" branch record a trust denial without us needing a second
// callback shape. handleConfirmKey / handleConfirmMouse consult it
// when they fire confirmCancel — see modals.go.
type formatDenyContext struct {
	rootDir string
	hash    string
	armed   bool
}

// armFormatDenyOnCancel returns true and records a denial when the
// confirm modal's cancel branch was set up by the format-trust flow.
// Returns false otherwise so the same cancel branch can be reused
// for non-format prompts (today: Delete) without side effects.
func (a *App) armFormatDenyOnCancel() bool {
	if !a.formatDenyArmed.armed {
		return false
	}
	ctx := a.formatDenyArmed
	a.formatDenyArmed = formatDenyContext{}
	tf, _ := format.LoadTrust(format.DefaultTrustPath())
	if tf == nil {
		tf = &format.TrustFile{Projects: map[string]format.TrustEntry{}}
	}
	tf.SetTrust(ctx.rootDir, ctx.hash, false)
	if err := format.SaveTrust(format.DefaultTrustPath(), tf); err != nil {
		a.flash("format trust: " + err.Error())
	}
	return true
}

// execFormatter shells out to argv with the file path already
// substituted in. Runs in a goroutine and posts a formatDoneEvent on
// completion so the main loop can reload the buffer and flash a
// status — exactly the same pattern runCustomAction uses.
//
// We deliberately use exec.Command (not sh -c) with an explicit argv
// so a shell-injection vector via a malicious format.json is just
// not available: each arg is passed as-is to execve, no shell
// interpretation, no globbing, no command chaining.
func (a *App) execFormatter(tabPath string, argv []string) {
	if len(argv) == 0 {
		return
	}
	scr := a.screen
	label := argv[0]
	a.flash(label + "…")
	go func() {
		cmd := exec.Command(argv[0], argv[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Distinguish "binary not installed" from "formatter ran
			// but failed" — the first is a silent skip (per the spec),
			// the second is a status flash so the user sees breakage.
			var pathErr *exec.Error
			if errors.As(err, &pathErr) && errors.Is(pathErr.Err, exec.ErrNotFound) {
				err = nil
			} else if len(out) > 0 {
				preview := string(out)
				if i := indexNewline(preview); i >= 0 {
					preview = preview[:i]
				}
				if len(preview) > 80 {
					preview = preview[:80] + "…"
				}
				err = fmt.Errorf("%v: %s", err, preview)
			}
		}
		_ = scr.PostEvent(&formatDoneEvent{
			when:    time.Now(),
			tabPath: tabPath,
			label:   label,
			err:     err,
		})
	}()
}

// indexNewline returns the index of the first newline in s, or -1.
// Tiny helper kept local because strings.IndexByte('\n') reads
// awkwardly in the middle of error formatting.
func indexNewline(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}

// handleFormatDone surfaces the result of a background formatter run.
// On success it reloads the affected tab from disk so the buffer
// shows the formatted output — but only if the user hasn't started
// editing again in the meantime (Dirty=true). Trampling unsaved
// edits would be the worst possible UX outcome of this feature.
func (a *App) handleFormatDone(e *formatDoneEvent) {
	if e == nil {
		return
	}
	if e.err != nil {
		a.flash(fmt.Sprintf("%s failed: %v", e.label, e.err))
		return
	}
	for _, tab := range a.tabs {
		if tab.Path != e.tabPath {
			continue
		}
		if tab.Dirty {
			a.flash(fmt.Sprintf("%s ran — kept your edits (file on disk was reformatted)", e.label))
			return
		}
		if err := tab.Reload(); err != nil {
			a.flash(fmt.Sprintf("%s ran but reload failed: %v", e.label, err))
			return
		}
		a.flash(fmt.Sprintf("Formatted with %s", e.label))
		return
	}
	// Tab was closed before the formatter finished — silent no-op.
}

// formatHash exposes the current project's format.json hash for tests
// and is otherwise unused. It returns "" when no config is loaded —
// the same signal as "no formatting configured". Pulled into a
// method so tests don't have to re-implement the load path.
func (a *App) formatHash() string {
	cfg, _ := format.Load(a.rootDir)
	return cfg.Hash()
}

// Compile-time check that formatDoneEvent really is a tcell.Event.
// Catches signature drift if the interface ever grows a method.
var _ tcell.Event = (*formatDoneEvent)(nil)
