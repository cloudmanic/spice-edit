// =============================================================================
// File: internal/customactions/customactions_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the customactions loader. Cover the contract documented on
// Load: missing file is fine, malformed JSON surfaces an error, empty
// or partially-blank entries are dropped, and the path resolver
// respects XDG_CONFIG_HOME ahead of $HOME.

package customactions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoad_MissingFile confirms the "no config" case is silent — we
// shouldn't fail editor startup just because the user hasn't created
// the file.
func TestLoad_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	got, err := Load(path)
	if err != nil {
		t.Fatalf("missing file: err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("missing file: got %v, want nil", got)
	}
}

// TestLoad_EmptyPath is the corner case where DefaultPath bailed
// (no XDG, no HOME). Caller passes "" — same silent no-op result.
func TestLoad_EmptyPath(t *testing.T) {
	if got, err := Load(""); err != nil || got != nil {
		t.Fatalf("empty path: got (%v, %v), want (nil, nil)", got, err)
	}
}

// TestLoad_EmptyFile mirrors a user who created the file then never
// wrote to it. Treat as "no actions" rather than a parse error.
func TestLoad_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("empty file: err = %v", err)
	}
	if got != nil {
		t.Fatalf("empty file: got %v, want nil", got)
	}
}

// TestLoad_HappyPath exercises the schema we ship in the README:
// two actions, both well-formed. Order is preserved.
func TestLoad_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {"label": "Open on Rager",   "command": "scp \"$FILE\" rager:~/Downloads/"},
	    {"label": "Open on Cascade", "command": "scp \"$FILE\" cascade:~/Downloads/"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Label != "Open on Rager" {
		t.Errorf("got[0].Label = %q", got[0].Label)
	}
	if !strings.Contains(got[1].Command, "cascade") {
		t.Errorf("got[1].Command = %q, expected cascade target", got[1].Command)
	}
}

// TestLoad_HappyPathWithPrompts walks the form-driven action shape:
// a Copy-from-remote action with a select host, a defaulted local
// destination, and a remote-source text field. Pins that prompts
// survive the round-trip in declaration order so the form modal
// renders fields in the same order the user wrote them.
func TestLoad_HappyPathWithPrompts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {
	      "label": "Copy from remote",
	      "prompts": [
	        {"key": "HOST",       "label": "Host",
	         "type": "select", "options": ["cascade", "rager"]},
	        {"key": "DEST_DIR",   "label": "Local destination",
	         "type": "text", "default": "${ACTIVE_FOLDER}"},
	        {"key": "REMOTE_SRC", "label": "Remote file", "type": "text"}
	      ],
	      "command": "scp \"$HOST:$REMOTE_SRC\" \"$DEST_DIR/\""
	    }
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || len(got[0].Prompts) != 3 {
		t.Fatalf("got %+v, want 1 action with 3 prompts", got)
	}
	if got[0].Prompts[0].Type != PromptSelect ||
		len(got[0].Prompts[0].Options) != 2 {
		t.Errorf("HOST prompt malformed: %+v", got[0].Prompts[0])
	}
	if got[0].Prompts[1].Default != "${ACTIVE_FOLDER}" {
		t.Errorf("DEST_DIR default lost: %q", got[0].Prompts[1].Default)
	}
}

// TestLoad_RejectsBadPromptKey enforces the env-var-safe shape on
// Prompt.Key. A lowercase or punctuation-laden key would either fail
// to read back as $KEY in the shell or silently shadow nothing — the
// user wants a typo to surface, not to silently misbehave at runtime.
func TestLoad_RejectsBadPromptKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {"label": "X", "command": "echo $foo",
	     "prompts": [{"key": "foo-bar", "label": "F", "type": "text"}]}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "foo-bar") {
		t.Fatalf("err = %v, want error mentioning foo-bar", err)
	}
}

// TestLoad_RejectsDuplicatePromptKey catches the easy mistake of
// pasting two HOST rows. The second would silently overwrite the
// first's env var; better to refuse to load and tell the user.
func TestLoad_RejectsDuplicatePromptKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {"label": "X", "command": "echo $HOST",
	     "prompts": [
	       {"key": "HOST", "label": "A", "type": "text"},
	       {"key": "HOST", "label": "B", "type": "text"}
	     ]}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v, want duplicate-key error", err)
	}
}

// TestLoad_RejectsSelectWithoutOptions guards against a select prompt
// the user can never resolve — without options there's nothing to
// pick, and the form modal would refuse to submit forever.
func TestLoad_RejectsSelectWithoutOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {"label": "X", "command": "echo $HOST",
	     "prompts": [{"key": "HOST", "label": "Host", "type": "select"}]}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "option") {
		t.Fatalf("err = %v, want options error", err)
	}
}

// TestLoad_RejectsUnknownPromptType pins the closed-set rule on
// Type. A typo'd "selct" or "input" would render as a blank row the
// user can't fill in, so this is loud-fail territory.
func TestLoad_RejectsUnknownPromptType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {"label": "X", "command": "echo $HOST",
	     "prompts": [{"key": "HOST", "label": "Host", "type": "selct"}]}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "selct") {
		t.Fatalf("err = %v, want unknown-type error", err)
	}
}

// TestLoad_DropsBlankEntries confirms half-written entries are
// silently skipped — users editing the file shouldn't see the rest
// of their actions vanish because one row is mid-edit.
func TestLoad_DropsBlankEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{
	  "actions": [
	    {"label": "Real",   "command": "echo hi"},
	    {"label": "",        "command": "echo no-label"},
	    {"label": "no-cmd",  "command": ""},
	    {"label": "  ",      "command": "  "}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].Label != "Real" {
		t.Fatalf("got %+v, want one Real entry", got)
	}
}

// TestLoad_AllBlankReturnsNil confirms a file full of blanks ends up
// indistinguishable from "no file" — the menu shouldn't render an
// empty custom-actions group.
func TestLoad_AllBlankReturnsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	const body = `{"actions":[{"label":"","command":""}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

// TestLoad_BadJSONIsAnError surfaces malformed JSON to the caller so
// the editor can flash a status message — the user's typo shouldn't
// silently strand them with a half-loaded actions list.
func TestLoad_BadJSONIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// TestDefaultPath_PrefersXDG asserts XDG_CONFIG_HOME wins when set —
// users who follow the XDG convention shouldn't get their config
// silently ignored.
func TestDefaultPath_PrefersXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")
	got := DefaultPath()
	want := filepath.Join("/tmp/xdgtest", "spiceedit", "actions.json")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

// TestDefaultPath_FallsBackToHome covers the common case — no XDG
// env, plain Mac/Linux home directory.
func TestDefaultPath_FallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/Users/test")
	got := DefaultPath()
	want := filepath.Join("/Users/test", ".config", "spiceedit", "actions.json")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

// TestLogPath_PrefersXDGState confirms the state location respects
// XDG_STATE_HOME — distinct from XDG_CONFIG_HOME used by the config
// file. Logs are app-generated, configs are user-edited; they live
// in different XDG dirs by design.
func TestLogPath_PrefersXDGState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdgstate")
	got := LogPath()
	want := filepath.Join("/tmp/xdgstate", "spiceedit", "actions.log")
	if got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}

// TestLogPath_FallsBackToHome covers the common case — no XDG_STATE_HOME,
// log lives under ~/.local/state/spiceedit/.
func TestLogPath_FallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/Users/test")
	got := LogPath()
	want := filepath.Join("/Users/test", ".local", "state", "spiceedit", "actions.log")
	if got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}

// TestAppendLog_SuccessEntry pins down the line-by-line shape of a
// successful run's log entry. The format is the editor's main
// debugging surface — if it changes, tooling that greps the file
// changes too, so we lock down the structure rather than just
// "did anything write?"
func TestAppendLog_SuccessEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actions.log")
	rec := RunRecord{
		Time:     time.Date(2026, 4, 30, 13, 26, 32, 0, time.UTC),
		Duration: 1234 * time.Millisecond,
		Label:    "Open on Rager",
		Command:  `scp "$FILE" rager:~/Downloads/`,
		File:     "/Users/spicer/dev/foo.txt",
		Filename: "foo.txt",
		Output:   []byte("hello\nworld\n"),
	}
	if err := AppendLog(path, rec); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(got)
	for _, fragment := range []string{
		"[2026-04-30T13:26:32Z] Open on Rager (1.234s) → ok",
		`  command: scp "$FILE" rager:~/Downloads/`,
		"  FILE:     /Users/spicer/dev/foo.txt",
		"  FILENAME: foo.txt",
		"  --- output ---",
		"hello\nworld",
		"  --- end ---",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("log missing fragment %q\nfull log:\n%s", fragment, body)
		}
	}
}

// TestAppendLog_FailureRecordsErrorString verifies a failed run lands
// the error message in the status line so the user can grep for the
// actual exit error in a sea of successes.
func TestAppendLog_FailureRecordsErrorString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actions.log")
	rec := RunRecord{
		Time:    time.Date(2026, 4, 30, 13, 27, 1, 0, time.UTC),
		Label:   "Open on Cascade",
		Command: "false",
		ExitErr: errors.New("exit status 1"),
		Output:  []byte("ssh: connect refused\n"),
	}
	if err := AppendLog(path, rec); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "→ exit status 1") {
		t.Fatalf("expected exit-status arrow line; got:\n%s", body)
	}
	if !strings.Contains(string(body), "ssh: connect refused") {
		t.Fatalf("expected stderr captured; got:\n%s", body)
	}
}

// TestAppendLog_AppendsNotTruncates is the regression test for the
// O_APPEND flag. Two writes, both entries present, in order.
func TestAppendLog_AppendsNotTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actions.log")
	for _, label := range []string{"first", "second"} {
		if err := AppendLog(path, RunRecord{
			Time:  time.Now(),
			Label: label,
		}); err != nil {
			t.Fatalf("AppendLog %s: %v", label, err)
		}
	}
	body, _ := os.ReadFile(path)
	firstIdx := strings.Index(string(body), "] first")
	secondIdx := strings.Index(string(body), "] second")
	if firstIdx < 0 || secondIdx < 0 {
		t.Fatalf("expected both entries, got:\n%s", body)
	}
	if firstIdx >= secondIdx {
		t.Fatalf("entries out of order:\n%s", body)
	}
}

// TestAppendLog_CreatesParentDir confirms a missing directory tree
// doesn't cause the first action to silently lose its log entry.
func TestAppendLog_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "actions.log")
	if err := AppendLog(path, RunRecord{Time: time.Now(), Label: "x"}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

// TestAppendLog_EmptyPathIsNoOp lets callers pass "" (when LogPath
// can't resolve a home dir) without producing an error.
func TestAppendLog_EmptyPathIsNoOp(t *testing.T) {
	if err := AppendLog("", RunRecord{Label: "x"}); err != nil {
		t.Fatalf("empty path AppendLog: %v", err)
	}
}
