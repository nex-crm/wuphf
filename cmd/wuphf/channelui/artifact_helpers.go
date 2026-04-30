package channelui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// SummarizeJSONField returns a TruncateText'd one-line summary of a
// JSON-encoded raw message. JSON strings are unquoted first; objects
// and arrays are compacted via json.Compact; malformed input falls
// through to the trimmed raw text. Returns "" for empty / "null"
// values so callers can decide whether to emit a row at all.
func SummarizeJSONField(raw json.RawMessage, max int) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return TruncateText(strings.TrimSpace(plain), max)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err == nil {
		return TruncateText(compact.String(), max)
	}
	return TruncateText(text, max)
}

// TaskLogRoot returns the root directory where headless task tool
// logs live. WUPHF_TASK_LOG_ROOT wins when set; otherwise it
// resolves to ~/.wuphf/office/tasks (or .wuphf/office/tasks when
// the home directory is unavailable).
func TaskLogRoot() string {
	if root := strings.TrimSpace(os.Getenv("WUPHF_TASK_LOG_ROOT")); root != "" {
		return root
	}
	if home := config.RuntimeHomeDir(); home != "" {
		return filepath.Join(home, ".wuphf", "office", "tasks")
	}
	return filepath.Join(".wuphf", "office", "tasks")
}
