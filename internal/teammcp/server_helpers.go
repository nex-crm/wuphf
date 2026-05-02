package teammcp

import "github.com/modelcontextprotocol/go-sdk/mcp"

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func toolError(err error) *mcp.CallToolResult {
	res := textResult(err.Error())
	res.IsError = true
	return res
}

func truncate(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
}
