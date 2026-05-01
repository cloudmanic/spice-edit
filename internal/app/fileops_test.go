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

// TestDeletePath_File removes an existing file and confirms it's gone.
func TestDeletePath_File(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "trash.txt")
	if err := os.WriteFile(target, []byte("nope"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := deletePath(target); err != nil {
		t.Fatalf("deletePath: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file still exists after delete: err=%v", err)
	}
}

// TestDeletePath_DirectoryRecursive pins the new folder-delete
// behaviour: an os.RemoveAll under the hood that takes nested files
// and subdirectories down with the parent. Without this the user
// would have to walk leaf-to-root one file at a time.
func TestDeletePath_DirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	nested := filepath.Join(sub, "deeper", "leaf.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0755); err != nil {
		t.Fatalf("seed subdir: %v", err)
	}
	if err := os.WriteFile(nested, []byte("x"), 0644); err != nil {
		t.Fatalf("seed leaf: %v", err)
	}

	if err := deletePath(sub); err != nil {
		t.Fatalf("deletePath: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("directory still exists: err=%v", err)
	}
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatalf("nested file survived parent delete: err=%v", err)
	}
}

// TestDeletePath_Missing returns the underlying os error so callers
// can surface a useful message rather than letting RemoveAll's silent
// success on a missing path mask a typo or race.
func TestDeletePath_Missing(t *testing.T) {
	dir := t.TempDir()
	if err := deletePath(filepath.Join(dir, "ghost")); err == nil {
		t.Fatal("expected error deleting a missing path")
	}
}

// TestTabPathRemoved_ExactMatch is the simplest case: a tab pointing
// at the deleted file is orphaned and must close.
func TestTabPathRemoved_ExactMatch(t *testing.T) {
	if !tabPathRemoved("/proj/main.go", "/proj/main.go") {
		t.Fatal("exact match should be flagged removed")
	}
}

// TestTabPathRemoved_InsideDeletedDir pins the folder-delete case:
// every tab living under the deleted directory is orphaned. Without
// this the editor would keep showing buffers backed by files that
// no longer exist, and the next save would silently re-create them
// at the deleted location.
func TestTabPathRemoved_InsideDeletedDir(t *testing.T) {
	if !tabPathRemoved("/proj/sub/leaf.go", "/proj/sub") {
		t.Fatal("descendant tab should be flagged removed")
	}
	if !tabPathRemoved("/proj/sub/deep/leaf.go", "/proj/sub") {
		t.Fatal("nested descendant should be flagged removed")
	}
}

// TestTabPathRemoved_PrefixCollisionSafe is the trap the +"/" check
// guards against: deleting /proj/foo must not also close a tab at
// /proj/foobar.go just because the strings share a prefix.
func TestTabPathRemoved_PrefixCollisionSafe(t *testing.T) {
	if tabPathRemoved("/proj/foobar.go", "/proj/foo") {
		t.Fatal("sibling with shared prefix should not be flagged removed")
	}
}

// TestDoRenameFolder_RewritesDescendantTabPaths is the most
// important invariant of folder rename: an open tab pointing at a
// file inside the renamed directory must follow the rename, or the
// next save would write to the old (now nonexistent) path and
// silently re-create the folder under the wrong name.
func TestDoRenameFolder_RewritesDescendantTabPaths(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	if err := os.MkdirAll(filepath.Join(oldDir, "deep"), 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	leaf := filepath.Join(oldDir, "deep", "leaf.go")
	if err := os.WriteFile(leaf, []byte("package x\n"), 0644); err != nil {
		t.Fatalf("seed leaf: %v", err)
	}
	a := newTestApp(t, root)
	a.openFile(leaf)
	a.setActiveFolder(oldDir)

	a.doRenameFolder(oldDir, "renamed")

	newLeaf := filepath.Join(root, "renamed", "deep", "leaf.go")
	if _, err := os.Stat(newLeaf); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if got := a.tabs[0].Path; got != newLeaf {
		t.Fatalf("descendant tab path: got %q, want %q", got, newLeaf)
	}
	if want := filepath.Join(root, "renamed"); a.activeFolder != want {
		t.Fatalf("activeFolder: got %q, want %q", a.activeFolder, want)
	}
}

// TestDoRenameFolder_RefusesPathSeparator pins the input-validation
// rule shared with file rename: typing a slash should be rejected
// rather than silently moving the folder somewhere unexpected. The
// flash gives the user something actionable.
func TestDoRenameFolder_RefusesPathSeparator(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, root)

	a.doRenameFolder(sub, "nested/inside")

	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("source folder vanished despite refusal: %v", err)
	}
	if !strings.Contains(a.statusMsg, "path separator") {
		t.Fatalf("expected separator flash, got %q", a.statusMsg)
	}
}

// TestDoRenameFolder_RefusesClobber confirms the rename helper
// won't overwrite a sibling that already exists. Same safety rail
// renameFile gives file rename, just exercised through the folder
// path so we don't accidentally regress it for directories.
func TestDoRenameFolder_RefusesClobber(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "lib")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatalf("seed dst: %v", err)
	}
	a := newTestApp(t, root)

	a.doRenameFolder(src, "lib")

	if _, err := os.Stat(src); err != nil {
		t.Fatalf("src disappeared despite refusal: %v", err)
	}
}

// TestMenuRenameFolder_OpensPrompt walks the menu wiring: clicking
// Rename folder must open the prompt with the folder's basename
// already filled in (so the user only edits, not retypes).
func TestMenuRenameFolder_OpensPrompt(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "victim")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, root)
	a.setActiveFolder(sub)

	a.menuRenameFolder()
	if !a.promptOpen {
		t.Fatal("expected prompt to open")
	}
	if got := string(a.promptValue); got != "victim" {
		t.Fatalf("prompt value: got %q, want %q", got, "victim")
	}
}

// TestMenuRenameFolder_RefusesRoot mirrors menuDeleteFolder's
// guard. Renaming the project root would invalidate the editor's
// own working directory and confuse every open tab — must be a
// no-op even if some future caller sets activeFolder to root.
func TestMenuRenameFolder_RefusesRoot(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	a.setActiveFolder(root)

	a.menuRenameFolder()
	if a.promptOpen {
		t.Fatal("root should not open the rename prompt")
	}
}

// TestRenameFolderLabel_DynamicSuffix matches the delete-folder
// label test — bare label at root, "(subdir/)" suffix elsewhere
// so the user sees what's about to be renamed before clicking.
func TestRenameFolderLabel_DynamicSuffix(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, root)

	a.setActiveFolder(root)
	if got := a.renameFolderLabel(); got != "Rename folder" {
		t.Fatalf("root label = %q", got)
	}

	a.setActiveFolder(sub)
	got := a.renameFolderLabel()
	if !strings.Contains(got, "src") {
		t.Fatalf("subdir label should mention folder, got %q", got)
	}
}

// TestMenuDeleteFolder_Confirms walks the happy path: with a real
// active folder, menuDeleteFolder opens the confirm modal and the
// Yes branch removes the folder from disk plus resets activeFolder
// back to root so a follow-up New File doesn't try to write into a
// deleted directory.
func TestMenuDeleteFolder_Confirms(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "victim")
	if err := os.MkdirAll(filepath.Join(sub, "deep"), 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, root)
	a.setActiveFolder(sub)

	a.menuDeleteFolder()
	if !a.confirmOpen {
		t.Fatal("expected confirm modal to open")
	}
	a.confirmHover = 1
	a.confirmYes()

	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("folder still exists: err=%v", err)
	}
	if a.activeFolder != root {
		t.Fatalf("activeFolder = %q, want project root", a.activeFolder)
	}
}

// TestMenuDeleteFolder_RefusesRoot guards the most destructive
// possibility: the project root must never be deletable from the
// menu, even if some future caller manages to set activeFolder to
// it. The early return in menuDeleteFolder is the only thing
// preventing the editor from rm -rf-ing its own working dir.
func TestMenuDeleteFolder_RefusesRoot(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	a.setActiveFolder(root)

	a.menuDeleteFolder()
	if a.confirmOpen {
		t.Fatal("root folder should not open a confirm modal")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root vanished: %v", err)
	}
}

// TestHasActiveSubfolder_Predicate pins the menu enable rule: true
// when activeFolder points at a real subdirectory, false for the
// root, an empty active folder, or a folder that's been deleted
// externally. The menu row uses this to dim itself when the action
// would no-op, so a regression here would let the user click into
// a flash they can't act on.
func TestHasActiveSubfolder_Predicate(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "live")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, root)

	a.setActiveFolder(root)
	if a.hasActiveSubfolder() {
		t.Fatal("root should not be deletable")
	}

	a.activeFolder = ""
	if a.hasActiveSubfolder() {
		t.Fatal("empty active folder should not be deletable")
	}

	a.setActiveFolder(sub)
	if !a.hasActiveSubfolder() {
		t.Fatal("real subfolder should be deletable")
	}

	if err := os.Remove(sub); err != nil {
		t.Fatalf("remove for stale test: %v", err)
	}
	if a.hasActiveSubfolder() {
		t.Fatal("stale (externally-removed) folder should not be deletable")
	}
}

// TestDeleteFolderLabel_DynamicSuffix mirrors the New File label
// pattern: bare label at root, "(subdir/)" suffix when the active
// folder is somewhere we'd actually act on. Without this, the menu
// row would just say "Delete folder" with no hint of which folder —
// the user could click it not realising what was about to vanish.
func TestDeleteFolderLabel_DynamicSuffix(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, root)

	a.setActiveFolder(root)
	if got := a.deleteFolderLabel(); got != "Delete folder" {
		t.Fatalf("root label = %q, want bare 'Delete folder'", got)
	}

	a.setActiveFolder(sub)
	got := a.deleteFolderLabel()
	if !strings.Contains(got, "src") {
		t.Fatalf("subdir label should include folder name, got %q", got)
	}
}

// TestTabPathRemoved_UnrelatedSafe sanity-checks the negative case:
// a tab outside the deleted path stays open. This is the everyday
// path during a regular file delete.
func TestTabPathRemoved_UnrelatedSafe(t *testing.T) {
	if tabPathRemoved("/proj/other.go", "/proj/sub") {
		t.Fatal("unrelated tab should not be flagged removed")
	}
	if tabPathRemoved("", "/proj/sub") {
		t.Fatal("empty tab path should not be flagged removed")
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
