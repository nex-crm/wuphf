package team

// headless_logging.go owns the file-based logging utilities used by
// every headless turn runner (codex, claude, opencode, openai-compat).
// Logs land under ~/.wuphf/logs by default; tests redirect via the
// wuphfLogDirOverride atomic.Pointer (set in TestMain) so suites
// don't pollute the developer's real log directory. The previous
// WUPHF_LOG_DIR env var was retired because env leaks into spawned
// CLI subprocesses — the in-process pointer doesn't.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// wuphfLogDirOverride is a test hook for redirecting headless log writes to
// an isolated path. Stored as atomic.Pointer so reads on the headless write
// path don't take a lock; nil in production. Tests set this via TestMain so
// log files don't pollute the user's real ~/.wuphf/logs while the suite
// runs. The previous WUPHF_LOG_DIR environment variable was retired in
// favour of this in-process hook — env vars leak into spawned codex/claude
// subprocesses, which is not what tests want.
var wuphfLogDirOverride atomic.Pointer[string]

func wuphfLogDir() string {
	if p := wuphfLogDirOverride.Load(); p != nil {
		override := strings.TrimSpace(*p)
		if override == "" {
			return ""
		}
		if err := os.MkdirAll(override, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "wuphf: log dir override %q unwritable: %v — headless logging disabled\n", override, err)
			return ""
		}
		return override
	}
	if home := config.RuntimeHomeDir(); home != "" {
		dir := filepath.Join(home, ".wuphf", "logs")
		_ = os.MkdirAll(dir, 0o700)
		return dir
	}
	return ""
}

func appendHeadlessCodexLog(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-codex-"+slug+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(line))
}

func appendHeadlessCodexLatency(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-codex-latency.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(slug), strings.TrimSpace(line))
}

func appendHeadlessClaudeLog(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-claude-"+slug+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(line))
}

func appendHeadlessClaudeLatency(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-claude-latency.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(slug), strings.TrimSpace(line))
}
