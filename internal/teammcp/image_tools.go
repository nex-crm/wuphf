package teammcp

// image_tools.go registers wiki_image_describe — the on-demand vision alt
// text synthesis tool. Registered only when WUPHF_MEMORY_BACKEND=markdown.
//
// Upload itself is HTTP-multipart from the web UI, not an MCP tool. Agents
// currently cannot upload images; they reference existing ones by path.
// That ordering is deliberate — vision alt-text is safe to expose because
// it only reads + annotates, while uploads would let an agent store
// attacker-supplied bytes without human review.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WikiImageDescribeArgs is the contract for wiki_image_describe.
type WikiImageDescribeArgs struct {
	AssetPath string `json:"asset_path" jsonschema:"Path to the image asset under team/assets/ — e.g. team/assets/202604/ab12cd34ef56-diagram.png"`
	ActorSlug string `json:"actor_slug,omitempty" jsonschema:"Optional actor slug for audit. Defaults to archivist."`
}

// registerImageTools attaches wiki_image_describe. Caller (markdown branch of
// configureServerTools) is responsible for gating on the memory backend.
func registerImageTools(server *mcp.Server) {
	mcp.AddTool(server, officeWriteTool(
		"wiki_image_describe",
		"Request vision-LLM alt-text synthesis for a wiki image. Returns immediately with status=queued; the resulting alt sidecar is committed under the archivist git identity and surfaces via SSE wiki:image_alt_updated.",
	), handleWikiImageDescribe)
}

func handleWikiImageDescribe(ctx context.Context, _ *mcp.CallToolRequest, args WikiImageDescribeArgs) (*mcp.CallToolResult, any, error) {
	path := strings.TrimSpace(args.AssetPath)
	if path == "" {
		return toolError(fmt.Errorf("asset_path is required")), nil, nil
	}
	if !strings.HasPrefix(path, "team/assets/") {
		return toolError(fmt.Errorf("asset_path %q must start with team/assets/", path)), nil, nil
	}
	body := map[string]any{
		"asset_path": path,
	}
	if actor := strings.TrimSpace(args.ActorSlug); actor != "" {
		body["actor_slug"] = actor
	}
	var result struct {
		AssetPath string `json:"asset_path"`
		Status    string `json:"status"`
	}
	if err := brokerPostJSON(ctx, "/wiki/images/describe", body, &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result)
	return textResult(string(payload)), nil, nil
}
