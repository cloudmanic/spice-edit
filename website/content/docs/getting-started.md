---
title: "Getting Started"
metaTitle: "Getting Started with SpiceEdit in 3 Minutes"
metaDescription: "Open a project, click a file, edit a line, save. The first three minutes with SpiceEdit — the mouse-first terminal code editor for SSH workflows."
summary: "Open a project, click a file, edit a line, save."
weight: 20
---

This page walks the first three minutes. Open a project, click around, save a file, get back to your terminal.

## Open a project

From any directory:

```sh
spiceedit              # opens the current directory as the project root
spiceedit ~/code/app   # opens a specific project root
spiceedit main.go      # opens that file (project root = its parent dir)
spiceedit new-file.go  # creates the file on first save (vim-style)
```

<figure class="screenshot-figure">
  <img
    src="/img/screenshots/opening-a-project.png"
    srcset="/img/screenshots/opening-a-project-1200.png 1200w, /img/screenshots/opening-a-project.png 2000w"
    sizes="(min-width: 1024px) 800px, 100vw"
    width="2000" height="1414"
    alt="SpiceEdit just after opening the spice-edit project — sidebar shows the file tree, the editor area shows 'No file open' with a hint to click a file or press the menu icon."
    loading="lazy" decoding="async"
  />
</figure>

The layout is what you'd expect: a file tree on the left, a tab bar across the top, the editor in the middle, a status bar at the bottom.

## Click a file

Single-click any file in the tree to open it. Single-click a folder to expand it. The active folder — the one New File and Rename Folder will target — bolds in the sidebar so you always know where the next file lands.

<figure class="screenshot-figure">
  <img
    src="/img/screenshots/clicking-a-file.png"
    srcset="/img/screenshots/clicking-a-file-1200.png 1200w, /img/screenshots/clicking-a-file.png 2000w"
    sizes="(min-width: 1024px) 800px, 100vw"
    width="2000" height="1409"
    alt="main.go open in the editor with Tokyo Night syntax highlighting after clicking it in the sidebar."
    loading="lazy" decoding="async"
  />
</figure>

## Switch tabs

Each open file is a tab. Click a tab body to switch. Click the `×` to close. Re-opening a file that's already open just switches to its existing tab.

## Open the menu

Every action lives in the action menu. Open it three ways:

1. Click the `≡` icon in the top-left corner.
2. Right-click anywhere outside the file tree (works in most terminals; macOS Terminal + tmux often eats Button3).
3. Double-tap `Esc`.

<figure class="screenshot-figure">
  <img
    src="/img/screenshots/clicking-the-menu.png"
    srcset="/img/screenshots/clicking-the-menu-1200.png 1200w, /img/screenshots/clicking-the-menu.png 2000w"
    sizes="(min-width: 1024px) 800px, 100vw"
    width="2000" height="1412"
    alt="Action menu modal expanded over the editor — Save, Close tab, Find file, Open on Repo, Quit editor, and other actions visible."
    loading="lazy" decoding="async"
  />
</figure>

The menu is keyboard-navigable too — arrow keys to move, Enter to select, Esc to dismiss.

## Edit and save

Click in the editor body to place the cursor. Type. Drag to select. The standard editor keys all work: arrow keys move, Shift+arrow extends selection, Home/End jump to line ends, PgUp/PgDn scroll a viewport.

To save: open the menu and pick **Save**, or press `Esc s`.

<figure class="screenshot-figure">
  <img
    src="/img/screenshots/saving.png"
    srcset="/img/screenshots/saving-1200.png 1200w, /img/screenshots/saving.png 2000w"
    sizes="(min-width: 1024px) 800px, 100vw"
    width="2000" height="1411"
    alt="Editor with a line selected mid-edit, status bar at the bottom showing the cursor position and dirty marker."
    loading="lazy" decoding="async"
  />
</figure>

## Resize the sidebar

Drag the column between the tree and the editor. Minimum sidebar width is 18 columns; the editor won't shrink below 40.

## Quit

Open the menu and pick **Quit editor**, or press `Esc q`. If any tabs have unsaved changes, you'll see a Save / Discard / Cancel modal — Save & Close blocks the quit if a save fails, so no work is lost.

That's it. You now know enough to use SpiceEdit. The rest of these docs are reference.
