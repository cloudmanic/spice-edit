// =============================================================================
// File: internal/icons/icons_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package icons

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/spiceconfig"
)

// TestForFolders pins the folder open/closed glyph pairing — flipping
// these by accident would silently invert what every directory in the
// tree displays.
func TestForFolders(t *testing.T) {
	if got := For("anything", true, false); got != FolderClosed {
		t.Fatalf("collapsed dir = %q, want FolderClosed", got)
	}
	if got := For("anything", true, true); got != FolderOpen {
		t.Fatalf("expanded dir = %q, want FolderOpen", got)
	}
}

// TestForByExtension covers the common extension lookups — happy path
// for the bulk of files in any project.
func TestForByExtension(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"main.go", extIcons[".go"]},
		{"app.py", extIcons[".py"]},
		{"index.JS", extIcons[".js"]}, // case-insensitive
		{"style.css", extIcons[".css"]},
		{"README.markdown", extIcons[".markdown"]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := For(tc.name, false, false); got != tc.want {
				t.Fatalf("For(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestForByFullName verifies extension-less files resolve via the
// full-name table — Dockerfile and Makefile are the canonical reasons
// this lookup tier exists at all.
func TestForByFullName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Dockerfile", nameIcons["dockerfile"]},
		{"Makefile", nameIcons["makefile"]},
		{"go.mod", nameIcons["go.mod"]},
		{".gitignore", nameIcons[".gitignore"]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := For(tc.name, false, false); got != tc.want {
				t.Fatalf("For(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestForFallback verifies an unknown extension returns FileDefault
// rather than the empty string — the renderer relies on a non-empty
// glyph to keep the indent column-aligned.
func TestForFallback(t *testing.T) {
	if got := For("mystery.xyzzy", false, false); got != FileDefault {
		t.Fatalf("unknown ext = %q, want FileDefault", got)
	}
	if got := For("no_extension_at_all", false, false); got != FileDefault {
		t.Fatalf("no-ext file = %q, want FileDefault", got)
	}
}

// TestResolveExplicitOverrides verifies the on/off modes bypass
// detection entirely — important for users on a terminal where
// detection would lie either way.
func TestResolveExplicitOverrides(t *testing.T) {
	if !Resolve(spiceconfig.IconsOn) {
		t.Fatalf("IconsOn should always resolve true")
	}
	if Resolve(spiceconfig.IconsOff) {
		t.Fatalf("IconsOff should always resolve false")
	}
}

// TestResolveAutoIsBoolean is a smoke test for the "auto" path: it
// just runs Detect() on the test machine and asserts the result is a
// real bool. We can't assert true or false here because CI may or may
// not have Nerd Fonts installed.
func TestResolveAutoIsBoolean(t *testing.T) {
	got := Resolve(spiceconfig.IconsAuto)
	_ = got // any bool is fine; the assertion is "doesn't panic"
}

// TestDetectViaFilesystemFindsNerdFont sets up a fake font directory
// containing a file whose name carries "Nerd" and verifies the walker
// picks it up — proves the fallback path works without depending on
// the host's actual font install.
func TestDetectViaFilesystemFindsNerdFont(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "HackNerdFont-Regular.ttf"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !walkForNerdFont(dir) {
		t.Fatalf("expected to find Nerd Font in %s", dir)
	}
}

// TestDetectViaFilesystemMissesNonMatching verifies the walker
// doesn't false-positive on plain fonts — important so we don't
// claim icons-OK for users whose system has only stock fonts.
func TestDetectViaFilesystemMissesNonMatching(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Arial.ttf"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if walkForNerdFont(dir) {
		t.Fatalf("Arial.ttf should not match a Nerd Font search")
	}
}

// TestDetectViaFilesystemRejectsWrongExtension covers the case of a
// "Nerd"-named file that isn't actually a font — we don't want a
// stray "nerd-readme.txt" to accidentally enable icons.
func TestDetectViaFilesystemRejectsWrongExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nerd-readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if walkForNerdFont(dir) {
		t.Fatalf("non-font with 'nerd' in name should not match")
	}
}

// TestDetectViaFilesystemMissingDir confirms a non-existent dir is a
// quiet no-match rather than an error — the walker is called for
// every candidate path and most won't exist on any given system.
func TestDetectViaFilesystemMissingDir(t *testing.T) {
	if walkForNerdFont("/definitely/does/not/exist/at/all") {
		t.Fatalf("missing dir should return false, not panic or true")
	}
}
