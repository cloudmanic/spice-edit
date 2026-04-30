// =============================================================================
// File: internal/theme/theme.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package theme defines the editor's curated color palette. The editor
// intentionally ships one opinionated dark theme — there is no runtime
// configuration, no theme file, no JSON. To restyle the editor, edit this
// file and recompile. The palette is inspired by Tokyo Night and tuned so
// the syntax colors stay legible against the chrome.
package theme

import "github.com/gdamore/tcell/v2"

// Theme bundles every color the editor renders. UI surfaces, accents, and
// syntax-highlight colors all live in one struct so that adjusting one
// element of the palette can be balanced against the others.
type Theme struct {
	// --- Surfaces ---
	BG        tcell.Color // Editor background.
	SidebarBG tcell.Color // File tree / inactive tab background, slightly darker than BG.
	StatusBG  tcell.Color // Status bar background.
	LineHL    tcell.Color // Active line highlight.

	// --- Foregrounds & accents ---
	Text       tcell.Color // Primary editor text.
	Muted      tcell.Color // Line numbers, inactive tabs, secondary UI text.
	Subtle     tcell.Color // Even more subtle (separators, hints).
	Accent     tcell.Color // Active tab accent, root label, important UI.
	AccentSoft tcell.Color // Softer accent (active line number).
	Selection  tcell.Color // Selection background.
	Modified   tcell.Color // Dirty indicator (unsaved changes).
	Error      tcell.Color // Error messages.

	// --- File tree ---
	FolderColor tcell.Color
	FileColor   tcell.Color

	// --- Syntax highlighting ---
	SynKeyword  tcell.Color
	SynString   tcell.Color
	SynNumber   tcell.Color
	SynComment  tcell.Color
	SynFunction tcell.Color
	SynType     tcell.Color
	SynBuiltin  tcell.Color
	SynVariable tcell.Color
	SynOperator tcell.Color
	SynPunct    tcell.Color
	SynConstant tcell.Color
}

// Default returns the editor's curated dark theme. It is the only theme the
// editor ships with — calling code can tweak fields on the returned value if
// it really needs to, but there is no theme-loading machinery on purpose.
func Default() Theme {
	return Theme{
		// Surfaces.
		BG:        tcell.NewRGBColor(0x1a, 0x1b, 0x26),
		SidebarBG: tcell.NewRGBColor(0x16, 0x16, 0x1e),
		StatusBG:  tcell.NewRGBColor(0x7a, 0xa2, 0xf7),
		LineHL:    tcell.NewRGBColor(0x1f, 0x20, 0x2e),

		// Foregrounds & accents.
		Text:       tcell.NewRGBColor(0xc0, 0xca, 0xf5),
		Muted:      tcell.NewRGBColor(0x56, 0x5f, 0x89),
		Subtle:     tcell.NewRGBColor(0x32, 0x34, 0x4a),
		Accent:     tcell.NewRGBColor(0x7a, 0xa2, 0xf7),
		AccentSoft: tcell.NewRGBColor(0xbb, 0x9a, 0xf7),
		Selection:  tcell.NewRGBColor(0x33, 0x46, 0x7c),
		Modified:   tcell.NewRGBColor(0xe0, 0xaf, 0x68),
		Error:      tcell.NewRGBColor(0xf7, 0x76, 0x8e),

		// Tree.
		FolderColor: tcell.NewRGBColor(0x7a, 0xa2, 0xf7),
		FileColor:   tcell.NewRGBColor(0xa9, 0xb1, 0xd6),

		// Syntax — Tokyo Night-ish.
		SynKeyword:  tcell.NewRGBColor(0xbb, 0x9a, 0xf7), // purple
		SynString:   tcell.NewRGBColor(0x9e, 0xce, 0x6a), // green
		SynNumber:   tcell.NewRGBColor(0xff, 0x9e, 0x64), // orange
		SynComment:  tcell.NewRGBColor(0x56, 0x5f, 0x89), // muted slate
		SynFunction: tcell.NewRGBColor(0x7a, 0xa2, 0xf7), // blue
		SynType:     tcell.NewRGBColor(0x2a, 0xc3, 0xde), // cyan
		SynBuiltin:  tcell.NewRGBColor(0xf7, 0x76, 0x8e), // red
		SynVariable: tcell.NewRGBColor(0xc0, 0xca, 0xf5), // text-like
		SynOperator: tcell.NewRGBColor(0x89, 0xdd, 0xff), // light cyan
		SynPunct:    tcell.NewRGBColor(0xa9, 0xb1, 0xd6), // soft text
		SynConstant: tcell.NewRGBColor(0xff, 0x9e, 0x64), // orange
	}
}
