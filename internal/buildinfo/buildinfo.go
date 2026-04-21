package buildinfo

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var embeddedVersion string

// Version is set at link time via
// `-ldflags "-X github.com/nex-crm/wuphf/internal/buildinfo.Version=<tag>"`
// for tagged release builds. When empty (e.g. `go build ./cmd/wuphf` with no
// ldflags), Current() falls back to the embedded VERSION file so contributor
// builds still report a real version instead of a hardcoded default.
var (
	Version        = ""
	BuildTimestamp = ""
)

type Info struct {
	Version        string `json:"version"`
	BuildTimestamp string `json:"build_timestamp"`
}

func Current() Info {
	version := strings.TrimSpace(Version)
	if version == "" {
		version = strings.TrimSpace(embeddedVersion)
	}
	if version == "" {
		version = "dev"
	}
	buildTimestamp := strings.TrimSpace(BuildTimestamp)
	if buildTimestamp == "" {
		buildTimestamp = "unknown"
	}
	return Info{
		Version:        version,
		BuildTimestamp: buildTimestamp,
	}
}
