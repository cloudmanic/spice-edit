// =============================================================================
// File: internal/theme/theme_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the theme package. The package is pure data, but we still want
// to pin down a few invariants: every color is set, and the few pairs that
// must visually contrast (BG vs Text, BG vs SidebarBG, BG vs Selection) are
// not accidentally equal. A future palette tweak that breaks one of these
// would render the editor unusable, so the tests act as a tripwire.

package theme

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDefault_AllColorsSet walks every documented field on the Theme struct
// and asserts the value is not the zero tcell.Color. A missing assignment
// in Default() would otherwise silently render that UI element invisible.
func TestDefault_AllColorsSet(t *testing.T) {
	th := Default()

	// Each entry is the human-readable field name and its color value. We
	// list explicitly rather than reflecting so a new field forces us to
	// decide whether it belongs in the contrast invariants too.
	cases := []struct {
		name  string
		color tcell.Color
	}{
		{"BG", th.BG},
		{"SidebarBG", th.SidebarBG},
		{"StatusBG", th.StatusBG},
		{"LineHL", th.LineHL},
		{"Text", th.Text},
		{"Muted", th.Muted},
		{"Subtle", th.Subtle},
		{"Accent", th.Accent},
		{"AccentSoft", th.AccentSoft},
		{"Selection", th.Selection},
		{"Modified", th.Modified},
		{"Error", th.Error},
		{"FolderColor", th.FolderColor},
		{"FileColor", th.FileColor},
		{"SynKeyword", th.SynKeyword},
		{"SynString", th.SynString},
		{"SynNumber", th.SynNumber},
		{"SynComment", th.SynComment},
		{"SynFunction", th.SynFunction},
		{"SynType", th.SynType},
		{"SynBuiltin", th.SynBuiltin},
		{"SynVariable", th.SynVariable},
		{"SynOperator", th.SynOperator},
		{"SynPunct", th.SynPunct},
		{"SynConstant", th.SynConstant},
	}

	for _, c := range cases {
		// tcell.ColorDefault is the zero/sentinel; an unset RGB color from
		// Default() would also be zero, so we treat "0" as missing.
		if c.color == 0 {
			t.Errorf("Default(): field %s is unset (zero color)", c.name)
		}
	}
}

// TestDefault_ContrastInvariants asserts the small handful of color pairs
// that must differ for the editor to be readable. If any of these collapse
// to the same color the user sees a blank panel or invisible selection.
func TestDefault_ContrastInvariants(t *testing.T) {
	th := Default()

	cases := []struct {
		name string
		a, b tcell.Color
	}{
		// Text on background — without contrast there's nothing to read.
		{"BG vs Text", th.BG, th.Text},
		// Sidebar must read as a separate panel from the editor surface.
		{"BG vs SidebarBG", th.BG, th.SidebarBG},
		// Selection block must stand out against the unselected background.
		{"Selection vs BG", th.Selection, th.BG},
	}

	for _, c := range cases {
		if c.a == c.b {
			t.Errorf("Default(): %s collide (%v == %v)", c.name, c.a, c.b)
		}
	}
}
