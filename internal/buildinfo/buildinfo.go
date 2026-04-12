package buildinfo

import "strings"

var (
	Version        = "0.1.0"
	BuildTimestamp = "unknown"
)

type Info struct {
	Version        string `json:"version"`
	BuildTimestamp string `json:"build_timestamp"`
}

func Current() Info {
	version := strings.TrimSpace(Version)
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
