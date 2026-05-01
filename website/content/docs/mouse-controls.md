---
title: "Mouse Controls"
metaTitle: "Mouse Controls in SpiceEdit — Click, Drag, Scroll"
metaDescription: "Every mouse gesture SpiceEdit responds to — clicks, drags, double-clicks, right-clicks, scroll wheel — across the editor, tree, tab bar, and modals."
summary: "Every mouse gesture SpiceEdit responds to."
weight: 40
---

SpiceEdit is mouse-first. Every UI surface is clickable, draggable, or scrollable. This page is the complete reference.

## Editor body

| Gesture                                | Effect                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------- |
| Left-click                             | Place the cursor at the click point.                                   |
| Left-click + drag                      | Extend a selection from the press point.                               |
| Drag past the top or bottom edge       | Auto-scroll while extending the selection at your last column.         |
| Double-click                           | Select the word under the cursor (`[A-Za-z0-9_]` definition).          |
| Scroll wheel                           | Scroll three lines per tick.                                           |
| Right-click                            | Open the action menu (in terminals that forward Button3).              |

## File tree

| Gesture                                | Effect                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------- |
| Left-click on a folder                 | Toggle expand / collapse. Sets the folder as the active folder.        |
| Left-click on a file                   | Open the file in a new or existing tab.                                |
| Right-click on a folder                | Per-folder context menu: New File, Rename, Delete, Copy paths.         |
| Right-click on a file                  | Per-file context menu: Rename, Delete, Copy relative path, Copy absolute. |
| Scroll wheel                           | Scroll the tree.                                                       |

The active folder — the one shown bold in the sidebar — is the default target for New File. The label in the action menu reflects this: "New file in `cmd/`" when a subfolder is active, plain "New file" at the project root.

Note: macOS Terminal + tmux often swallows Button3. Every right-click action also lives in the main `≡` menu, so you're never stuck.

## Tab bar

| Gesture                                | Effect                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------- |
| Left-click a tab body                  | Switch to that tab.                                                    |
| Left-click the `×` on a tab            | Close that tab. Dirty tabs prompt Save / Discard / Cancel.             |
| Left-click the `≡` icon                | Open the action menu.                                                  |

## Splitter

The single column between the sidebar and the editor is the splitter.

| Gesture                                | Effect                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------- |
| Press on the splitter column, drag     | Resize the sidebar. Min sidebar width: 18. Min editor width: 40.       |

## Modals (action menu, find, finder, confirms)

| Gesture                                | Effect                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------- |
| Left-click a row                       | Activate the row's action.                                             |
| Hover                                  | Highlight the row under the cursor.                                    |
| Click outside the modal                | Dismiss.                                                               |

## Status bar

The status bar shows the active file's path, language, cursor position, dirty marker, git branch (when applicable), and any flash messages from background work. It's display-only — the status bar does not respond to clicks.
