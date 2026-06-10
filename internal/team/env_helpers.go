package team

// env_helpers.go holds small shared environment parsing helpers. Extracted
// from the (removed) notebook signal scanner because other call sites
// (self_healing.go, headless_codex.go) still use them.

import (
	"fmt"
	"os"
	"strings"
)

// envIntDefault reads an int from env, falling back to fallback on missing
// or unparseable values.
func envIntDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return fallback
	}
	return n
}
