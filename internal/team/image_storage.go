package team

// image_storage.go handles the content-addressed layout + magic-byte
// validation for wiki image assets. Images live at
//
//	team/assets/{yyyymm}/{sha256[:12]}-{safeName}.{ext}
//	team/assets/{yyyymm}/thumbs/{sha256[:12]}.jpg (or .webp passthrough for svg)
//
// Content-addressed naming gives us three properties we would otherwise have
// to bolt on:
//   - duplicate uploads dedupe by sha
//   - the path is stable, so markdown embeds never stale
//   - stripping the user-supplied portion is trivial (just the prefix)
//
// Everything lives under team/assets/ so the same validateArticlePath checks
// that guard markdown writes continue to apply for cross-team reads without
// a separate traversal check.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// ErrImageTooLarge is returned when an uploaded image exceeds the configured
// size ceiling. Default is DefaultImageMaxMB; override via WUPHF_IMAGE_MAX_MB.
var ErrImageTooLarge = errors.New("image: file exceeds configured size limit")

// ErrImageUnsupportedFormat is returned when the magic bytes don't match any
// of the accepted raster or vector formats.
var ErrImageUnsupportedFormat = errors.New("image: unsupported format — allowed: png, jpeg, webp, gif, svg")

// ErrImageAssetPathInvalid is returned by validateAssetPath for anything
// outside team/assets/.
var ErrImageAssetPathInvalid = errors.New("image: asset path must be within team/assets/")

// DefaultImageMaxMB is the ceiling enforced per upload. 10 MiB is enough for
// high-res screenshots and diagrams without letting agents accidentally
// commit multi-megabyte photos into the git history.
const DefaultImageMaxMB = 10

// ImageFormat enumerates the formats we accept. Kept internal — callers work
// in mime-like strings via DetectImageFormat.
type ImageFormat string

const (
	FormatPNG  ImageFormat = "png"
	FormatJPEG ImageFormat = "jpeg"
	FormatWebP ImageFormat = "webp"
	FormatGIF  ImageFormat = "gif"
	FormatSVG  ImageFormat = "svg"
)

// MIME returns the canonical Content-Type for the format. SVG always carries
// a locked CSP via the serve path; this function does not know about that.
func (f ImageFormat) MIME() string {
	switch f {
	case FormatPNG:
		return "image/png"
	case FormatJPEG:
		return "image/jpeg"
	case FormatWebP:
		return "image/webp"
	case FormatGIF:
		return "image/gif"
	case FormatSVG:
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

// Ext returns the filesystem extension for the format, without the dot.
func (f ImageFormat) Ext() string {
	if f == FormatJPEG {
		return "jpg"
	}
	return string(f)
}

// IsRaster reports whether the format carries bitmap data we can decode and
// thumbnail. SVG returns false because it passes through unchanged.
func (f ImageFormat) IsRaster() bool {
	return f == FormatPNG || f == FormatJPEG || f == FormatWebP || f == FormatGIF
}

// ResolveImageMaxBytes returns the configured upload ceiling in bytes. Reads
// WUPHF_IMAGE_MAX_MB at call time so tests can set it via t.Setenv without
// rebuilding the worker. Falls back to DefaultImageMaxMB on parse failure
// rather than panicking — a typo should not disable uploads.
func ResolveImageMaxBytes() int64 {
	mb := DefaultImageMaxMB
	if raw := strings.TrimSpace(os.Getenv("WUPHF_IMAGE_MAX_MB")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			mb = v
		}
	}
	return int64(mb) * 1024 * 1024
}

// ResolveAutoAltText reports whether uploads without an explicit alt should
// auto-request vision alt-text synthesis. Default true — opt-out via
// WUPHF_IMAGE_AUTO_ALT=false. Empty / unset means true.
func ResolveAutoAltText() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("WUPHF_IMAGE_AUTO_ALT")))
	if raw == "" {
		return true
	}
	switch raw {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// DetectImageFormat inspects the first ~32 bytes and returns the identified
// format. Deliberately does NOT trust the uploaded filename extension —
// magic-byte matching is the security-relevant check against
// extension-swap attacks.
func DetectImageFormat(data []byte) (ImageFormat, error) {
	if len(data) < 4 {
		return "", ErrImageUnsupportedFormat
	}
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		return FormatPNG, nil
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return FormatJPEG, nil
	case len(data) >= 12 &&
		bytes.Equal(data[0:4], []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP")):
		return FormatWebP, nil
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return FormatGIF, nil
	}
	if looksLikeSVG(data) {
		return FormatSVG, nil
	}
	return "", ErrImageUnsupportedFormat
}

// looksLikeSVG scans the first ~1 KiB for an <svg root element. This is a
// deliberately loose match — SVG has no fixed magic number and commonly
// starts with an XML declaration, a doctype, or whitespace. We only accept
// documents that contain "<svg" near the head; further sanitization happens
// at serve time via CSP.
func looksLikeSVG(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	lower := bytes.ToLower(head)
	if !bytes.Contains(lower, []byte("<svg")) {
		return false
	}
	trimmed := bytes.TrimSpace(lower)
	// Reject obvious HTML/embedded-SVG wrappers that could exfiltrate via
	// <script> elsewhere. We still set CSP on serve, but early rejection is
	// cheaper.
	if bytes.Contains(lower, []byte("<!doctype html")) {
		return false
	}
	// Require the first recognizable token to be either <?xml, <!DOCTYPE
	// svg, or <svg itself.
	if bytes.HasPrefix(trimmed, []byte("<?xml")) ||
		bytes.HasPrefix(trimmed, []byte("<!doctype svg")) ||
		bytes.HasPrefix(trimmed, []byte("<svg")) {
		return true
	}
	return false
}

// ImageAssetsDir returns the on-disk root for image assets. Lives under the
// wiki repo so everything stays git-native and backed up by BackupMirror.
func ImageAssetsDir() string {
	return filepath.Join(WikiRootDir(), "team", "assets")
}

// assetRelPathForHash assembles the canonical relative path for an image
// identified by its sha256. safeName is the sanitized user-visible name
// suffix — if it is empty we fall back to the short hash.
func assetRelPathForHash(sum []byte, uploadedName string, format ImageFormat, now time.Time) (relPath string, short string) {
	short = hex.EncodeToString(sum)[:12]
	safe := sanitizeFilename(uploadedName)
	if safe == "" {
		safe = short
	}
	name := fmt.Sprintf("%s-%s.%s", short, safe, format.Ext())
	yyyymm := now.UTC().Format("200601")
	rel := filepath.ToSlash(filepath.Join("team", "assets", yyyymm, name))
	return rel, short
}

// thumbRelPathForHash returns the sibling thumb path for a given raster
// asset hash. SVGs do not go through this helper — they serve unchanged.
func thumbRelPathForHash(sum []byte, yyyymm string) string {
	short := hex.EncodeToString(sum)[:12]
	return filepath.ToSlash(filepath.Join("team", "assets", yyyymm, "thumbs", short+".jpg"))
}

// altSidecarRelPath returns the sibling vision-alt-text path.
func altSidecarRelPath(assetRelPath string) string {
	ext := filepath.Ext(assetRelPath)
	return strings.TrimSuffix(assetRelPath, ext) + ".alt.md"
}

// sanitizeFilename strips directory separators and non-basic characters from
// an uploaded filename, collapsing whitespace to '-'. The result is always
// safe to concatenate into a path segment without further escaping.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Strip any directory portion.
	name = filepath.Base(filepath.ToSlash(name))
	// Drop the extension — we re-derive it from the detected format.
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		name = name[:dot]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// hashBytes returns the sha256 of the payload. Separated so callers can keep
// the hash alongside the detected format without re-reading.
func hashBytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// validateAssetPath rejects anything outside team/assets/ or containing ..
// segments. Keeps the same shape as validateArticlePath but scoped to the
// assets subtree — images are NOT wiki markdown and do not live under team/
// alongside articles.
func validateAssetPath(relPath string) error {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return fmt.Errorf("image: asset path is required")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("image: asset path must be relative; got %q", relPath)
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if clean != filepath.ToSlash(relPath) {
		// filepath.Clean collapsed ../, duplicate separators, or .. — reject
		// rather than silently accept the cleaned path.
		return fmt.Errorf("%w: got %q", ErrImageAssetPathInvalid, relPath)
	}
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
		return fmt.Errorf("%w: path traversal in %q", ErrImageAssetPathInvalid, relPath)
	}
	if !strings.HasPrefix(clean, "team/assets/") {
		return fmt.Errorf("%w: got %q", ErrImageAssetPathInvalid, relPath)
	}
	return nil
}

// assetFullPath resolves a validated asset relative path to its on-disk
// absolute location. Caller MUST validate first.
func assetFullPath(relPath string) string {
	return filepath.Join(WikiRootDir(), relPath)
}

// ImageUploadResult is the response envelope for a successful upload.
type ImageUploadResult struct {
	AssetPath string `json:"asset_path"`
	ThumbPath string `json:"thumb_path,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	SHA256    string `json:"sha256"`
	SizeBytes int    `json:"size_bytes"`
	Format    string `json:"format"`
	CommitSHA string `json:"commit_sha,omitempty"`
	AltPath   string `json:"alt_path,omitempty"`
}

// resolveWikiRoot is a small indirection so tests can override the wiki root
// without monkey-patching config. Currently unused by production but kept for
// symmetry with the rest of the package.
func resolveWikiRoot() string {
	if home := strings.TrimSpace(config.RuntimeHomeDir()); home != "" {
		return filepath.Join(home, ".wuphf", "wiki")
	}
	return filepath.Join(".wuphf", "wiki")
}
