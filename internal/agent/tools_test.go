package agent_test

import (
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
