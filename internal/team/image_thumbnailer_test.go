package team

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// synthPNG returns a deterministic png of the given dimensions with a
// simple gradient. Enough signal for the thumbnail path to exercise real
// decode + scale.
func synthPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode synth png: %v", err)
	}
	return buf.Bytes()
}

// TestGenerateThumbnail_ScalesDown confirms we actually resize a large image
// down to ThumbWidth and produce a valid JPEG.
func TestGenerateThumbnail_ScalesDown(t *testing.T) {
	src := synthPNG(t, 1600, 1200)
	var out bytes.Buffer
	dims, err := GenerateThumbnail(src, FormatPNG, &out)
	if err != nil {
		t.Fatalf("thumb: %v", err)
	}
	if dims.Width != 1600 || dims.Height != 1200 {
		t.Fatalf("source dims: got %dx%d want 1600x1200", dims.Width, dims.Height)
	}
	if out.Len() == 0 {
		t.Fatal("thumb was empty")
	}
	decoded, err := jpeg.Decode(&out)
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if decoded.Bounds().Dx() != ThumbWidth {
		t.Fatalf("thumb width: got %d want %d", decoded.Bounds().Dx(), ThumbWidth)
	}
	// 1600/1200 = 4/3, so the 480-wide thumb should be ~360 tall.
	if decoded.Bounds().Dy() < 350 || decoded.Bounds().Dy() > 370 {
		t.Fatalf("thumb height out of range: got %d want ~360", decoded.Bounds().Dy())
	}
}

// TestGenerateThumbnail_SmallerThanTargetSkipsUpscale makes sure we don't
// waste bytes upscaling a 200px source to 480px.
func TestGenerateThumbnail_SmallerThanTargetSkipsUpscale(t *testing.T) {
	src := synthPNG(t, 200, 100)
	var out bytes.Buffer
	if _, err := GenerateThumbnail(src, FormatPNG, &out); err != nil {
		t.Fatalf("thumb: %v", err)
	}
	decoded, err := jpeg.Decode(&out)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Bounds().Dx() != 200 {
		t.Fatalf("width preserved: got %d want 200", decoded.Bounds().Dx())
	}
}

// TestGenerateThumbnail_RejectsSVG — SVG must flow through the pass-through
// path, not the raster encoder.
func TestGenerateThumbnail_RejectsSVG(t *testing.T) {
	var out bytes.Buffer
	_, err := GenerateThumbnail([]byte(`<svg/>`), FormatSVG, &out)
	if err == nil {
		t.Fatal("expected error for SVG input")
	}
}
