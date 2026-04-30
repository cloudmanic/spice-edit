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
	"time"
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

// LogPath returns the canonical log location:
// $XDG_STATE_HOME/spiceedit/actions.log, falling back to
// ~/.local/state/spiceedit/actions.log when XDG_STATE_HOME isn't set.
// Returns "" when neither resolves to anything usable — callers
// should treat that as "no logging" and quietly skip.
//
// We use the XDG state directory, *not* config, because the file is
// generated and rewritten by the app — config is for hand-edited
// rules, state is for things the app produces (logs, caches, history).
func LogPath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "spiceedit", "actions.log")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", "spiceedit", "actions.log")
}

// RunRecord captures everything we want to log about one custom-action
// invocation. Time is the moment the command was launched; Duration
// is wall-clock from launch to exit. Output is the combined stdout +
// stderr the command produced (truncated by the caller if huge).
// ExitErr is nil when the command succeeded.
type RunRecord struct {
	Time     time.Time
	Duration time.Duration
	Label    string
	Command  string
	File     string
	Filename string
	ExitErr  error
	Output   []byte
}

// AppendLog appends a human-readable record of one run to logPath.
// Creates the parent directory on demand. Best-effort: any IO failure
// is returned for the caller's diagnostics, but the editor never
// blocks on or aborts because of a log write — runCustomAction
// ignores the return value on purpose.
//
// Format is intentionally line-oriented and grep-friendly:
//
//	[2026-04-30T13:26:32-07:00] Open on Rager (1.234s) → ok
//	  command: scp "$FILE" rager:~/Downloads/ ...
//	  FILE:     /abs/path/to/file
//	  FILENAME: file
//	  --- output ---
//	  <combined stdout + stderr, with trailing newline>
//	  --- end ---
//
// A blank line separates entries so two consecutive runs read clearly.
func AppendLog(logPath string, r RunRecord) error {
	if logPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	status := "ok"
	if r.ExitErr != nil {
		status = r.ExitErr.Error()
	}
	header := fmt.Sprintf("[%s] %s (%s) → %s\n",
		r.Time.Format(time.RFC3339),
		r.Label,
		r.Duration.Round(time.Millisecond),
		status,
	)
	body := fmt.Sprintf("  command: %s\n  FILE:     %s\n  FILENAME: %s\n  --- output ---\n",
		r.Command, r.File, r.Filename,
	)

	out := strings.TrimRight(string(r.Output), "\n")
	if out != "" {
		out += "\n"
	}

	if _, err := f.WriteString(header + body + out + "  --- end ---\n\n"); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	return nil
}
