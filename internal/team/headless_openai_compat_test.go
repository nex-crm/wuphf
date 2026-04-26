package team

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// TestIsOpenAICompatKind documents the closed set of provider Kinds that the
// dispatcher routes to runHeadlessOpenAICompatTurn. New OpenAI-compatible
// runtimes added under internal/provider/ MUST also be added here so the
// broker-driven turn queue actually calls them — otherwise the dispatcher
// silently falls through to runHeadlessClaudeTurn and the user wonders why
// `wuphf --provider mlx-lm` keeps invoking claude.
func TestIsOpenAICompatKind(t *testing.T) {
	for _, kind := range []string{
		provider.KindMLXLM,
		provider.KindOllama,
		provider.KindExo,
	} {
		if !isOpenAICompatKind(kind) {
			t.Errorf("isOpenAICompatKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{
		provider.KindClaudeCode,
		provider.KindCodex,
		provider.KindOpencode,
		provider.KindOpenclaw,
		"",
		"unknown",
	} {
		if isOpenAICompatKind(kind) {
			t.Errorf("isOpenAICompatKind(%q) = true, want false", kind)
		}
	}
}

// TestLooksUnparsedToolCall pins the post-loop backstop predicate
// directly. The runner replaces finalText with a friendly message
// when this returns true; a regression that loosens the predicate
// (e.g. fires on prose with the word `arguments`) would silently
// hide real model output, while a regression that tightens it would
// re-introduce the user-visible bug of raw JSON in chat. Conservative
// requirement: starts with `{` AND contains both `"name"` and
// `"arguments"` substrings.
func TestLooksUnparsedToolCall(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"bare tool-call shape", `{"name":"x","arguments":{}}`, true},
		{"with leading whitespace", `   {"name":"x","arguments":{}}`, true},
		{"object missing arguments", `{"name":"x"}`, false},
		{"object missing name", `{"arguments":{}}`, false},
		{"prose mentions arguments", `here are the arguments: see the docs`, false},
		{"prose mentions name", `the name field is required`, false},
		{"prose with both keywords but no leading brace", `it has a "name" and "arguments" but is prose`, false},
		{"empty", "", false},
		{"only braces", "{}", false},
		// Real-world Qwen output that prompted this fix:
		{
			name: "Qwen markdown-fenced shape",
			in:   "```json\n{\"name\":\"team_broadcast\",\"arguments\":{\"channel\":\"general\"}}\n```",
			want: false, // doesn't START with { (leads with the fence) — caller already strips fences
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksUnparsedToolCall(tc.in); got != tc.want {
				t.Errorf("looksUnparsedToolCall(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestIsUserVisiblePostTool freezes the closed set of MCP tools the
// runner treats as "this tool ALREADY posted a user-visible reply
// to the channel; suppress the post-loop finalText post so we don't
// double-up". Adding a new posting tool to the team-MCP surface
// (e.g. `direct_message_v2`, `thread_reply`) MUST also be added
// here, or the user will see two replies per turn AND the broker
// fan-out cascade will compound across multiple turns.
func TestIsUserVisiblePostTool(t *testing.T) {
	for _, name := range []string{
		"team_broadcast",
		"team_reply",
		"reply",
		"direct_message",
		"broker_post_message",
	} {
		if !isUserVisiblePostTool(name) {
			t.Errorf("isUserVisiblePostTool(%q) = false, want true — duplicate-post regression incoming", name)
		}
	}
	for _, name := range []string{
		"",
		"unknown",
		"team_wiki_read",
		"team_wiki_search",
		"run_lint",
		"resolve_contradiction",
		"claim_task",
	} {
		if isUserVisiblePostTool(name) {
			t.Errorf("isUserVisiblePostTool(%q) = true, want false — read-only/admin tool was misclassified as a user-visible post", name)
		}
	}
}

// TestOpenAICompatToolLoop_OnToolResultFiresPerInvocation locks in the
// callback contract the runner relies on for its `broadcastedThisTurn`
// gate: every successful tool dispatch invokes onToolResult with the
// right name. Without this, a future loop refactor that batches or
// drops result callbacks would silently re-introduce the duplicate-
// post regression.
func TestOpenAICompatToolLoop_OnToolResultFiresPerInvocation(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{
				chunks: []agent.StreamChunk{
					{
						Type:       "tool_use",
						ToolName:   "team_broadcast",
						ToolParams: map[string]any{"channel": "human__planner", "content": "hi"},
						ToolInput:  `{"channel":"human__planner","content":"hi"}`,
					},
				},
			},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "Posted."},
				},
			},
		},
	}
	tool := agent.AgentTool{
		Name: "team_broadcast",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) {
			return "Posted to #human__planner as @planner", nil
		},
	}
	var (
		callbackHits []string
		broadcasted  bool
	)
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{tool},
		toolByName:  map[string]agent.AgentTool{"team_broadcast": tool},
		maxIters:    4,
		toolTimeout: time.Second,
		onToolResult: func(name, _ string, err error) {
			if err != nil {
				return
			}
			callbackHits = append(callbackHits, name)
			// Mirror the runner's gate: any user-visible post tool
			// flips broadcastedThisTurn so post-loop final-text
			// posting gets suppressed.
			if isUserVisiblePostTool(name) {
				broadcasted = true
			}
		},
	}
	final, _, _, streamErr, err := loop.run(context.Background(),
		[]agent.Message{{Role: "user", Content: "x"}})
	if err != nil || streamErr != "" {
		t.Fatalf("unexpected: err=%v streamErr=%q", err, streamErr)
	}
	if len(callbackHits) != 1 || callbackHits[0] != "team_broadcast" {
		t.Errorf("onToolResult hits = %+v, want [team_broadcast]", callbackHits)
	}
	if !broadcasted {
		t.Error("broadcastedThisTurn never flipped — duplicate-post suppression won't trigger in production")
	}
	if final != "Posted." {
		t.Errorf("finalText = %q, want Posted.", final)
	}
}

// TestOpenAICompatToolLoop_OnToolResultDoesNotFlipForReadOnlyTools is
// the negative case: a tool that doesn't post (run_lint, team_wiki_*)
// must NOT flip broadcastedThisTurn, otherwise a turn where the agent
// only ran a wiki-read would silently swallow its final reply.
func TestOpenAICompatToolLoop_OnToolResultDoesNotFlipForReadOnlyTools(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{
				chunks: []agent.StreamChunk{
					{Type: "tool_use", ToolName: "team_wiki_read", ToolParams: map[string]any{"slug": "x"}, ToolInput: `{"slug":"x"}`},
				},
			},
			{
				chunks: []agent.StreamChunk{
					{Type: "text", Content: "Here's what I found: …"},
				},
			},
		},
	}
	tool := agent.AgentTool{
		Name: "team_wiki_read",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) {
			return "wiki body", nil
		},
	}
	var broadcasted bool
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{tool},
		toolByName:  map[string]agent.AgentTool{"team_wiki_read": tool},
		maxIters:    4,
		toolTimeout: time.Second,
		onToolResult: func(name, _ string, err error) {
			if err == nil && isUserVisiblePostTool(name) {
				broadcasted = true
			}
		},
	}
	final, _, _, _, _ := loop.run(context.Background(),
		[]agent.Message{{Role: "user", Content: "x"}})
	if broadcasted {
		t.Fatal("broadcastedThisTurn flipped for a read-only wiki tool — runner will silently drop the agent's reply")
	}
	if final == "" {
		t.Error("loop did not return final text")
	}
}

// TestOpenAICompatToolLoop_OnToolResultErrorDoesNotFlipBroadcasted is
// a defensive case: if the tool dispatch itself fails, broadcasted
// must NOT flip — the runner needs to post finalText so the user
// sees an error message rather than silence. The runner's onToolResult
// already early-returns on err != nil, but this test pins the
// contract.
func TestOpenAICompatToolLoop_OnToolResultErrorDoesNotFlipBroadcasted(t *testing.T) {
	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{chunks: []agent.StreamChunk{
				{Type: "tool_use", ToolName: "team_broadcast", ToolParams: map[string]any{}, ToolInput: "{}"},
			}},
			{chunks: []agent.StreamChunk{{Type: "text", Content: "Sorry, that failed."}}},
		},
	}
	broken := agent.AgentTool{
		Name: "team_broadcast",
		Execute: func(_ map[string]any, _ context.Context, _ func(string)) (string, error) {
			return "", errors.New("broker offline")
		},
	}
	var broadcasted bool
	loop := openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       []agent.AgentTool{broken},
		toolByName:  map[string]agent.AgentTool{"team_broadcast": broken},
		maxIters:    4,
		toolTimeout: time.Second,
		onToolResult: func(name, _ string, err error) {
			if err == nil && isUserVisiblePostTool(name) {
				broadcasted = true
			}
		},
	}
	final, _, _, _, _ := loop.run(context.Background(),
		[]agent.Message{{Role: "user", Content: "x"}})
	if broadcasted {
		t.Error("broadcastedThisTurn flipped on tool error — user would see no reply at all")
	}
	if final == "" {
		t.Error("loop returned empty finalText on tool-error path; user has no explanation")
	}
}
