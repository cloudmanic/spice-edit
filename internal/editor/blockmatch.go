// =============================================================================
// File: internal/editor/blockmatch.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// blockmatch.go is a small "chafa-lite" image renderer. Where image.go's
// half-block path packs two vertical pixels into every cell, this one
// packs *four* — a 2×2 sub-cell grid — and chooses, per cell, the
// Unicode block glyph that best approximates the source pixels.
//
// Pipeline per cell:
//   1. Sample the four source pixels for that cell's 2×2 quadrant.
//   2. For every candidate glyph (the 16-entry blockSymbols table),
//      partition the four pixels into "foreground" and "background"
//      sets according to the glyph's coverage mask, take the mean of
//      each set, and score the glyph by the sum of squared deviations
//      from those means (intra-partition variance).
//   3. Emit the lowest-error glyph with its derived fg / bg colours.
//
// The symbol set is restricted to glyphs that are reliably present in
// every monospace font we care about — space, the four quadrants
// (▘▝▖▗), the four half blocks (▀▄▌▐), the two diagonal pairs (▞▚),
// the four three-quadrant glyphs (▛▜▟▙) and the full block (█).
// Sextants and octants would buy more density but their font support
// on stock macOS Terminal / iTerm2 / Ghostty is patchy enough that
// we skip them — tofu cells would be worse than lower resolution.
//
// 16 symbols × 4 pixels per cell is trivial work; even a 200×100-cell
// image is ~3M floating-point ops and renders in well under a frame.

package editor

import (
	"image"
	"image/color"
	"math"

	"github.com/gdamore/tcell/v2"
)

// blockSymbol pairs a Unicode glyph with the set of sub-cells it
// "occupies" — that is, paints in the foreground colour. Sub-cells are
// numbered: bit 0 = TL, bit 1 = TR, bit 2 = BL, bit 3 = BR.
type blockSymbol struct {
	glyph rune
	mask  uint8
}

// blockSymbols is the universal block-character vocabulary the matcher
// chooses from. Order matters only for tie-breaking: the first entry
// that achieves the minimum error wins, so a perfectly flat cell picks
// space (mask = 0) and renders as a solid-coloured background — the
// cheapest possible cell to draw.
var blockSymbols = [...]blockSymbol{
	{' ', 0b0000}, // empty cell — bg fills the whole square
	{'▘', 0b0001}, // TL only
	{'▝', 0b0010}, // TR only
	{'▖', 0b0100}, // BL only
	{'▗', 0b1000}, // BR only
	{'▀', 0b0011}, // top half (TL + TR)
	{'▄', 0b1100}, // bottom half (BL + BR)
	{'▌', 0b0101}, // left half (TL + BL)
	{'▐', 0b1010}, // right half (TR + BR)
	{'▞', 0b0110}, // diagonal TR + BL
	{'▚', 0b1001}, // diagonal TL + BR
	{'▛', 0b0111}, // three corners (TL + TR + BL)
	{'▜', 0b1011}, // three corners (TL + TR + BR)
	{'▟', 0b1110}, // three corners (TR + BL + BR)
	{'▙', 0b1101}, // three corners (TL + BL + BR)
	{'█', 0b1111}, // full block — fg fills the whole square
}

// matchBlockCell returns the best (glyph, foreground, background) for
// the four source pixels of one cell. p is in TL, TR, BL, BR order.
//
// "Best" = lowest sum of squared RGB deviations from the per-partition
// means. RGB Euclidean is not perceptually perfect (a Lab-space
// distance would be slightly nicer) but it's cheap, dependency-free,
// and visually indistinguishable from Lab on most natural images at
// this resolution.
func matchBlockCell(p [4]color.RGBA) (rune, tcell.Color, tcell.Color) {
	bestErr := math.Inf(1)
	bestGlyph := ' '
	var bestFG, bestBG color.RGBA

	for _, sym := range blockSymbols {
		fgR, fgG, fgB, fgN := sumPartition(p, sym.mask, true)
		bgR, bgG, bgB, bgN := sumPartition(p, sym.mask, false)

		var fgMR, fgMG, fgMB float64
		if fgN > 0 {
			fgMR = fgR / float64(fgN)
			fgMG = fgG / float64(fgN)
			fgMB = fgB / float64(fgN)
		}
		var bgMR, bgMG, bgMB float64
		if bgN > 0 {
			bgMR = bgR / float64(bgN)
			bgMG = bgG / float64(bgN)
			bgMB = bgB / float64(bgN)
		}

		err := partitionError(p, sym.mask, fgMR, fgMG, fgMB, bgMR, bgMG, bgMB)
		if err < bestErr {
			bestErr = err
			bestGlyph = sym.glyph
			bestFG = color.RGBA{R: roundClamp(fgMR), G: roundClamp(fgMG), B: roundClamp(fgMB), A: 255}
			bestBG = color.RGBA{R: roundClamp(bgMR), G: roundClamp(bgMG), B: roundClamp(bgMB), A: 255}
		}
	}

	fg := tcell.NewRGBColor(int32(bestFG.R), int32(bestFG.G), int32(bestFG.B))
	bg := tcell.NewRGBColor(int32(bestBG.R), int32(bestBG.G), int32(bestBG.B))
	return bestGlyph, fg, bg
}

// sumPartition returns the channel sums and pixel count for the
// foreground (foreground = true) or background (foreground = false)
// partition defined by mask.
func sumPartition(p [4]color.RGBA, mask uint8, foreground bool) (float64, float64, float64, int) {
	var r, g, b float64
	var n int
	for i := 0; i < 4; i++ {
		bit := mask&(1<<i) != 0
		if bit != foreground {
			continue
		}
		r += float64(p[i].R)
		g += float64(p[i].G)
		b += float64(p[i].B)
		n++
	}
	return r, g, b, n
}

// partitionError sums squared RGB deviations from the partition means.
// The bit-set side is compared against (fgR, fgG, fgB); the bit-clear
// side against (bgR, bgG, bgB).
func partitionError(p [4]color.RGBA, mask uint8, fgR, fgG, fgB, bgR, bgG, bgB float64) float64 {
	var err float64
	for i := 0; i < 4; i++ {
		var refR, refG, refB float64
		if mask&(1<<i) != 0 {
			refR, refG, refB = fgR, fgG, fgB
		} else {
			refR, refG, refB = bgR, bgG, bgB
		}
		dr := float64(p[i].R) - refR
		dg := float64(p[i].G) - refG
		db := float64(p[i].B) - refB
		err += dr*dr + dg*dg + db*db
	}
	return err
}

// roundClamp converts a float channel back to a uint8, clamping to
// [0, 255]. Means of uint8 inputs always land in-range, but we still
// round-half-up so the colour we emit is the closest representable
// approximation.
func roundClamp(v float64) uint8 {
	r := math.Round(v)
	if r < 0 {
		return 0
	}
	if r > 255 {
		return 255
	}
	return uint8(r)
}

// blockMatchRender draws img into the cell rectangle (x, y, w, h),
// scaled to fit while preserving aspect ratio and centred inside the
// rectangle. Each rendered cell carries one of blockSymbols' glyphs
// with fg / bg chosen to minimise intra-partition variance over the
// cell's 2×2 source quadrant.
//
// w and h are in cells; the underlying pixel grid is 2*w wide and 2*h
// tall (vs the half-block renderer's w wide and 2*h tall — twice the
// horizontal resolution).
func blockMatchRender(scr tcell.Screen, x, y, w, h int, img image.Image) {
	if img == nil || w <= 0 || h <= 0 {
		return
	}
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW == 0 || imgH == 0 {
		return
	}

	pxW, pxH := blockMatchFitSize(imgW, imgH, w, h)
	if pxW < 2 || pxH < 2 {
		return
	}

	cellsW := pxW / 2
	cellsH := pxH / 2

	offX := x + (w-cellsW)/2
	offY := y + (h-cellsH)/2

	resized := resizeNearest(img, pxW, pxH)
	if resized == nil {
		return
	}

	for cy := 0; cy < cellsH; cy++ {
		for cx := 0; cx < cellsW; cx++ {
			tl := resized.RGBAAt(cx*2, cy*2)
			tr := resized.RGBAAt(cx*2+1, cy*2)
			bl := resized.RGBAAt(cx*2, cy*2+1)
			br := resized.RGBAAt(cx*2+1, cy*2+1)

			glyph, fg, bg := matchBlockCell([4]color.RGBA{tl, tr, bl, br})
			st := tcell.StyleDefault.Foreground(fg).Background(bg)
			scr.SetContent(offX+cx, offY+cy, glyph, nil, st)
		}
	}
}

// blockMatchFitSize picks the largest (pxW, pxH) for an image of
// (srcW, srcH) that fits inside a (cellW, cellH) cell rectangle while
// preserving aspect ratio. Each cell renders as a 2×2 sub-pixel grid
// whose sub-pixels are roughly 1:2 on screen (half a cell wide, a full
// cell-width tall), so to keep the *image's* true 1:1 source pixels
// looking square we want pxW : pxH = 2 * imgW : imgH.
//
// Two constraints bound the result: pxW ≤ 2*cellW (one source pixel
// per sub-cell column) and pxH ≤ 2*cellH (one source pixel per
// sub-cell row). Returned pxW and pxH are both even so the per-cell
// 2×2 sample never has an orphan column or row.
func blockMatchFitSize(srcW, srcH, cellW, cellH int) (int, int) {
	if srcW <= 0 || srcH <= 0 || cellW <= 0 || cellH <= 0 {
		return 0, 0
	}
	// Scale s such that pxW = 2*srcW*s and pxH = srcH*s — the *2 on
	// width is what compensates for sub-pixels being 2× taller than
	// wide on screen.
	scaleW := float64(cellW) / float64(srcW)
	scaleH := float64(cellH*2) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	pxW := int(2 * float64(srcW) * scale)
	pxH := int(float64(srcH) * scale)
	if pxW < 2 {
		pxW = 2
	}
	if pxH < 2 {
		pxH = 2
	}
	if pxW > cellW*2 {
		pxW = cellW * 2
	}
	if pxH > cellH*2 {
		pxH = cellH * 2
	}
	if pxW%2 != 0 {
		pxW--
	}
	if pxH%2 != 0 {
		pxH--
	}
	return pxW, pxH
}
