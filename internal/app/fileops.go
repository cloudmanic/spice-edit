// =============================================================================
// File: internal/app/fileops.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// fileops.go implements the editor's three file-management actions:
// create-empty-file, rename-file, and delete-file. Each one is exposed two
// ways:
//
//   • From the main ≡ action menu, targeting the currently active tab
//     (Rename / Delete only — there's no obvious "where" for a new file
//     in that context, so New File lives only on the tree right-click).
//
//   • From the right-click context menu over a file-tree row. For folders
//     the menu offers New File (creates a child) plus Rename / Delete; for
//     files it offers Rename / Delete on the file itself.
//
// All three operations refresh the file tree afterwards so the sidebar
// reflects the change immediately, without waiting for the 10-second
// background poller.

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudmanic/spice-edit/internal/clipboard"
	"github.com/cloudmanic/spice-edit/internal/filetree"
)

// -----------------------------------------------------------------------------
// Backend: the actual file-system operations.
// -----------------------------------------------------------------------------

// createEmptyFile creates an empty file at path. It uses O_EXCL so it
// refuses to clobber an existing file. The caller is expected to have
// resolved path against a known parent directory.
func createEmptyFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

// renameFile moves oldPath to newPath. It refuses to clobber an existing
// destination so the user can't accidentally lose a file by typing a name
// that collides.
func renameFile(oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("a file named %q already exists", filepath.Base(newPath))
	}
	return os.Rename(oldPath, newPath)
}

// tabPathRemoved reports whether a tab pointing at tabPath is now
// orphaned because deletedPath was removed. True when the tab was
// the deleted file itself, or when it lived inside the deleted
// directory. The "/" separator is appended so /proj/foo deletion
// doesn't also catch /proj/foobar — a substring match would.
func tabPathRemoved(tabPath, deletedPath string) bool {
	if tabPath == "" {
		return false
	}
	if tabPath == deletedPath {
		return true
	}
	prefix := deletedPath + string(filepath.Separator)
	return strings.HasPrefix(tabPath, prefix)
}

// deletePath removes the file or directory at path. Directories are
// removed recursively (os.RemoveAll), so callers must take a confirm
// before invoking this on a folder — the operation is unrecoverable
// from inside the editor. Returns the underlying os error so the
// caller can surface a useful message; we deliberately don't swallow
// "no such file or directory" because RemoveAll silently succeeds on
// missing paths and the caller may want to know the path was already
// gone (today no callsite cares, but the contract is the safer one).
func deletePath(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// -----------------------------------------------------------------------------
// App glue: wrap the backend ops in tab/tree-aware helpers.
// -----------------------------------------------------------------------------

// doCreateFile creates an empty file inside parent at the relative path
// name, refreshes the tree, and opens the new file in a tab. Errors are
// surfaced as a flash. name may contain path separators so the user can
// drop a file into a subdirectory ("subdir/foo.go") — but the parent
// directories must already exist; we don't silently mkdir to avoid
// creating folders the user didn't realise they were making.
func (a *App) doCreateFile(parent, name string) {
	name = trimSpace(name)
	if name == "" {
		return
	}
	target := filepath.Join(parent, name)
	if err := createEmptyFile(target); err != nil {
		// Translate the noisy "open <path>: no such file or directory"
		// case into something the user can actually act on. ENOENT here
		// means the parent directory doesn't exist.
		if os.IsNotExist(err) {
			a.flash(fmt.Sprintf("Create failed: %s doesn't exist — create it first",
				filepath.Dir(target)))
			return
		}
		a.flash(fmt.Sprintf("Create failed: %v", err))
		return
	}
	a.tree.Refresh()
	a.refreshGitStatus()
	a.openFile(target)
	a.flash(fmt.Sprintf("Created %s", name))
}

// doRenameFile renames oldPath to a sibling whose basename is newName,
// refreshing the tree and updating any open tab that points at the file.
func (a *App) doRenameFile(oldPath, newName string) {
	newName = trimSpace(newName)
	if newName == "" {
		return
	}
	if strings.ContainsAny(newName, string(os.PathSeparator)+"/\\") {
		a.flash("File name can't contain a path separator")
		return
	}
	newPath := filepath.Join(filepath.Dir(oldPath), newName)
	if err := renameFile(oldPath, newPath); err != nil {
		a.flash(fmt.Sprintf("Rename failed: %v", err))
		return
	}
	// Update any open tab that pointed at oldPath so its title reflects the
	// new name and its disk-reconciliation logic stays correct.
	for _, t := range a.tabs {
		if t.Path == oldPath {
			t.Path = newPath
			if info, err := os.Stat(newPath); err == nil {
				t.Mtime = info.ModTime()
			} else {
				t.Mtime = time.Time{}
			}
			t.DiskGone = false
		}
	}
	a.tree.Refresh()
	a.refreshGitStatus()
	a.flash(fmt.Sprintf("Renamed to %s", newName))
}

// doDeletePath removes path (file or directory), closes any open tab
// whose file is gone as a result, and refreshes the tree.
//
// For a folder delete we have to close not just an exact-path tab but
// every tab living *inside* the folder — otherwise the editor would
// keep showing buffers backed by files that no longer exist, and the
// next save would silently re-create them. tabPathRemoved encodes that
// "is this tab orphaned?" check so the loop reads as the rule it's
// enforcing rather than path arithmetic.
func (a *App) doDeletePath(path string) {
	if err := deletePath(path); err != nil {
		a.flash(fmt.Sprintf("Delete failed: %v", err))
		return
	}
	for i := len(a.tabs) - 1; i >= 0; i-- {
		if tabPathRemoved(a.tabs[i].Path, path) {
			a.closeTab(i)
		}
	}
	a.tree.Refresh()
	a.refreshGitStatus()
	a.flash(fmt.Sprintf("Deleted %s", filepath.Base(path)))
}

// -----------------------------------------------------------------------------
// Main menu actions: rename / delete the file backing the active tab.
// -----------------------------------------------------------------------------

// menuNewFile prompts the user for a filename and creates an empty file in
// the editor's active folder. The active folder is shown in the prompt's
// hint line so the user can see exactly where the file is going. Path
// separators are allowed in the input — typing "subdir/foo.go" lands the
// new file in subdir, relative to the active folder.
//
// If the active folder has been deleted on disk while the editor was open
// we silently fall back to the project root rather than handing the user
// a prompt rooted at a path that no longer exists.
func (a *App) menuNewFile() {
	a.closeMenu()
	folder := a.activeFolder
	if folder == "" {
		folder = a.rootDir
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		folder = a.rootDir
		a.setActiveFolder(folder)
	}
	hint := "in " + a.relativeFolderLabel(folder)
	a.openPrompt(
		"New file",
		hint,
		"",
		func(app *App, value string) {
			app.doCreateFile(folder, value)
		},
	)
}

// newFileLabel is the dynamic label hook for the New File menu row. It
// shows the bare label when the active folder is the project root and a
// "(in subfolder)" suffix otherwise, so the user can tell at a glance
// where the file will land before they even click.
func (a *App) newFileLabel() string {
	folder := a.activeFolder
	if folder == "" || folder == a.rootDir {
		return "New file"
	}
	rel := a.relativeFolderLabel(folder)
	// Truncate so the row never overflows the modal width. The modal is
	// 38 cells wide; "▸" + label + padding leaves ~30 cells for text.
	const maxLen = 28
	suffix := " (in " + rel + ")"
	if runeLen(suffix) > maxLen {
		// Drop characters from the middle of rel so the trailing folder
		// name (the most informative part) stays visible.
		keep := maxLen - len(" (in …)")
		if keep < 4 {
			keep = 4
		}
		if keep < len(rel) {
			rel = "…" + rel[len(rel)-keep:]
		}
		suffix = " (in " + rel + ")"
	}
	return "New file" + suffix
}

// relativeFolderLabel returns folder rendered relative to the project root,
// or just the basename when folder is the root itself. Used in the New
// File prompt's hint and the menu row's dynamic label.
func (a *App) relativeFolderLabel(folder string) string {
	if folder == a.rootDir {
		return filepath.Base(a.rootDir) + string(filepath.Separator)
	}
	rel, err := filepath.Rel(a.rootDir, folder)
	if err != nil || rel == "." {
		return filepath.Base(folder) + string(filepath.Separator)
	}
	return rel + string(filepath.Separator)
}

// menuRename opens a prompt pre-filled with the active tab's basename and
// renames the file on submit. Untitled tabs are skipped — the menu row is
// disabled for them anyway via hasSavableTab.
func (a *App) menuRename() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return
	}
	old := tab.Path
	a.openPrompt(
		"Rename file",
		"in "+filepath.Dir(old),
		filepath.Base(old),
		func(app *App, value string) {
			app.doRenameFile(old, value)
		},
	)
}

// menuDelete opens a Yes/No confirm modal; on Yes, removes the active tab's
// file from disk and closes the tab.
func (a *App) menuDelete() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return
	}
	target := tab.Path
	a.openConfirm(
		"Delete file",
		"Permanently delete "+filepath.Base(target)+"?",
		func(app *App) {
			app.doDeletePath(target)
		},
	)
}

// doRenameFolder renames oldPath to a sibling whose basename is
// newName, refreshes the tree, and rewrites every open tab whose
// file lives under the renamed directory so the buffers don't end
// up backed by a stale path. Reuses renameFile under the hood since
// os.Rename works on directories the same as files.
//
// The descendant-tab path-rewriting case is what doRenameFile lacks:
// renaming /proj/foo to /proj/bar must also point a tab at
// /proj/foo/main.go to /proj/bar/main.go. tabPathRemoved-style
// prefix matching with the trailing separator avoids the
// /proj/foo vs /proj/foobar collision a substring match would hit.
func (a *App) doRenameFolder(oldPath, newName string) {
	newName = trimSpace(newName)
	if newName == "" {
		return
	}
	if strings.ContainsAny(newName, string(os.PathSeparator)+"/\\") {
		a.flash("Folder name can't contain a path separator")
		return
	}
	newPath := filepath.Join(filepath.Dir(oldPath), newName)
	if err := renameFile(oldPath, newPath); err != nil {
		a.flash(fmt.Sprintf("Rename failed: %v", err))
		return
	}
	prefix := oldPath + string(filepath.Separator)
	for _, t := range a.tabs {
		switch {
		case t.Path == oldPath:
			// Defensive — tabs shouldn't be backed by directories,
			// but if one is we still rewrite it cleanly.
			t.Path = newPath
		case strings.HasPrefix(t.Path, prefix):
			t.Path = filepath.Join(newPath, t.Path[len(prefix):])
		default:
			continue
		}
		if info, err := os.Stat(t.Path); err == nil {
			t.Mtime = info.ModTime()
		} else {
			t.Mtime = time.Time{}
		}
		t.DiskGone = false
	}
	// Keep activeFolder in sync. If we don't, the next "New file"
	// would target the deleted path and fail confusingly.
	if a.activeFolder == oldPath || strings.HasPrefix(a.activeFolder, prefix) {
		if a.activeFolder == oldPath {
			a.setActiveFolder(newPath)
		} else {
			a.setActiveFolder(filepath.Join(newPath, a.activeFolder[len(prefix):]))
		}
	}
	a.tree.Refresh()
	a.refreshGitStatus()
	a.flash(fmt.Sprintf("Renamed to %s", newName))
}

// menuRenameFolder opens a prompt pre-filled with the active
// folder's basename and renames the directory on submit. Mirrors
// menuRename but targets a folder rather than a file. The project
// root is gated out by hasActiveSubfolder so this never fires when
// rooted on the working dir itself.
func (a *App) menuRenameFolder() {
	a.closeMenu()
	folder := a.activeFolder
	if folder == "" || folder == a.rootDir {
		return
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return
	}
	old := folder
	a.openPrompt(
		"Rename folder",
		"in "+filepath.Dir(old),
		filepath.Base(old),
		func(app *App, value string) {
			app.doRenameFolder(old, value)
		},
	)
}

// renameFolderLabel is the dynamic label hook for the Rename Folder
// menu row. Same shape as deleteFolderLabel — bare label at root,
// "(subdir/)" suffix otherwise. Without the suffix the user would
// have no way to tell what's about to be renamed before clicking.
func (a *App) renameFolderLabel() string {
	folder := a.activeFolder
	if folder == "" || folder == a.rootDir {
		return "Rename folder"
	}
	rel := a.relativeFolderLabel(folder)
	const maxLen = 28
	suffix := " (" + rel + ")"
	if runeLen(suffix) > maxLen {
		keep := maxLen - len(" (…)")
		if keep < 4 {
			keep = 4
		}
		if keep < len(rel) {
			rel = "…" + rel[len(rel)-keep:]
		}
		suffix = " (" + rel + ")"
	}
	return "Rename folder" + suffix
}

// menuDeleteFolder removes the editor's active folder (the same folder
// the New File entry targets) and everything inside it. Lives in the
// main menu so folder deletion has a discoverable, non-right-click
// path — macOS Terminal eats Button3, and the project's CLAUDE.md
// rule says every file action must be reachable from the ≡ menu.
//
// The project root is never deletable — hasActiveSubfolder gates the
// row out for that case so the user can't even see the action when
// it would be destructive enough to take down the whole session.
func (a *App) menuDeleteFolder() {
	a.closeMenu()
	folder := a.activeFolder
	if folder == "" || folder == a.rootDir {
		return
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		// The active folder vanished externally — bail rather than
		// flashing a confusing "Delete failed" once the confirm fires.
		return
	}
	target := folder
	a.openConfirm(
		"Delete folder",
		"Permanently delete "+filepath.Base(target)+" and everything inside?",
		func(app *App) {
			app.doDeletePath(target)
			// After the directory is gone we can't keep activeFolder
			// pointing at it — fall back to the project root so the
			// next New File doesn't try to write into a deleted dir.
			app.setActiveFolder(app.rootDir)
		},
	)
}

// deleteFolderLabel is the dynamic label hook for the Delete Folder
// menu row. Mirrors newFileLabel: bare "Delete folder" when nothing
// useful is selected, "Delete folder (subdir/)" when there is — so
// the user can tell at a glance what's about to vanish before they
// even open the confirm dialog.
func (a *App) deleteFolderLabel() string {
	folder := a.activeFolder
	if folder == "" || folder == a.rootDir {
		return "Delete folder"
	}
	rel := a.relativeFolderLabel(folder)
	const maxLen = 28
	suffix := " (" + rel + ")"
	if runeLen(suffix) > maxLen {
		keep := maxLen - len(" (…)")
		if keep < 4 {
			keep = 4
		}
		if keep < len(rel) {
			rel = "…" + rel[len(rel)-keep:]
		}
		suffix = " (" + rel + ")"
	}
	return "Delete folder" + suffix
}

// hasActiveSubfolder is the menu predicate shared by every "act on
// the active folder" row (Delete, Rename, …). True when activeFolder
// points at a real subdirectory of the project root. Lives next to
// hasFileTab so the file/folder predicates form a matched pair.
func (a *App) hasActiveSubfolder() bool {
	folder := a.activeFolder
	if folder == "" || folder == a.rootDir {
		return false
	}
	info, err := os.Stat(folder)
	return err == nil && info.IsDir()
}

// -----------------------------------------------------------------------------
// Context-menu actions: rename / delete / new-file against a tree node.
// -----------------------------------------------------------------------------

// ctxNewFile opens a prompt to name a new empty file inside n. n is always
// a directory — openTreeContext only adds this row for folder nodes. The
// folder is auto-expanded so the new file is visible immediately after the
// post-create tree refresh.
func ctxNewFile(a *App, n *filetree.Node) {
	if !n.IsDir {
		return
	}
	parent := n.Path
	if !n.Expanded {
		a.tree.Toggle(n)
	}
	a.openPrompt(
		"New file",
		"in "+parent,
		"",
		func(app *App, value string) {
			app.doCreateFile(parent, value)
		},
	)
}

// ctxRename opens a prompt pre-filled with n's basename and renames the
// file or folder on submit.
func ctxRename(a *App, n *filetree.Node) {
	if n == a.tree.Root {
		return
	}
	old := n.Path
	a.openPrompt(
		"Rename",
		"in "+filepath.Dir(old),
		n.Name,
		func(app *App, value string) {
			app.doRenameFile(old, value)
		},
	)
}

// copyPathToSystemClipboard pushes path onto the host system clipboard via
// OSC 52 and flashes a confirmation (or the underlying error) so the user
// gets feedback — the OS clipboard is invisible from inside the TUI, and a
// silent action would leave the user wondering whether anything happened.
//
// label is the short word used in the success / error flash ("relative
// path" / "absolute path") so both menu paths share one helper without
// duplicating copy.
func (a *App) copyPathToSystemClipboard(path, label string) {
	if err := clipboard.CopyToSystem(path); err != nil {
		a.flash(fmt.Sprintf("Copy failed: %v", err))
		return
	}
	a.flash(fmt.Sprintf("Copied %s: %s", label, path))
}

// relativePathFor returns path rendered relative to the project root. We
// resolve the root via tree.Root.Path because that is always absolute —
// App.rootDir keeps the user-supplied string verbatim ("." in the common
// case), and filepath.Rel refuses to mix a relative base with an absolute
// target. Tab and tree node paths are always absolute, so basing on the
// absolute root is what gives a clean repo-relative result.
//
// On the rare error path (different volume, etc.) we fall back to the
// absolute path so the user still gets something useful on the clipboard.
func (a *App) relativePathFor(path string) string {
	base := a.rootDir
	if a.tree != nil && a.tree.Root != nil {
		base = a.tree.Root.Path
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

// absolutePathFor resolves path to an absolute filesystem path. Failures
// fall back to the input unchanged — Tab.Path and tree node paths are
// already absolute in normal use, so this is just defence-in-depth.
func absolutePathFor(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// menuCopyRelativePath copies the active tab's path, rendered relative to
// the project root, onto the host system clipboard via OSC 52. Works the
// same locally and over SSH — the terminal emulator is the thing that
// actually receives the clipboard write, regardless of where the editor
// is running.
func (a *App) menuCopyRelativePath() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return
	}
	a.copyPathToSystemClipboard(a.relativePathFor(tab.Path), "relative path")
}

// menuCopyAbsolutePath copies the active tab's absolute path onto the host
// system clipboard. Useful when pasting the path into a shell on the same
// remote machine (e.g. another tmux pane running over the same SSH session).
func (a *App) menuCopyAbsolutePath() {
	a.closeMenu()
	tab := a.activeTabPtr()
	if tab == nil || tab.Path == "" {
		return
	}
	a.copyPathToSystemClipboard(absolutePathFor(tab.Path), "absolute path")
}

// ctxCopyRelativePath copies n's path (relative to the project root) onto
// the host system clipboard. Tree-context counterpart to menuCopyRelativePath.
func ctxCopyRelativePath(a *App, n *filetree.Node) {
	a.copyPathToSystemClipboard(a.relativePathFor(n.Path), "relative path")
}

// ctxCopyAbsolutePath copies n's absolute path onto the host system
// clipboard. Tree-context counterpart to menuCopyAbsolutePath.
func ctxCopyAbsolutePath(a *App, n *filetree.Node) {
	a.copyPathToSystemClipboard(absolutePathFor(n.Path), "absolute path")
}

// ctxDelete confirms and removes the file or folder the user clicked.
// Folder deletion is recursive (os.RemoveAll under the hood) so the
// confirm copy spells out "and everything inside" — the stakes are
// much higher than a single-file delete and the user should see that
// before clicking Yes. The project root itself is never deletable.
func ctxDelete(a *App, n *filetree.Node) {
	if n == a.tree.Root {
		return
	}
	target := n.Path
	title := "Delete file"
	msg := "Permanently delete " + n.Name + "?"
	if n.IsDir {
		title = "Delete folder"
		msg = "Permanently delete " + n.Name + " and everything inside?"
	}
	a.openConfirm(title, msg, func(app *App) {
		app.doDeletePath(target)
	})
}

