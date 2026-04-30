package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestBuildHeadlessOpencodeArgsRequestsJSONFormat(t *testing.T) {
	args := buildHeadlessOpencodeArgs("anthropic/claude-sonnet-4", "do the thing")
	formatIdx := slices.Index(args, "--format")
	if formatIdx == -1 || formatIdx+1 >= len(args) || args[formatIdx+1] != "json" {
		t.Fatalf("expected `--format json` in args, got %v", args)
	}
	if args[len(args)-1] != "do the thing" {
		t.Fatalf("expected prompt as last positional arg, got %v", args)
	}
}

func TestBuildHeadlessOpencodeArgsOmitsModelWhenUnset(t *testing.T) {
	args := buildHeadlessOpencodeArgs("", "ack")
	if slices.Contains(args, "--model") {
		t.Fatalf("expected no --model flag when model is empty, got %v", args)
	}
	if !slices.Contains(args, "--format") {
		t.Fatalf("expected --format json even without a model, got %v", args)
	}
}

// TestWriteHeadlessOpencodeMCPConfigConcurrent verifies that concurrent calls
// to writeHeadlessOpencodeMCPConfig (as happen when CEO + planner + reviewer
// all spawn at the same time) write agent-scoped configs with the right MCP
// environment instead of racing to rewrite one shared opencode.json.
func TestWriteHeadlessOpencodeMCPConfigConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair WUPHF_RUNTIME_HOME with HOME so the post-Phase-0 opencode race fix
	// (per-agent configs under <runtime_home>/.wuphf/opencode-configs/) lands
	// in the test tempdir and not the worktree_guard_test process-wide pin.
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	configDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed an existing opencode.json with some user content that should survive
	// the merge (theme key is untouched by WUPHF).
	seed := `{"$schema":"https://opencode.ai/config.json","theme":"dark","ai":{"ollama":{"type":"openai-compatible","url":"http://localhost:11434/v1"}}}`
	configPath := filepath.Join(configDir, "opencode.json")
	if err := os.WriteFile(configPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	// Point the executable-path hook at a harmless path so the launcher can
	// construct the MCP entry without needing the real wuphf binary.
	orig := headlessOpencodeExecutablePath
	headlessOpencodeExecutablePath = func() (string, error) { return "/usr/local/bin/wuphf", nil }
	defer func() { headlessOpencodeExecutablePath = orig }()

	l := &Launcher{}

	const goroutines = 20
	slugs := []string{"ceo", "planner", "reviewer"}
	paths := make(map[string]string)
	var pathsMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(slug string) {
			defer wg.Done()
			path, err := l.writeHeadlessOpencodeMCPConfig(slug)
			if err != nil {
				t.Errorf("writeHeadlessOpencodeMCPConfig(%q): %v", slug, err)
				return
			}
			pathsMu.Lock()
			paths[slug] = path
			pathsMu.Unlock()
		}(slugs[i%len(slugs)])
	}
	wg.Wait()

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read base opencode.json after concurrent writes: %v", err)
	}
	var base map[string]any
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("base opencode.json is not valid JSON after concurrent writes: %v\n\ncontent:\n%s", err, raw)
	}
	if _, ok := base["mcp"]; ok {
		t.Fatal("base opencode.json should not be rewritten with WUPHF's agent-scoped MCP entry")
	}

	for _, slug := range slugs {
		path := paths[slug]
		if path == "" {
			t.Fatalf("missing generated config path for %s", slug)
		}
		if want := headlessOpencodeAgentConfigPath(home, slug); path != want {
			t.Fatalf("config path for %s = %q, want %q", slug, path, want)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s config: %v", slug, err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s config is not valid JSON after concurrent writes: %v\n\ncontent:\n%s", slug, err, raw)
		}
		if out["theme"] != "dark" {
			t.Fatalf("%s config did not preserve theme: %#v", slug, out["theme"])
		}
		mcp, _ := out["mcp"].(map[string]any)
		if mcp == nil {
			t.Fatalf("mcp key missing from %s config", slug)
		}
		wuphfOffice, _ := mcp["wuphf-office"].(map[string]any)
		if wuphfOffice == nil {
			t.Fatalf("mcp.wuphf-office missing from %s config", slug)
		}
		env, _ := wuphfOffice["environment"].(map[string]any)
		if env == nil {
			t.Fatalf("mcp.wuphf-office.environment missing from %s config", slug)
		}
		if env["WUPHF_AGENT_SLUG"] != slug {
			t.Fatalf("%s config has WUPHF_AGENT_SLUG=%#v", slug, env["WUPHF_AGENT_SLUG"])
		}
	}
}

// TestWriteHeadlessOpencodeMCPConfigLogsBaseConfigParseFailure verifies that a
// malformed base ~/.config/opencode/opencode.json (e.g. trailing comma) causes
// writeHeadlessOpencodeMCPConfig to surface the parse error via the agent
// log instead of silently dropping the user's `model`/`provider` blocks. (#313)
func TestWriteHeadlessOpencodeMCPConfigLogsBaseConfigParseFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logDir := t.TempDir()
	prevLogDir := wuphfLogDirOverride.Load()
	wuphfLogDirOverride.Store(&logDir)
	t.Cleanup(func() { wuphfLogDirOverride.Store(prevLogDir) })

	configDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Trailing comma — valid Opencode-side JSON5 in some hand-edited configs,
	// but encoding/json rejects it. The legacy code silently swallowed this.
	malformed := `{"theme":"dark","model":"openrouter/foo",}`
	configPath := filepath.Join(configDir, "opencode.json")
	if err := os.WriteFile(configPath, []byte(malformed), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := headlessOpencodeExecutablePath
	headlessOpencodeExecutablePath = func() (string, error) { return "/usr/local/bin/wuphf", nil }
	defer func() { headlessOpencodeExecutablePath = orig }()

	l := &Launcher{}
	if _, err := l.writeHeadlessOpencodeMCPConfig("ceo"); err != nil {
		t.Fatalf("writeHeadlessOpencodeMCPConfig: %v", err)
	}

	logBytes, err := os.ReadFile(filepath.Join(logDir, "headless-codex-ceo.log"))
	if err != nil {
		t.Fatalf("read agent log: %v", err)
	}
	logStr := string(logBytes)
	if !strings.Contains(logStr, "opencode_base-config-parse-failed") {
		t.Fatalf("expected opencode_base-config-parse-failed in agent log, got:\n%s", logStr)
	}
	if !strings.Contains(logStr, configPath) {
		t.Fatalf("expected base config path %q in agent log, got:\n%s", configPath, logStr)
	}
}
