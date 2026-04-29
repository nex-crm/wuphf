package imagegen

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// outputDir returns the local filesystem directory where generated images
// are persisted. Default is ~/.wuphf/office/artist/<YYYY-MM-DD>/. Override
// with WUPHF_IMAGEGEN_DIR.
func outputDir() string {
	if root := strings.TrimSpace(os.Getenv("WUPHF_IMAGEGEN_DIR")); root != "" {
		return root
	}
	if home := config.RuntimeHomeDir(); home != "" {
		return filepath.Join(home, ".wuphf", "office", "artist")
	}
	return filepath.Join(".wuphf", "office", "artist")
}

// SaveResult is the output of SavePNG: both the on-disk path AND the
// HTTP-relative URL the BoardRoom serves the file at. Callers should set
// the HTTP URL on Result.ImageURL so the BoardRoom can render <img> inline,
// and keep the disk path around for the agent's debug log.
type SaveResult struct {
	DiskPath string
	HTTPURL  string // relative — `/artist-files/<date>/<file>.png`
}

// SavePNG writes a base64-encoded PNG (or raw bytes when b64=false) to disk
// under outputDir/<date>/<sha-prefix>.png and returns both the on-disk
// path and the HTTP-relative URL.
//
// Splitting this out here keeps every provider from re-rolling a tempfile
// dance — they hand us bytes, we hand them a SaveResult.
func SavePNG(prompt string, data []byte, b64 bool) (SaveResult, error) {
	if b64 {
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return SaveResult{}, fmt.Errorf("imagegen: decode base64: %w", err)
		}
		data = decoded
	}
	day := time.Now().UTC().Format("2006-01-02")
	dir := filepath.Join(outputDir(), day)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SaveResult{}, fmt.Errorf("imagegen: mkdir %q: %w", dir, err)
	}
	// Filename: first 12 hex chars of sha256(prompt+timestamp). Stable enough
	// for dedup, short enough to read in chat.
	h := sha256.Sum256([]byte(prompt + time.Now().Format(time.RFC3339Nano)))
	name := hex.EncodeToString(h[:6]) + ".png"
	diskPath := filepath.Join(dir, name)
	if err := os.WriteFile(diskPath, data, 0o600); err != nil {
		return SaveResult{}, fmt.Errorf("imagegen: write %q: %w", diskPath, err)
	}
	httpURL := "/artist-files/" + day + "/" + name
	return SaveResult{DiskPath: diskPath, HTTPURL: httpURL}, nil
}

// HTTPClientWithTimeout returns a shared *http.Client with a generous but
// bounded timeout. Image gen can take 30-90s; we give it 180s.
func HTTPClientWithTimeout() *http.Client {
	return &http.Client{Timeout: 180 * time.Second}
}
