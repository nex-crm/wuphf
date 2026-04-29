package brokeraddr

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// On Windows, the broker token file MUST live under os.TempDir() (typically
// %LOCALAPPDATA%\Temp). A literal /tmp/... path silently creates C:\tmp at
// the drive root or fails outright — the kind of bug that compiles fine on
// Mac/Linux and breaks the binary on Windows with no clear error.
func TestDefaultTokenFile_OnWindows_NotInSlashTmp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("only meaningful on Windows; current GOOS=%s", runtime.GOOS)
	}
	if strings.HasPrefix(DefaultTokenFile, "/tmp") {
		t.Fatalf("DefaultTokenFile must not start with /tmp on Windows: %q", DefaultTokenFile)
	}
	if !strings.HasPrefix(DefaultTokenFile, os.TempDir()) {
		t.Fatalf("DefaultTokenFile must live under os.TempDir() on Windows. got %q, want prefix %q",
			DefaultTokenFile, os.TempDir())
	}
}

// On Unix the historical /tmp/wuphf-broker-token path is the contract — keep
// it stable so existing scripts and the daemon's atomic-rename machinery
// continue to work.
func TestDefaultTokenFile_OnUnix_StaysAtSlashTmp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only check")
	}
	want := "/tmp/wuphf-broker-token"
	if DefaultTokenFile != want {
		t.Fatalf("DefaultTokenFile = %q; want %q", DefaultTokenFile, want)
	}
}

// Same regression vector for non-default ports.
func TestTokenFileForPort_OnWindows_NotInSlashTmp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("only meaningful on Windows; current GOOS=%s", runtime.GOOS)
	}
	got := tokenFileForPort(7891)
	if strings.HasPrefix(got, "/tmp") {
		t.Fatalf("tokenFileForPort(7891) must not start with /tmp on Windows: %q", got)
	}
	if !strings.Contains(got, "7891") {
		t.Fatalf("tokenFileForPort(7891) must include the port: %q", got)
	}
	if !strings.HasPrefix(got, os.TempDir()) {
		t.Fatalf("tokenFileForPort(7891) must live under os.TempDir() on Windows: got %q, want prefix %q",
			got, os.TempDir())
	}
}

// Env override beats both the default and the per-port computed path. This
// is the contract the broker daemon and clients agree on for non-default
// runtime layouts (sandbox tests, multi-tenant local dev).
func TestResolveTokenFile_RespectsEnvOverride(t *testing.T) {
	t.Setenv("WUPHF_BROKER_TOKEN_FILE", filepath.Join(t.TempDir(), "custom-token"))
	t.Setenv("NEX_BROKER_TOKEN_FILE", "")
	t.Setenv("WUPHF_BROKER_PORT", "")
	t.Setenv("NEX_BROKER_PORT", "")
	t.Setenv("WUPHF_BROKER_BASE_URL", "")
	t.Setenv("NEX_BROKER_BASE_URL", "")
	t.Setenv("WUPHF_TEAM_BROKER_URL", "")
	t.Setenv("NEX_TEAM_BROKER_URL", "")
	want := os.Getenv("WUPHF_BROKER_TOKEN_FILE")
	if got := ResolveTokenFile(); got != want {
		t.Fatalf("ResolveTokenFile() = %q; want %q", got, want)
	}
}

// The non-default-port path must use the per-port helper, not collide with
// the singleton DefaultTokenFile (concurrent brokers on different ports).
func TestResolveTokenFile_NonDefaultPort_UsesPortVariant(t *testing.T) {
	t.Setenv("WUPHF_BROKER_TOKEN_FILE", "")
	t.Setenv("NEX_BROKER_TOKEN_FILE", "")
	t.Setenv("WUPHF_BROKER_PORT", "8000")
	t.Setenv("NEX_BROKER_PORT", "")
	t.Setenv("WUPHF_BROKER_BASE_URL", "")
	t.Setenv("NEX_BROKER_BASE_URL", "")
	t.Setenv("WUPHF_TEAM_BROKER_URL", "")
	t.Setenv("NEX_TEAM_BROKER_URL", "")
	got := ResolveTokenFile()
	if got == DefaultTokenFile {
		t.Fatalf("ResolveTokenFile() returned DefaultTokenFile %q for non-default port; want per-port path", got)
	}
	if !strings.Contains(got, "8000") {
		t.Fatalf("ResolveTokenFile() = %q; expected port 8000 in path", got)
	}
}

// Default port must use DefaultTokenFile so consumers that cache it stay in
// sync with ResolveTokenFile() callers.
func TestResolveTokenFile_DefaultPort_UsesDefaultPath(t *testing.T) {
	t.Setenv("WUPHF_BROKER_TOKEN_FILE", "")
	t.Setenv("NEX_BROKER_TOKEN_FILE", "")
	t.Setenv("WUPHF_BROKER_PORT", "")
	t.Setenv("NEX_BROKER_PORT", "")
	t.Setenv("WUPHF_BROKER_BASE_URL", "")
	t.Setenv("NEX_BROKER_BASE_URL", "")
	t.Setenv("WUPHF_TEAM_BROKER_URL", "")
	t.Setenv("NEX_TEAM_BROKER_URL", "")
	if got := ResolveTokenFile(); got != DefaultTokenFile {
		t.Fatalf("ResolveTokenFile() = %q; want DefaultTokenFile %q", got, DefaultTokenFile)
	}
}
