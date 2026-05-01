// =============================================================================
// File: internal/format/defaults_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package format

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDefaults_MissingFile pins the "no defaults configured"
// case: returns (nil, nil) so callers can cleanly skip the
// install-prompt path without surfacing a nonexistent-file error
// every save.
func TestLoadDefaults_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "format-defaults.json")
	cfg, err := LoadDefaults(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil cfg, got %#v", cfg)
	}
}

// TestLoadDefaults_EmptyPath covers the case where no config dir
// resolved at all (no $HOME, no XDG_CONFIG_HOME). Same nil/nil
// return so the caller can degrade gracefully.
func TestLoadDefaults_EmptyPath(t *testing.T) {
	cfg, err := LoadDefaults("")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil cfg, got %#v", cfg)
	}
}

// TestLoadDefaults_ParsesValidJSON is the happy path. Note that
// defaults intentionally leave Hash() empty — they never go through
// the trust system, so there's no key to compute.
func TestLoadDefaults_ParsesValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "format-defaults.json")
	body := `{"commands":{"go":["gofmt","-w","$FILE"],"php":["pint","$FILE"]}}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, err := LoadDefaults(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	if got := cfg.Commands["go"]; len(got) != 3 || got[0] != "gofmt" {
		t.Fatalf("commands[go]: %v", got)
	}
	if cfg.Hash() != "" {
		t.Fatalf("defaults should not carry a hash, got %q", cfg.Hash())
	}
}

// TestInstallCommandIntoProject_NewFile pins the bootstrap path:
// project has no .spiceedit dir at all, and the install creates
// both the dir and the file with the single command.
func TestInstallCommandIntoProject_NewFile(t *testing.T) {
	root := t.TempDir()
	hash, err := InstallCommandIntoProject(root, "go", []string{"gofmt", "-w", "$FILE"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash after install")
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg should exist after install")
	}
	if got := cfg.Commands["go"]; len(got) != 3 {
		t.Fatalf("argv: %v", got)
	}
	if cfg.Hash() != hash {
		t.Fatalf("returned hash %q != reload hash %q", hash, cfg.Hash())
	}
}

// TestInstallCommandIntoProject_PreservesExisting is the merge
// invariant: installing a new ext must not drop entries the user
// (or a teammate) already had. Without this, the install button
// would feel like a footgun on first use in a configured project.
func TestInstallCommandIntoProject_PreservesExisting(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ConfigDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"commands":{"php":["php-cs-fixer","fix","$FILE","--quiet"]}}`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(body), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := InstallCommandIntoProject(root, "go", []string{"gofmt", "-w", "$FILE"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := Load(root)
	if len(cfg.Commands) != 2 {
		t.Fatalf("expected 2 entries after install, got %d: %v", len(cfg.Commands), cfg.Commands)
	}
	if got := cfg.Commands["php"]; len(got) != 4 || got[0] != "php-cs-fixer" {
		t.Fatalf("php entry was clobbered: %v", got)
	}
	if got := cfg.Commands["go"]; len(got) != 3 || got[0] != "gofmt" {
		t.Fatalf("go entry missing or wrong: %v", got)
	}
}

// TestInstallCommandIntoProject_OverwritesSameExt is the second
// merge case: installing for an extension that's already present
// replaces just that entry. This is the "I changed my mind, use
// pint instead of php-cs-fixer" path.
func TestInstallCommandIntoProject_OverwritesSameExt(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallCommandIntoProject(root, "php", []string{"php-cs-fixer", "fix", "$FILE"}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := InstallCommandIntoProject(root, "php", []string{"pint", "$FILE"}); err != nil {
		t.Fatalf("second install: %v", err)
	}
	cfg, _ := Load(root)
	if got := cfg.Commands["php"]; len(got) != 2 || got[0] != "pint" {
		t.Fatalf("php entry should be replaced, got %v", got)
	}
}

// TestInstallCommandIntoProject_HashChangesPerInstall guarantees the
// trust system gets a fresh signal after every install. Same path,
// different bytes → different hash → trust file picks up the new
// authorisation cleanly without a stale entry shadowing it.
func TestInstallCommandIntoProject_HashChangesPerInstall(t *testing.T) {
	root := t.TempDir()
	h1, err := InstallCommandIntoProject(root, "go", []string{"gofmt", "-w", "$FILE"})
	if err != nil {
		t.Fatalf("install 1: %v", err)
	}
	h2, err := InstallCommandIntoProject(root, "php", []string{"pint", "$FILE"})
	if err != nil {
		t.Fatalf("install 2: %v", err)
	}
	if h1 == h2 {
		t.Fatalf("hash should change per install, both = %q", h1)
	}
}

// TestInstallCommandIntoProject_RejectsEmptyArgs is a small
// defensive check — a malformed call site (empty ext, empty argv)
// should error rather than silently writing a useless config entry.
func TestInstallCommandIntoProject_RejectsEmptyArgs(t *testing.T) {
	root := t.TempDir()
	if _, err := InstallCommandIntoProject(root, "", []string{"x"}); err == nil {
		t.Fatal("expected error for empty ext")
	}
	if _, err := InstallCommandIntoProject(root, "go", nil); err == nil {
		t.Fatal("expected error for empty argv")
	}
}

// TestDefaultsPath_Override is the test-only env hook so other
// packages can redirect the path without touching the user's real
// config dir. Catching a regression here means catching a leak of
// test state into the user's home dir.
func TestDefaultsPath_Override(t *testing.T) {
	t.Setenv(defaultsPathEnv, "/tmp/spice-defaults-test.json")
	if got := DefaultsPath(); got != "/tmp/spice-defaults-test.json" {
		t.Fatalf("override ignored: got %q", got)
	}
}
