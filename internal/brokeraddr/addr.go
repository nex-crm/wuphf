package brokeraddr

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const DefaultPort = 7890

// DefaultTokenFile is the path the broker writes its session token to and
// clients read from. On Unix we keep the historical /tmp location; on Windows
// /tmp doesn't exist, so fall back to os.TempDir() (typically %LOCALAPPDATA%\Temp).
//
// It's a `var` rather than a `const` so the runtime.GOOS branch above can
// take effect at package init. Once init is done the value is effectively
// frozen — downstream packages (internal/team, internal/teammcp, cmd/wuphf)
// copy this string into their own package-level vars at *their* init time
// for direct test stubbing. Mutating DefaultTokenFile at runtime will NOT
// propagate to those caches; if you need to redirect token reads in a test,
// stub the consumer-side var directly (e.g. team.brokerTokenFilePath).
var DefaultTokenFile = defaultTokenFile()

func defaultTokenFile() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), "wuphf-broker-token")
	}
	return "/tmp/wuphf-broker-token"
}

func tokenFileForPort(port int) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("wuphf-broker-token-%d", port))
	}
	return fmt.Sprintf("/tmp/wuphf-broker-token-%d", port)
}

func ResolveBaseURL() string {
	if base := envBaseURL(); base != "" {
		return base
	}
	return fmt.Sprintf("http://127.0.0.1:%d", ResolvePort())
}

func ResolvePort() int {
	for _, key := range []string{"WUPHF_BROKER_PORT", "NEX_BROKER_PORT"} {
		if port := parsePort(os.Getenv(key)); port > 0 {
			return port
		}
	}
	if port := portFromBaseURL(envBaseURL()); port > 0 {
		return port
	}
	return DefaultPort
}

func ResolveTokenFile() string {
	for _, key := range []string{"WUPHF_BROKER_TOKEN_FILE", "NEX_BROKER_TOKEN_FILE"} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			return path
		}
	}
	port := ResolvePort()
	if port == DefaultPort {
		return DefaultTokenFile
	}
	return tokenFileForPort(port)
}

func parsePort(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 {
		return 0
	}
	return port
}

func envBaseURL() string {
	for _, key := range []string{
		"WUPHF_BROKER_BASE_URL",
		"NEX_BROKER_BASE_URL",
		"WUPHF_TEAM_BROKER_URL",
		"NEX_TEAM_BROKER_URL",
	} {
		if base := strings.TrimSpace(os.Getenv(key)); base != "" {
			return strings.TrimRight(base, "/")
		}
	}
	return ""
}

func portFromBaseURL(base string) int {
	base = strings.TrimSpace(base)
	if base == "" {
		return 0
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed == nil {
		return 0
	}
	return parsePort(parsed.Port())
}
