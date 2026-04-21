package team

// image_thumbnailer.go decodes raster image payloads and writes a 480px-wide
// JPEG thumbnail alongside the source asset.
//
// Format tradeoffs
// ================
//
// The original v1.3 spec called for webp thumbnails. Go's standard library
// plus golang.org/x/image/webp is decode-only — there is no pure-Go webp
// encoder, and the CGO-backed chai2010/webp bindings would cross a hard
// "pure-Go build" line for WUPHF's npm-shipped binaries. We settled on JPEG
// thumbnails instead because:
//
//   - stdlib image/jpeg is deterministic and CGO-free
//   - quality 80 at 480px wide produces ~30-60 KiB files, comparable to webp
//   - lightbox lookups always hit the full-resolution original anyway, so
//     the lossy thumbnail never regresses the display path
//
// SVG does not flow through this file — it passes through unchanged because
// the browser rasterizes vectors on demand.

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp decoder
)

// ThumbWidth is the fixed horizontal resolution for thumbnails. Kept small
// so the bookshelf / article listing stays snappy on dial-up and on slow
// 4G connections.
const ThumbWidth = 480

// ThumbJPEGQuality is the JPEG encoder quality. 80 is the sweet spot where
// visible artifacts are rare and file sizes stay well under 100 KiB.
const ThumbJPEGQuality = 80

// ImageDimensions captures the decoded width/height so callers can emit
// width/height attributes on <img> tags (prevents CLS on load).
type ImageDimensions struct {
	Width  int
	Height int
}

// decodeRaster decodes the payload using the appropriate stdlib decoder.
// Returns the decoded image + the dimensions as metadata. webp requires the
// x/image/webp side-effect import above to register with image.Decode.
func decodeRaster(data []byte, format ImageFormat) (image.Image, ImageDimensions, error) {
	r := bytes.NewReader(data)
	switch format {
	case FormatPNG:
		img, err := png.Decode(r)
		if err != nil {
			return nil, ImageDimensions{}, fmt.Errorf("decode png: %w", err)
		}
		return img, ImageDimensions{Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}, nil
	case FormatJPEG:
		img, err := jpeg.Decode(r)
		if err != nil {
			return nil, ImageDimensions{}, fmt.Errorf("decode jpeg: %w", err)
		}
		return img, ImageDimensions{Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}, nil
	case FormatGIF:
		img, err := gif.Decode(r)
		if err != nil {
			return nil, ImageDimensions{}, fmt.Errorf("decode gif: %w", err)
		}
		return img, ImageDimensions{Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}, nil
	case FormatWebP:
		img, _, err := image.Decode(r)
		if err != nil {
			return nil, ImageDimensions{}, fmt.Errorf("decode webp: %w", err)
		}
		return img, ImageDimensions{Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}, nil
	}
	return nil, ImageDimensions{}, fmt.Errorf("decodeRaster: unsupported format %q", format)
}

// GenerateThumbnail decodes the payload, scales it to ThumbWidth preserving
// aspect ratio (skipping upscale for already-small images), and encodes as
// JPEG into out. Returns the source image dimensions so the caller can cache
// them for the manifest.
//
// SVGs are not handled here — callers should detect SVG upstream and serve
// the source file as its own thumbnail.
func GenerateThumbnail(data []byte, format ImageFormat, out io.Writer) (ImageDimensions, error) {
	if !format.IsRaster() {
		return ImageDimensions{}, fmt.Errorf("thumbnailer: non-raster format %q", format)
	}
	src, dims, err := decodeRaster(data, format)
	if err != nil {
		return dims, err
	}
	dst := scaleTo(src, ThumbWidth)
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: ThumbJPEGQuality}); err != nil {
		return dims, fmt.Errorf("encode thumb: %w", err)
	}
	return dims, nil
}

// scaleTo returns a copy of src scaled to targetWidth preserving aspect
// ratio. Images already narrower than targetWidth are returned unchanged —
// upscaling a 320px screenshot to 480px wastes bytes without improving the
// browser display.
func scaleTo(src image.Image, targetWidth int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= targetWidth {
		return src
	}
	// Preserve aspect ratio with a floor of 1 to avoid zero-height outputs
	// on pathological panoramas.
	targetH := int(float64(srcH) * float64(targetWidth) / float64(srcW))
	if targetH < 1 {
		targetH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	return dst
}
