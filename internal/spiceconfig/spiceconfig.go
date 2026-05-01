// =============================================================================
// File: internal/spiceconfig/spiceconfig.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package spiceconfig loads the editor's small user-level config from
// ~/.config/spiceedit/config.json. It's separate from customactions on
// purpose: actions.json is a list of shell-out menu entries, config.json
// is editor preferences. Keeping them apart means a malformed actions
// file can't break editor settings and vice-versa.
//
// Schema today is intentionally tiny — one key — but the loader is
// already wrapped in a struct so we can grow new top-level fields
// without breaking older configs:
//
//	{"icons": "auto"}    // default; auto-detect Nerd Fonts on startup
//	{"icons": "on"}      // force-on, even if detection would say no
//	{"icons": "off"}     // force-off, even if a Nerd Font is installed
//
// The loader is best-effort the same way customactions is: missing
// file → defaults, malformed file → error returned for the app to
// flash, but the editor still starts cleanly.
package spiceconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IconsMode is the user's preference for Nerd Font icons in the file
// tree. "auto" means "use them iff a Nerd Font is installed"; the
// other two values bypass detection entirely.
type IconsMode string

const (
	IconsAuto IconsMode = "auto"
	IconsOn   IconsMode = "on"
	IconsOff  IconsMode = "off"
)

// Config is the resolved, validated form of config.json. Callers get a
// fully-populated Config back from Load — defaults are filled in for
// any field the file omitted, so consumers never need to nil-check.
type Config struct {
	Icons IconsMode
}

// Defaults returns a Config populated with the values used when no
// config file is present (or every field in it is blank). Centralised
// so tests and the loader can't drift from each other.
func Defaults() Config {
	return Config{Icons: IconsAuto}
}

// fileFormat mirrors the on-disk JSON shape. We decode into this and
// then promote into Config so the public type doesn't have to carry
// JSON tags or pointer fields just for "field was absent" detection.
type fileFormat struct {
	Icons string `json:"icons,omitempty"`
}

// DefaultPath returns the canonical config-file location:
// $XDG_CONFIG_HOME/spiceedit/config.json, falling back to
// ~/.config/spiceedit/config.json. Returns "" when neither resolves
// — callers should treat that as "use defaults".
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "spiceedit", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "spiceedit", "config.json")
}

// Load reads and parses the config file at path, returning a Config
// with defaults filled in for any missing or blank fields.
//
// Contract:
//   - path == ""              → (Defaults(), nil). Treated as "no
//     config configured".
//   - file doesn't exist      → (Defaults(), nil). Same as above.
//   - file unreadable         → (Defaults(), err). Caller can flash a
//     message; editor keeps running on defaults.
//   - file empty / all-blank  → (Defaults(), nil).
//   - unknown icons value     → (Defaults(), err). We'd rather tell
//     the user their config has a typo than silently fall back to
//     defaults and hide the bug.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return cfg, nil
	}

	var ff fileFormat
	if err := json.Unmarshal(data, &ff); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}

	switch IconsMode(strings.ToLower(strings.TrimSpace(ff.Icons))) {
	case "":
		// field omitted — keep default
	case IconsAuto:
		cfg.Icons = IconsAuto
	case IconsOn:
		cfg.Icons = IconsOn
	case IconsOff:
		cfg.Icons = IconsOff
	default:
		return Defaults(), fmt.Errorf(
			"%s: icons must be %q, %q, or %q (got %q)",
			path, IconsAuto, IconsOn, IconsOff, ff.Icons,
		)
	}
	return cfg, nil
}
