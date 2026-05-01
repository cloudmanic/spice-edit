// =============================================================================
// File: internal/format/defaults.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package format

// Global format-defaults live at:
//
//	$XDG_CONFIG_HOME/spiceedit/format-defaults.json   (preferred)
//	~/.config/spiceedit/format-defaults.json          (fallback)
//
// The shape is identical to a per-project format.json:
//
//	{
//	  "commands": {
//	    "go":  ["gofmt", "-w", "$FILE"],
//	    "php": ["php-cs-fixer", "fix", "$FILE", "--quiet"]
//	  }
//	}
//
// These are the user's *personal* preferences — what they reach for
// in their own projects. They never run automatically: instead, when
// a save fires in a project where the project's format.json has no
// entry for that file's extension, the editor offers to "install"
// the default into the project's config (writing it into
// .spiceedit/format.json on consent). That's the only time defaults
// are consulted; without an install, the per-project config is the
// sole source of truth at save time.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// DefaultsFile is the filename used inside the user's config dir.
const DefaultsFile = "format-defaults.json"

// defaultsPathEnv lets tests redirect the defaults file location.
// Production code uses DefaultsPath. We use a separate env var from
// the trust override so a single test can set both independently.
var defaultsPathEnv = "SPICEEDIT_DEFAULTS_FILE"

// DefaultsPath returns the canonical defaults-file location:
// $XDG_CONFIG_HOME/spiceedit/format-defaults.json, falling back to
// ~/.config/spiceedit/format-defaults.json. Returns "" when neither
// resolves — the caller treats that as "no defaults file possible"
// and skips the install-prompt feature entirely.
//
// Tests can override the path via SPICEEDIT_DEFAULTS_FILE.
func DefaultsPath() string {
	if override := os.Getenv(defaultsPathEnv); override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "spiceedit", DefaultsFile)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "spiceedit", DefaultsFile)
}

// LoadDefaults reads the user's global format-defaults file. A
// missing file returns (nil, nil) — that's the "no defaults
// configured" signal, not an error. Parse errors are surfaced so a
// typo doesn't get silently ignored. Path may be "" when no config
// dir resolved; that yields (nil, nil) too.
func LoadDefaults(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// LoadDefaults intentionally leaves cfg.hash empty — defaults
	// don't go through the trust system, so there's no key to
	// compute. Project configs use Load (which sets the hash).
	return &cfg, nil
}

// InstallCommandIntoProject merges (ext, argv) into the project's
// format.json, preserving any existing entries. Creates the
// .spiceedit directory and the file if either is missing. Writes
// atomically (temp + rename) so a crash mid-write can't corrupt the
// config a teammate just pulled.
//
// Returns the new SHA-256 of the on-disk file so the caller can
// trust the freshly-written config without an extra read. (Same
// hash the next Load would produce — they share the same hashing
// path.) An empty hash + non-nil error means the write failed and
// the file was not modified; tests rely on that contract.
func InstallCommandIntoProject(rootDir, ext string, argv []string) (string, error) {
	if ext == "" {
		return "", errors.New("install: empty extension")
	}
	if len(argv) == 0 {
		return "", errors.New("install: empty argv")
	}
	dir := filepath.Join(rootDir, ConfigDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, ConfigFile)

	// Load existing entries so install acts as a merge, not a
	// replace. A user with `php` already configured shouldn't lose
	// that line when they install `go`.
	cfg, err := Load(rootDir)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		cfg = &Config{Commands: map[string][]string{}}
	}
	if cfg.Commands == nil {
		cfg.Commands = map[string][]string{}
	}
	cfg.Commands[ext] = append([]string(nil), argv...)

	data, err := json.MarshalIndent(struct {
		Commands map[string][]string `json:"commands"`
	}{cfg.Commands}, "", "  ")
	if err != nil {
		return "", err
	}
	// Trailing newline so the file plays nicely with tools that
	// expect POSIX text files (many editors, lints).
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}

	// Reload to pick up the freshly-written hash. We could compute
	// the hash inline from `data` but routing through Load means
	// install hashes can never drift from check-time hashes — they
	// share the same code path.
	fresh, err := Load(rootDir)
	if err != nil {
		return "", err
	}
	if fresh == nil {
		return "", errors.New("install: post-write reload found nothing")
	}
	return fresh.Hash(), nil
}
