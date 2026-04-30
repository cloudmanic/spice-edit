// =============================================================================
// File: internal/editor/find.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// find.go implements the editor's in-file search primitives. Matching is
// case-insensitive substring on rune-decoded lines so multi-byte characters
// behave as one column each (consistent with how the cursor / selection
// already treat columns elsewhere). Regex, whole-word, and case-sensitive
// toggles are intentionally out of scope for v1 — the 80/20 here is the
// VS-Code-style "type and jump" loop, not power-user search.

package editor

import "strings"

// Match describes one find hit. Line and Col follow the same rune-indexed
// convention as Position; Width is the rune count of the query so the
// renderer can paint the right number of cells without re-running the
// matcher.
type Match struct {
	Line  int
	Col   int
	Width int
}

// FindAll returns every case-insensitive substring match of query inside
// buf, in document order. An empty query returns nil — the caller is
// expected to clear its UI rather than show "0 of 0" results. Matches do
// not overlap: after a hit the scanner advances past the matched run, so
// "aaaa" with query "aa" yields two matches at columns 0 and 2.
func FindAll(buf *Buffer, query string) []Match {
	if query == "" || buf == nil {
		return nil
	}
	needle := []rune(strings.ToLower(query))
	if len(needle) == 0 {
		return nil
	}
	var out []Match
	for lineIdx, raw := range buf.Lines {
		hay := []rune(strings.ToLower(raw))
		col := 0
		for col+len(needle) <= len(hay) {
			if runesEqual(hay[col:col+len(needle)], needle) {
				out = append(out, Match{Line: lineIdx, Col: col, Width: len(needle)})
				col += len(needle)
				continue
			}
			col++
		}
	}
	return out
}

// runesEqual returns true when two equal-length rune slices match
// element-for-element. Inlined so the hot inner loop of FindAll doesn't
// pay for a generic slices.Equal call (which exists in 1.21+ but pulls in
// the slices package).
func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// FirstMatchAtOrAfter returns the index into matches of the first hit at
// or after cursor, or 0 when cursor sits past the last match (we wrap
// around to the top — that's what the user expects after typing a query
// at the bottom of a file).
//
// Returns -1 when matches is empty so callers can short-circuit without
// re-checking the length.
func FirstMatchAtOrAfter(matches []Match, cursor Position) int {
	if len(matches) == 0 {
		return -1
	}
	for i, m := range matches {
		if m.Line > cursor.Line || (m.Line == cursor.Line && m.Col >= cursor.Col) {
			return i
		}
	}
	// Cursor is past every match — wrap to the top.
	return 0
}

// MatchPosition returns the cursor-friendly Position at the start of m.
// Trivial helper, but it keeps callers from constructing Position literals
// by hand (which loses the "rune-indexed" intent at the call site).
func MatchPosition(m Match) Position {
	return Position{Line: m.Line, Col: m.Col}
}

// MatchEndPosition returns the position one past the end of m — useful
// when the caller wants to set a selection that covers the match.
func MatchEndPosition(m Match) Position {
	return Position{Line: m.Line, Col: m.Col + m.Width}
}

// SetFindQuery installs a new search query on the tab, recomputes the
// match list against the current buffer, and points FindIndex at the
// first match at or after the cursor (so the user lands on the nearest
// hit, not always the first hit in the file). An empty query clears all
// find state — symmetrical with closing the bar via Esc.
//
// The cursor is left where it is; SetFindQuery only updates state. It is
// the caller's job to call FocusCurrentMatch when they want the cursor
// to actually move (which is what happens on the first non-empty query
// and on every Enter / Shift-Enter press).
func (t *Tab) SetFindQuery(query string) {
	t.FindQuery = query
	if query == "" {
		t.FindMatches = nil
		t.FindIndex = -1
		return
	}
	t.FindMatches = FindAll(t.Buffer, query)
	t.FindIndex = FirstMatchAtOrAfter(t.FindMatches, t.Cursor)
}

// FocusCurrentMatch moves the cursor (and anchor — we don't want a
// dangling selection from an earlier action) to the start of the
// currently-pointed match. No-op when FindIndex is out of range, so
// callers don't have to re-check it themselves.
func (t *Tab) FocusCurrentMatch() {
	if t.FindIndex < 0 || t.FindIndex >= len(t.FindMatches) {
		return
	}
	m := t.FindMatches[t.FindIndex]
	t.Cursor = MatchPosition(m)
	t.Anchor = t.Cursor
	t.cursorMoved = true
}

// FindNext advances FindIndex by one (wrapping at the end) and moves
// the cursor onto the new match. No-op when there are no matches. Used
// by Enter inside the find bar and by the Esc-g "again" leader.
func (t *Tab) FindNext() {
	if len(t.FindMatches) == 0 {
		return
	}
	t.FindIndex = (t.FindIndex + 1) % len(t.FindMatches)
	t.FocusCurrentMatch()
}

// FindPrev moves FindIndex backwards by one (wrapping at the start) and
// moves the cursor onto the new match. Used by Shift-Enter inside the
// find bar.
func (t *Tab) FindPrev() {
	if len(t.FindMatches) == 0 {
		return
	}
	t.FindIndex--
	if t.FindIndex < 0 {
		t.FindIndex = len(t.FindMatches) - 1
	}
	t.FocusCurrentMatch()
}

// matchAtRune returns the index into FindMatches of the match that
// covers (line, col), or -1 when none does. The renderer calls this once
// per visible cell, so the implementation has to stay cheap — a linear
// scan is fine for realistic file sizes (hundreds of matches at most),
// and avoids the overhead of building a per-line index. If this ever
// shows up in a profile, switch to a sorted-by-line bsearch.
func (t *Tab) matchAtRune(line, col int) int {
	for i, m := range t.FindMatches {
		if m.Line != line {
			continue
		}
		if col >= m.Col && col < m.Col+m.Width {
			return i
		}
	}
	return -1
}

// ClearFind drops every piece of find state. The app calls this when the
// buffer has been edited enough that the cached match list is stale and
// can't safely be re-used; the user will re-type their query.
func (t *Tab) ClearFind() {
	t.FindQuery = ""
	t.FindMatches = nil
	t.FindIndex = -1
}
