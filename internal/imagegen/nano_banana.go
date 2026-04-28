package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Nano Banana is the codename for Google's gemini-2.5-flash-image model,
// served through the Generative Language API at:
//
//   POST https://generativelanguage.googleapis.com/v1beta/models/<model>:generateContent?key=<API_KEY>
//
// The API takes a list of "contents" parts and returns image bytes inline
// (base64) under candidates[].content.parts[].inlineData.data.

const (
	defaultNanoBananaBase   = "https://generativelanguage.googleapis.com/v1beta"
	defaultNanoBananaModel  = "gemini-2.5-flash-image"
	envNanoBananaAPIKey     = "GOOGLE_AI_STUDIO_API_KEY"
	envNanoBananaAPIKeyAlt  = "WUPHF_NANO_BANANA_API_KEY"
	envNanoBananaBaseURL    = "WUPHF_NANO_BANANA_BASE_URL"
	envNanoBananaModel      = "WUPHF_NANO_BANANA_MODEL"
)

type nanoBanana struct{}

func init() { Register(&nanoBanana{}) }

func (n *nanoBanana) Kind() Kind { return KindNanoBanana }

func (n *nanoBanana) Status(_ context.Context) Status {
	apiKey := nanoBananaAPIKey()
	base := nanoBananaBaseURL()
	model := nanoBananaModel()
	st := Status{
		Kind:             KindNanoBanana,
		Label:            "Nano Banana",
		Blurb:            "Google gemini-2.5-flash-image — fast, cheap, good at composition + text rendering.",
		BaseURL:          base,
		DefaultModel:     model,
		SupportedModels:  []string{"gemini-2.5-flash-image", "gemini-2.5-flash-image-preview"},
		SupportsImage:    true,
		SupportsVideo:    false,
		NeedsAPIKey:      true,
		APIKeySet:        apiKey != "",
		ImplementationOK: true,
	}
	st.Configured = st.APIKeySet
	st.Reachable = st.APIKeySet // light check; real probe happens at first call
	if !st.APIKeySet {
		st.SetupHint = "Paste a Google AI Studio API key in Settings → Image generation → Nano Banana, or export GOOGLE_AI_STUDIO_API_KEY. Get one at https://aistudio.google.com/app/apikey"
	}
	return st
}

func (n *nanoBanana) Generate(ctx context.Context, req Request) (Result, error) {
	apiKey := nanoBananaAPIKey()
	if apiKey == "" {
		return Result{}, fmt.Errorf("nano-banana: missing API key (set %s or paste in Settings)", envNanoBananaAPIKey)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = nanoBananaModel()
	}

	// Build the request body. Generative Language API takes a single role+parts.
	body := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": req.Prompt},
				},
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, fmt.Errorf("nano-banana: marshal: %w", err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		strings.TrimRight(nanoBananaBaseURL(), "/"), model, apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("nano-banana: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	startedAt := time.Now()
	resp, err := HTTPClientWithTimeout().Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("nano-banana: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text       string `json:"text,omitempty"`
					InlineData struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"` // base64
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason,omitempty"`
		} `json:"promptFeedback,omitempty"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Result{}, fmt.Errorf("nano-banana: decode response: %w", err)
	}
	if decoded.Error.Message != "" {
		return Result{}, fmt.Errorf("nano-banana: api error %d %s: %s",
			decoded.Error.Code, decoded.Error.Status, decoded.Error.Message)
	}
	if decoded.PromptFeedback.BlockReason != "" {
		return Result{}, fmt.Errorf("nano-banana: prompt blocked: %s", decoded.PromptFeedback.BlockReason)
	}
	if len(decoded.Candidates) == 0 || len(decoded.Candidates[0].Content.Parts) == 0 {
		return Result{}, fmt.Errorf("nano-banana: empty response from %s", model)
	}

	// Find the first inline image part. Skip any text-only parts.
	for _, part := range decoded.Candidates[0].Content.Parts {
		if part.InlineData.Data == "" {
			continue
		}
		path, err := SavePNG(req.Prompt, []byte(part.InlineData.Data), true)
		if err != nil {
			return Result{}, err
		}
		return Result{
			Provider:   KindNanoBanana,
			Model:      model,
			PromptUsed: req.Prompt,
			ImageURL:   "file://" + path,
			MimeType:   part.InlineData.MimeType,
			DurationMs: time.Since(startedAt).Milliseconds(),
		}, nil
	}
	return Result{}, fmt.Errorf("nano-banana: response had no image parts (text-only?)")
}

// ── env / config resolution ──

func nanoBananaAPIKey() string {
	if v := strings.TrimSpace(os.Getenv(envNanoBananaAPIKey)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envNanoBananaAPIKeyAlt)); v != "" {
		return v
	}
	return strings.TrimSpace(configString("nano-banana", "api_key"))
}

func nanoBananaBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(envNanoBananaBaseURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("nano-banana", "base_url")); v != "" {
		return v
	}
	return defaultNanoBananaBase
}

func nanoBananaModel() string {
	if v := strings.TrimSpace(os.Getenv(envNanoBananaModel)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("nano-banana", "model")); v != "" {
		return v
	}
	return defaultNanoBananaModel
}
