package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

func makeTestTool(name string) agent.AgentTool {
	return agent.AgentTool{
		Name:        name,
		Description: "A test tool",
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "number"},
			},
		},
	}
}

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := agent.NewToolRegistry()
	tool := makeTestTool("search")
	r.Register(tool)

	got, ok := r.Get("search")
	if !ok {
		t.Fatal("expected tool to be found")
	}
	if got.Name != "search" {
		t.Errorf("expected name %q, got %q", "search", got.Name)
	}
}

func TestToolRegistry_Has(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("search"))

	if !r.Has("search") {
		t.Error("expected Has to return true for registered tool")
	}
	if r.Has("missing") {
		t.Error("expected Has to return false for unregistered tool")
	}
}

func TestToolRegistry_Unregister(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("search"))
	r.Unregister("search")

	if r.Has("search") {
		t.Error("expected tool to be removed after Unregister")
	}
}

func TestToolRegistry_List(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("alpha"))
	r.Register(makeTestTool("beta"))

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(list))
	}
}

func TestToolRegistry_GetMissing(t *testing.T) {
	r := agent.NewToolRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for missing tool")
	}
}

func TestToolRegistry_Validate_UnknownTool(t *testing.T) {
	r := agent.NewToolRegistry()
	ok, errs := r.Validate("nope", map[string]any{"query": "test"})
	if ok {
		t.Error("expected validation to fail for unknown tool")
	}
	if len(errs) == 0 {
		t.Error("expected error messages")
	}
}

func TestToolRegistry_Validate_MissingRequired(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("search"))

	ok, errs := r.Validate("search", map[string]any{
		"limit": 10.0,
	})
	if ok {
		t.Error("expected validation to fail when required param missing")
	}
	found := false
	for _, e := range errs {
		if e == `missing required param: "query"` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-required error, got: %v", errs)
	}
}

func TestToolRegistry_Validate_UnknownParam(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("search"))

	ok, errs := r.Validate("search", map[string]any{
		"query":   "hello",
		"unknown": "value",
	})
	if ok {
		t.Error("expected validation to fail with unknown param")
	}
	found := false
	for _, e := range errs {
		if e == `unknown param: "unknown"` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unknown-param error, got: %v", errs)
	}
}

func TestToolRegistry_Validate_Valid(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("search"))

	ok, errs := r.Validate("search", map[string]any{
		"query": "hello",
		"limit": 5.0,
	})
	if !ok {
		t.Errorf("expected validation to pass, got errors: %v", errs)
	}
}

func TestToolRegistry_Validate_RequiredOnly(t *testing.T) {
	r := agent.NewToolRegistry()
	r.Register(makeTestTool("search"))

	ok, errs := r.Validate("search", map[string]any{
		"query": "test",
	})
	if !ok {
		t.Errorf("expected validation to pass with only required params, got: %v", errs)
	}
}

func builtinTool(t *testing.T, name string) agent.AgentTool {
	t.Helper()
	for _, tool := range agent.CreateBuiltinTools(nil) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("builtin tool %q not found", name)
	return agent.AgentTool{}
}

func decodeToolResult(t *testing.T, raw string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	return result
}

func TestBuiltinToolsIncludeLocalToolset(t *testing.T) {
	tools := agent.CreateBuiltinTools(nil)
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"read_file", "grep_search", "glob", "write_file", "bash", "send_message"} {
		if !names[name] {
			t.Fatalf("expected builtin tool %q", name)
		}
	}
}

func TestReadFileTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := builtinTool(t, "read_file")
	raw, err := tool.Execute(map[string]any{
		"path":              "notes.txt",
		"working_directory": dir,
	}, context.Background(), func(string) {})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	result := decodeToolResult(t, raw)
	if got := result["combined"]; got != "hello\nworld\n" {
		t.Fatalf("expected file content, got %#v", got)
	}
}

func TestWriteFileAndGlobTools(t *testing.T) {
	dir := t.TempDir()
	writeTool := builtinTool(t, "write_file")
	if _, err := writeTool.Execute(map[string]any{
		"path":              "nested/report.txt",
		"content":           "alpha\nbeta",
		"working_directory": dir,
	}, context.Background(), func(string) {}); err != nil {
		t.Fatalf("write_file: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "nested", "report.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "alpha\nbeta" {
		t.Fatalf("unexpected file content %q", string(data))
	}

	globTool := builtinTool(t, "glob")
	raw, err := globTool.Execute(map[string]any{
		"pattern":           "nested/*.txt",
		"working_directory": dir,
	}, context.Background(), func(string) {})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	result := decodeToolResult(t, raw)
	files, ok := result["files"].([]any)
	if !ok || len(files) != 1 || files[0] != "nested/report.txt" {
		t.Fatalf("unexpected glob files %#v", result["files"])
	}
}

func TestGrepSearchTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("BubbleTea\nother\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("none here\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	tool := builtinTool(t, "grep_search")
	raw, err := tool.Execute(map[string]any{
		"pattern":           "BubbleTea",
		"working_directory": dir,
	}, context.Background(), func(string) {})
	if err != nil {
		t.Fatalf("grep_search: %v", err)
	}
	result := decodeToolResult(t, raw)
	if got := int(result["match_count"].(float64)); got != 1 {
		t.Fatalf("expected 1 match, got %d", got)
	}
	matches, ok := result["matches"].([]any)
	if !ok || len(matches) != 1 || !strings.Contains(matches[0].(string), "a.txt:1:BubbleTea") {
		t.Fatalf("unexpected matches %#v", result["matches"])
	}
}

func TestBashToolCapturesStdoutAndStderr(t *testing.T) {
	dir := t.TempDir()
	tool := builtinTool(t, "bash")
	raw, err := tool.Execute(map[string]any{
		"command":           "printf 'out'; printf 'err' >&2",
		"working_directory": dir,
	}, context.Background(), func(string) {})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	result := decodeToolResult(t, raw)
	if result["stdout"] != "out" {
		t.Fatalf("expected stdout 'out', got %#v", result["stdout"])
	}
	if result["stderr"] != "err" {
		t.Fatalf("expected stderr 'err', got %#v", result["stderr"])
	}
	if got := int(result["exit_code"].(float64)); got != 0 {
		t.Fatalf("expected exit code 0, got %d", got)
	}
}

func TestSendMessageToolWritesOutbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tool := builtinTool(t, "send_message")
	raw, err := tool.Execute(map[string]any{
		"recipient": "be",
		"message":   "API is ready",
		"channel":   "launch",
	}, context.Background(), func(string) {})
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	result := decodeToolResult(t, raw)
	if result["recipient"] != "be" {
		t.Fatalf("expected recipient be, got %#v", result["recipient"])
	}

	data, err := os.ReadFile(filepath.Join(home, ".wuphf", "office", "messages", "outbox.jsonl"))
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if !strings.Contains(string(data), "\"recipient\":\"be\"") {
		t.Fatalf("expected outbox entry, got %q", string(data))
	}
}
