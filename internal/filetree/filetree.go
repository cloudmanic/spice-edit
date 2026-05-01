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

	"github.com/cloudmanic/spice-edit/internal/icons"
	"github.com/cloudmanic/spice-edit/internal/theme"
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

	// ActiveFolder is the absolute path of the folder the user is
	// currently "working in" — the default target for actions like New
	// File. The Render() method bolds the matching row so the choice is
	// always visible. The app updates this whenever the user clicks a
	// tree node or opens a file.
	ActiveFolder string

	// DirtyFiles and DirtyFolders carry the project's git status — both
	// indexed by absolute path. Files in DirtyFiles render in the theme's
	// Modified color; folders in DirtyFolders do the same so a collapsed
	// branch still signals there's a change inside. Both maps are nil
	// when the project isn't a git repo or when git status hasn't been
	// loaded yet, and the renderer treats nil as "everything clean".
	DirtyFiles   map[string]bool
	DirtyFolders map[string]bool

	// IconsEnabled toggles the Nerd Font glyph that prefixes each row.
	// Set by App.loadSpiceConfig at startup based on the user's
	// config.json + auto-detection. Off means the row is rendered with
	// only the existing chevron (the legacy look) — important for
	// terminals or fonts that can't render the private-use glyphs.
	IconsEnabled bool
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

// loadChildren is the lazy-load entry point used the first time a directory
// is expanded. It defers to reload, which knows how to merge fresh disk
// state with whatever (if anything) we already had cached.
func loadChildren(n *Node) error {
	if !n.IsDir || n.Loaded {
		return nil
	}
	return n.reload()
}

// reload re-reads the directory's children from disk and replaces n.Children
// with the new list. Existing child Nodes whose names still appear on disk
// are kept by-pointer so their Expanded state, loaded grandchildren, etc.
// survive a refresh. New names get fresh Nodes; vanished names are dropped.
func (n *Node) reload() error {
	if !n.IsDir {
		return nil
	}
	entries, err := os.ReadDir(n.Path)
	if err != nil {
		return err
	}

	existing := make(map[string]*Node, len(n.Children))
	for _, c := range n.Children {
		existing[c.Name] = c
	}

	children := make([]*Node, 0, len(entries))
	for _, e := range entries {
		if shouldHide(e.Name()) {
			continue
		}
		if old, ok := existing[e.Name()]; ok && old.IsDir == e.IsDir() {
			children = append(children, old)
			continue
		}
		children = append(children, &Node{
			Path:  filepath.Join(n.Path, e.Name()),
			Name:  e.Name(),
			IsDir: e.IsDir(),
		})
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

// Refresh re-reads every directory in the tree that has been loaded at
// least once (i.e. anywhere the user has previously expanded). Surviving
// entries keep their Node pointers so deeper Expanded state is preserved;
// new files appear, deleted files vanish.
func (t *Tree) Refresh() {
	refreshNode(t.Root)
}

// refreshNode is Tree.Refresh's recursive worker. It reloads only Loaded
// directories — there's no value in reading directories the user has
// never seen.
func refreshNode(n *Node) {
	if !n.IsDir || !n.Loaded {
		return
	}
	_ = n.reload()
	for _, c := range n.Children {
		refreshNode(c)
	}
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

	// Header — small all-caps label above the project name. The
	// project name itself is also a click target: it's the only way
	// to reset the active folder back to the root once a subfolder
	// has been selected. Render bold/Accent when it *is* the active
	// folder, plain text otherwise — same visual rule the children
	// rows follow, so the highlight is honest.
	headerStyle := tcell.StyleDefault.Background(bg).Foreground(th.Muted).Bold(true)
	drawString(scr, x, y, w, " EXPLORER", headerStyle)
	rootActive := t.ActiveFolder == "" || t.ActiveFolder == t.Root.Path
	rootStyle := tcell.StyleDefault.Background(bg).Foreground(th.Text).Bold(true)
	if rootActive {
		rootStyle = tcell.StyleDefault.Background(bg).Foreground(th.Accent).Bold(true)
	}
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
		active := item.Node.IsDir && item.Node.Path == t.ActiveFolder
		dirty := t.isDirty(item.Node)
		drawNodeRow(scr, th, x, listTop+row, w, item, active, dirty, t.IconsEnabled)
		visible = append(visible, item.Node)
	}
	t.visible = visible
}

// isDirty reports whether a node should render in the Modified color —
// either because the file itself has uncommitted changes or because a
// folder somewhere below it does. Returns false for any node when git
// status hasn't been loaded.
func (t *Tree) isDirty(n *Node) bool {
	if n == nil {
		return false
	}
	if n.IsDir {
		return t.DirtyFolders[n.Path]
	}
	return t.DirtyFiles[n.Path]
}

// drawNodeRow renders one tree row with proper indent, chevron, and color.
// active=true marks this folder as the editor's current working folder
// (the New File default), and is drawn bold + accent-tinted so the user
// can see at a glance where the next "New file" will land. dirty=true
// marks the node as having uncommitted git changes (or, for folders,
// containing some) — it overrides the normal foreground with the
// theme's Modified color so changed files stand out at a glance.
// withIcons=true prefixes the name with a Nerd Font glyph + space; off
// renders the legacy chevron-only look for terminals that can't show
// the private-use glyphs.
//
// When icons are enabled the row is rendered in three segments
// (prefix → glyph → name) so the glyph can take its own per-language
// colour while the name keeps the row's normal fg/dirty/active
// styling. That's the visual cue you find in nvim-tree and friends:
// a quick eye-scan picks out Go from Ruby from Markdown without
// reading any text.
func drawNodeRow(scr tcell.Screen, th theme.Theme, x, y, w int, item flatNode, active, dirty, withIcons bool) {
	bg := th.SidebarBG
	indent := strings.Repeat("  ", item.Depth)

	// Compute the row-level foreground (active/dirty/normal cascade).
	var fg tcell.Color
	if item.Node.IsDir {
		fg = th.FolderColor
		if active {
			fg = th.Accent
		}
	} else {
		fg = th.FileColor
	}
	// Dirty wins over the normal foreground but the active-folder bold
	// stays — the user still wants to see *which* changed folder is the
	// current target.
	if dirty {
		fg = th.Modified
	}
	rowStyle := tcell.StyleDefault.Background(bg).Foreground(fg)
	if active {
		rowStyle = rowStyle.Bold(true)
	}

	// Build the left chunk (indent + chevron + space) and right chunk
	// (name, with a trailing slash for dirs). Both render in rowStyle;
	// only the glyph between them gets its own colour.
	var prefix, suffix string
	if item.Node.IsDir {
		chev := "▸"
		if item.Node.Expanded {
			chev = "▾"
		}
		prefix = " " + indent + chev + " "
		suffix = item.Node.Name + "/"
	} else {
		prefix = " " + indent + "  "
		suffix = item.Node.Name
	}

	if !withIcons {
		drawString(scr, x, y, w, prefix+suffix, rowStyle)
		return
	}

	glyph := icons.For(item.Node.Name, item.Node.IsDir, item.Node.Expanded)
	glyphFg := icons.ColorFor(item.Node.Name, item.Node.IsDir, fg)
	// Dirty files keep their per-language glyph colour — the language
	// hue is the at-a-glance cue, and the name turning Modified is
	// already enough to flag "this is dirty".
	glyphStyle := tcell.StyleDefault.Background(bg).Foreground(glyphFg)
	if active {
		glyphStyle = glyphStyle.Bold(true)
	}

	drawString(scr, x, y, w, prefix, rowStyle)
	px := len([]rune(prefix))
	drawString(scr, x+px, y, w-px, glyph, glyphStyle)
	gx := len([]rune(glyph))
	drawString(scr, x+px+gx, y, w-px-gx, "  "+suffix, rowStyle)
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
// Row 0 is the "EXPLORER" header (not clickable). Row 1 is the project
// root name — clicking it returns t.Root so the caller can set the
// active folder back to the project root, which is otherwise
// unreachable once the user has selected any subfolder. Rows 2+ map
// into the rendered children list.
//
// ok=false means the click landed on the EXPLORER header or empty
// space below the last entry.
func (t *Tree) HitTest(localX, localY int) (*Node, bool) {
	_ = localX
	if localY < 1 {
		return nil, false
	}
	if localY == 1 {
		return t.Root, true
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
