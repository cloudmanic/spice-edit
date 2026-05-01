// =============================================================================
// File: internal/format/format_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig is a small helper that drops a format.json into a fake
// project root. Pulled out so each test reads as the scenario it's
// pinning down rather than the boilerplate of mkdir+write.
func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ConfigDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestLoad_MissingFileReturnsNil pins the opt-in behavior: no config
// file means "no formatting configured," not an error. Without this
// the editor would surface a noisy load error every time someone
// opened a project that simply doesn't use SpiceEdit's formatter.
func TestLoad_MissingFileReturnsNil(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg != nil {
		t.Fatalf("cfg: got %#v, want nil", cfg)
	}
}

// TestLoad_ParsesValidJSON is the happy path: a well-formed config
// returns a populated Commands map plus a non-empty hash that the
// trust system later keys on.
func TestLoad_ParsesValidJSON(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"commands": {"go": ["gofmt", "-w", "$FILE"]}}`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	got := cfg.Commands["go"]
	want := []string{"gofmt", "-w", "$FILE"}
	if len(got) != len(want) {
		t.Fatalf("commands[go]: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commands[go][%d]: got %q, want %q", i, got[i], want[i])
		}
	}
	if cfg.Hash() == "" {
		t.Fatal("hash should be populated after Load")
	}
}

// TestLoad_MalformedJSONErrors guards against silent fallback: a
// typo'd config should bubble up so the user can fix it, not
// quietly turn into "no formatters configured."
func TestLoad_MalformedJSONErrors(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{this is not json`)

	if _, err := Load(root); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// TestLoad_HashChangesWithContent confirms the trust system can tell
// when the config has been edited. Same path, different bytes →
// different hash → trust prompt re-fires.
func TestLoad_HashChangesWithContent(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"commands":{"go":["gofmt","-w","$FILE"]}}`)
	a, err := Load(root)
	if err != nil {
		t.Fatalf("load a: %v", err)
	}
	writeConfig(t, root, `{"commands":{"go":["goimports","-w","$FILE"]}}`)
	b, err := Load(root)
	if err != nil {
		t.Fatalf("load b: %v", err)
	}
	if a.Hash() == b.Hash() {
		t.Fatalf("hash should differ after content change, both = %q", a.Hash())
	}
}

// TestCommandFor_SubstitutesFileToken pins the only template feature
// we support: $FILE → absolute path. Anything fancier and we'd need
// a real template engine, which is overkill for a one-liner.
func TestCommandFor_SubstitutesFileToken(t *testing.T) {
	cfg := &Config{Commands: map[string][]string{
		"go": {"gofmt", "-w", "$FILE"},
	}}
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("package main"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := cfg.CommandFor(target)
	if len(got) != 3 {
		t.Fatalf("argv length: got %d, want 3 (%v)", len(got), got)
	}
	if got[0] != "gofmt" || got[1] != "-w" {
		t.Fatalf("argv head: got %v", got)
	}
	// $FILE should be absolute, not the relative form the caller passed.
	if !filepath.IsAbs(got[2]) {
		t.Fatalf("$FILE should be substituted as absolute path, got %q", got[2])
	}
	if !strings.HasSuffix(got[2], "main.go") {
		t.Fatalf("$FILE should resolve to main.go, got %q", got[2])
	}
}

// TestCommandFor_UnknownExtensionReturnsNil covers the "config exists
// but doesn't mention this file type" case — most files in a mixed
// project. Returning nil lets callers cleanly skip formatting.
func TestCommandFor_UnknownExtensionReturnsNil(t *testing.T) {
	cfg := &Config{Commands: map[string][]string{"go": {"gofmt", "-w", "$FILE"}}}
	if argv := cfg.CommandFor("/tmp/foo.txt"); argv != nil {
		t.Fatalf("got %v, want nil for unconfigured extension", argv)
	}
}

// TestCommandFor_NilConfigSafe makes nil-receiver use ergonomic for
// callers that haven't separately checked whether a config was found.
func TestCommandFor_NilConfigSafe(t *testing.T) {
	var cfg *Config
	if argv := cfg.CommandFor("/tmp/foo.go"); argv != nil {
		t.Fatalf("nil config should return nil, got %v", argv)
	}
}

// TestCommandFor_NoExtensionReturnsNil covers files like "Makefile" —
// we only resolve formatters by extension, so a name with no ext
// gets no formatter.
func TestCommandFor_NoExtensionReturnsNil(t *testing.T) {
	cfg := &Config{Commands: map[string][]string{"go": {"gofmt", "-w", "$FILE"}}}
	if argv := cfg.CommandFor("/tmp/Makefile"); argv != nil {
		t.Fatalf("got %v, want nil for extension-less file", argv)
	}
}

// TestCommandFor_DoesNotMutateTemplate guarantees repeated calls
// produce independent slices — important because the caller may pass
// argv into exec.Command which holds onto it.
func TestCommandFor_DoesNotMutateTemplate(t *testing.T) {
	cfg := &Config{Commands: map[string][]string{"go": {"gofmt", "-w", "$FILE"}}}
	first := cfg.CommandFor("/tmp/a.go")
	first[0] = "tampered"
	second := cfg.CommandFor("/tmp/b.go")
	if second[0] != "gofmt" {
		t.Fatalf("template was mutated: %v", second)
	}
}

// TestConfigPath is a smoke test guarding the exact on-disk location.
// If this constant ever moves we want a loud failure, since people
// will have files committed at the old path.
func TestConfigPath(t *testing.T) {
	got := ConfigPath("/proj")
	want := filepath.Join("/proj", ".spiceedit", "format.json")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
