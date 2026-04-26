package team

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestOpenAICompatBridge_E2E wires the two halves of the local-llm
// integration that, before this PR, were unwired:
//
//  1. A real MCP server registers a tool typed in the same shape the
//     wuphf broker would expose (using mcp.AddTool with a typed input
//     struct).
//  2. The bridge converts that tool into agent.AgentTool entries.
//  3. A scripted StreamFn (standing in for a local OpenAI-compatible
//     model) invokes the tool, then replies with text after seeing the
//     result.
//  4. The openAICompatToolLoop drives the whole thing end-to-end.
//
// The test mirrors what would happen at runtime with mlx-lm/ollama/exo
// against the actual `wuphf mcp-team` subprocess — minus the subprocess
// boundary (which is verified separately by the live MLX test).
//
// If any of: ListTools, CallTool routing, Execute callback closure, or
// loop turn-tracking regressed, this test would fail.
func TestOpenAICompatBridge_E2E(t *testing.T) {
	// Server side: a typed tool that the model is expected to invoke.
	type postMessageInput struct {
		Channel string `json:"channel" jsonschema:"required"`
		Body    string `json:"body" jsonschema:"required"`
	}
	type postMessageOutput struct {
		PostedAs string `json:"posted_as"`
	}

	var posted []postMessageInput
	server := mcp.NewServer(&mcp.Implementation{Name: "fake-broker", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "broker_post_message",
		Description: "Post a message to a wuphf broker channel as the agent.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in postMessageInput) (*mcp.CallToolResult, postMessageOutput, error) {
		posted = append(posted, in)
		summary := "posted to #" + in.Channel
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: summary}},
		}, postMessageOutput{PostedAs: "agent-zed"}, nil
	})

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTr) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "bridge-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := mcpSessionToAgentTools(ctx, session)
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "broker_post_message" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	// Client-side scripted "model" — turn 1 emits the tool call, turn 2
	// reads the synthetic tool-result message and replies with text.
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{
				chunks: []agent.StreamChunk{
					{
						Type:       "tool_use",
						ToolName:   "broker_post_message",
						ToolParams: map[string]any{"channel": "general", "body": "hi team"},
						ToolInput:  `{"channel":"general","body":"hi team"}`,
						ToolUseID:  "c1",
					},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					// Sanity check: turn 1 sees the system prompt + the
					// user notification, with no synthetic noise leaked
					// in from prior turns.
					if len(msgs) != 2 {
						t.Fatalf("turn 1 expected 2 msgs (system+user), got %d: %+v", len(msgs), msgs)
					}
					if msgs[0].Role != "system" || msgs[1].Role != "user" {
						t.Errorf("turn 1 roles = [%s, %s], want [system, user]", msgs[0].Role, msgs[1].Role)
					}
				},
			},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "Done — message posted."},
				},
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					last := msgs[len(msgs)-1]
					if last.Role != "user" {
						t.Errorf("turn 2 last role = %q", last.Role)
					}
					if !strings.Contains(last.Content, "posted to #general") {
						t.Errorf("turn 2 missing tool result: %q", last.Content)
					}
				},
			},
		},
	}

	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       tools,
		toolByName:  map[string]agent.AgentTool{"broker_post_message": tools[0]},
		maxIters:    4,
		toolTimeout: 3 * time.Second,
	}

	final, iters, streamErr, err := loop.run(ctx, []agent.Message{
		{Role: "system", Content: "You're a helpful office agent."},
		{Role: "user", Content: "Tell the team hi in #general."},
	})
	if err != nil {
		t.Fatalf("loop.run: %v", err)
	}
	if streamErr != "" {
		t.Fatalf("stream error: %s", streamErr)
	}
	if iters != 2 {
		t.Errorf("iterations = %d, want 2", iters)
	}
	if final != "Done — message posted." {
		t.Errorf("finalText = %q", final)
	}
	if len(posted) != 1 {
		t.Fatalf("server got %d posts, want 1", len(posted))
	}
	if posted[0].Channel != "general" || posted[0].Body != "hi team" {
		t.Errorf("server received %+v", posted[0])
	}
}
