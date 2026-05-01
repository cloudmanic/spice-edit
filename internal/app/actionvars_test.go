// =============================================================================
// File: internal/app/actionvars_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/customactions"
)

// TestCaptureActionVars_NoFileOpen pins the no-file-open case: the
// SCP-from-remote action doesn't need a tab open, so File / Filename
// must be empty strings rather than referencing a previous tab or
// blowing up. ProjectRoot and ActiveFolder still resolve from app
// state.
func TestCaptureActionVars_NoFileOpen(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)

	v := a.captureActionVars()
	if v.File != "" || v.Filename != "" {
		t.Errorf("expected empty File/Filename with no tab open: %+v", v)
	}
	if !strings.HasSuffix(v.ProjectRoot, filepath.Base(root)) {
		t.Errorf("ProjectRoot = %q, want a path ending in %q",
			v.ProjectRoot, filepath.Base(root))
	}
	// ActiveFolder defaults to ProjectRoot when nothing more specific
	// is selected — without this the form modal would render an empty
	// default for any "Local destination" prompt that uses
	// ${ACTIVE_FOLDER}.
	if v.ActiveFolder != v.ProjectRoot {
		t.Errorf("ActiveFolder %q should match ProjectRoot %q at startup",
			v.ActiveFolder, v.ProjectRoot)
	}
	if v.ActiveFolderRel != "" {
		t.Errorf("ActiveFolderRel should be empty when ActiveFolder == root, got %q",
			v.ActiveFolderRel)
	}
}

// TestCaptureActionVars_SubfolderRel verifies the relative-path
// fields actually reflect a non-root active folder. This is the
// case the user cares about: dropping a copied file into a subdir
// requires ${ACTIVE_FOLDER_REL} to expand to "subdir", not "".
func TestCaptureActionVars_SubfolderRel(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	sub := filepath.Join(root, "internal", "app")
	if err := makeDirAll(sub); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a.setActiveFolder(sub)

	v := a.captureActionVars()
	if v.ActiveFolderRel != "internal/app" {
		t.Errorf("ActiveFolderRel = %q, want %q", v.ActiveFolderRel, "internal/app")
	}
	if v.ActiveFolder != sub {
		// On macOS t.TempDir() returns a path under /var/folders that
		// resolves through /private/var via a symlink. We tolerate
		// that resolution since filepath.Abs follows symlinks lazily,
		// but the suffix should always be the subfolder we made.
		if !strings.HasSuffix(v.ActiveFolder, filepath.Join("internal", "app")) {
			t.Errorf("ActiveFolder = %q, want suffix internal/app", v.ActiveFolder)
		}
	}
}

// TestActionVars_Expand walks the full substitution table. The
// modal calls expand on every Default before showing it, so any
// drift between the env-var set and the expander would mean
// "${FOO}" silently leaks through to the user as literal text.
func TestActionVars_Expand(t *testing.T) {
	v := actionVars{
		File:            "/p/foo.go",
		Filename:        "foo.go",
		ProjectRoot:     "/p",
		ActiveFolder:    "/p/sub",
		ActiveFolderRel: "sub",
		CurrentFile:     "/p/foo.go",
		CurrentFileRel:  "foo.go",
	}
	cases := []struct{ in, want string }{
		{"${ACTIVE_FOLDER}", "/p/sub"},
		{"${PROJECT_ROOT}/${ACTIVE_FOLDER_REL}", "/p/sub"},
		{"file=${FILENAME}", "file=foo.go"},
		{"plain string", "plain string"},
		// Bare $VAR (no braces) is intentionally left alone — the
		// shell will see it later, the modal preview shouldn't.
		{"$ACTIVE_FOLDER", "$ACTIVE_FOLDER"},
		// Unknown brace tokens pass through so a typo in a
		// user's actions.json is visible at preview time, not
		// silently swallowed.
		{"${MYSTERY}", "${MYSTERY}"},
	}
	for _, c := range cases {
		got := v.expand(c.in)
		if got != c.want {
			t.Errorf("expand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestActionVars_EnvSlice checks the env-var formatting that the
// shell runner appends to cmd.Env. Empty values must still emit so
// "[ -z \"$FILE\" ]" works for command authors who key off the
// no-file-open case.
func TestActionVars_EnvSlice(t *testing.T) {
	v := actionVars{
		File:            "",
		Filename:        "",
		ProjectRoot:     "/p",
		ActiveFolder:    "/p",
		ActiveFolderRel: "",
		CurrentFile:     "",
		CurrentFileRel:  "",
	}
	got := v.envSlice()
	have := map[string]bool{}
	for _, kv := range got {
		have[kv] = true
	}
	for _, want := range []string{
		"FILE=", "FILENAME=", "PROJECT_ROOT=/p",
		"ACTIVE_FOLDER=/p", "ACTIVE_FOLDER_REL=",
		"CURRENT_FILE=", "CURRENT_FILE_REL=",
	} {
		if !have[want] {
			t.Errorf("envSlice missing %q (got %v)", want, got)
		}
	}
}

// TestPromptValuesEnv pins the (key, value) → KEY=VALUE shape and
// the order: the form's KEY=VALUE pairs ride on top of the editor-
// state vars, in declaration order, so the user's config reads
// top-to-bottom in the env list too.
func TestPromptValuesEnv(t *testing.T) {
	prompts := []customactions.Prompt{
		{Key: "HOST", Type: customactions.PromptSelect, Options: []string{"a"}},
		{Key: "DEST_DIR", Type: customactions.PromptText},
	}
	values := map[string]string{"HOST": "cascade", "DEST_DIR": "/tmp"}
	got := promptValuesEnv(prompts, values)
	want := []string{"HOST=cascade", "DEST_DIR=/tmp"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRelOrEmpty_EscapeBlocked verifies that ../ paths return empty
// rather than leaking out of the project root into the user's home
// directory. Without this guard, an unusual app state where
// activeFolder ended up outside rootDir would silently expose that
// outside path to the action's command line.
func TestRelOrEmpty_EscapeBlocked(t *testing.T) {
	if got := relOrEmpty("/p", "/q/r"); got != "" {
		t.Errorf("relOrEmpty escape = %q, want empty", got)
	}
	if got := relOrEmpty("/p", "/p/sub"); got != "sub" {
		t.Errorf("relOrEmpty(/p, /p/sub) = %q, want %q", got, "sub")
	}
	if got := relOrEmpty("", "/p"); got != "" {
		t.Errorf("relOrEmpty empty-base = %q, want empty", got)
	}
}

// makeDirAll is a tiny test-only helper kept in this file so each
// test reads as the case it's pinning rather than directory-creation
// boilerplate.
func makeDirAll(p string) error {
	return os.MkdirAll(p, 0o755)
}
