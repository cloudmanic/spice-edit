// =============================================================================
// File: internal/editor/image.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// image.go gives Tab a lightweight read-only image viewer mode. PNG / JPEG /
// GIF (first frame only) are decoded with the Go standard library and
// rendered into the editor pane using the "half-block" technique: each
// terminal cell carries U+2580 (▀) with the top half painted in the
// foreground colour and the bottom half in the background colour, so a
// single cell encodes two vertical pixels of the source image.
//
// Resolution is capped by the terminal grid (a typical full-screen view
// is roughly 160 effective vertical pixels), but the approach has two
// big advantages over Sixel / iTerm2 / Kitty graphics:
//
//   1. It works in *every* truecolor-capable terminal, including macOS
//      Terminal where none of the image protocols are supported.
//   2. It passes through tmux without any passthrough config — the
//      output is just SGR truecolor escapes, the same colour codes the
//      editor already uses everywhere else.
//
// We use a nearest-neighbour scaler because it's small, dependency-free,
// and good enough for a preview. Bilinear / Lanczos would look slightly
// nicer but pull in golang.org/x/image; not worth it for V1.

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

// upperHalfBlock is the Unicode glyph used for half-block rendering.
// Foreground = top pixel, background = bottom pixel.
const upperHalfBlock = '▀'

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

// halfblockRender draws img into the cell rectangle (x, y, w, h),
// scaled to fit while preserving aspect ratio and centred inside the
// rectangle. Each rendered cell is one ▀ character whose foreground
// = top pixel and background = bottom pixel of the local 1×2 sample.
//
// w and h are in *cells*; the underlying pixel grid is therefore
// w wide and 2*h tall.
func halfblockRender(scr tcell.Screen, x, y, w, h int, img image.Image) {
	if img == nil || w <= 0 || h <= 0 {
		return
	}
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW == 0 || imgH == 0 {
		return
	}

	pxW, pxH := halfblockFitSize(imgW, imgH, w, h)
	if pxW < 1 || pxH < 2 {
		return
	}

	cellsW := pxW
	cellsH := pxH / 2

	// Centre the image inside the requested rectangle so a small image
	// doesn't hug the top-left corner.
	offX := x + (w-cellsW)/2
	offY := y + (h-cellsH)/2

	resized := resizeNearest(img, pxW, pxH)
	if resized == nil {
		return
	}

	for cy := 0; cy < cellsH; cy++ {
		for cx := 0; cx < cellsW; cx++ {
			top := resized.RGBAAt(cx, cy*2)
			bot := resized.RGBAAt(cx, cy*2+1)
			fg := tcell.NewRGBColor(int32(top.R), int32(top.G), int32(top.B))
			bg := tcell.NewRGBColor(int32(bot.R), int32(bot.G), int32(bot.B))
			st := tcell.StyleDefault.Foreground(fg).Background(bg)
			scr.SetContent(offX+cx, offY+cy, upperHalfBlock, nil, st)
		}
	}
}

// halfblockFitSize picks the largest (pixelW, pixelH) for an image of
// (srcW, srcH) that fits inside a (cellW, cellH) cell rectangle while
// preserving aspect ratio. The pixel grid is one pixel per horizontal
// cell and two pixels per vertical cell. Returned pxH is always even
// so we never leave a 1-pixel orphan beneath the last cell row.
func halfblockFitSize(srcW, srcH, cellW, cellH int) (int, int) {
	if srcW <= 0 || srcH <= 0 || cellW <= 0 || cellH <= 0 {
		return 0, 0
	}
	maxPxW := cellW
	maxPxH := cellH * 2

	scaleW := float64(maxPxW) / float64(srcW)
	scaleH := float64(maxPxH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	pxW := int(float64(srcW) * scale)
	pxH := int(float64(srcH) * scale)
	if pxW < 1 {
		pxW = 1
	}
	if pxH < 2 {
		pxH = 2
	}
	if pxW > maxPxW {
		pxW = maxPxW
	}
	if pxH > maxPxH {
		pxH = maxPxH
	}
	if pxH%2 != 0 {
		pxH--
	}
	return pxW, pxH
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
	halfblockRender(scr, x, y, w, h, t.Image)
	scr.HideCursor()
}
