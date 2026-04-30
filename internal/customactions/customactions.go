// =============================================================================
// File: internal/customactions/customactions.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// customactions loads user-defined shell-out actions from
// ~/.config/spiceedit/actions.json and exposes them to the editor's
// menu modal. The intended use case is the SSH-into-tmux workflow:
// the editor runs on a remote box, the user clicks an action like
// "Open on Rager", and the action shells out to scp the current file
// back to the user's laptop and run `open` over the reverse SSH
// connection. Anything an `sh -c` line can do is fair game.
//
// Schema:
//
//	{
//	  "actions": [
//	    {"label": "Open on Rager",
//	     "command": "scp \"$FILE\" rager:~/Downloads/ && ssh rager open ~/Downloads/\"$FILENAME\""},
//	    {"label": "Open on Cascade",
//	     "command": "scp \"$FILE\" cascade:~/Downloads/ && ssh cascade open ~/Downloads/\"$FILENAME\""}
//	  ]
//	}
//
// Two env vars are exported when the command runs:
//
//	FILE      — absolute path of the active tab's file
//	FILENAME  — basename of the same file
//
// Anything else the user wants from the environment they can pull in
// themselves (`$HOME`, `$USER`, etc.) — we just run `sh -c` and
// inherit the editor's environment.
//
// The loader is best-effort: a missing config file, malformed JSON,
// or any read error returns an empty action list rather than crashing
// the editor. We surface load errors via the returned error so the
// caller (App) can flash a status message if it wants to, but the
// editor still starts cleanly either way.

package customactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Action is one row that will appear in the menu modal. Label is the
// human-readable text the user clicks; Command is what we hand to
// `sh -c` when they do. We keep both as plain strings — no template
// pre-parsing — because shell expansion against $FILE / $FILENAME
// happens inside the spawned shell, not here.
type Action struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// fileFormat mirrors the on-disk JSON shape. Wrapped in a struct (vs
// a bare array) so we can grow new top-level keys later without
// breaking older config files.
type fileFormat struct {
	Actions []Action `json:"actions"`
}

// DefaultPath returns the canonical config-file location:
// $XDG_CONFIG_HOME/spiceedit/actions.json, falling back to
// ~/.config/spiceedit/actions.json when XDG_CONFIG_HOME isn't set.
// Returns "" when neither variable resolves to anything usable —
// callers should treat that as "no custom actions configured."
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "spiceedit", "actions.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "spiceedit", "actions.json")
}

// Load reads and parses the actions file at path. The contract:
//
//   - File doesn't exist          → (nil, nil). Not an error; the
//     user simply hasn't configured anything.
//   - File exists but unreadable  → (nil, err). The editor flashes a
//     status message; users notice and fix it.
//   - File parses but is empty    → (nil, nil). Same as "no file".
//   - Any individual action with
//     a blank label or command    → dropped silently. We'd rather
//     skip a half-written entry
//     than refuse the whole file.
func Load(path string) ([]Action, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil, nil
	}

	var ff fileFormat
	if err := json.Unmarshal(data, &ff); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	out := make([]Action, 0, len(ff.Actions))
	for _, a := range ff.Actions {
		a.Label = strings.TrimSpace(a.Label)
		a.Command = strings.TrimSpace(a.Command)
		if a.Label == "" || a.Command == "" {
			continue
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
