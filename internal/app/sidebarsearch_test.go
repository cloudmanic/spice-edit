// =============================================================================
// File: internal/app/sidebarsearch_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-12
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/finder"
)

func TestSwitchSidebarTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarTab = "files"

	a.switchSidebarTab("search")
	if a.sidebarTab != "search" || !a.sidebarSearchFocused {
		t.Fatalf("search: tab=%q focused=%v", a.sidebarTab, a.sidebarSearchFocused)
	}

	a.switchSidebarTab("files")
	if a.sidebarTab != "files" || a.sidebarSearchFocused {
		t.Fatalf("files: tab=%q focused=%v", a.sidebarTab, a.sidebarSearchFocused)
	}
}

func TestSidebarSearchGroups_OrderAndCollapse(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarSearchResults = []finder.ContentMatch{
		{Path: "a.go", Line: 0, Col: 0, Width: 3, Preview: "aaa"},
		{Path: "a.go", Line: 5, Col: 1, Width: 3, Preview: "bbb"},
		{Path: "b.go", Line: 2, Col: 0, Width: 3, Preview: "ccc"},
	}
	a.sidebarSearchCollapsed = map[string]bool{"a.go": true}

	groups := a.sidebarSearchGroups()
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Path != "a.go" || len(groups[0].Matches) != 2 || !groups[0].Collapsed {
		t.Errorf("group[0] = %+v, want a.go/2/collapsed", groups[0])
	}
	if groups[1].Path != "b.go" || len(groups[1].Matches) != 1 || groups[1].Collapsed {
		t.Errorf("group[1] = %+v, want b.go/1/expanded", groups[1])
	}
}

func TestSidebarClick_TabStrip(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarTab = "files"
	_, _, sw, _ := a.sidebarRect()

	a.sidebarClick(1, 0)
	if a.sidebarTab != "files" {
		t.Fatalf("left half: tab=%q, want files", a.sidebarTab)
	}

	a.sidebarClick(sw-1, 0)
	if a.sidebarTab != "search" {
		t.Fatalf("right half: tab=%q, want search", a.sidebarTab)
	}
}

func TestSidebarSearchClick_OpenMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\nsecond\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := newTestApp(t, dir)
	a.sidebarTab = "search"
	a.sidebarSearchResults = []finder.ContentMatch{
		{Path: "a.txt", Line: 0, Col: 0, Width: 5, Preview: "hello world"},
	}
	// Populate the transient row list used for click hit-testing.
	a.drawSidebarSearch()

	// Header at y=2, match row at y=3 (results area starts at y=2).
	a.sidebarSearchClick(1, 3)

	tab := a.activeTabPtr()
	if tab == nil || tab.Path != filepath.Join(dir, "a.txt") {
		t.Fatalf("active tab = %+v, want a.txt", tab)
	}
	if tab.Cursor.Line != 0 || tab.Cursor.Col != 0 {
		t.Errorf("cursor = %d:%d, want 0:0", tab.Cursor.Line, tab.Cursor.Col)
	}
}

func TestSidebarSearchKey_EscReturnsToFiles(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarTab = "search"
	a.sidebarSearchFocused = true

	a.handleSidebarSearchKey(tcell.NewEventKey(tcell.KeyEsc, 0, 0))

	if a.sidebarTab != "files" {
		t.Fatalf("tab = %q, want files", a.sidebarTab)
	}
	if a.sidebarSearchFocused {
		t.Fatal("focused still true after Esc")
	}
}
