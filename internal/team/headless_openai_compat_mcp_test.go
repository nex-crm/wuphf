package team

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeMCPInput is the typed input for the fake tool below; declared at
// package scope because mcp.AddTool's reflection-based schema extraction
// needs a named type for jsonschema tags.
type fakeMCPInput struct {
	City  string `json:"city" jsonschema:"required"`
	Units string `json:"units,omitempty"`
}

type fakeMCPOutput struct {
	Summary string `json:"summary"`
}

// TestMCPSessionToAgentTools_RoundTripsRealMCPCall stands up a real MCP
// server in-process via mcp.NewInMemoryTransports, registers a typed tool,
// and verifies the bridge:
//   - lists the tool with name + description + schema
//   - routes Execute() back through MCP CallTool
//   - returns the tool's text content as the AgentTool's string result
//
// This is the most realistic e2e test we can run for the bridge without
// spawning the actual `wuphf mcp-team` binary, and it would have caught
// any of: schema serialization regressions, name routing bugs, or
// CallToolResult content extraction problems.
func TestMCPSessionToAgentTools_RoundTripsRealMCPCall(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-team", Version: "0.1.0"}, nil)

	var (
		captured fakeMCPInput
		calls    int
	)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "lookup_weather",
		Description: "Return the current weather for a city.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in fakeMCPInput) (*mcp.CallToolResult, fakeMCPOutput, error) {
		calls++
		captured = in
		summary := "It is 22°C in " + in.City + "."
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: summary}},
		}, fakeMCPOutput{Summary: summary}, nil
	})

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// The server.Run blocks on the transport; ignore the error on shutdown.
		_ = server.Run(ctx, serverTr)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-bridge", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	tools, err := mcpSessionToAgentTools(ctx, session)
	if err != nil {
		t.Fatalf("mcpSessionToAgentTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	got := tools[0]
	if got.Name != "lookup_weather" {
		t.Errorf("Name = %q, want lookup_weather", got.Name)
	}
	if !strings.Contains(got.Description, "weather") {
		t.Errorf("Description = %q", got.Description)
	}
	if got.Schema == nil {
		t.Fatal("Schema = nil; bridge dropped the input schema")
	}
	if t2, _ := got.Schema["type"].(string); t2 != "object" {
		t.Errorf("Schema[type] = %v, want object", got.Schema["type"])
	}

	out, execErr := got.Execute(map[string]any{"city": "Lisbon", "units": "metric"}, ctx, nil)
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if !strings.Contains(out, "Lisbon") || !strings.Contains(out, "22") {
		t.Errorf("Execute returned %q", out)
	}
	if calls != 1 {
		t.Errorf("server saw %d calls, want 1", calls)
	}
	if captured.City != "Lisbon" || captured.Units != "metric" {
		t.Errorf("server captured input = %+v", captured)
	}
}

// TestMCPSessionToAgentTools_ErrorResultSurfacesAsERROR verifies that
// when an MCP tool returns IsError, the AgentTool wrapper surfaces the
// content with an "ERROR:" prefix instead of swallowing it. This is
// load-bearing for the openai-compat tool loop: the model needs to see
// that the tool failed so it can self-correct.
func TestMCPSessionToAgentTools_ErrorResultSurfacesAsERROR(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "always_fails",
		Description: "Always returns an error result.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "permission denied"}},
		}, struct{}{}, nil
	})

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTr) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "x", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := mcpSessionToAgentTools(ctx, session)
	if err != nil || len(tools) != 1 {
		t.Fatalf("setup: err=%v len=%d", err, len(tools))
	}

	out, execErr := tools[0].Execute(map[string]any{}, ctx, nil)
	if execErr != nil {
		t.Fatalf("Execute returned a Go error; expected the IsError result to come back as a string: %v", execErr)
	}
	if !strings.HasPrefix(out, "ERROR: ") {
		t.Errorf("expected ERROR: prefix, got %q", out)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("error message lost: %q", out)
	}
}

// TestMCPSessionToAgentTools_MultiToolFanOut sanity-checks that all
// registered tools end up in the AgentTool slice with stable identity.
// Catches any future bug where the loop short-circuits on the first
// result page or mishandles concurrent CallTool invocations.
func TestMCPSessionToAgentTools_MultiToolFanOut(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0"}, nil)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: name}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: name + "-result"}}}, struct{}{}, nil
		})
	}

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTr) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "x", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := mcpSessionToAgentTools(ctx, session)
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(tools))
	}
	gotNames := map[string]bool{}
	for _, tt := range tools {
		gotNames[tt.Name] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !gotNames[want] {
			t.Errorf("missing tool %q (got %v)", want, gotNames)
		}
	}

	// Verify Execute calls reach the right server-side handler — if the
	// bridge crossed wires (e.g. all callbacks captured the same loop var)
	// every tool would return the same text.
	for _, tt := range tools {
		got, err := tt.Execute(map[string]any{}, ctx, nil)
		if err != nil {
			t.Errorf("Execute %s: %v", tt.Name, err)
			continue
		}
		want := tt.Name + "-result"
		if !strings.Contains(got, want) {
			t.Errorf("Execute %s: got %q, want substring %q (closure capture bug?)", tt.Name, got, want)
		}
	}
}
