// =============================================================================
// File: internal/app/format_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/format"
)

// writeFormatConfig drops a .spiceedit/format.json into root with the
// given JSON body. Pulled out so each test reads as the scenario it's
// pinning down rather than mkdir+write boilerplate.
func writeFormatConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, format.ConfigDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, format.ConfigFile), []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// useTestTrustFile redirects the trust file to a temp path for the
// duration of the test. Without this every format test would either
// pollute the real ~/.config/spiceedit/format-trust.json or carry
// state across runs.
func useTestTrustFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trust.json")
	t.Setenv("SPICEEDIT_TRUST_FILE", path)
	return path
}

// useTestDefaultsFile redirects the global defaults file the same
// way the trust hook does. Tests that exercise the install flow
// need both pointed at temp paths so they don't read real user
// config or leak state across runs.
func useTestDefaultsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "format-defaults.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("seed defaults: %v", err)
		}
	}
	t.Setenv("SPICEEDIT_DEFAULTS_FILE", path)
	return path
}

// preTrust writes a "yes" entry into the trust file so a test can
// exercise the run path without going through the prompt.
func preTrust(t *testing.T, root string, allowed bool) {
	t.Helper()
	cfg, err := format.Load(root)
	if err != nil {
		t.Fatalf("load cfg: %v", err)
	}
	if cfg == nil {
		t.Fatal("preTrust: format.json missing — write it before pre-trusting")
	}
	tf, _ := format.LoadTrust(format.DefaultTrustPath())
	if tf == nil {
		tf = &format.TrustFile{Projects: map[string]format.TrustEntry{}}
	}
	tf.SetTrust(root, cfg.Hash(), allowed)
	if err := format.SaveTrust(format.DefaultTrustPath(), tf); err != nil {
		t.Fatalf("save trust: %v", err)
	}
}

// openTabAtPath wires a Tab into the App at a given file path. Mirrors
// what OpenFile does without touching the real file tree. Tests use
// this to set up exactly the tab state they want before saving.
func openTabAtPath(t *testing.T, a *App, path string) *editor.Tab {
	t.Helper()
	tab, err := editor.NewTab(path)
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	return tab
}

// TestRunFormatOnSave_NoConfigIsNoop pins the central opt-in promise:
// without a .spiceedit/format.json, save behaves exactly like before
// — no exec, no prompt, no flash about formatting.
func TestRunFormatOnSave_NoConfigIsNoop(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	if a.confirmOpen {
		t.Fatal("no config should never open a confirm modal")
	}
	if a.confirmCancelHook != nil {
		t.Fatal("no config should not install a cancel hook")
	}
}

// TestRunFormatOnSave_UnknownExtensionIsNoop covers a project that
// ships a config but doesn't list this file's extension. The save
// should land cleanly with no prompt and no flash about formatting.
func TestRunFormatOnSave_UnknownExtensionIsNoop(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	writeFormatConfig(t, root, `{"commands":{"go":["gofmt","-w","$FILE"]}}`)
	a := newTestApp(t, root)
	target := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	if a.confirmOpen {
		t.Fatal("unknown extension should not prompt")
	}
}

// TestRunFormatOnSave_UnknownTrustOpensPrompt is the security
// linchpin: a config we've never seen before must prompt the user
// before any command runs. Catching a regression here means the
// arbitrary-command-execution risk is back.
func TestRunFormatOnSave_UnknownTrustOpensPrompt(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	writeFormatConfig(t, root, `{"commands":{"go":["echo","ran","$FILE"]}}`)
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	if !a.confirmOpen {
		t.Fatal("untrusted config should open the trust prompt")
	}
	if a.confirmCancelHook == nil {
		t.Fatal("trust prompt should install a cancel hook")
	}
}

// TestRunFormatOnSave_DeniedIsNoop pins the half of the trust model
// that's easy to forget: a remembered "No" should not re-prompt and
// should not run the formatter. Otherwise the user gets nagged on
// every save in a project they explicitly rejected.
func TestRunFormatOnSave_DeniedIsNoop(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	writeFormatConfig(t, root, `{"commands":{"go":["echo","ran"]}}`)
	preTrust(t, root, false)
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	if a.confirmOpen {
		t.Fatal("denied trust should not re-prompt")
	}
	if a.confirmCancelHook != nil {
		t.Fatal("denied trust should not install a cancel hook")
	}
}

// TestTrustPromptCancel_PersistsDeny exercises the bridge between
// the confirm modal's cancel branch and the trust file: hitting No
// (or Esc) on the prompt fires the cancel hook, which records a
// denial so the next save in this project goes silently, not back
// to another prompt.
func TestTrustPromptCancel_PersistsDeny(t *testing.T) {
	trustPath := useTestTrustFile(t)
	root := t.TempDir()
	writeFormatConfig(t, root, `{"commands":{"go":["echo","ran","$FILE"]}}`)
	cfg, _ := format.Load(root)
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	// Run the save flow up to the prompt, then drive cancel directly.
	a.runFormatOnSave(0)
	if !a.confirmOpen {
		t.Fatal("expected trust prompt to be open")
	}
	a.confirmCancel()

	tf, err := format.LoadTrust(trustPath)
	if err != nil {
		t.Fatalf("reload trust: %v", err)
	}
	if d := tf.CheckTrust(root, cfg.Hash()); d != format.TrustDenied {
		t.Fatalf("expected TrustDenied recorded, got %v", d)
	}
}

// TestConfirmCancel_NoHookIsInert guarantees the cancel branch is
// inert for non-format confirm modals (today: Delete). Without this
// isolation, cancelling a Delete prompt could fire a stale hook
// from a format flow that never finished closing properly.
func TestConfirmCancel_NoHookIsInert(t *testing.T) {
	useTestTrustFile(t)
	a := newTestApp(t, t.TempDir())
	a.confirmOpen = true
	a.confirmCancel()
	// No assertion beyond "did not panic" — the test passes if we
	// reach this line, since a stray hook would have run side effects.
}

// TestExecFormatter_RunsAndPostsEvent walks the async happy path: the
// goroutine shells out, the formatter rewrites the file, and a
// formatDoneEvent lands on the screen's queue with no error.
func TestExecFormatter_RunsAndPostsEvent(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("orig\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	a.execFormatter(target, []string{"sh", "-c", "echo formatted > " + target})

	ev := waitForFormatEvent(t, a)
	if ev.err != nil {
		t.Fatalf("formatter err: %v", ev.err)
	}
	if ev.tabPath != target {
		t.Fatalf("event tabPath: got %q, want %q", ev.tabPath, target)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "formatted\n" {
		t.Fatalf("file contents: got %q, want %q", string(got), "formatted\n")
	}
}

// TestExecFormatter_MissingBinaryIsSilent codifies the "skip when not
// installed" rule: a missing binary must not flash an error or
// otherwise punish the user. The done event should arrive with err
// == nil so handleFormatDone treats it as a no-op.
func TestExecFormatter_MissingBinaryIsSilent(t *testing.T) {
	useTestTrustFile(t)
	a := newTestApp(t, t.TempDir())
	a.execFormatter("/tmp/nope.go", []string{"definitely-not-a-real-binary-xyzzy"})

	ev := waitForFormatEvent(t, a)
	if ev.err != nil {
		t.Fatalf("missing binary should be silent, got err=%v", ev.err)
	}
}

// TestHandleFormatDone_ReloadsCleanBuffer is the success path for
// the main-loop side: after the formatter rewrites the file, a
// clean tab should reload so the user sees the new contents.
func TestHandleFormatDone_ReloadsCleanBuffer(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("first\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tab := openTabAtPath(t, a, target)
	tab.Dirty = false
	if err := os.WriteFile(target, []byte("formatted\n"), 0644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	a.handleFormatDone(&formatDoneEvent{tabPath: target, label: "fmt"})

	if got := tab.Buffer.String(); got != "formatted\n" {
		t.Fatalf("buffer after reload: got %q, want %q", got, "formatted\n")
	}
}

// TestHandleFormatDone_PreservesDirtyBuffer is the most important
// invariant of the whole feature: if the user typed during a slow
// formatter run, their unsaved edits must survive. Tramping them
// would be the worst possible UX outcome.
func TestHandleFormatDone_PreservesDirtyBuffer(t *testing.T) {
	useTestTrustFile(t)
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("seed\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tab := openTabAtPath(t, a, target)
	tab.Buffer = editor.NewBuffer("user-typed-this\n")
	tab.Dirty = true
	if err := os.WriteFile(target, []byte("formatted\n"), 0644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	a.handleFormatDone(&formatDoneEvent{tabPath: target, label: "fmt"})

	if got := tab.Buffer.String(); got != "user-typed-this\n" {
		t.Fatalf("dirty buffer was overwritten: got %q", got)
	}
}

// TestHandleFormatDone_ClosedTabIsNoop covers the race where the
// user closed the tab before the formatter finished. The handler
// should silently return without crashing or flashing an error.
func TestHandleFormatDone_ClosedTabIsNoop(t *testing.T) {
	useTestTrustFile(t)
	a := newTestApp(t, t.TempDir())
	a.handleFormatDone(&formatDoneEvent{tabPath: "/tmp/never-opened.go", label: "fmt"})
	// No assertion — the test passes if we don't panic.
}

// -----------------------------------------------------------------------------
// Install prompt — global defaults flow
// -----------------------------------------------------------------------------

// TestMaybeOfferInstall_NoDefaultsIsNoop pins the most common case:
// a user who has never created format-defaults.json should see no
// prompt, ever, regardless of what the project has configured.
func TestMaybeOfferInstall_NoDefaultsIsNoop(t *testing.T) {
	useTestTrustFile(t)
	useTestDefaultsFile(t, "")
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	if a.confirmOpen {
		t.Fatal("missing defaults should never prompt")
	}
}

// TestMaybeOfferInstall_OpensPrompt is the headline path: defaults
// have a command for this extension, project has none, no decline
// recorded → the install modal opens with a cancel hook armed.
func TestMaybeOfferInstall_OpensPrompt(t *testing.T) {
	useTestTrustFile(t)
	useTestDefaultsFile(t, `{"commands":{"go":["gofmt","-w","$FILE"]}}`)
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	if !a.confirmOpen {
		t.Fatal("expected install prompt to open")
	}
	if a.confirmCancelHook == nil {
		t.Fatal("expected cancel hook to be armed for install decline")
	}
}

// TestMaybeOfferInstall_AcceptWritesProjectConfig walks the Yes
// path end to end: the user consents, the project's format.json
// is created with the default's argv, trust is auto-recorded for
// the new hash, and the formatter runs against the just-saved file.
func TestMaybeOfferInstall_AcceptWritesProjectConfig(t *testing.T) {
	trustPath := useTestTrustFile(t)
	root := t.TempDir()
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("orig\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Use a fake "formatter" that writes a sentinel so we can confirm
	// it actually ran. argv[0] is sh — install will be persisted as
	// the verbatim argv we pass here.
	defaultsBody := fmt.Sprintf(
		`{"commands":{"go":["sh","-c","echo formatted > %s"]}}`, target)
	useTestDefaultsFile(t, defaultsBody)

	a := newTestApp(t, root)
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)
	if !a.confirmOpen {
		t.Fatal("expected install prompt to open")
	}
	// Drive the Yes path manually.
	a.confirmHover = 1
	a.confirmYes()

	// Project config should now exist with a "go" entry.
	cfg, err := format.Load(root)
	if err != nil {
		t.Fatalf("load project cfg: %v", err)
	}
	if cfg == nil || len(cfg.Commands["go"]) == 0 {
		t.Fatalf("expected project to have go entry, got %v", cfg)
	}

	// Trust should record the new hash as allowed.
	tf, err := format.LoadTrust(trustPath)
	if err != nil {
		t.Fatalf("load trust: %v", err)
	}
	if d := tf.CheckTrust(root, cfg.Hash()); d != format.TrustAllowed {
		t.Fatalf("expected TrustAllowed after install, got %v", d)
	}

	// Wait for the formatter goroutine to land its done event.
	ev := waitForFormatEvent(t, a)
	if ev.err != nil {
		t.Fatalf("formatter failed: %v", ev.err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "formatted\n" {
		t.Fatalf("file after format: got %q", string(got))
	}
}

// TestMaybeOfferInstall_DeclinePersists pins the No path: cancel
// records a per-extension decline so the next save in this project
// for the same file type goes silently.
func TestMaybeOfferInstall_DeclinePersists(t *testing.T) {
	trustPath := useTestTrustFile(t)
	useTestDefaultsFile(t, `{"commands":{"go":["gofmt","-w","$FILE"]}}`)
	root := t.TempDir()
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)
	if !a.confirmOpen {
		t.Fatal("expected install prompt to open")
	}
	a.confirmCancel()

	tf, err := format.LoadTrust(trustPath)
	if err != nil {
		t.Fatalf("load trust: %v", err)
	}
	if !tf.IsInstallDeclined(root, "go") {
		t.Fatal("expected install decline persisted for go")
	}

	// Next save should be silent.
	a.runFormatOnSave(0)
	if a.confirmOpen {
		t.Fatal("declined extension should not re-prompt")
	}
}

// TestMaybeOfferInstall_ProjectHasEntryUsesTrustPath confirms the
// install path doesn't fire when the project already lists this
// extension — that case belongs to the trust prompt instead.
func TestMaybeOfferInstall_ProjectHasEntryUsesTrustPath(t *testing.T) {
	useTestTrustFile(t)
	useTestDefaultsFile(t, `{"commands":{"go":["pint","$FILE"]}}`)
	root := t.TempDir()
	writeFormatConfig(t, root, `{"commands":{"go":["gofmt","-w","$FILE"]}}`)
	a := newTestApp(t, root)
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	openTabAtPath(t, a, target)

	a.runFormatOnSave(0)

	// The trust prompt is open — not the install prompt — and its
	// hook is set. We can't trivially distinguish the two by struct
	// shape, but the title text and hook signature would differ for
	// install vs trust. The presence of *some* prompt + the project
	// config's hash being TrustUnknown is the trust-path signal.
	if !a.confirmOpen {
		t.Fatal("expected some prompt to open")
	}
	cfg, _ := format.Load(root)
	tf, _ := format.LoadTrust(format.DefaultTrustPath())
	if d := tf.CheckTrust(root, cfg.Hash()); d != format.TrustUnknown {
		t.Fatalf("project trust should be unknown at this point, got %v", d)
	}
}

// waitForFormatEvent drains the simulation screen's event queue
// until a formatDoneEvent shows up. Cap the wait at 2s so a hung
// goroutine fails the test instead of hanging CI forever.
func waitForFormatEvent(t *testing.T, a *App) *formatDoneEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ev := a.screen.PollEvent()
		if ev == nil {
			t.Fatal("screen returned nil event")
		}
		if fe, ok := ev.(*formatDoneEvent); ok {
			return fe
		}
	}
	t.Fatal("timed out waiting for formatDoneEvent")
	return nil
}
