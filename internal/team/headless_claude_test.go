package team

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// minimalLauncher builds a Launcher with a predictable two-member pack so
// officeLeadSlug() always returns "ceo".
func minimalLauncher(opusCEO bool) *Launcher {
	return &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
				{Slug: "pm", Name: "Product Manager"},
			},
		},
		opusCEO:         opusCEO,
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}
}

// ─── headlessClaudeModel ──────────────────────────────────────────────────

// TestHeadlessClaudeModel_SonnetByDefault verifies that every agent, including
// the lead, uses the Sonnet model when opusCEO is false.
func TestHeadlessClaudeModel_SonnetByDefault(t *testing.T) {
	l := minimalLauncher(false)
	for _, slug := range []string{"ceo", "eng", "pm"} {
		t.Run(slug, func(t *testing.T) {
			if got := l.headlessClaudeModel(slug); got != "claude-sonnet-4-6" {
				t.Fatalf("slug=%q opusCEO=false: want claude-sonnet-4-6, got %q", slug, got)
			}
		})
	}
}

// TestHeadlessClaudeModel_OpusForLeadOnly verifies that only the lead (CEO)
// gets upgraded to Opus when opusCEO is true; non-lead agents stay on Sonnet.
func TestHeadlessClaudeModel_OpusForLeadOnly(t *testing.T) {
	l := minimalLauncher(true)
	tests := []struct {
		slug string
		want string
	}{
		{"ceo", "claude-opus-4-6"},
		{"eng", "claude-sonnet-4-6"},
		{"pm", "claude-sonnet-4-6"},
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			if got := l.headlessClaudeModel(tc.slug); got != tc.want {
				t.Fatalf("slug=%q opusCEO=true: want %q, got %q", tc.slug, tc.want, got)
			}
		})
	}
}

// TestHeadlessClaudeModel_CustomLeadSlug verifies model selection when the
// pack defines a non-"ceo" lead slug.
// brokerStatePath is redirected to an empty temp dir so officeMembersSnapshot()
// falls through to the pack definition instead of loading live state.
func TestHeadlessClaudeModel_CustomLeadSlug(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "captain",
			Agents: []agent.AgentConfig{
				{Slug: "captain", Name: "Captain"},
				{Slug: "crew", Name: "Crew"},
			},
		},
		opusCEO:         true,
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	tests := []struct {
		slug string
		want string
	}{
		{"captain", "claude-opus-4-6"},
		{"crew", "claude-sonnet-4-6"},
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			if got := l.headlessClaudeModel(tc.slug); got != tc.want {
				t.Fatalf("slug=%q: want %q, got %q", tc.slug, tc.want, got)
			}
		})
	}
}

// ─── runHeadlessClaudeTurn: no --resume flag in fresh sessions ────────────

// TestRunHeadlessClaudeTurn_NoResumeFlag verifies that the command assembled
// for a fresh (non-resumed) session does NOT contain --resume.
//
// We intercept headlessClaudeCommandContext to record the argv before any
// process is started. The binary is pointed at /bin/true so the process exits
// cleanly; the function will fail at JSON parsing (no output), but the
// captured args are all we need.
func TestRunHeadlessClaudeTurn_NoResumeFlag(t *testing.T) {
	// Redirect broker state to an isolated temp dir.
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	origCommandContext := headlessClaudeCommandContext
	origLookPath := headlessClaudeLookPath
	defer func() {
		headlessClaudeCommandContext = origCommandContext
		headlessClaudeLookPath = origLookPath
	}()

	var capturedArgs []string

	// Simulate claude found on PATH.
	headlessClaudeLookPath = func(file string) (string, error) { return "/bin/true", nil }

	// Intercept command creation: record the args then delegate to a real
	// exec.Cmd pointing at /bin/true so Start()/Wait() succeed trivially.
	headlessClaudeCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = append(capturedArgs, args...)
		return exec.CommandContext(ctx, "/bin/true")
	}

	b := NewBroker()
	l := minimalLauncher(false)
	l.broker = b
	l.cwd = tmpDir

	// Write a valid (empty) MCP config so ensureAgentMCPConfig succeeds.
	mcpPath := filepath.Join(tmpDir, "mcp.json")
	_ = os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0o600)
	l.mcpConfig = mcpPath

	// The function returns a parse error because /bin/true produces no JSON.
	// That is expected; we only care about capturedArgs.
	_ = l.runHeadlessClaudeTurn(context.Background(), "eng", "do the thing")

	if len(capturedArgs) == 0 {
		t.Fatal("no args captured; headlessClaudeCommandContext hook was not called")
	}
	for _, arg := range capturedArgs {
		if arg == "--resume" {
			t.Fatalf("--resume must not appear in a fresh session, got args: %v", capturedArgs)
		}
	}
}

// ─── MCP manifest: no-nex mode ────────────────────────────────────────────

// TestBuildMCPServerMap_NoNexExcludesNexServer verifies that when
// WUPHF_NO_NEX=true the built server map contains no "nex" entry, even if a
// non-empty API key is present.
func TestBuildMCPServerMap_NoNexExcludesNexServer(t *testing.T) {
	t.Setenv("WUPHF_NO_NEX", "true")
	// Provide a non-empty API key so we would enter the nex branch if WUPHF_NO_NEX
	// were not checked.
	t.Setenv("WUPHF_API_KEY", "test-key-12345")

	l := minimalLauncher(false)
	servers, err := l.buildMCPServerMap()
	if err != nil {
		t.Fatalf("buildMCPServerMap: %v", err)
	}
	if _, ok := servers["nex"]; ok {
		t.Fatalf("'nex' server must be absent when WUPHF_NO_NEX=true, got servers: %v", mapKeys(servers))
	}
	// wuphf-office must always be present regardless of no-nex mode.
	if _, ok := servers["wuphf-office"]; !ok {
		t.Fatalf("'wuphf-office' server must always be present, got servers: %v", mapKeys(servers))
	}
}

// TestEnsureAgentMCPConfig_NoNexEntryInWrittenFile verifies that the per-agent
// MCP config file written to disk contains no "nex" key when WUPHF_NO_NEX=true.
func TestEnsureAgentMCPConfig_NoNexEntryInWrittenFile(t *testing.T) {
	t.Setenv("WUPHF_NO_NEX", "true")
	t.Setenv("WUPHF_API_KEY", "test-key-12345")

	l := minimalLauncher(false)
	path, err := l.ensureAgentMCPConfig("ceo")
	if err != nil {
		t.Fatalf("ensureAgentMCPConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP config file: %v", err)
	}
	var cfg struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse MCP config: %v", err)
	}
	if _, hasNex := cfg.MCPServers["nex"]; hasNex {
		t.Fatalf("'nex' server must be absent in written MCP config when WUPHF_NO_NEX=true, got servers: %v", mapKeys(cfg.MCPServers))
	}
}

// TestBuildMCPServerMap_NexPresentWhenEnabled verifies that when WUPHF_NO_NEX
// is unset and a fake nex-mcp binary exists on PATH, the "nex" key appears.
func TestBuildMCPServerMap_NexPresentWhenEnabled(t *testing.T) {
	t.Setenv("WUPHF_NO_NEX", "")
	t.Setenv("WUPHF_API_KEY", "test-key-12345")

	// Inject a fake nex-mcp binary onto a prepended PATH dir.
	binDir := t.TempDir()
	nexBin := filepath.Join(binDir, "nex-mcp")
	if err := os.WriteFile(nexBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake nex-mcp: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	l := minimalLauncher(false)
	servers, err := l.buildMCPServerMap()
	if err != nil {
		t.Fatalf("buildMCPServerMap: %v", err)
	}
	if _, ok := servers["nex"]; !ok {
		t.Fatalf("'nex' server must be present when nex-mcp is on PATH and WUPHF_NO_NEX unset, got servers: %v", mapKeys(servers))
	}
}

func TestBuildMCPServerMap_GBrainPresentWhenSelected(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "gbrain")
	t.Setenv("WUPHF_OPENAI_API_KEY", "openai-test-key")

	binDir := t.TempDir()
	gbrainBin := filepath.Join(binDir, "gbrain")
	if err := os.WriteFile(gbrainBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake gbrain: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	l := minimalLauncher(false)
	servers, err := l.buildMCPServerMap()
	if err != nil {
		t.Fatalf("buildMCPServerMap: %v", err)
	}
	entry, ok := servers["gbrain"]
	if !ok {
		t.Fatalf("'gbrain' server must be present when GBrain is selected, got servers: %v", mapKeys(servers))
	}
	server, ok := entry.(map[string]any)
	if !ok {
		t.Fatalf("expected gbrain entry to be an object, got %T", entry)
	}
	if got := server["command"]; got != gbrainBin {
		t.Fatalf("expected gbrain command %q, got %#v", gbrainBin, got)
	}
	args, ok := server["args"].([]string)
	if !ok || len(args) != 1 || args[0] != "serve" {
		t.Fatalf("expected gbrain args [serve], got %#v", server["args"])
	}
	env, ok := server["env"].(map[string]string)
	if !ok {
		t.Fatalf("expected gbrain env map, got %#v", server["env"])
	}
	if env["OPENAI_API_KEY"] != "openai-test-key" {
		t.Fatalf("expected OPENAI_API_KEY to be forwarded, got %#v", env)
	}
}

// mapKeys returns the keys of map[string]V for human-readable error messages.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
