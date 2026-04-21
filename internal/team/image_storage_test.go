package team

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// TestDetectImageFormat exercises the magic-byte path for every accepted
// format plus a few rejects. Extension-swap is the main risk — we make sure
// the detector trusts the header only.
func TestDetectImageFormat(t *testing.T) {
	t.Parallel()
	// Minimal but real headers. Enough to match the magic, nothing beyond.
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	webpHeader := append([]byte("RIFF\x00\x00\x00\x00"), []byte("WEBP")...)
	gifHeader := []byte("GIF89a\x00\x00\x00\x00")
	svgBody := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>`)
	heicHeader := []byte{0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70, 0x68, 0x65, 0x69, 0x63}
	htmlSvg := []byte(`<!DOCTYPE html><html><body><svg>...</svg></body></html>`)

	cases := []struct {
		name    string
		data    []byte
		want    ImageFormat
		wantErr bool
	}{
		{"png", pngHeader, FormatPNG, false},
		{"jpeg", jpegHeader, FormatJPEG, false},
		{"webp", webpHeader, FormatWebP, false},
		{"gif", gifHeader, FormatGIF, false},
		{"svg", svgBody, FormatSVG, false},
		{"heic rejected", heicHeader, "", true},
		{"html wrapped svg rejected", htmlSvg, "", true},
		{"too short", []byte{0xFF}, "", true},
		{"random garbage", []byte("just some text here"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DetectImageFormat(tc.data)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got format %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q got %q", tc.want, got)
			}
		})
	}
}

// TestSanitizeFilename ensures we never let attacker-controlled path segments
// or dangerous characters into the asset name suffix.
func TestSanitizeFilename(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Clean Name.png":        "Clean-Name",
		"../../etc/passwd":      "passwd",
		"weird<>name!@#.jpg":    "weirdname",
		"":                      "",
		"a.b.c.png":             "a-b-c",
		"   spaces   ":          "spaces",
		"rónaldo":               "rnaldo", // unicode stripped
		strings.Repeat("x", 60): strings.Repeat("x", 48),
	}
	for in, want := range cases {
		got := sanitizeFilename(in)
		if got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

// TestValidateAssetPath covers traversal + required-prefix rejection.
func TestValidateAssetPath(t *testing.T) {
	t.Parallel()
	okCases := []string{
		"team/assets/202604/abcdef123456-name.png",
		"team/assets/202604/thumbs/abcdef123456.jpg",
	}
	badCases := []string{
		"",
		"/absolute/path",
		"team/assets/../../../etc/passwd",
		"not-under-assets/file.png",
		"team/people/nazz.md",
		"team/assets/../../escape",
		"team/assets//double",
	}
	for _, c := range okCases {
		if err := validateAssetPath(c); err != nil {
			t.Errorf("expected %q to validate, got %v", c, err)
		}
	}
	for _, c := range badCases {
		if err := validateAssetPath(c); err == nil {
			t.Errorf("expected %q to fail", c)
		}
	}
}

// TestAssetRelPathForHash confirms the content-addressed scheme dedupes by
// sha256 prefix and includes a sanitized suffix.
func TestAssetRelPathForHash(t *testing.T) {
	t.Parallel()
	payload := []byte("hello world")
	sum := sha256.Sum256(payload)
	now, _ := time.Parse(time.RFC3339, "2026-04-21T00:00:00Z")

	p1, short := assetRelPathForHash(sum[:], "Screenshot At Noon.png", FormatPNG, now)
	if short != hex.EncodeToString(sum[:])[:12] {
		t.Fatalf("unexpected short hash %q", short)
	}
	if !strings.Contains(p1, "team/assets/202604/") {
		t.Fatalf("expected path to contain yyyymm, got %q", p1)
	}
	if !strings.HasSuffix(p1, ".png") {
		t.Fatalf("expected .png suffix, got %q", p1)
	}
	if !strings.Contains(p1, short) {
		t.Fatalf("expected short hash in path, got %q", p1)
	}

	// Same payload + different original filename must produce the same
	// hash but distinct asset paths (so distinct uploads keep their user-
	// visible labels). We only guarantee short-hash prefix match.
	p2, _ := assetRelPathForHash(sum[:], "diagram.png", FormatPNG, now)
	if !strings.HasPrefix(p2, "team/assets/202604/"+short) {
		t.Fatalf("expected same short hash prefix in %q", p2)
	}
}

// TestResolveImageMaxBytes verifies env override + default fallback.
func TestResolveImageMaxBytes(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_MAX_MB", "")
	if got := ResolveImageMaxBytes(); got != DefaultImageMaxMB*1024*1024 {
		t.Fatalf("default: got %d want %d", got, DefaultImageMaxMB*1024*1024)
	}
	t.Setenv("WUPHF_IMAGE_MAX_MB", "5")
	if got := ResolveImageMaxBytes(); got != 5*1024*1024 {
		t.Fatalf("override: got %d want %d", got, 5*1024*1024)
	}
	t.Setenv("WUPHF_IMAGE_MAX_MB", "bogus")
	if got := ResolveImageMaxBytes(); got != DefaultImageMaxMB*1024*1024 {
		t.Fatalf("bogus: got %d want default %d", got, DefaultImageMaxMB*1024*1024)
	}
}

// TestResolveAutoAltText covers the env-var parsing for the opt-out toggle.
func TestResolveAutoAltText(t *testing.T) {
	t.Setenv("WUPHF_IMAGE_AUTO_ALT", "")
	if !ResolveAutoAltText() {
		t.Fatalf("default should be true")
	}
	for _, v := range []string{"0", "false", "no", "off", "FALSE"} {
		t.Setenv("WUPHF_IMAGE_AUTO_ALT", v)
		if ResolveAutoAltText() {
			t.Errorf("expected false for %q", v)
		}
	}
	for _, v := range []string{"1", "true", "yes"} {
		t.Setenv("WUPHF_IMAGE_AUTO_ALT", v)
		if !ResolveAutoAltText() {
			t.Errorf("expected true for %q", v)
		}
	}
}

// Ensure bytes.Equal helper is actually used in production — keeps the
// import non-orphaned in test refactors.
var _ = bytes.Equal
