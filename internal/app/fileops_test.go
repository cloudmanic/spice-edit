// =============================================================================
// File: internal/app/fileops_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the small file-system helpers in fileops.go. The App-level glue
// (modals, menu wiring) is exercised manually via the TUI; here we just
// pin down the behavior of the three primitives so future refactors don't
// silently regress them.

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateEmptyFile_New writes a brand-new empty file and verifies it
// exists on disk and is zero bytes.
func TestCreateEmptyFile_New(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")

	if err := createEmptyFile(target); err != nil {
		t.Fatalf("createEmptyFile: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after create: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected 0-byte file, got %d bytes", info.Size())
	}
}

// TestCreateEmptyFile_RefusesExisting ensures we don't clobber an existing
// file — the user's content should be safe even if they typo a name.
func TestCreateEmptyFile_RefusesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := createEmptyFile(target); err == nil {
		t.Fatal("expected error when creating an existing file, got nil")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after create attempt: %v", err)
	}
	if string(got) != "keep me" {
		t.Fatalf("file contents were clobbered: %q", got)
	}
}

// TestRenameFile_Basic renames a file and confirms the source is gone and
// the destination has the original contents.
func TestRenameFile_Basic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "before.txt")
	dst := filepath.Join(dir, "after.txt")
	if err := os.WriteFile(src, []byte("payload"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := renameFile(src, dst); err != nil {
		t.Fatalf("renameFile: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists after rename: err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("payload mismatch: %q", got)
	}
}

// TestRenameFile_RefusesClobber proves we won't overwrite an existing
// destination — important so the user can't accidentally erase a sibling
// by typing its name into the rename prompt.
func TestRenameFile_RefusesClobber(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("src"), 0644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("dst"), 0644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	err := renameFile(src, dst)
	if err == nil {
		t.Fatal("expected rename to fail when destination exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error should mention conflict, got: %v", err)
	}
	// Both files should be untouched.
	if got, _ := os.ReadFile(src); string(got) != "src" {
		t.Fatalf("src corrupted: %q", got)
	}
	if got, _ := os.ReadFile(dst); string(got) != "dst" {
		t.Fatalf("dst corrupted: %q", got)
	}
}

// TestRenameFile_SamePathNoop allows rename(x, x) without erroring — the
// UI path can hit this when the user opens the prompt and submits without
// editing the pre-filled value.
func TestRenameFile_SamePathNoop(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(target, []byte("same"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := renameFile(target, target); err != nil {
		t.Fatalf("renameFile same-path: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "same" {
		t.Fatalf("contents changed: %q", got)
	}
}

// TestDeleteFile_Basic removes an existing file and confirms it's gone.
func TestDeleteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "trash.txt")
	if err := os.WriteFile(target, []byte("nope"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := deleteFile(target); err != nil {
		t.Fatalf("deleteFile: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file still exists after delete: err=%v", err)
	}
}

// TestDeleteFile_RefusesDirectory verifies the helper's safety rail: it
// will not recursively delete a directory. Folder deletion needs its own
// confirm flow before we ever wire it up.
func TestDeleteFile_RefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inside"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := deleteFile(sub)
	if err == nil {
		t.Fatal("expected error when deleting a directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("error should mention directory, got: %v", err)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("directory was removed despite refusal: %v", err)
	}
}

// TestDeleteFile_Missing returns the underlying os error for callers to
// surface — we don't swallow it so the user sees a useful message.
func TestDeleteFile_Missing(t *testing.T) {
	dir := t.TempDir()
	if err := deleteFile(filepath.Join(dir, "ghost")); err == nil {
		t.Fatal("expected error deleting a missing file")
	}
}

// TestRelativePathFor_InsideRoot returns a path relative to the project
// root. This is what the user expects on the clipboard when the editor's
// "root" is their repo and they want to paste the path into a commit
// message or another tool inside that same repo.
func TestRelativePathFor_InsideRoot(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	a.rootDir = dir

	target := filepath.Join(dir, "sub", "thing.go")
	got := a.relativePathFor(target)
	want := filepath.Join("sub", "thing.go")
	if got != want {
		t.Fatalf("relativePathFor = %q, want %q", got, want)
	}
}

// TestRelativePathFor_RelativeRootDir is the regression test for the bug
// where `spiceedit` with no argument leaves App.rootDir = "." while tree
// and tab paths are absolute — filepath.Rel refuses to mix the two and
// the helper used to silently fall back to the absolute path. Now we
// base the relativisation on tree.Root.Path which is always absolute.
func TestRelativePathFor_RelativeRootDir(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	a.rootDir = "." // simulate `spiceedit` invoked with no argument

	target := filepath.Join(a.tree.Root.Path, "sub", "thing.go")
	got := a.relativePathFor(target)
	want := filepath.Join("sub", "thing.go")
	if got != want {
		t.Fatalf("relativePathFor with rootDir=\".\": got %q, want %q", got, want)
	}
}

// TestAbsolutePathFor_Resolves turns a relative path into a fully-qualified
// absolute path so the clipboard contents work even if the user pastes
// into a shell whose cwd doesn't match the editor's root.
func TestAbsolutePathFor_Resolves(t *testing.T) {
	got := absolutePathFor("relative/thing.go")
	if !filepath.IsAbs(got) {
		t.Fatalf("absolutePathFor returned non-absolute: %q", got)
	}
	if !strings.HasSuffix(got, filepath.Join("relative", "thing.go")) {
		t.Fatalf("absolutePathFor = %q, want suffix relative/thing.go", got)
	}
}

// TestMenuCopyPath_NoTabSilent guards against a nil-deref when the user
// somehow triggers the action without a tab open. The menu disables the
// row in that case but keyboard activation can still race; the action
// must be a no-op.
func TestMenuCopyPath_NoTabSilent(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuOpen = true
	a.menuCopyRelativePath()
	a.menuOpen = true
	a.menuCopyAbsolutePath()
	// Reaching here without a panic is the whole assertion.
}

// TestCopyPathToSystemClipboard_FlashMessage exercises the shared helper
// and confirms it sets a status flash so the user gets feedback —
// silent OSC 52 leaves the user wondering if the copy worked.
func TestCopyPathToSystemClipboard_FlashMessage(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.copyPathToSystemClipboard("/tmp/sample.go", "relative path")
	if a.statusMsg == "" {
		t.Fatal("expected a status flash after copy")
	}
	// Either success ("Copied …") or failure ("Copy failed: …") is
	// acceptable here — the test environment may not have a usable
	// /dev/tty. The contract is just "user gets feedback."
	if !strings.Contains(a.statusMsg, "/tmp/sample.go") &&
		!strings.Contains(a.statusMsg, "Copy failed") {
		t.Fatalf("status flash didn't mention the path or an error: %q", a.statusMsg)
	}
}
