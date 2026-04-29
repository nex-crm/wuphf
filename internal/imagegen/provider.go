// Package imagegen provides a unified interface to image (and video)
// generation backends. The Artist agent dispatches through this package
// rather than calling each provider's API directly, so the same MCP tool
// `image_generate` can route to ComfyUI, Nano Banana, GPT Image, Seedance,
// or Higgsfield based on a `provider` parameter.
//
// Mirrors the pattern in internal/provider/ for LLMs: a per-kind init()
// registers a Provider with the registry; runtime config (api keys, base
// URLs, default models) is resolved via internal/config.
package imagegen

import (
	"context"
	"fmt"
	"strings"
)

// Kind identifies an image-gen backend.
type Kind string

const (
	KindNanoBanana Kind = "nano-banana"
	KindHiggsfield Kind = "higgsfield"
	KindGPTImage   Kind = "gpt-image"
	KindSeedance   Kind = "seedance"
	KindComfyUI    Kind = "comfyui"
)

// AllKinds returns every registered kind in display order.
func AllKinds() []Kind {
	return []Kind{KindNanoBanana, KindHiggsfield, KindGPTImage, KindSeedance, KindComfyUI}
}

// Request is a normalized image-gen request. Each provider takes what it
// supports and ignores the rest. Width/Height are hints; providers may
// snap to the nearest supported aspect ratio.
type Request struct {
	Prompt         string         `json:"prompt"`
	NegativePrompt string         `json:"negative_prompt,omitempty"`
	Model          string         `json:"model,omitempty"`
	Width          int            `json:"width,omitempty"`
	Height         int            `json:"height,omitempty"`
	Seed           int64          `json:"seed,omitempty"`
	NumImages      int            `json:"num_images,omitempty"`
	ReferenceImage string         `json:"reference_image,omitempty"` // URL or base64
	Extra          map[string]any `json:"extra,omitempty"`           // provider-specific
}

// Result is the normalized response. Providers populate ImageURL when the
// backend hosts the result, ImageB64 when they only return inline bytes.
// AgentSlug + Channel are filled by the MCP tool, not the provider.
type Result struct {
	Provider      Kind   `json:"provider"`
	Model         string `json:"model"`
	PromptUsed    string `json:"prompt_used"`
	ImageURL      string `json:"image_url,omitempty"`
	ImageB64      string `json:"image_b64,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	WidthPx       int    `json:"width_px,omitempty"`
	HeightPx      int    `json:"height_px,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	ProviderRefID string `json:"provider_ref_id,omitempty"` // upstream task / job id
}

// Status describes a provider's runtime state for the Settings UI / doctor.
type Status struct {
	Kind             Kind     `json:"kind"`
	Label            string   `json:"label"`
	Blurb            string   `json:"blurb"`
	Reachable        bool     `json:"reachable"`
	Configured       bool     `json:"configured"`
	BaseURL          string   `json:"base_url,omitempty"`
	DefaultModel     string   `json:"default_model,omitempty"`
	SupportedModels  []string `json:"supported_models,omitempty"`
	SupportsImage    bool     `json:"supports_image"`
	SupportsVideo    bool     `json:"supports_video"`
	NeedsAPIKey      bool     `json:"needs_api_key"`
	APIKeySet        bool     `json:"api_key_set"`
	ImplementationOK bool     `json:"implementation_ok"` // false for stubs
	SetupHint        string   `json:"setup_hint,omitempty"`
}

// Provider is the interface every backend implements.
type Provider interface {
	Kind() Kind
	Status(ctx context.Context) Status
	Generate(ctx context.Context, req Request) (Result, error)
}

// ParseKind canonicalises a kind string. Accepts the canonical slug, the
// human label ("Nano Banana"), or common typos ("nanobanana", "comfy ui").
func ParseKind(s string) (Kind, error) {
	k := strings.ToLower(strings.TrimSpace(s))
	k = strings.ReplaceAll(k, " ", "-")
	k = strings.ReplaceAll(k, "_", "-")
	switch k {
	case "nano-banana", "nanobanana", "gemini-image", "gemini":
		return KindNanoBanana, nil
	case "higgsfield":
		return KindHiggsfield, nil
	case "gpt-image", "gpt-image-1", "chatgpt-image", "openai-image", "dalle":
		return KindGPTImage, nil
	case "seedance", "seedance-2", "seedance2":
		return KindSeedance, nil
	case "comfyui", "comfy-ui", "comfy":
		return KindComfyUI, nil
	default:
		return "", fmt.Errorf("unknown image-gen kind %q (valid: %v)", s, AllKinds())
	}
}
