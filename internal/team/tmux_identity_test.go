package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
)

// nameWithPortSuffix decides the tmux socket and session names at package
// init based on the broker port. When two WUPHF instances ran on the same
// machine they used to share "wuphf" and "wuphf-team" and race each other's
// kill-session / new-session / split-window calls, which surfaced as
// "spawn first agent: exit status 1" when the server was torn down
// mid-launch. These tests pin the rule so the isolation can't regress
// silently.

func TestNameWithPortSuffixDefaultPort(t *testing.T) {
	if got := nameWithPortSuffixForPort("wuphf", brokeraddr.DefaultPort); got != "wuphf" {
		t.Fatalf("default port should not suffix: got %q, want %q", got, "wuphf")
	}
	if got := nameWithPortSuffixForPort("wuphf-team", brokeraddr.DefaultPort); got != "wuphf-team" {
		t.Fatalf("default port should not suffix session: got %q, want %q", got, "wuphf-team")
	}
}

func TestNameWithPortSuffixNonDefault(t *testing.T) {
	cases := []struct {
		base string
		port int
		want string
	}{
		{"wuphf", 7899, "wuphf-7899"},
		{"wuphf-team", 7899, "wuphf-team-7899"},
		{"wuphf", 8080, "wuphf-8080"},
	}
	for _, tc := range cases {
		if got := nameWithPortSuffixForPort(tc.base, tc.port); got != tc.want {
			t.Fatalf("port %d base %q: got %q, want %q", tc.port, tc.base, got, tc.want)
		}
	}
}

func TestNameWithPortSuffixInvalidPortFallsBack(t *testing.T) {
	if got := nameWithPortSuffixForPort("wuphf", 0); got != "wuphf" {
		t.Fatalf("zero port should fall back: got %q", got)
	}
	if got := nameWithPortSuffixForPort("wuphf", -1); got != "wuphf" {
		t.Fatalf("negative port should fall back: got %q", got)
	}
}

// TestPackageLevelNamesHonorBaseNames guards against someone inadvertently
// changing the base constants in a way that leaks the port suffix into
// external consumers that hardcode "wuphf-team".
func TestPackageLevelNamesHonorBaseNames(t *testing.T) {
	if baseSessionName != "wuphf-team" {
		t.Fatalf("baseSessionName drifted: got %q", baseSessionName)
	}
	if baseTmuxSocketName != "wuphf" {
		t.Fatalf("baseTmuxSocketName drifted: got %q", baseTmuxSocketName)
	}
}
