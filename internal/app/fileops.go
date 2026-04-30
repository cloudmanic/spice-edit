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

// deleteFile removes the file at path. It refuses to remove directories —
// the editor's UX for now only deletes individual files; folder deletion
// is a separate, riskier operation we'd want a different confirm flow for.
func deleteFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("refusing to delete a directory: %s", filepath.Base(path))
	}
	return os.Remove(path)
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

// doDeleteFile removes path, closes any open tab pointing at it, and
// refreshes the tree.
func (a *App) doDeleteFile(path string) {
	if err := deleteFile(path); err != nil {
		a.flash(fmt.Sprintf("Delete failed: %v", err))
		return
	}
	for i := len(a.tabs) - 1; i >= 0; i-- {
		if a.tabs[i].Path == path {
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
			app.doDeleteFile(target)
		},
	)
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

// ctxDelete confirms and removes the file the user clicked. We refuse on
// directories — see deleteFile — so the confirm copy is file-specific.
func ctxDelete(a *App, n *filetree.Node) {
	if n == a.tree.Root {
		return
	}
	target := n.Path
	if n.IsDir {
		a.flash("Folder deletion isn't supported yet — delete the files inside first")
		return
	}
	a.openConfirm(
		"Delete file",
		"Permanently delete "+n.Name+"?",
		func(app *App) {
			app.doDeleteFile(target)
		},
	)
}

