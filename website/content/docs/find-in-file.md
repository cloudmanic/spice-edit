---
title: "Find in File"
metaTitle: "Find in File with SpiceEdit (Esc f)"
metaDescription: "Esc f opens SpiceEdit's in-file search bar. Case-insensitive substring, live highlight, Enter for next, Shift+Enter for previous, Esc to close."
summary: "Search the current file with Esc f."
weight: 50
---

`Esc f` (or **Find in file** from the `≡` menu) opens a single-row search bar pinned above the status bar.

```
 Find: foo█                       3 of 12   Enter: next · Shift+Enter: prev · Esc: close
```

## How matching works

- Case-insensitive substring. No regex, no whole-word, no case-sensitive toggle.
- Results highlight live as you type — the editor shows every match in the visible buffer the moment you change the query.
- The active match paints in a brighter color than the rest, so you can pick out where you are in the result set.

## Navigation

| Key             | Action                                |
| --------------- | ------------------------------------- |
| Type            | Update the search query.              |
| `Enter`         | Jump to the next match (wraps).       |
| `Shift+Enter`   | Jump to the previous match (wraps).   |
| `Esc`           | Close the bar and clear highlights.   |

## State across tabs

Each tab carries its own search state. Switch to another tab, search there, and switching back leaves the first tab's last query waiting if you reopen the bar — useful when you're hopping between two files looking for related symbols.

Closing the bar with `Esc` clears highlights. Opening it again starts fresh.

## Why no regex

Regex search is a pile of UI surface (toggles, error states, escape rules) for a feature most "find in file" users never reach for. The 90% case is "I know roughly what I'm looking for, take me there." That's what `Esc f` does. For the 10% case, the right tool is `rg` in the next pane of your tmux session.
