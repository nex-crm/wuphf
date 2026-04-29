package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/imagegen"
)

// imageGenerateInput is the schema the MCP client sees for image_generate.
type imageGenerateInput struct {
	Prompt         string `json:"prompt"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	ReferenceImage string `json:"reference_image,omitempty"`
}

// registerImageTools wires image_generate (and friends) into the MCP server
// for an artist agent. Only artists get these tools — wiring up every agent
// would add ~6k tokens of schema overhead to no benefit.
func registerImageTools(server *mcp.Server) {
	mcp.AddTool(server,
		readOnlyTool(
			"image_list_providers",
			"List image-generation backends available to this office, with each provider's status (configured / reachable / supports image+video). Use this BEFORE image_generate when you're unsure which backend the human has set up.",
		),
		handleImageListProviders,
	)

	mcp.AddTool(server,
		officeWriteTool(
			"image_generate",
			"Render an image (or video frame) from a text prompt. Pick `provider` deliberately: nano-banana for fast composition + text rendering, higgsfield for cinematic, gpt-image for photoreal hero shots, seedance for video, comfyui for self-hosted control. Returns {image_url, model, duration_ms}. The image_url is BoardRoom-relative (e.g. /artist-files/2026-04-28/abc123.png) and renders inline as a markdown image. Post the result to the channel via team_broadcast formatted as: `![alt](IMAGE_URL)\n\nPrompt: ...\nProvider: ...\nModel: ...` so the iteration is auditable AND visible.",
		),
		handleImageGenerate,
	)
}

func handleImageListProviders(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	statuses := imagegen.AllStatuses(ctx)
	body, err := json.MarshalIndent(map[string]any{"providers": statuses}, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleImageGenerate(ctx context.Context, _ *mcp.CallToolRequest, in imageGenerateInput) (*mcp.CallToolResult, any, error) {
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return toolErrorMsg("prompt is required"), nil, nil
	}

	providerStr := strings.TrimSpace(in.Provider)
	if providerStr == "" {
		providerStr = "nano-banana"
	}
	kind, err := imagegen.ParseKind(providerStr)
	if err != nil {
		return toolErrorMsg(err.Error()), nil, nil
	}

	req := imagegen.Request{
		Prompt:         prompt,
		NegativePrompt: in.NegativePrompt,
		Model:          in.Model,
		Width:          in.Width,
		Height:         in.Height,
		ReferenceImage: in.ReferenceImage,
	}
	res, err := imagegen.Generate(ctx, kind, req)
	if err != nil {
		return toolErrorMsg(fmt.Sprintf("image_generate failed: %v", err)), nil, nil
	}

	body, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

// toolErrorMsg wraps a string into a tool-error result without forcing
// callers to wrap msg in fmt.Errorf at every call site.
func toolErrorMsg(msg string) *mcp.CallToolResult {
	return toolError(fmt.Errorf("%s", msg))
}
