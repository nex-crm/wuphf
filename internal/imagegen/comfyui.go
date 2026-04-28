package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ComfyUI protocol (self-hosted, default :8188 — works against any
// ComfyUI instance the operator points us at):
//
//   POST /prompt      body { prompt: <workflow JSON>, extra_data: {api_key_comfy_org: ...} }
//                     → { prompt_id }
//   GET  /history/<prompt_id>   → { <prompt_id>: { outputs: { <node_id>: { images: [{filename, subfolder, type}, ...] } } } }
//   GET  /view?filename=<>&subfolder=<>&type=output    → image bytes
//
// Auth: when calling ComfyUI's cloud-asset routes (api_key_comfy_org), the
// key goes inside the JSON body under extra_data — NOT in headers.
//
// We ship a minimal SDXL text-to-image workflow as the default. Operators
// can override by passing a `workflow` key in Request.Extra (a full
// ComfyUI workflow JSON object) — useful for Flux / ControlNet / multi-LoRA
// rigs without code changes.

const (
	defaultComfyUIBase  = "http://192.168.88.14:8188"
	defaultComfyUIModel = "sd_xl_base_1.0.safetensors"
	envComfyUIBaseURL   = "WUPHF_COMFYUI_BASE_URL"
	envComfyUIAPIKey    = "COMFY_ORG_API_KEY"
	envComfyUIModel     = "WUPHF_COMFYUI_MODEL"

	comfyUIPollInterval = 2 * time.Second
	comfyUIPollTimeout  = 5 * time.Minute
)

type comfyUI struct{}

func init() { Register(&comfyUI{}) }

func (c *comfyUI) Kind() Kind { return KindComfyUI }

func (c *comfyUI) Status(ctx context.Context) Status {
	base := comfyUIBaseURL()
	st := Status{
		Kind:             KindComfyUI,
		Label:            "ComfyUI",
		Blurb:            "Self-hosted ComfyUI — node-based image gen, full workflow control, runs on your own GPU. Default points at .14:8188.",
		BaseURL:          base,
		DefaultModel:     comfyUIModel(),
		SupportedModels:  []string{"sd_xl_base_1.0.safetensors", "flux1-dev.safetensors", "flux1-schnell.safetensors"},
		SupportsImage:    true,
		SupportsVideo:    false,
		NeedsAPIKey:      false, // base ComfyUI doesn't need one
		APIKeySet:        comfyUIOrgKey() != "",
		ImplementationOK: true,
	}
	// Configured = the base URL is reachable. Cheap probe with a short
	// timeout so the Settings panel doesn't block.
	st.Reachable = comfyUIPing(ctx, base)
	st.Configured = st.Reachable
	if !st.Reachable {
		st.SetupHint = "Install ComfyUI on .14 (RTX 5080): `git clone https://github.com/comfyanonymous/ComfyUI && pip install -r requirements.txt && python main.py --listen 0.0.0.0`. Then set base_url in Settings → Image generation → ComfyUI to http://192.168.88.14:8188 (default)."
	}
	return st
}

func (c *comfyUI) Generate(ctx context.Context, req Request) (Result, error) {
	base := comfyUIBaseURL()
	if !comfyUIPing(ctx, base) {
		return Result{}, fmt.Errorf("comfyui: %s is not reachable — install ComfyUI on .14 or update base_url in Settings", base)
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = comfyUIModel()
	}

	// Workflow: prefer caller-supplied (req.Extra["workflow"]); fall back
	// to the embedded default SDXL t2i graph below.
	var workflow map[string]any
	if req.Extra != nil {
		if w, ok := req.Extra["workflow"].(map[string]any); ok && len(w) > 0 {
			workflow = w
		}
	}
	if workflow == nil {
		workflow = defaultSDXLWorkflow(model, req)
	}

	body := map[string]any{
		"prompt": workflow,
	}
	if key := comfyUIOrgKey(); key != "" {
		body["extra_data"] = map[string]any{"api_key_comfy_org": key}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, fmt.Errorf("comfyui: marshal: %w", err)
	}

	startedAt := time.Now()
	endpoint := strings.TrimRight(base, "/") + "/prompt"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("comfyui: build submit: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := HTTPClientWithTimeout().Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("comfyui: submit: %w", err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("comfyui: submit HTTP %d: %s", resp.StatusCode, truncate(string(rawBody), 400))
	}

	var submitDecoded struct {
		PromptID string         `json:"prompt_id"`
		Number   int            `json:"number"`
		NodeErrs map[string]any `json:"node_errors"`
	}
	if err := json.Unmarshal(rawBody, &submitDecoded); err != nil {
		return Result{}, fmt.Errorf("comfyui: decode submit: %w", err)
	}
	if len(submitDecoded.NodeErrs) > 0 {
		return Result{}, fmt.Errorf("comfyui: workflow validation: %s", truncate(string(rawBody), 400))
	}
	if submitDecoded.PromptID == "" {
		return Result{}, fmt.Errorf("comfyui: submit returned no prompt_id")
	}

	// Poll /history/<prompt_id> until outputs appear.
	images, err := comfyUIWaitForOutputs(ctx, base, submitDecoded.PromptID)
	if err != nil {
		return Result{}, err
	}
	if len(images) == 0 {
		return Result{}, fmt.Errorf("comfyui: completed but no images returned")
	}
	first := images[0]

	// Fetch the file via /view.
	q := url.Values{}
	q.Set("filename", first.Filename)
	if first.Subfolder != "" {
		q.Set("subfolder", first.Subfolder)
	}
	if first.Type != "" {
		q.Set("type", first.Type)
	} else {
		q.Set("type", "output")
	}
	viewURL := strings.TrimRight(base, "/") + "/view?" + q.Encode()
	bs, mimeType, err := downloadMedia(ctx, viewURL)
	if err != nil {
		return Result{}, fmt.Errorf("comfyui: fetch %s: %w", viewURL, err)
	}
	saved, err := SavePNG(req.Prompt, bs, false)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Provider:      KindComfyUI,
		Model:         model,
		PromptUsed:    req.Prompt,
		ImageURL:      saved.HTTPURL,
		MimeType:      mimeType,
		DurationMs:    time.Since(startedAt).Milliseconds(),
		ProviderRefID: submitDecoded.PromptID,
	}, nil
}

type comfyImageOutput struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

func comfyUIWaitForOutputs(ctx context.Context, base, promptID string) ([]comfyImageOutput, error) {
	pollURL := strings.TrimRight(base, "/") + "/history/" + promptID
	deadline := time.Now().Add(comfyUIPollTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(comfyUIPollInterval):
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := HTTPClientWithTimeout().Do(req)
		if err != nil {
			return nil, fmt.Errorf("comfyui: poll: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("comfyui: poll HTTP %d", resp.StatusCode)
		}
		// /history/<prompt_id> returns {} until the queue picks it up,
		// then {"<prompt_id>": {"outputs": {<node_id>: {"images": [...]}}, "status": {...}}}.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("comfyui: decode history: %w", err)
		}
		entry, ok := raw[promptID]
		if !ok {
			continue // not yet started
		}
		var item struct {
			Outputs map[string]struct {
				Images []comfyImageOutput `json:"images"`
			} `json:"outputs"`
			Status struct {
				Completed bool   `json:"completed"`
				StatusStr string `json:"status_str"`
			} `json:"status"`
		}
		if err := json.Unmarshal(entry, &item); err != nil {
			return nil, fmt.Errorf("comfyui: decode history entry: %w", err)
		}
		if !item.Status.Completed && len(item.Outputs) == 0 {
			continue
		}
		// Walk every node's images and flatten.
		out := []comfyImageOutput{}
		for _, node := range item.Outputs {
			out = append(out, node.Images...)
		}
		if len(out) > 0 {
			return out, nil
		}
		if item.Status.StatusStr != "" && strings.Contains(strings.ToLower(item.Status.StatusStr), "error") {
			return nil, fmt.Errorf("comfyui: %s", item.Status.StatusStr)
		}
	}
	return nil, fmt.Errorf("comfyui: poll timed out after %s", comfyUIPollTimeout)
}

// defaultSDXLWorkflow returns a minimal text-to-image graph for SDXL.
// It's intentionally small — operators with bigger needs pass their own
// workflow via Request.Extra["workflow"].
func defaultSDXLWorkflow(model string, req Request) map[string]any {
	w := req.Width
	if w <= 0 {
		w = 1024
	}
	h := req.Height
	if h <= 0 {
		h = 1024
	}
	seed := req.Seed
	if seed == 0 {
		seed = time.Now().UnixNano() % 1_000_000_000
	}
	negative := req.NegativePrompt
	if negative == "" {
		negative = "blurry, low quality, watermark"
	}
	return map[string]any{
		"4": map[string]any{
			"class_type": "CheckpointLoaderSimple",
			"inputs":     map[string]any{"ckpt_name": model},
		},
		"5": map[string]any{
			"class_type": "EmptyLatentImage",
			"inputs":     map[string]any{"width": w, "height": h, "batch_size": 1},
		},
		"6": map[string]any{
			"class_type": "CLIPTextEncode",
			"inputs":     map[string]any{"text": req.Prompt, "clip": []any{"4", 1}},
		},
		"7": map[string]any{
			"class_type": "CLIPTextEncode",
			"inputs":     map[string]any{"text": negative, "clip": []any{"4", 1}},
		},
		"3": map[string]any{
			"class_type": "KSampler",
			"inputs": map[string]any{
				"seed":          seed,
				"steps":         28,
				"cfg":           7.0,
				"sampler_name":  "dpmpp_2m",
				"scheduler":     "karras",
				"denoise":       1.0,
				"model":         []any{"4", 0},
				"positive":      []any{"6", 0},
				"negative":      []any{"7", 0},
				"latent_image":  []any{"5", 0},
			},
		},
		"8": map[string]any{
			"class_type": "VAEDecode",
			"inputs":     map[string]any{"samples": []any{"3", 0}, "vae": []any{"4", 2}},
		},
		"9": map[string]any{
			"class_type": "SaveImage",
			"inputs":     map[string]any{"images": []any{"8", 0}, "filename_prefix": "wuphf"},
		},
	}
}

func comfyUIPing(ctx context.Context, base string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(base, "/")+"/system_stats", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 500
}

func comfyUIBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(envComfyUIBaseURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("comfyui", "base_url")); v != "" {
		return v
	}
	return defaultComfyUIBase
}
func comfyUIOrgKey() string {
	if v := strings.TrimSpace(os.Getenv(envComfyUIAPIKey)); v != "" {
		return v
	}
	return strings.TrimSpace(configString("comfyui", "api_key"))
}
func comfyUIModel() string {
	if v := strings.TrimSpace(os.Getenv(envComfyUIModel)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("comfyui", "model")); v != "" {
		return v
	}
	return defaultComfyUIModel
}
