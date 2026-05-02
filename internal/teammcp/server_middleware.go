package teammcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// agentToolEventMiddleware wraps every incoming MCP method so tools/call
// invocations are teed to the broker's per-agent stream. This gives the web
// UI an audit trail of what tool each agent called, with arguments and
// either the result summary or an error — visibility the raw pane capture
// can't provide for agents that do their work through MCP calls.
func agentToolEventMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		toolName, argsJSON := extractToolCallRequest(req)
		if toolName != "" {
			postAgentToolEvent(ctx, resolveSlugOptional(""), "call", toolName, argsJSON, "", "")
		}
		result, err := next(ctx, method, req)
		if toolName != "" {
			phase := "result"
			errStr := ""
			if err != nil {
				phase = "error"
				errStr = err.Error()
			}
			postAgentToolEvent(ctx, resolveSlugOptional(""), phase, toolName, "", summarizeToolResult(result), errStr)
		}
		return result, err
	}
}

func extractToolCallRequest(req mcp.Request) (tool, args string) {
	if req == nil {
		return "", ""
	}
	sr, ok := req.(*mcp.ServerRequest[*mcp.CallToolParamsRaw])
	if !ok || sr == nil || sr.Params == nil {
		return "", ""
	}
	tool = sr.Params.Name
	if len(sr.Params.Arguments) > 0 {
		args = string(sr.Params.Arguments)
	}
	return tool, args
}

func summarizeToolResult(res mcp.Result) string {
	r, ok := res.(*mcp.CallToolResult)
	if !ok || r == nil {
		return ""
	}
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc != nil {
			return tc.Text
		}
	}
	return ""
}

func postAgentToolEvent(ctx context.Context, slug, phase, tool, args, result, errStr string) {
	slug = strings.TrimSpace(slug)
	if slug == "" || tool == "" {
		return
	}
	body := map[string]string{
		"slug":   slug,
		"phase":  phase,
		"tool":   tool,
		"args":   args,
		"result": result,
		"error":  errStr,
	}
	// Fire-and-forget; dropping a log line must never fail a tool call.
	go func() {
		// Ignore errors — the broker might be restarting or unreachable,
		// and an audit-log failure is not worth surfacing to the agent.
		_ = brokerPostJSON(context.Background(), "/agent-tool-event", body, nil)
	}()
	_ = ctx
}
