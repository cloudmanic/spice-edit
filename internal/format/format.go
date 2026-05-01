// =============================================================================
// File: internal/format/format.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package format loads the per-project formatter configuration that
// drives format-on-save. The config lives at
//
//	<projectRoot>/.spiceedit/format.json
//
// and looks like:
//
//	{
//	  "commands": {
//	    "go":  ["gofmt", "-w", "$FILE"],
//	    "php": ["php-cs-fixer", "fix", "$FILE", "--quiet"],
//	    "py":  ["ruff", "format", "$FILE"]
//	  }
//	}
//
// Keys are file extensions without the leading dot. Values are argv
// arrays — we exec.Command directly, no shell, no injection surface.
// $FILE in any arg is substituted with the absolute file path.
//
// The whole feature is opt-in: no config file → no formatting, ever.
// That keeps quick edits to a stranger's repo from silently rewriting
// their files. When the config exists but the user has not yet
// approved it for this project, the app prompts via the trust system
// in this same package (see trust.go).
package format

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir is the directory inside a project that holds SpiceEdit's
// project-local config. We deliberately use a folder (not a single
// dotfile) so we can grow other per-project files later without
// adding more top-level entries to people's repos.
const ConfigDir = ".spiceedit"

// ConfigFile is the formatter-config filename inside ConfigDir.
const ConfigFile = "format.json"

// FileToken is the argv placeholder substituted with the absolute
// path of the file being formatted. Lifted into a constant so tests
// and docs reference the same string the resolver uses.
const FileToken = "$FILE"

// Config is the parsed shape of format.json. Commands maps an
// extension (no leading dot, e.g. "go") to the argv we'll exec when a
// file with that extension is saved.
type Config struct {
	Commands map[string][]string `json:"commands"`

	// hash is the SHA-256 of the on-disk file contents at load time.
	// The trust system keys decisions on (path, hash) so a teammate
	// changing the formatter command re-prompts the user — fixing the
	// "trust once, run anything forever" footgun.
	hash string
}

// Hash returns the SHA-256 of the on-disk contents this Config was
// loaded from, hex-encoded. Empty when the Config wasn't loaded from
// disk (e.g. tests that build one directly).
func (c *Config) Hash() string {
	if c == nil {
		return ""
	}
	return c.hash
}

// ConfigPath returns the canonical location of format.json inside a
// project root. Pure path math — does not check for existence.
func ConfigPath(rootDir string) string {
	return filepath.Join(rootDir, ConfigDir, ConfigFile)
}

// Load reads and parses the format config at <rootDir>/.spiceedit/format.json.
// Returns (nil, nil) when the file does not exist — that's the
// "no formatting configured" signal, not an error. Returns an error
// only for IO problems and malformed JSON, which the caller should
// surface so a typo doesn't get silently ignored.
func Load(rootDir string) (*Config, error) {
	path := ConfigPath(rootDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	cfg.hash = hex.EncodeToString(sum[:])
	return &cfg, nil
}

// CommandFor returns the argv to run for the given file path, with
// FileToken substituted to the file's absolute path. Returns nil
// when the file's extension has no entry in the config — the caller
// treats that as "no formatting for this file," not an error.
//
// The returned slice is a fresh copy; callers can append/mutate
// without disturbing the cached Config.
func (c *Config) CommandFor(filePath string) []string {
	if c == nil || len(c.Commands) == 0 {
		return nil
	}
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	if ext == "" {
		return nil
	}
	template := c.Commands[ext]
	if len(template) == 0 {
		return nil
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		abs = filePath
	}
	out := make([]string, len(template))
	for i, arg := range template {
		out[i] = strings.ReplaceAll(arg, FileToken, abs)
	}
	return out
}
