// =============================================================================
// File: internal/filetree/filetree.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package filetree implements the left-hand sidebar's file explorer. It is a
// lazy directory tree: children are only read from disk when their parent is
// expanded, so opening the editor on a huge repo is still instant. The tree
// also keeps a flat list of "currently visible" rows so that hit-testing a
// click against rendered rows is O(1).
package filetree

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spiceedit/internal/theme"
)

// Node is a single entry in the file tree. Directories also carry their
// children (loaded lazily on first expansion); files carry only their path.
type Node struct {
	Path     string
	Name     string
	IsDir    bool
	Expanded bool
	Loaded   bool
	Children []*Node
}

// Tree owns the root node and the most recently rendered flat list of
// visible rows. Click hit-testing maps a screen row index back to the Node
// drawn at that row.
type Tree struct {
	Root    *Node
	visible []*Node // index = screen row in the list area; nil for blank rows.
	ScrollY int
}

// New creates a tree rooted at root and pre-loads its top-level children so
// the user sees something immediately. Hidden entries (dotfiles) are kept
// because they're often what people actually want to inspect over SSH.
func New(root string) (*Tree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, os.ErrInvalid
	}
	n := &Node{Path: abs, Name: filepath.Base(abs), IsDir: true, Expanded: true}
	if err := loadChildren(n); err != nil {
		return nil, err
	}
	return &Tree{Root: n}, nil
}

// loadChildren reads directory entries from disk and builds child Nodes.
// Directories are sorted first, then files; both alphabetically (case-
// insensitive). Hidden files are included on purpose — see the package doc.
func loadChildren(n *Node) error {
	if !n.IsDir || n.Loaded {
		return nil
	}
	entries, err := os.ReadDir(n.Path)
	if err != nil {
		return err
	}
	children := make([]*Node, 0, len(entries))
	for _, e := range entries {
		if shouldHide(e.Name()) {
			continue
		}
		c := &Node{
			Path:  filepath.Join(n.Path, e.Name()),
			Name:  e.Name(),
			IsDir: e.IsDir(),
		}
		children = append(children, c)
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})
	n.Children = children
	n.Loaded = true
	return nil
}

// shouldHide is the project's small, opinionated list of names the file
// tree refuses to show. These are universally noise: VCS metadata, OS
// junk, language-specific build caches.
func shouldHide(name string) bool {
	switch name {
	case ".git", ".svn", ".hg",
		".DS_Store",
		"node_modules",
		".idea", ".vscode":
		return true
	}
	return false
}

// flatNode pairs a Node with its render depth so the renderer can indent
// without re-walking the tree.
type flatNode struct {
	Node  *Node
	Depth int
}

// flattenInto appends node into out. If node is an expanded directory, it
// recursively appends its children at depth+1.
func flattenInto(n *Node, depth int, out *[]flatNode) {
	if n == nil {
		return
	}
	*out = append(*out, flatNode{Node: n, Depth: depth})
	if n.IsDir && n.Expanded {
		for _, c := range n.Children {
			flattenInto(c, depth+1, out)
		}
	}
}

// Render draws the tree into the rectangle (x, y, w, h). Each visible row
// is also remembered (in t.visible) so HitTest can map a click back to a
// node without re-walking the tree.
func (t *Tree) Render(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	bg := th.SidebarBG
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(th.Text)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	// Header — small all-caps label above the project name.
	headerStyle := tcell.StyleDefault.Background(bg).Foreground(th.Muted).Bold(true)
	drawString(scr, x, y, w, " EXPLORER", headerStyle)
	rootStyle := tcell.StyleDefault.Background(bg).Foreground(th.Accent).Bold(true)
	drawString(scr, x, y+1, w, " "+t.Root.Name, rootStyle)

	// Build the flat list of visible rows from the root's children.
	flat := make([]flatNode, 0, 128)
	for _, c := range t.Root.Children {
		flattenInto(c, 0, &flat)
	}

	listTop := y + 2
	listH := h - 2
	if listH < 0 {
		listH = 0
	}
	t.clampScroll(len(flat), listH)

	visible := make([]*Node, 0, listH)
	for row := 0; row < listH; row++ {
		idx := t.ScrollY + row
		if idx < 0 || idx >= len(flat) {
			visible = append(visible, nil)
			continue
		}
		item := flat[idx]
		drawNodeRow(scr, th, x, listTop+row, w, item)
		visible = append(visible, item.Node)
	}
	t.visible = visible
}

// drawNodeRow renders one tree row with proper indent, chevron, and color.
func drawNodeRow(scr tcell.Screen, th theme.Theme, x, y, w int, item flatNode) {
	bg := th.SidebarBG
	indent := strings.Repeat("  ", item.Depth)
	var line string
	var fg tcell.Color
	if item.Node.IsDir {
		chev := "▸"
		if item.Node.Expanded {
			chev = "▾"
		}
		line = " " + indent + chev + " " + item.Node.Name + "/"
		fg = th.FolderColor
	} else {
		line = " " + indent + "  " + item.Node.Name
		fg = th.FileColor
	}
	style := tcell.StyleDefault.Background(bg).Foreground(fg)
	drawString(scr, x, y, w, line, style)
}

// drawString writes s left-aligned within [x, x+w). Excess content is
// truncated; short content is implicitly padded by the row's pre-painted bg.
func drawString(scr tcell.Screen, x, y, w int, s string, st tcell.Style) {
	col := 0
	for _, r := range s {
		if col >= w {
			return
		}
		scr.SetContent(x+col, y, r, nil, st)
		col++
	}
}

// clampScroll keeps ScrollY within bounds for the current visible-row count.
func (t *Tree) clampScroll(total, viewH int) {
	if total <= viewH {
		t.ScrollY = 0
		return
	}
	max := total - viewH
	if t.ScrollY > max {
		t.ScrollY = max
	}
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
}

// HitTest maps a click within the tree's render rectangle to a Node.
// ok=false means the click landed on the header rows or empty space below
// the last entry.
func (t *Tree) HitTest(localX, localY int) (*Node, bool) {
	_ = localX
	if localY < 2 {
		return nil, false
	}
	row := localY - 2
	if row < 0 || row >= len(t.visible) {
		return nil, false
	}
	n := t.visible[row]
	if n == nil {
		return nil, false
	}
	return n, true
}

// Toggle expands or collapses a directory node, lazily loading its children
// the first time it is expanded.
func (t *Tree) Toggle(n *Node) {
	if !n.IsDir {
		return
	}
	if !n.Expanded {
		_ = loadChildren(n)
	}
	n.Expanded = !n.Expanded
}

// Scroll moves the file tree's viewport by delta rows (negative = up).
func (t *Tree) Scroll(delta int) {
	t.ScrollY += delta
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
}
