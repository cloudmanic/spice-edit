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
	"strings"
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
// write. It branches three ways:
//
//   - Project format.json has an entry for this extension → trust
//     check, prompt if needed, then run.
//   - Project has no entry but global format-defaults.json does →
//     offer to install the global preference into the project.
//   - Neither → silent no-op (the spec).
//
// Each branch is its own helper so the routing logic stays shallow
// and easy to follow. Errors loading config files are surfaced once
// (so a typo isn't silently ignored) but never block the save itself
// — that already happened before this function was called.
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

	argv := cfg.CommandFor(tab.Path)
	if argv != nil {
		a.runWithTrust(idx, cfg, argv)
		return
	}

	// Project doesn't format this extension. See if the user has a
	// personal default we can offer to install.
	a.maybeOfferInstall(idx, tab.Path)
}

// runWithTrust drives the existing trust-check + run path, factored
// out so runFormatOnSave can stay a flat router. Behaviour matches
// the previous monolithic version exactly — denied stays silent,
// unknown opens the prompt, allowed runs.
func (a *App) runWithTrust(idx int, cfg *format.Config, argv []string) {
	tab := a.tabs[idx]
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

// maybeOfferInstall checks whether the user has a global default
// formatter for this file's extension and, if so, prompts them to
// install it into the project's .spiceedit/format.json. Skips
// silently when:
//
//   - no global defaults file exists (the common case for users
//     who haven't set personal preferences),
//   - the user's defaults don't cover this extension,
//   - the user already declined the install for this (project, ext),
//   - the project's format.json exists but is currently denied at
//     the trust level — piling on an install prompt while trust is
//     denied would be confusing UX.
func (a *App) maybeOfferInstall(idx int, tabPath string) {
	defaults, err := format.LoadDefaults(format.DefaultsPath())
	if err != nil {
		a.flash("format defaults: " + err.Error())
		return
	}
	if defaults == nil {
		return
	}
	argv := defaults.CommandFor(tabPath)
	if argv == nil {
		return
	}
	ext := strings.TrimPrefix(filepath.Ext(tabPath), ".")
	if ext == "" {
		return
	}

	tf, err := format.LoadTrust(format.DefaultTrustPath())
	if err != nil {
		a.flash("format trust: " + err.Error())
		return
	}
	if tf.IsInstallDeclined(a.rootDir, ext) {
		return
	}
	// If a project format.json exists and trust is currently denied,
	// don't pile on. The user already said no to formatting in this
	// project; offering to add a new entry would feel like a nag.
	if cfg, _ := format.Load(a.rootDir); cfg != nil {
		if tf.CheckTrust(a.rootDir, cfg.Hash()) == format.TrustDenied {
			return
		}
	}

	a.openFormatInstallPrompt(idx, ext, argv)
}

// openFormatTrustPrompt asks the user whether to allow this project's
// format.json to run commands on save. Yes records trust + runs the
// formatter on the file we just saved; No records denial and skips.
// Cancel (Esc) goes through the same deny path as No — there's no
// safe "decide later" because every save would re-fire the prompt,
// training the user to dismiss it without thinking.
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
	root := a.rootDir
	hash := cfg.Hash()

	msg := fmt.Sprintf("Allow %s to run formatters on save?", filepath.Join(format.ConfigDir, format.ConfigFile))
	a.openConfirm("Trust this project's formatter?", msg, func(app *App) {
		// Yes — record allow, persist, and run.
		app.persistTrust(root, hash, true)
		app.execFormatter(tabPath, argv)
	})
	// Cancel/No path: persist a denial so we don't re-prompt every
	// save. We pin this on confirmCancelHook (cleared by
	// closeAllModals) so an unrelated future confirm modal can't
	// inherit the side effect.
	a.confirmCancelHook = func(app *App) {
		app.persistTrust(root, hash, false)
	}
}

// openFormatInstallPrompt asks whether to install the user's global
// default formatter for this extension into the project's
// .spiceedit/format.json. Yes merges the entry, auto-trusts the
// resulting config (the user's consent here implies trust — same
// reasoning as "you wrote the file yourself"), and runs the
// formatter on the freshly-saved file. No persists a per-extension
// decline so the prompt won't fire on every save in this project.
//
// The prompt uses the same Yes/No confirm modal as the trust prompt
// so we stay within the existing modal vocabulary instead of
// inventing a new shape for one feature.
func (a *App) openFormatInstallPrompt(idx int, ext string, argv []string) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	tab := a.tabs[idx]
	tabPath := tab.Path
	root := a.rootDir
	// Use just the executable name (argv[0]) in the prompt — the
	// full argv with flags would crowd the modal and most of the
	// time the user only cares which formatter is being added.
	formatterName := argv[0]

	title := "Install formatter for this project?"
	msg := fmt.Sprintf("Add %s for .%s to %s?", formatterName, ext,
		filepath.Join(format.ConfigDir, format.ConfigFile))

	a.openConfirm(title, msg, func(app *App) {
		// Yes — merge into project config, trust the new hash, run.
		hash, err := format.InstallCommandIntoProject(root, ext, argv)
		if err != nil {
			app.flash("install failed: " + err.Error())
			return
		}
		// Auto-trust: the user just consented to the exact contents
		// they wrote. Re-prompting would be busywork. Also clear any
		// past "declined" entry for this ext so installing now means
		// the prompt was really opt-in, not a leftover dismissal.
		app.persistTrust(root, hash, true)
		app.persistInstallDecline(root, ext, false)
		app.execFormatter(tabPath, argv)
	})
	a.confirmCancelHook = func(app *App) {
		app.persistInstallDecline(root, ext, true)
	}
}

// persistInstallDecline writes a per-extension install decision to
// the trust store. Mirrors persistTrust so the Yes / No / Esc
// branches of the install prompt all share one IO path with
// consistent error reporting.
func (a *App) persistInstallDecline(root, ext string, declined bool) {
	tf, _ := format.LoadTrust(format.DefaultTrustPath())
	if tf == nil {
		tf = &format.TrustFile{Projects: map[string]format.TrustEntry{}}
	}
	tf.SetInstallDeclined(root, ext, declined)
	if err := format.SaveTrust(format.DefaultTrustPath(), tf); err != nil {
		a.flash("format trust: " + err.Error())
	}
}

// persistTrust writes a trust decision to the on-disk trust store.
// Pulled out so both the Yes branch (in the trust callback) and the
// No branch (in the cancel hook) share one error-handling path —
// they should agree on what "best-effort" means.
func (a *App) persistTrust(root, hash string, trusted bool) {
	tf, _ := format.LoadTrust(format.DefaultTrustPath())
	if tf == nil {
		tf = &format.TrustFile{Projects: map[string]format.TrustEntry{}}
	}
	tf.SetTrust(root, hash, trusted)
	if err := format.SaveTrust(format.DefaultTrustPath(), tf); err != nil {
		a.flash("format trust: " + err.Error())
	}
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
