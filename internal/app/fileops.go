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

// doCreateFile creates an empty file inside parent named name, refreshes the
// tree, and opens the new file in a tab. Errors are surfaced as a flash.
func (a *App) doCreateFile(parent, name string) {
	name = trimSpace(name)
	if name == "" {
		return
	}
	if strings.ContainsAny(name, string(os.PathSeparator)+"/\\") {
		a.flash("File name can't contain a path separator")
		return
	}
	target := filepath.Join(parent, name)
	if err := createEmptyFile(target); err != nil {
		a.flash(fmt.Sprintf("Create failed: %v", err))
		return
	}
	a.tree.Refresh()
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
	a.flash(fmt.Sprintf("Deleted %s", filepath.Base(path)))
}

// -----------------------------------------------------------------------------
// Main menu actions: rename / delete the file backing the active tab.
// -----------------------------------------------------------------------------

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

