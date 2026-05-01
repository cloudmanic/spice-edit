// =============================================================================
// File: internal/app/actionvars.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// actionvars.go owns the small set of editor-state variables that custom
// actions can reference — both inside a Prompt's Default string (expanded
// when the form modal opens) and as env vars exported to the spawned
// shell command. The set is intentionally closed: a fixed list of well-
// understood variables beats an open-ended templating language for a
// feature whose users are writing one-line shell snippets.

package app

import (
	"path/filepath"
	"strings"

	"github.com/cloudmanic/spice-edit/internal/customactions"
)

// actionVars snapshots the editor-state variables a custom action can
// reference. Built once per action invocation so every Default expansion
// and every env-var export sees the same values, even if the user is
// somehow racing the action against a tab switch.
type actionVars struct {
	File            string // absolute path of the active tab's file, or ""
	Filename        string // basename of File
	ProjectRoot     string // absolute path of the project root
	ActiveFolder    string // absolute path of the sidebar's active folder
	ActiveFolderRel string // ActiveFolder relative to ProjectRoot
	CurrentFile     string // alias of File for prompt-default symmetry
	CurrentFileRel  string // File relative to ProjectRoot
}

// captureActionVars freezes the variables from app state at call time.
// Pulled out of runCustomAction so the form modal can call the same
// resolver to expand Prompt.Default strings before showing the form.
func (a *App) captureActionVars() actionVars {
	v := actionVars{
		ProjectRoot:  a.rootDir,
		ActiveFolder: a.activeFolder,
	}
	if v.ActiveFolder == "" {
		v.ActiveFolder = a.rootDir
	}
	if abs, err := filepath.Abs(v.ProjectRoot); err == nil {
		v.ProjectRoot = abs
	}
	if abs, err := filepath.Abs(v.ActiveFolder); err == nil {
		v.ActiveFolder = abs
	}
	v.ActiveFolderRel = relOrEmpty(v.ProjectRoot, v.ActiveFolder)

	if tab := a.activeTabPtr(); tab != nil && tab.Path != "" {
		if abs, err := filepath.Abs(tab.Path); err == nil {
			v.File = abs
		} else {
			v.File = tab.Path
		}
		v.Filename = filepath.Base(v.File)
		v.CurrentFile = v.File
		v.CurrentFileRel = relOrEmpty(v.ProjectRoot, v.File)
	}
	return v
}

// expand replaces every ${NAME} occurrence in s with the corresponding
// field value, leaving unknown ${...} tokens alone. We deliberately
// don't honour bare $NAME (no braces) so a Default string like
// "$HOME/foo" stays a literal — the shell will see it later, but the
// modal display shouldn't pre-resolve it.
func (v actionVars) expand(s string) string {
	if s == "" || !strings.Contains(s, "${") {
		return s
	}
	repl := strings.NewReplacer(
		"${FILE}", v.File,
		"${FILENAME}", v.Filename,
		"${PROJECT_ROOT}", v.ProjectRoot,
		"${ACTIVE_FOLDER}", v.ActiveFolder,
		"${ACTIVE_FOLDER_REL}", v.ActiveFolderRel,
		"${CURRENT_FILE}", v.CurrentFile,
		"${CURRENT_FILE_REL}", v.CurrentFileRel,
	)
	return repl.Replace(s)
}

// envSlice formats the variables as KEY=VALUE strings suitable for
// appending to exec.Cmd.Env. The shell command can then read them
// back as $FILE, $ACTIVE_FOLDER, etc. without any extra plumbing.
// Empty values are still emitted — an action that branches on
// `[ -z "$FILE" ]` to detect "no file open" must see an empty
// FILE rather than no FILE at all.
func (v actionVars) envSlice() []string {
	return []string{
		"FILE=" + v.File,
		"FILENAME=" + v.Filename,
		"PROJECT_ROOT=" + v.ProjectRoot,
		"ACTIVE_FOLDER=" + v.ActiveFolder,
		"ACTIVE_FOLDER_REL=" + v.ActiveFolderRel,
		"CURRENT_FILE=" + v.CurrentFile,
		"CURRENT_FILE_REL=" + v.CurrentFileRel,
	}
}

// promptValuesEnv turns the form-modal's collected (key, value) pairs
// into the same KEY=VALUE shape exec.Cmd.Env wants. Pulled out so
// runCustomAction's wiring stays declarative — capture vars, expand
// defaults, render modal, take results, append.
func promptValuesEnv(prompts []customactions.Prompt, values map[string]string) []string {
	out := make([]string, 0, len(prompts))
	for _, p := range prompts {
		out = append(out, p.Key+"="+values[p.Key])
	}
	return out
}

// relOrEmpty returns target relative to base when both are non-empty
// and the relative path doesn't escape base. Anything else falls back
// to an empty string — better than dropping a "../../tmp/whatever"
// surprise into the shell, and an unset variable is something the
// command author can detect with `[ -n "$ACTIVE_FOLDER_REL" ]`.
func relOrEmpty(base, target string) string {
	if base == "" || target == "" {
		return ""
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." {
		return ""
	}
	if strings.HasPrefix(rel, "..") {
		return ""
	}
	// On Windows filepath.Rel returns backslashes; the shell command
	// runs through `sh -c` and the editor's main targets are POSIX,
	// so normalise. This is a no-op on Mac / Linux.
	return filepath.ToSlash(rel)
}

