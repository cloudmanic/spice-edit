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
	"os"
	"path/filepath"
	"strings"
	"testing"
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
