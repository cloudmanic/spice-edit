// =============================================================================
// File: internal/editor/indent.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// indent.go owns two related concerns:
//
//   1. Visual-column math. Tabs render as multi-cell tab stops, so a rune's
//      "screen position" is no longer equal to its index in the line. The
//      renderer, cursor placement, and mouse hit-test all consult these
//      helpers when converting between rune indices and visual cells.
//
//   2. Indent style detection. When a tab is opened, we sample its existing
//      indentation and pick the unit (tab character vs N spaces) the user
//      gets when they press Tab. This avoids the "spaces in a tab-indented
//      file" bug that has historically been the #1 complaint about new
//      editors.

package editor

import (
	"path/filepath"
	"strings"
)

// TabStop is the visual cell width of a hard tab. Four matches what most
// modern editors and viewers default to (cat, less, GitHub) and matches the
// editor's typical insert width when the user presses Tab.
const TabStop = 4

// defaultSpaceIndent is the fallback indent unit when detection has nothing
// to go on (empty file, no leading whitespace anywhere). Four spaces is
// the common convention across the languages the editor most often opens.
const defaultSpaceIndent = "    "

// RuneVisualWidth returns the cell count occupied by r when it lands at the
// given visualCol (0-based, measured from the start of the line). Hard tabs
// expand to fill enough cells to reach the next TabStop boundary; everything
// else is one cell. This deliberately ignores east-asian wide chars and
// combining marks — the rest of the buffer code treats one rune as one cell
// and adding wide-char support is a separate, larger project.
func RuneVisualWidth(r rune, visualCol int) int {
	if r == '\t' {
		w := TabStop - (visualCol % TabStop)
		if w == 0 {
			w = TabStop
		}
		return w
	}
	return 1
}

// LineVisualCol returns the visual column (0-based) at the rune position
// runeCol within runes. Tabs in the prefix expand to tab stops. Used for
// cursor placement and selection / find highlighting.
//
// runeCol is clamped to [0, len(runes)] so callers can pass an end-of-line
// position without bounds-checking.
func LineVisualCol(runes []rune, runeCol int) int {
	if runeCol > len(runes) {
		runeCol = len(runes)
	}
	visualCol := 0
	for i := 0; i < runeCol; i++ {
		visualCol += RuneVisualWidth(runes[i], visualCol)
	}
	return visualCol
}

// RuneColAtVisual returns the rune index whose start column is at or just
// before targetVisualCol, snapping clicks inside a tab's visual span back
// to the tab's start. Used by mouse hit-testing — clicking anywhere in a
// 4-cell tab places the cursor at the tab character itself, not somewhere
// "inside" it (which would be a no-op anyway).
//
// When targetVisualCol is past the line's end, the rune count is returned
// (cursor lands at the end-of-line virtual position).
func RuneColAtVisual(runes []rune, targetVisualCol int) int {
	if targetVisualCol <= 0 {
		return 0
	}
	visualCol := 0
	for i, r := range runes {
		w := RuneVisualWidth(r, visualCol)
		if targetVisualCol < visualCol+w {
			return i
		}
		visualCol += w
	}
	return len(runes)
}

// DetectIndent picks the indent unit a freshly-opened buffer should use
// when the user presses Tab. The algorithm in priority order:
//
//  1. Walk every line; classify lines that start with whitespace as either
//     tab-indented or space-indented. Count both.
//  2. If tab-indented lines outnumber space-indented ones, return "\t".
//  3. If space-indented wins, return that many spaces (using the smallest
//     non-zero leading-space count as the indent width — that matches what
//     "infer" tools in other editors do).
//  4. With no signal, fall back to the path's extension: ".go" / Makefiles /
//     ".tsv" default to tabs (those file types either require or strongly
//     prefer tabs); everything else defaults to four spaces.
//
// The result is what the *Tab* key inserts; existing characters in the file
// are not rewritten.
func DetectIndent(lines []string, path string) string {
	tabLines := 0
	spaceLines := 0
	smallestSpaceWidth := 0 // 0 = "no space-indented line seen yet"

	for _, line := range lines {
		if line == "" {
			continue
		}
		switch line[0] {
		case '\t':
			tabLines++
		case ' ':
			spaceLines++
			n := leadingSpaces(line)
			if n > 0 && (smallestSpaceWidth == 0 || n < smallestSpaceWidth) {
				smallestSpaceWidth = n
			}
		}
	}

	if tabLines > spaceLines && tabLines > 0 {
		return "\t"
	}
	if spaceLines > 0 && smallestSpaceWidth > 0 {
		return strings.Repeat(" ", smallestSpaceWidth)
	}

	// No signal — pick a sensible default by file extension.
	return defaultIndentForPath(path)
}

// leadingSpaces counts the number of leading ' ' bytes in s, stopping at
// the first non-space byte.
func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// defaultIndentForPath returns "\t" for languages whose conventions strongly
// prefer or require tabs (Go's gofmt rewrites to tabs; Makefiles literally
// require tabs at the start of recipe lines; TSV is tab-separated by name).
// Everything else defaults to four spaces. This is consulted only when the
// file has no existing indentation to learn from — a real Python source
// with 4-space indents will be detected as "    " regardless of the path.
func defaultIndentForPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if base == "makefile" || base == "gnumakefile" {
		return "\t"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".tsv":
		return "\t"
	}
	return defaultSpaceIndent
}
