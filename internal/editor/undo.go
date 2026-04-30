// =============================================================================
// File: internal/editor/undo.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// undo.go gives every Tab a per-tab snapshot-based undo history plus a
// one-shot "revert to original" hatch.
//
// The model is intentionally simple: each entry is a full copy of the
// buffer's lines plus the cursor / anchor at the moment the snapshot was
// taken. That uses more memory than an operation log but keeps the code
// small enough to reason about and is plenty fast for the file sizes a
// terminal editor opens. The stack is FIFO-capped so a runaway typing
// session can't grow it forever.
//
// Coalescing — every typed rune doesn't get its own entry. Consecutive
// inserts of the same kind (typing, backspace, delete) inside a 500ms
// window collapse into a single undo step, so undoing a 50-character
// word is one click, not 50. Anything structural — pasting, hitting
// Enter, deleting a selection, an explicit cursor move — closes the
// current group so the next mutation starts a fresh entry.

package editor

import "time"

// maxUndoEntries caps the per-tab stack depth. 500 is generous enough
// that typical editing sessions never hit the wall; once we do, the
// oldest entry is dropped (FIFO) so the user can still undo a long
// way back without unbounded memory growth.
const maxUndoEntries = 500

// undoCoalesceWindow is the inactivity gap that closes a coalescing
// group. 500ms feels right — pause-and-think between words almost
// always lasts longer than this, while a typing burst lands well inside.
const undoCoalesceWindow = 500 * time.Millisecond

// undoGroup tags each snapshot with the kind of operation that produced
// it so consecutive ops of the same kind can be merged. Anything labelled
// undoGroupStructural never coalesces — it's the explicit "this is its
// own undo step" signal.
type undoGroup int

const (
	undoGroupNone       undoGroup = iota // no recent op — next push always lands.
	undoGroupTyping                      // a printable rune was inserted.
	undoGroupBackspace                   // a single char was removed before the cursor.
	undoGroupDelete                      // a single char was removed after the cursor.
	undoGroupStructural                  // paste, Enter, delete-selection, etc.
)

// snapshot captures everything needed to reproduce the editor's content
// state at a moment in time. We don't include scroll position — undo is
// about *what* was in the buffer, not where the user happened to be
// looking — but cursor + anchor are part of the user's state.
type snapshot struct {
	Lines  []string
	Cursor Position
	Anchor Position
}

// captureSnapshot returns a deep copy of the current buffer state so a
// later mutation can't bleed into history.
func (t *Tab) captureSnapshot() snapshot {
	if t.Buffer == nil {
		return snapshot{Cursor: t.Cursor, Anchor: t.Anchor}
	}
	lines := make([]string, len(t.Buffer.Lines))
	copy(lines, t.Buffer.Lines)
	return snapshot{Lines: lines, Cursor: t.Cursor, Anchor: t.Anchor}
}

// applySnapshot replaces the buffer with s. It also re-copies the lines
// so the live buffer doesn't share storage with the history entry —
// otherwise the next edit would silently rewrite the past.
func (t *Tab) applySnapshot(s snapshot) {
	lines := make([]string, len(s.Lines))
	copy(lines, s.Lines)
	if t.Buffer == nil {
		t.Buffer = NewBuffer("")
	}
	t.Buffer.Lines = lines
	t.Cursor = s.Cursor
	t.Anchor = s.Anchor
	t.cursorMoved = true
	t.StyleStale = true
}

// initUndo seeds the original-state snapshot used by RevertFile. Called
// from NewTab and Reload — both moments where the buffer is now "what's
// on disk" and any prior history is meaningless.
func (t *Tab) initUndo() {
	t.undoOriginal = t.captureSnapshot()
	t.undoStack = nil
	t.redoStack = nil
	t.lastUndoGroup = undoGroupNone
}

// pushUndo records the *current* state on the undo stack so the caller
// can mutate freely afterwards. group selects coalescing behavior — a
// typing push within undoCoalesceWindow of another typing push is a
// no-op, while undoGroupStructural always creates a new entry. Any new
// edit invalidates the redo stack.
func (t *Tab) pushUndo(group undoGroup) {
	if t.canCoalesce(group) {
		t.lastUndoAt = time.Now() // extend the window
		return
	}
	snap := t.captureSnapshot()
	t.undoStack = append(t.undoStack, snap)
	if len(t.undoStack) > maxUndoEntries {
		// Drop the oldest entry. Keep the original snapshot intact —
		// it lives in undoOriginal, not the stack.
		t.undoStack = t.undoStack[1:]
	}
	t.redoStack = nil
	t.lastUndoGroup = group
	t.lastUndoAt = time.Now()
}

// canCoalesce reports whether a pending push of the given group should
// be skipped because it would collapse into the previous one.
func (t *Tab) canCoalesce(group undoGroup) bool {
	if t.lastUndoGroup == undoGroupNone || group == undoGroupStructural {
		return false
	}
	if t.lastUndoGroup != group {
		return false
	}
	return time.Since(t.lastUndoAt) <= undoCoalesceWindow
}

// breakUndoGroup closes the current coalescing window so the next
// content-changing op starts a fresh entry. Called from cursor-moving
// methods (the user moved the caret, so the next typing burst is
// clearly a separate intent) and from Save (a save is a natural
// "logical step" boundary).
func (t *Tab) breakUndoGroup() {
	t.lastUndoGroup = undoGroupNone
}

// CanUndo reports whether there is anything to roll back. The action
// menu uses this to enable / disable the Undo row.
func (t *Tab) CanUndo() bool {
	return len(t.undoStack) > 0
}

// CanRedo reports whether a previously undone change can be re-applied.
func (t *Tab) CanRedo() bool {
	return len(t.redoStack) > 0
}

// CanRevert reports whether the buffer differs from the original state
// captured at NewTab / Reload time. Used to gate the Revert menu row.
func (t *Tab) CanRevert() bool {
	if t.Buffer == nil {
		return false
	}
	if len(t.Buffer.Lines) != len(t.undoOriginal.Lines) {
		return true
	}
	for i, ln := range t.Buffer.Lines {
		if ln != t.undoOriginal.Lines[i] {
			return true
		}
	}
	return false
}

// Undo restores the previous snapshot. Returns true when a step was
// actually undone — false lets the caller flash a "nothing to undo"
// message. The current state is moved onto the redo stack first so a
// subsequent Redo can replay it forward.
func (t *Tab) Undo() bool {
	if len(t.undoStack) == 0 {
		return false
	}
	current := t.captureSnapshot()
	t.redoStack = append(t.redoStack, current)

	last := len(t.undoStack) - 1
	prev := t.undoStack[last]
	t.undoStack = t.undoStack[:last]

	t.applySnapshot(prev)
	t.Dirty = t.CanRevert()
	t.breakUndoGroup()
	return true
}

// Redo re-applies a step that was just undone. Returns true when a step
// was actually redone.
func (t *Tab) Redo() bool {
	if len(t.redoStack) == 0 {
		return false
	}
	current := t.captureSnapshot()
	t.undoStack = append(t.undoStack, current)

	last := len(t.redoStack) - 1
	next := t.redoStack[last]
	t.redoStack = t.redoStack[:last]

	t.applySnapshot(next)
	t.Dirty = t.CanRevert()
	t.breakUndoGroup()
	return true
}

// RevertFile rewinds the buffer all the way back to the snapshot
// captured when the file was first opened (or last reloaded). The
// current state is pushed onto the undo stack first so the user can
// recover from an accidental Revert with one Undo. Returns true when
// the buffer actually changed; false if it was already at the original.
func (t *Tab) RevertFile() bool {
	if !t.CanRevert() {
		return false
	}
	current := t.captureSnapshot()
	t.undoStack = append(t.undoStack, current)
	if len(t.undoStack) > maxUndoEntries {
		t.undoStack = t.undoStack[1:]
	}
	t.redoStack = nil
	t.applySnapshot(t.undoOriginal)
	t.Dirty = t.CanRevert() // false now — buffer matches original
	t.breakUndoGroup()
	return true
}
