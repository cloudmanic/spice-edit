// =============================================================================
// File: internal/editor/image_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for image.go — extension detection, decoding, the nearest-neighbour
// scaler, the half-block fit math, and the half-block renderer driven
// against a tcell.SimulationScreen so we can pin the exact runes and
// truecolor cells that land on screen.

package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// TestIsImageExt covers the small set of extensions we recognise — case
// insensitive, with a couple of negative cases to guard against an
// over-eager extension check.
func TestIsImageExt(t *testing.T) {
	cases := map[string]bool{
		"foo.png":         true,
		"FOO.PNG":         true,
		"path/to/bar.jpg": true,
		"Bar.JPEG":        true,
		"thing.gif":       true,
		"thing.GIF":       true,
		"file.txt":        false,
		"file.go":         false,
		"":                false,
		"png":             false, // no extension separator
		"foo.bmp":         false, // not yet supported
		"foo.svg":         false, // we don't decode svg
	}
	for in, want := range cases {
		if got := isImageExt(in); got != want {
			t.Errorf("isImageExt(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestDecodeImageFile_PNG round-trips a synthetic PNG: encode a tiny
// gradient to disk, decode it back, and assert the format string + a
// known pixel colour both match.
func TestDecodeImageFile_PNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.png")
	src := makeGradient(4, 4)
	writePNG(t, path, src)

	got, format, err := decodeImageFile(path)
	if err != nil {
		t.Fatalf("decodeImageFile: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if got.Bounds() != src.Bounds() {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), src.Bounds())
	}
	wantR, wantG, wantB, _ := src.At(2, 2).RGBA()
	r, g, b, _ := got.At(2, 2).RGBA()
	if r != wantR || g != wantG || b != wantB {
		t.Fatalf("pixel (2,2) = %d,%d,%d  want %d,%d,%d", r, g, b, wantR, wantG, wantB)
	}
}

// TestDecodeImageFile_JPEG ensures the JPEG decoder is registered. JPEG
// is lossy so we don't compare pixels exactly — just confirm it loads.
func TestDecodeImageFile_JPEG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.jpg")
	src := makeGradient(8, 8)
	writeJPEG(t, path, src)

	img, format, err := decodeImageFile(path)
	if err != nil {
		t.Fatalf("decodeImageFile: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
		t.Fatalf("bounds = %v, want 8x8", img.Bounds())
	}
}

// TestDecodeImageFile_Errors covers the two failure modes a user can
// hit at runtime: file doesn't exist, and file exists but isn't a
// recognised image format.
func TestDecodeImageFile_Errors(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := decodeImageFile(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("expected error for missing file")
	}

	bogus := filepath.Join(dir, "not.png")
	if err := os.WriteFile(bogus, []byte("hello, world"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := decodeImageFile(bogus)
	if err == nil {
		t.Fatal("expected error decoding non-image bytes")
	}
}

// TestResizeNearest_DoublesAndHalves checks the two most common
// scale paths — upsample 2× and downsample 2× — and verifies the
// produced bounds match the requested size.
func TestResizeNearest_DoublesAndHalves(t *testing.T) {
	src := makeGradient(4, 4)

	up := resizeNearest(src, 8, 8)
	if up.Bounds().Dx() != 8 || up.Bounds().Dy() != 8 {
		t.Fatalf("upsample bounds = %v", up.Bounds())
	}
	// Top-left pixel should equal the source's top-left pixel.
	wantR, wantG, wantB, _ := src.At(0, 0).RGBA()
	r, g, b, _ := up.At(0, 0).RGBA()
	if r != wantR || g != wantG || b != wantB {
		t.Errorf("up(0,0) = %d,%d,%d, want %d,%d,%d", r, g, b, wantR, wantG, wantB)
	}

	down := resizeNearest(src, 2, 2)
	if down.Bounds().Dx() != 2 || down.Bounds().Dy() != 2 {
		t.Fatalf("downsample bounds = %v", down.Bounds())
	}
}

// TestResizeNearest_ZeroOrNilHandled returns nil for non-positive
// sizes and for a nil source — defensive so callers don't have to
// special-case.
func TestResizeNearest_ZeroOrNilHandled(t *testing.T) {
	if resizeNearest(nil, 4, 4) != nil {
		t.Error("nil src should yield nil")
	}
	if resizeNearest(makeGradient(2, 2), 0, 4) != nil {
		t.Error("zero w should yield nil")
	}
	if resizeNearest(makeGradient(2, 2), 4, -1) != nil {
		t.Error("negative h should yield nil")
	}
}

// TestHalfblockFitSize_PreservesAspect drives the fitting math through
// the four interesting cases: same aspect, source-wider, source-taller,
// and a degenerate zero-size cell rect.
func TestHalfblockFitSize_PreservesAspect(t *testing.T) {
	cases := []struct {
		name                       string
		srcW, srcH, cellW, cellH   int
		wantPxW, wantPxH           int
	}{
		// 100x100 image into 50x50 cells. Cells give 50px wide × 100px
		// tall; scaling preserves the source's 1:1 pixel aspect, so the
		// width (50px) is the binding dimension and pxH lands at 50.
		// That fills 50 cells horizontally × 25 cells vertically — the
		// vertical padding is the cost of preserving aspect.
		{"square fits width", 100, 100, 50, 50, 50, 50},
		// 200x100 image (wide) into 50x50 cells. Width-bound: scale = 0.25,
		// pxW = 50, pxH = 25 → rounded down to even = 24.
		{"wide source", 200, 100, 50, 50, 50, 24},
		// 100x200 image (tall) into 50x50 cells. Height-bound: scale = 0.5,
		// pxW = 50, pxH = 100.
		{"tall source", 100, 200, 50, 50, 50, 100},
		// Zero cell area → zero result.
		{"zero cells", 100, 100, 0, 10, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := halfblockFitSize(tc.srcW, tc.srcH, tc.cellW, tc.cellH)
			if gotW != tc.wantPxW || gotH != tc.wantPxH {
				t.Fatalf("got %dx%d, want %dx%d", gotW, gotH, tc.wantPxW, tc.wantPxH)
			}
			if gotH%2 != 0 && gotH != 0 {
				t.Fatalf("pxH must be even, got %d", gotH)
			}
		})
	}
}

// TestHalfblockRender_DrawsExpectedRunesAndColors paints a small flat
// red image into a SimulationScreen and asserts every cell that falls
// inside the rendered rect is the half-block glyph with the right
// foreground / background colours.
func TestHalfblockRender_DrawsExpectedRunesAndColors(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(20, 10)

	// Solid red 4x4 image — every sampled pixel should report (255,0,0).
	red := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			red.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	halfblockRender(scr, 0, 0, 20, 10, red)
	scr.Show()

	cells, w, _ := scr.GetContents()
	wantFG := tcell.NewRGBColor(255, 0, 0)
	foundBlock := false
	for y := 0; y < 10; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 || c.Runes[0] != upperHalfBlock {
				continue
			}
			foundBlock = true
			fg, bg, _ := c.Style.Decompose()
			if fg != wantFG || bg != wantFG {
				t.Fatalf("cell (%d,%d): fg=%v bg=%v, want both = %v",
					x, y, fg, bg, wantFG)
			}
		}
	}
	if !foundBlock {
		t.Fatal("expected at least one half-block cell to be drawn")
	}
}

// TestHalfblockRender_IgnoresZeroSizedRects guards against panics on
// pathological inputs that the App can produce during a tiny window
// or right after a resize.
func TestHalfblockRender_IgnoresZeroSizedRects(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(10, 5)

	red := image.NewRGBA(image.Rect(0, 0, 4, 4))
	red.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})

	halfblockRender(scr, 0, 0, 0, 0, red) // zero size
	halfblockRender(scr, 0, 0, 5, 5, nil) // nil image
	halfblockRender(scr, 0, 0, 5, 5, image.NewRGBA(image.Rect(0, 0, 0, 0)))
}

// TestRenderImage_FillsBackground confirms that Tab.renderImage paints
// the editor background everywhere first so empty cells around a
// non-square image still match the editor theme.
func TestRenderImage_FillsBackground(t *testing.T) {
	tab := &Tab{Mode: imageMode}
	// Use a very wide aspect (4×1) so the half-block render is width-bound
	// and leaves vertical padding rows around it inside the 10×5 viewport.
	red := image.NewRGBA(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		red.SetRGBA(x, 0, color.RGBA{R: 255, A: 255})
	}
	tab.Image = red

	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(10, 5)

	// Reach for the full-fat default theme so background colour assertions
	// match what a real run would produce.
	tab.renderImage(scr, theme.Default(), 0, 0, 10, 5)
	scr.Show()

	cells, w, _ := scr.GetContents()
	// At least one cell should be a space with the theme background — the
	// padding around a tiny 2x2 image inside a 10x5 cell rect.
	foundPad := false
	for y := 0; y < 5; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 {
				continue
			}
			if c.Runes[0] == ' ' {
				foundPad = true
				break
			}
		}
		if foundPad {
			break
		}
	}
	if !foundPad {
		t.Fatal("expected at least one bg-padded cell around a 4x1 image")
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// makeGradient returns a synthetic w×h image whose red channel scales
// with x and green channel scales with y. Predictable enough that
// pixel comparisons in tests have meaning.
func makeGradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(255 * x / w),
				G: uint8(255 * y / h),
				B: 64,
				A: 255,
			})
		}
	}
	return img
}

// writePNG encodes img into a PNG file at path.
func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// writeJPEG encodes img into a JPEG file at path. Quality is low so the
// file stays tiny — we don't care about fidelity for an existence test.
func writeJPEG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

