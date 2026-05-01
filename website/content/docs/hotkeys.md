---
title: "Hotkeys"
metaTitle: "SpiceEdit Hotkeys — The Esc-Leader Table"
metaDescription: "SpiceEdit avoids Ctrl+ shortcuts that fight tmux. Esc is the leader. Tap Esc, then a letter inside half a second. The complete table, with rationale."
summary: "The complete Esc-leader table, plus rationale."
weight: 30
---

SpiceEdit avoids `Ctrl+` shortcuts on purpose. They fight tmux. They fight Zellij. They fight your terminal — `Ctrl+S` is XOFF flow control on a real serial line, and modern emulators still honor it. They fight remote sessions where keystrokes hop through three layers of software.

So `Esc` is the leader. Tap `Esc`, then within half a second tap a bound letter. A lone `Esc` with no follow-up is a no-op — your next keystroke reaches the editor as normal, so accidental Escs never swallow a real character.

## The full table

| Combo       | Action                |
| ----------- | --------------------- |
| `Esc Esc`   | Open ≡ menu           |
| `Esc s`     | Save                  |
| `Esc u`     | Undo                  |
| `Esc r`     | Redo                  |
| `Esc w`     | Close tab             |
| `Esc q`     | Quit                  |
| `Esc n`     | New file              |
| `Esc t`     | Toggle sidebar        |
| `Esc f`     | Find in file          |
| `Esc p`     | Find file in project  |

## Editor keys (no Esc needed)

Standard movement and editing keys behave the way every editor since the Macintosh has trained you to expect:

| Key                  | Action                            |
| -------------------- | --------------------------------- |
| Arrow keys           | Move the cursor.                  |
| Shift + arrow        | Extend the selection.             |
| Home / End           | Jump to the line start / end.     |
| PgUp / PgDn          | Scroll a viewport.                |
| Tab / Shift+Tab      | Indent / dedent.                  |
| Enter                | New line.                         |
| Backspace / Delete   | Remove a character or selection.  |
| Mouse drag           | Select.                           |
| Double-click         | Select the word under the cursor. |
| Scroll wheel         | Scroll the panel under the mouse. |

## Why not bind clipboard

`c`, `x`, and `v` are deliberately unbound. Your terminal's Cmd+C / Cmd+V already covers that path; adding a third channel just creates confusion about which buffer holds what. Copy and Paste live in the action menu, where they belong.

## Why not bind destructive ops

Rename, Delete, and Revert are deliberately unbound. They're destructive enough that a confirm modal is the right gate, and the menu's confirm flow makes the action a deliberate gesture instead of muscle memory.

## Double-tap Esc

The leader window is 500 ms. Two `Esc` taps inside that window open the action menu. Outside it, each `Esc` is its own no-op leader prefix — there is no way to accidentally summon the menu by leaning on the key.
