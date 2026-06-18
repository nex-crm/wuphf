package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func TestSlackMCPResolvers(t *testing.T) {
	t.Setenv("WUPHF_SLACK_MCP", "")
	t.Setenv("WUPHF_SLACK_MCP_TOKEN", "")
	if config.ResolveSlackMCPEnabled() {
		t.Fatal("disabled by default")
	}
	t.Setenv("WUPHF_SLACK_MCP", "1")
	if !config.ResolveSlackMCPEnabled() {
		t.Fatal("WUPHF_SLACK_MCP=1 should enable")
	}
	t.Setenv("WUPHF_SLACK_MCP", "")
	t.Setenv("WUPHF_SLACK_MCP_TOKEN", "xoxp-mcp")
	if !config.ResolveSlackMCPEnabled() || config.ResolveSlackMCPToken() != "xoxp-mcp" {
		t.Fatal("a token alone should enable + resolve")
	}
}

func TestBuildMCPServerMap_SlackGated(t *testing.T) {
	l := &Launcher{}

	// Off by default → no slack server.
	t.Setenv("WUPHF_SLACK_MCP", "")
	t.Setenv("WUPHF_SLACK_MCP_TOKEN", "")
	m, err := l.buildMCPServerMap()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := m["slack"]; ok {
		t.Fatal("slack MCP must be off by default")
	}

	// Enabled, no token → http remote, no auth header.
	t.Setenv("WUPHF_SLACK_MCP", "1")
	m, _ = l.buildMCPServerMap()
	slack, ok := m["slack"].(map[string]any)
	if !ok {
		t.Fatalf("slack MCP should be wired when enabled: %+v", m["slack"])
	}
	if slack["type"] != "http" || slack["url"] != "https://mcp.slack.com/mcp" {
		t.Fatalf("unexpected slack MCP entry: %+v", slack)
	}
	if _, hasHeaders := slack["headers"]; hasHeaders {
		t.Fatal("no auth header without a token (rely on client OAuth)")
	}

	// Token → Authorization bearer header.
	t.Setenv("WUPHF_SLACK_MCP_TOKEN", "xoxp-mcp")
	m, _ = l.buildMCPServerMap()
	slack = m["slack"].(map[string]any)
	h, _ := slack["headers"].(map[string]string)
	if h["Authorization"] != "Bearer xoxp-mcp" {
		t.Fatalf("expected bearer header, got %+v", slack["headers"])
	}
}

func TestAgentMCPServers_SlackForNonCodingOnly(t *testing.T) {
	t.Setenv("WUPHF_CHANNEL", "")
	if got := agentMCPServers("ceo"); !containsString(got, "slack") {
		t.Fatalf("ceo should get the slack MCP, got %v", got)
	}
	if got := agentMCPServers("eng"); containsString(got, "slack") {
		t.Fatalf("coding agents stay minimal, got %v", got)
	}
}
