// =============================================================================
// File: internal/editor/image.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// image.go gives Tab a lightweight read-only image viewer mode. PNG /
// JPEG / GIF (first frame only) are decoded with the Go standard
// library and rendered into the editor pane via the "chafa-lite"
// adaptive block-character matcher in blockmatch.go: each terminal
// cell carries the Unicode block glyph that best approximates a 2×2
// source quadrant, with foreground and background colours chosen to
// minimise intra-cell error.
//
// Why not Sixel / iTerm2 / Kitty graphics? Real bitmap protocols give
// higher fidelity but they don't survive the user's actual workflow
// (Ghostty rejects Sixel; zellij — and tmux without passthrough —
// strips Kitty). The block-character output is just SGR truecolor
// escapes, which every truecolor terminal renders and every
// multiplexer passes through unchanged. Half the resolution of a real
// bitmap, but it's resolution we always actually get.
//
// We use a nearest-neighbour scaler because it's small,
// dependency-free, and good enough for a preview. Bilinear / Lanczos
// would look slightly nicer but pull in golang.org/x/image; not worth
// it.

package editor

import (
	"fmt"
	"image"
	_ "image/gif"  // register decoder so image.Decode handles .gif
	_ "image/jpeg" // register decoder so image.Decode handles .jpg / .jpeg
	_ "image/png"  // register decoder so image.Decode handles .png
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// imageMode is the value Tab.Mode takes when the tab is showing an
// image instead of text. Lives here (not tab.go) so all the image-only
// state and behaviour stays close to its definition.
const imageMode = "image"

// isImageExt reports whether path's extension is one we know how to
// decode. Case-insensitive so "FOO.PNG" works the same as "foo.png".
func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// decodeImageFile opens path, decodes whatever image format the magic
// bytes advertise, and returns the image plus the format name (handy
// for the status bar). Errors are wrapped with the basename so the
// flash message is useful.
func decodeImageFile(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return img, format, nil
}

// resizeNearest produces a w×h RGBA copy of src using nearest-neighbour
// sampling. Returns nil for non-positive sizes so callers don't have
// to special-case a zero-area target.
func resizeNearest(src image.Image, w, h int) *image.RGBA {
	if w <= 0 || h <= 0 || src == nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	sw := sb.Dx()
	sh := sb.Dy()
	if sw == 0 || sh == 0 {
		return dst
	}
	for y := 0; y < h; y++ {
		sy := y * sh / h
		for x := 0; x < w; x++ {
			sx := x * sw / w
			dst.Set(x, y, src.At(sb.Min.X+sx, sb.Min.Y+sy))
		}
	}
	return dst
}

// renderImage paints the image tab. Called from Tab.Render when
// Mode == imageMode. The viewport is filled with the editor background
// colour first so blank cells around a small / non-square image still
// look themed.
func (t *Tab) renderImage(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	bgStyle := tcell.StyleDefault.Background(th.BG)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}
	blockMatchRender(scr, x, y, w, h, t.Image)
	scr.HideCursor()
}
