// =============================================================================
// File: internal/editor/highlight.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// Highlight tokenises src using a Chroma lexer chosen by filename (falling
// back to content-based detection, then to a plain-text lexer) and returns a
// per-line slice of styles parallel to the buffer's lines: styles[i][j] is
// the style for rune j on line i.
//
// Returning a per-rune style grid keeps the renderer simple — it just looks
// up the style for each cell it draws — at the cost of some memory.
// For files small enough to comfortably review, that's a fine trade.
func Highlight(filename, src string, t theme.Theme) [][]tcell.Style {
	return highlightSource(filename, src, t)
}

// HighlightVisible returns a style grid for the current viewport. Only visible
// rows are tokenised so keystroke cost follows terminal height, not file size.
func HighlightVisible(filename string, lines []string, startLine, height int, t theme.Theme) [][]tcell.Style {
	styles := make([][]tcell.Style, len(lines))
	if height <= 0 || startLine >= len(lines) {
		return styles
	}
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + height
	if endLine > len(lines) {
		endLine = len(lines)
	}
	visible := strings.Join(lines[startLine:endLine], "\n")
	visibleStyles := highlightSource(filename, visible, t)
	for i, row := range visibleStyles {
		lineIdx := startLine + i
		if lineIdx >= endLine || lineIdx >= len(styles) {
			break
		}
		styles[lineIdx] = row
	}
	return styles
}

// highlightSource tokenises src and returns one style row per source line.
func highlightSource(filename, src string, t theme.Theme) [][]tcell.Style {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	// Coalesce merges adjacent same-type tokens; cheaper to scan in render.
	lexer = chroma.Coalesce(lexer)

	base := tcell.StyleDefault.Background(t.BG).Foreground(t.Text)

	// Pre-allocate a styles grid sized to the source. We seed every cell
	// with the base style so untokenised runes still render readably.
	lines := strings.Split(src, "\n")
	styles := baseStyleGrid(lines, base)

	iter, err := lexer.Tokenise(nil, src)
	if err != nil {
		return styles
	}

	line, col := 0, 0
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		st := styleForToken(tok.Type, t, base)
		for _, r := range tok.Value {
			if r == '\n' {
				line++
				col = 0
				continue
			}
			if line < len(styles) && col < len(styles[line]) {
				styles[line][col] = st
			}
			col++
		}
	}
	return styles
}

// baseStyleGrid returns a correctly shaped grid pre-filled with base.
func baseStyleGrid(lines []string, base tcell.Style) [][]tcell.Style {
	styles := make([][]tcell.Style, len(lines))
	for i, ln := range lines {
		runes := []rune(ln)
		row := make([]tcell.Style, len(runes))
		for j := range row {
			row[j] = base
		}
		styles[i] = row
	}
	return styles
}

// styleForToken maps a Chroma token type to a tcell.Style using the active
// theme. We match by category first (Keyword, LiteralString, etc.) so the
// mapping stays tight across the dozens of language-specific subtypes.
func styleForToken(tt chroma.TokenType, t theme.Theme, base tcell.Style) tcell.Style {
	switch tt.Category() {
	case chroma.Keyword:
		return base.Foreground(t.SynKeyword)
	case chroma.LiteralString:
		return base.Foreground(t.SynString)
	case chroma.LiteralNumber:
		return base.Foreground(t.SynNumber)
	case chroma.Comment:
		return base.Foreground(t.SynComment).Italic(true)
	case chroma.Operator:
		return base.Foreground(t.SynOperator)
	case chroma.Punctuation:
		return base.Foreground(t.SynPunct)
	case chroma.Literal:
		return base.Foreground(t.SynConstant)
	case chroma.Name:
		switch tt {
		case chroma.NameFunction, chroma.NameFunctionMagic:
			return base.Foreground(t.SynFunction)
		case chroma.NameClass, chroma.NameNamespace:
			return base.Foreground(t.SynType)
		case chroma.NameBuiltin, chroma.NameBuiltinPseudo:
			return base.Foreground(t.SynBuiltin)
		case chroma.NameConstant:
			return base.Foreground(t.SynConstant)
		case chroma.NameVariable, chroma.NameVariableInstance,
			chroma.NameVariableClass, chroma.NameVariableGlobal,
			chroma.NameVariableAnonymous:
			return base.Foreground(t.SynVariable)
		case chroma.NameTag:
			return base.Foreground(t.SynType)
		case chroma.NameAttribute:
			return base.Foreground(t.SynVariable)
		}
		return base
	}
	return base
}
