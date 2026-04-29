package channelui

import (
	"regexp"
	"strings"
)

// Test-only helpers shared across channelui's test files. The cmd/wuphf
// package has copies of these — once the channel cluster fully migrates
// the package-main copies disappear with the alias bridge.

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func joinRenderedLines(lines []RenderedLine) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, line.Text)
	}
	return strings.Join(parts, "\n")
}
