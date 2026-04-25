package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestWriteHeadlessOpencodeMCPConfigConcurrent verifies that concurrent calls
// to writeHeadlessOpencodeMCPConfig (as happen when CEO + planner + reviewer
// all spawn at the same time) write agent-scoped configs with the right MCP
// environment instead of racing to rewrite one shared opencode.json.
func TestWriteHeadlessOpencodeMCPConfigConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

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
