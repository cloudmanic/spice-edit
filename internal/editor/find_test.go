// =============================================================================
// File: internal/editor/find_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"reflect"
	"testing"
)

// TestFindAll_BasicMatches walks across multiple lines and pins down the
// document-order ordering plus the rune-indexed Col / Width fields.
func TestFindAll_BasicMatches(t *testing.T) {
	buf := NewBuffer("foo bar foo\nbaz foo\n")
	got := FindAll(buf, "foo")
	want := []Match{
		{Line: 0, Col: 0, Width: 3},
		{Line: 0, Col: 8, Width: 3},
		{Line: 1, Col: 4, Width: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FindAll mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestFindAll_CaseInsensitive proves matching ignores letter case in
// both the query and the buffer. Without this, the "type to find" UX
// is much less forgiving than users expect from VS Code.
func TestFindAll_CaseInsensitive(t *testing.T) {
	buf := NewBuffer("Foo FOO foO")
	got := FindAll(buf, "fOo")
	if len(got) != 3 {
		t.Fatalf("expected 3 case-insensitive matches, got %d: %v", len(got), got)
	}
}

// TestFindAll_EmptyQuery returns nil so the UI can render an empty
// state without a special "0 of 0" branch.
func TestFindAll_EmptyQuery(t *testing.T) {
	buf := NewBuffer("anything")
	if got := FindAll(buf, ""); got != nil {
		t.Fatalf("empty query should return nil, got %v", got)
	}
}

// TestFindAll_NonOverlapping pins down the scanner's advance-past-match
// behaviour. "aaa" in "aaaaaa" should yield two non-overlapping hits,
// matching VS Code's default search semantics.
func TestFindAll_NonOverlapping(t *testing.T) {
	buf := NewBuffer("aaaaaa")
	got := FindAll(buf, "aaa")
	want := []Match{
		{Line: 0, Col: 0, Width: 3},
		{Line: 0, Col: 3, Width: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected non-overlapping matches, got %v", got)
	}
}

// TestFindAll_MultiByteRunes pins down the rune-indexed column
// convention. The buffer contains a 3-byte UTF-8 character before the
// match — Col must report 1 (one rune in), not 3 (three bytes in).
func TestFindAll_MultiByteRunes(t *testing.T) {
	buf := NewBuffer("✓foo")
	got := FindAll(buf, "foo")
	want := []Match{{Line: 0, Col: 1, Width: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("multi-byte handling wrong, got %v", got)
	}
}

// TestFindAll_NilBuffer is the defensive guard — callers may hold a
// freshly-zeroed Tab during construction. Returning nil rather than
// panicking lets the UI cope without an explicit nil check.
func TestFindAll_NilBuffer(t *testing.T) {
	if got := FindAll(nil, "x"); got != nil {
		t.Fatalf("nil buffer should return nil, got %v", got)
	}
}

// TestFirstMatchAtOrAfter_BasicForward finds the first match at or
// after the cursor, which is what we want when a user types a query
// in the bar — we shouldn't snap them backwards past where they were
// already looking.
func TestFirstMatchAtOrAfter_BasicForward(t *testing.T) {
	matches := []Match{
		{Line: 0, Col: 0, Width: 3},
		{Line: 1, Col: 4, Width: 3},
		{Line: 2, Col: 0, Width: 3},
	}
	idx := FirstMatchAtOrAfter(matches, Position{Line: 1, Col: 0})
	if idx != 1 {
		t.Fatalf("expected idx=1 (line 1 match), got %d", idx)
	}
}

// TestFirstMatchAtOrAfter_WrapsToTop covers the case where the cursor
// is past every match: we wrap to the top so the user can keep
// pressing Enter to cycle.
func TestFirstMatchAtOrAfter_WrapsToTop(t *testing.T) {
	matches := []Match{{Line: 0, Col: 0, Width: 3}}
	idx := FirstMatchAtOrAfter(matches, Position{Line: 99, Col: 0})
	if idx != 0 {
		t.Fatalf("expected wrap to idx=0, got %d", idx)
	}
}

// TestFirstMatchAtOrAfter_Empty is the no-matches case — return -1 so
// the caller can short-circuit without checking length again.
func TestFirstMatchAtOrAfter_Empty(t *testing.T) {
	if got := FirstMatchAtOrAfter(nil, Position{}); got != -1 {
		t.Fatalf("expected -1 for empty matches, got %d", got)
	}
}

// TestTab_SetFindQuery_PicksNearestMatch installs a query and pins the
// "land on the nearest hit, not always the first hit" contract: with the
// cursor on line 1, the index should point at the line-1 match, not the
// earlier line-0 one.
func TestTab_SetFindQuery_PicksNearestMatch(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("foo\nfoo\nfoo")
	tab.Cursor = Position{Line: 1, Col: 0}

	tab.SetFindQuery("foo")
	if got, want := tab.FindIndex, 1; got != want {
		t.Fatalf("FindIndex = %d, want %d (nearest to cursor)", got, want)
	}
}

// TestTab_SetFindQuery_EmptyClears proves an empty query clears every
// piece of find state. Closing the bar relies on this behaviour to wipe
// out the highlight band.
func TestTab_SetFindQuery_EmptyClears(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("foo")
	tab.SetFindQuery("foo")
	if tab.FindIndex < 0 {
		t.Fatal("setup expected a current match")
	}
	tab.SetFindQuery("")
	if tab.FindMatches != nil || tab.FindIndex != -1 || tab.FindQuery != "" {
		t.Fatalf("empty query should clear all find state, got %+v", tab)
	}
}

// TestTab_FindNext_WrapsAndMovesCursor exercises the Enter-in-the-bar
// path. After three Next presses we should land on match 0 again (wrap)
// with the cursor on top of it.
func TestTab_FindNext_WrapsAndMovesCursor(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("foo\nfoo\nfoo")
	tab.SetFindQuery("foo") // FindIndex = 0
	tab.FindNext()          // -> 1
	tab.FindNext()          // -> 2
	tab.FindNext()          // -> 0 (wrap)
	if tab.FindIndex != 0 {
		t.Fatalf("expected wrap to 0, got %d", tab.FindIndex)
	}
	if tab.Cursor != (Position{Line: 0, Col: 0}) {
		t.Fatalf("cursor should follow the active match, got %+v", tab.Cursor)
	}
}

// TestTab_FindPrev_WrapsBackwards is the Shift-Enter equivalent — from
// the first match, Prev wraps to the last.
func TestTab_FindPrev_WrapsBackwards(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("foo\nfoo\nfoo")
	tab.SetFindQuery("foo")
	tab.FindPrev()
	if tab.FindIndex != 2 {
		t.Fatalf("expected wrap to last (2), got %d", tab.FindIndex)
	}
}

// TestTab_FindNext_NoMatchesIsSafe pins the contract that Find ops are
// no-ops when there's nothing to find. Without this, a stray hotkey on
// an empty result set would crash.
func TestTab_FindNext_NoMatchesIsSafe(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("hello world")
	tab.SetFindQuery("zzz")
	tab.FindNext() // must not panic
	tab.FindPrev() // must not panic
	if tab.FindIndex != -1 {
		t.Fatalf("FindIndex should stay -1 with no matches, got %d", tab.FindIndex)
	}
}

// TestTab_ClearFind wipes everything so the renderer stops highlighting.
func TestTab_ClearFind(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("foo")
	tab.SetFindQuery("foo")
	tab.ClearFind()
	if tab.FindQuery != "" || tab.FindMatches != nil || tab.FindIndex != -1 {
		t.Fatalf("ClearFind left residue: %+v", tab)
	}
}

// TestMatchAtRune_HitAndMiss proves the per-cell renderer probe finds
// the right match index for cells inside a hit and -1 outside.
func TestMatchAtRune_HitAndMiss(t *testing.T) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer("foo bar foo")
	tab.SetFindQuery("foo") // matches at (0,0) and (0,8)

	if got := tab.matchAtRune(0, 1); got != 0 {
		t.Fatalf("col 1 should be inside match 0, got %d", got)
	}
	if got := tab.matchAtRune(0, 4); got != -1 {
		t.Fatalf("col 4 (the space) should miss, got %d", got)
	}
	if got := tab.matchAtRune(0, 9); got != 1 {
		t.Fatalf("col 9 should be inside match 1, got %d", got)
	}
}
