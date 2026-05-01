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
//	    {"label": "Copy from remote",
//	     "prompts": [
//	       {"key": "HOST",       "label": "Host", "type": "select",
//	        "options": ["cascade", "rager"]},
//	       {"key": "DEST_DIR",   "label": "Local destination",
//	        "type": "text", "default": "${ACTIVE_FOLDER}"},
//	       {"key": "REMOTE_SRC", "label": "Remote file", "type": "text"}
//	     ],
//	     "command": "scp \"$HOST:$REMOTE_SRC\" \"$DEST_DIR/\""}
//	  ]
//	}
//
// When prompts is non-empty the editor opens a small form modal before
// running the command. Each prompt's value is exported as an env var
// named after Key. Defaults can include the editor-state variables
// listed below — they get expanded when the modal opens.
//
// Editor-state env vars exported on every action run:
//
//	FILE                — absolute path of the active tab's file (if any)
//	FILENAME            — basename of FILE
//	PROJECT_ROOT        — absolute path of the project root
//	ACTIVE_FOLDER       — absolute path of the sidebar's active folder
//	ACTIVE_FOLDER_REL   — same, relative to PROJECT_ROOT
//	CURRENT_FILE        — alias of FILE for prompt-default symmetry
//	CURRENT_FILE_REL    — FILE relative to PROJECT_ROOT
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
	"regexp"
	"strings"
	"time"
)

// Action is one row that will appear in the menu modal. Label is the
// human-readable text the user clicks; Command is what we hand to
// `sh -c` when they do. Prompts is an optional list of input fields
// the editor collects from the user before running Command — each
// field's value is exported as an env var the shell can read.
//
// We keep Command as a plain string (no template pre-parsing) because
// shell expansion against $HOST / $FILE / $FILENAME happens inside
// the spawned shell, which both keeps the loader simple and avoids
// re-implementing shell quoting in Go.
type Action struct {
	Label   string   `json:"label"`
	Command string   `json:"command"`
	Prompts []Prompt `json:"prompts,omitempty"`
}

// PromptType is the input widget the form modal renders for a Prompt.
// Two flavours for now — text for free-form strings, select for a
// fixed option list — so the schema's validation surface stays small.
type PromptType string

const (
	PromptText   PromptType = "text"
	PromptSelect PromptType = "select"
)

// Prompt is one field in a form modal. Key becomes the env var name
// the shell command reads; Label is the human-readable row title;
// Default seeds the field on open and may include editor-state
// variables like ${ACTIVE_FOLDER}, expanded by the caller before the
// modal renders. Options is required iff Type == PromptSelect.
type Prompt struct {
	Key     string     `json:"key"`
	Label   string     `json:"label"`
	Type    PromptType `json:"type"`
	Options []string   `json:"options,omitempty"`
	Default string     `json:"default,omitempty"`
}

// validKeyRE matches the env-var-safe identifier shape we require for
// Prompt.Key: an uppercase letter or underscore, then any number of
// uppercase letters / digits / underscores. This lines up with what
// POSIX shells will reliably read back as `$KEY` without surprises.
var validKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

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
//   - An action whose Prompts list
//     is malformed                → returns an error naming the
//                                    offending action's label so the
//                                    user can find it in the file.
//                                    Prompts are too easy to typo and
//                                    too dangerous to silently skip
//                                    (a missing select option means
//                                    the user can never submit the
//                                    form), so this one we surface.
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
		if err := validatePrompts(a.Label, a.Prompts); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// validatePrompts checks an action's prompt list for the rules the
// form modal and the env-var injection layer rely on:
//
//   - Every Key must match validKeyRE so the shell can read it back
//     as $KEY without quoting surprises.
//   - No two prompts in the same action may share a Key — last-write
//     wins on the env var, but the user clearly meant something
//     different so this is almost certainly a typo.
//   - Type must be one of the recognised constants. An unknown type
//     would render as a blank row the user can't fill in.
//   - Select prompts need at least one option, otherwise the user
//     has nothing to pick and the form can never submit.
//
// Errors quote the action's Label so the user can grep their config.
func validatePrompts(actionLabel string, prompts []Prompt) error {
	if len(prompts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(prompts))
	for i, p := range prompts {
		if !validKeyRE.MatchString(p.Key) {
			return fmt.Errorf("action %q prompt[%d]: key %q must match [A-Z_][A-Z0-9_]*",
				actionLabel, i, p.Key)
		}
		if _, dup := seen[p.Key]; dup {
			return fmt.Errorf("action %q: duplicate prompt key %q",
				actionLabel, p.Key)
		}
		seen[p.Key] = struct{}{}
		switch p.Type {
		case PromptText:
			// nothing to check — Default may be empty.
		case PromptSelect:
			if len(p.Options) == 0 {
				return fmt.Errorf("action %q prompt[%d] (%q): select needs at least one option",
					actionLabel, i, p.Key)
			}
		default:
			return fmt.Errorf("action %q prompt[%d] (%q): unknown type %q (want %q or %q)",
				actionLabel, i, p.Key, p.Type, PromptText, PromptSelect)
		}
	}
	return nil
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
