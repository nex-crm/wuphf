package team

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

func TestRichArtifactNaturalWorkflowPromptAndToolLoop(t *testing.T) {
	systemPrompt := (&promptBuilder{
		isOneOnOne:  func() bool { return false },
		isFocusMode: func() bool { return false },
		packName:    func() string { return "WUPHF Office" },
		leadSlug:    func() string { return "ceo" },
		members: func() []officeMember {
			return []officeMember{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "pm", Name: "Product Manager"},
			}
		},
		policies:       func() []officePolicy { return nil },
		nameFor:        func(slug string) string { return slug },
		markdownMemory: true,
	}).Build("pm")

	const (
		sourcePath = "agents/pm/notebook/launch-risk.md"
		artifactID = "ra_0123456789abcdef"
	)

	var calls []string
	var broadcast string
	tools := []agent.AgentTool{
		{
			Name:        "notebook_write",
			Description: "Save the durable markdown source note before creating richer visual companions.",
			Execute: func(params map[string]any, _ context.Context, _ func(string)) (string, error) {
				calls = append(calls, "notebook_write")
				if got := fmt.Sprint(params["article_path"]); got != sourcePath {
					return "", fmt.Errorf("article_path=%q, want %q", got, sourcePath)
				}
				return `{"path":"` + sourcePath + `","commit_sha":"abc123"}`, nil
			},
		},
		{
			Name:        "visual_artifact_create",
			Description: "Produce a self-contained HTML article for complex specs, PR reviews, diagrams, reports, and interactive tuning surfaces. The HTML article IS the deliverable; leave source_path empty and do not also call notebook_write for the same content.",
			Execute: func(params map[string]any, _ context.Context, _ func(string)) (string, error) {
				calls = append(calls, "visual_artifact_create")
				if got := fmt.Sprint(params["source_path"]); got != sourcePath {
					return "", fmt.Errorf("source_path=%q, want %q", got, sourcePath)
				}
				html := fmt.Sprint(params["html"])
				for _, want := range []string{"<!doctype html>", "<script>", "Launch Risk Dial"} {
					if !strings.Contains(html, want) {
						return "", fmt.Errorf("html missing %q", want)
					}
				}
				if strings.Contains(html, "https://") || strings.Contains(html, "http://") {
					return "", fmt.Errorf("html must be self-contained, got external URL")
				}
				return `{"artifact":{"id":"` + artifactID + `","source_markdown_path":"` + sourcePath + `"}}`, nil
			},
		},
		{
			Name:        "team_broadcast",
			Description: "Post a concise chat update. If you created an HTML visual artifact, include visual-artifact:ra_... on its own line.",
			Execute: func(params map[string]any, _ context.Context, _ func(string)) (string, error) {
				calls = append(calls, "team_broadcast")
				broadcast = fmt.Sprint(params["content"])
				if !strings.Contains("\n"+broadcast+"\n", "\nvisual-artifact:"+artifactID+"\n") {
					return "", fmt.Errorf("broadcast missing standalone visual-artifact marker: %q", broadcast)
				}
				return "Posted to #general as @pm", nil
			},
		},
	}
	toolByName := map[string]agent.AgentTool{}
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}

	stream := &scriptedStreamFn{
		turns: []scriptedTurn{
			{
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					t.Helper()
					if len(msgs) != 2 {
						t.Fatalf("turn 1 messages=%d, want system+user", len(msgs))
					}
					for _, want := range []string{
						"visual_artifact_create",
						"self-contained HTML article",
						"interactive tuning surfaces",
						"HTML visual artifact",
						"long markdown wall",
					} {
						if !strings.Contains(msgs[0].Content, want) {
							t.Fatalf("system prompt missing natural artifact guidance %q:\n%s", want, msgs[0].Content)
						}
					}
				},
				chunks: []agent.StreamChunk{{
					Type:       "tool_use",
					ToolName:   "notebook_write",
					ToolParams: map[string]any{"article_path": sourcePath, "mode": "create", "content": "# Launch risk\n\nDense source notes."},
					ToolInput:  `{"article_path":"` + sourcePath + `","mode":"create","content":"# Launch risk\n\nDense source notes."}`,
				}},
			},
			{
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					t.Helper()
					last := msgs[len(msgs)-1].Content
					if !strings.Contains(last, `Tool "notebook_write" returned`) || !strings.Contains(last, sourcePath) {
						t.Fatalf("turn 2 missing notebook_write result:\n%s", last)
					}
				},
				chunks: []agent.StreamChunk{{
					Type:     "tool_use",
					ToolName: "visual_artifact_create",
					ToolParams: map[string]any{
						"source_path": sourcePath,
						"title":       "Launch Risk Dial",
						"html":        "<!doctype html><html><body><h1>Launch Risk Dial</h1><input type=\"range\"><script>document.body.dataset.ready='1'</script></body></html>",
					},
					ToolInput: `{"source_path":"` + sourcePath + `","title":"Launch Risk Dial","html":"<!doctype html>..."}`,
				}},
			},
			{
				expectMessages: func(t *testing.T, msgs []agent.Message) {
					t.Helper()
					last := msgs[len(msgs)-1].Content
					if !strings.Contains(last, `Tool "visual_artifact_create" returned`) || !strings.Contains(last, artifactID) {
						t.Fatalf("turn 3 missing artifact create result:\n%s", last)
					}
				},
				chunks: []agent.StreamChunk{{
					Type:       "tool_use",
					ToolName:   "team_broadcast",
					ToolParams: map[string]any{"channel": "general", "content": "I saved the source note and made the launch-risk artifact.\nvisual-artifact:" + artifactID},
					ToolInput:  `{"channel":"general","content":"I saved the source note and made the launch-risk artifact.\nvisual-artifact:` + artifactID + `"}`,
				}},
			},
			{
				chunks: []agent.StreamChunk{{Type: "text", Content: "Done."}},
			},
		},
	}

	final, iterations, _, streamErr, err := (&openAICompatToolLoop{
		streamFn:    stream.fn(t),
		tools:       tools,
		toolByName:  toolByName,
		maxIters:    6,
		toolTimeout: time.Second,
	}).run(context.Background(), []agent.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Turn this launch-risk analysis into a durable note I can review quickly. Use whatever representation fits the work."},
	})
	if err != nil || streamErr != "" {
		t.Fatalf("tool loop failed: err=%v streamErr=%q", err, streamErr)
	}
	if final != "Done." || iterations != 4 {
		t.Fatalf("final=%q iterations=%d, want Done./4", final, iterations)
	}
	if want := []string{"notebook_write", "visual_artifact_create", "team_broadcast"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("tool call sequence=%v, want %v", calls, want)
	}
	if !strings.Contains(broadcast, "visual-artifact:"+artifactID) {
		t.Fatalf("broadcast lost artifact marker: %q", broadcast)
	}
	if !toolListIncludes(stream.turns[0].recordedTools, "visual_artifact_create", "interactive tuning surfaces") {
		t.Fatalf("model did not receive visual_artifact_create tool guidance: %+v", stream.turns[0].recordedTools)
	}
}

func toolListIncludes(tools []agent.AgentTool, name, descriptionFragment string) bool {
	for _, tool := range tools {
		if tool.Name == name && strings.Contains(tool.Description, descriptionFragment) {
			return true
		}
	}
	return false
}
