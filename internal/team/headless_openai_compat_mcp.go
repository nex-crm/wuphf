package team

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/agent"
)

// headlessOpenAICompatExecutablePath is overridable for tests.
var headlessOpenAICompatExecutablePath = os.Executable

// connectOpenAICompatMCPBridge spawns `wuphf mcp-team` as a subprocess in the
// same env the claude/opencode runners use, attaches an MCP client, lists the
// registered tools, and returns each as an agent.AgentTool whose Execute
// callback round-trips through the MCP session.
//
// This is the integration point that makes a local OpenAI-compatible runtime
// (mlx-lm, ollama, exo) actually usable as a wuphf agent: the model sees the
// same tools claude/opencode/codex see (claim_task, broker_post_message,
// team_wiki_*, etc.) and can drive the office instead of just chatting.
//
// cleanup must be called when the turn finishes so the subprocess exits.
func (l *Launcher) connectOpenAICompatMCPBridge(
	ctx context.Context, slug string, channel string,
) (tools []agent.AgentTool, cleanup func(), err error) {
	wuphfBin, err := headlessOpenAICompatExecutablePath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve wuphf binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, wuphfBin, "mcp-team")
	cmd.Env = l.buildHeadlessClaudeEnv(slug)
	cmd.Env = setEnvValue(cmd.Env, "WUPHF_HEADLESS_PROVIDER", "openai-compat")
	if ch := strings.TrimSpace(channel); ch != "" {
		cmd.Env = setEnvValue(cmd.Env, "WUPHF_CHANNEL", ch)
	}

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "wuphf-openai-compat-bridge",
		Version: "0.1.0",
	}, nil)

	connectCtx, connectCancel := context.WithTimeout(ctx, 10*time.Second)
	session, err := client.Connect(connectCtx, transport, nil)
	connectCancel()
	if err != nil {
		return nil, nil, fmt.Errorf("connect mcp client: %w", err)
	}

	tools, err = mcpSessionToAgentTools(ctx, session)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}

	cleanup = func() {
		// Best-effort: close the session, then ensure the subprocess exits.
		// Both can fail during normal shutdown (already-dead transport,
		// already-reaped pid) and we don't want to leak that to callers.
		_ = session.Close()
	}
	return tools, cleanup, nil
}

// mcpSessionToAgentTools lists the tools registered on an already-connected
// MCP session and wraps each as an agent.AgentTool whose Execute callback
// round-trips through session.CallTool. Pulled out from
// connectOpenAICompatMCPBridge so unit tests can exercise the conversion
// against an in-memory MCP server (mcp.NewInMemoryTransports) without
// spawning a real subprocess.
//
// Lifetime invariant: the returned AgentTool slice's Execute closures
// capture session, so they remain valid only while the session is open.
// Callers are expected to invoke the matching cleanup func returned by
// connectOpenAICompatMCPBridge before any reference to the AgentTools
// goes out of scope. Today the loop returns before cleanup fires; if a
// future async tool-execution refactor changes that, this contract needs
// an explicit guard.
func mcpSessionToAgentTools(ctx context.Context, session *mcp.ClientSession) ([]agent.AgentTool, error) {
	listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
	listed, err := session.ListTools(listCtx, &mcp.ListToolsParams{})
	listCancel()
	if err != nil {
		return nil, fmt.Errorf("list mcp tools: %w", err)
	}

	tools := make([]agent.AgentTool, 0, len(listed.Tools))
	for _, t := range listed.Tools {
		schema, _ := normalizeMCPInputSchema(t.InputSchema)
		tools = append(tools, agent.AgentTool{
			Name:        t.Name,
			Description: t.Description,
			Schema:      schema,
			Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
				// Pass the caller's ctx straight through — the loop owns
				// the per-call timeout (lp.toolTimeout). Wrapping with a
				// hardcoded inner timeout would silently shadow it.
				result, err := session.CallTool(ctx, &mcp.CallToolParams{
					Name:      t.Name,
					Arguments: params,
				})
				if err != nil {
					return "", fmt.Errorf("mcp call %s: %w", t.Name, err)
				}
				return summarizeMCPCallResult(result), nil
			},
		})
	}
	return tools, nil
}

// normalizeMCPInputSchema turns the SDK's `any`-typed InputSchema into the
// map[string]any wuphf's openai-compat layer expects to forward as
// `function.parameters`. Returns the empty-object fallback when the schema
// isn't a JSON object (defensive — mlx_lm.server rejects requests with a
// non-object parameters field).
func normalizeMCPInputSchema(raw any) (map[string]any, bool) {
	if raw == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, false
	}
	if m, ok := raw.(map[string]any); ok {
		return m, true
	}
	// Round-trip through JSON to coerce typed schema structs into map form.
	data, err := json.Marshal(raw)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, false
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, false
	}
	return m, true
}

// summarizeMCPCallResult flattens an MCP CallToolResult into a string the
// model can read back as a tool result. Concatenates text content, prefixes
// "ERROR: " when IsError is set, and falls back to the JSON-encoded
// StructuredContent if Content is empty.
func summarizeMCPCallResult(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	if result.IsError {
		b.WriteString("ERROR: ")
	}
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc != nil {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			b.WriteString(tc.Text)
		}
	}
	if b.Len() == 0 && result.StructuredContent != nil {
		if data, err := json.Marshal(result.StructuredContent); err == nil {
			b.Write(data)
		}
	}
	return b.String()
}
