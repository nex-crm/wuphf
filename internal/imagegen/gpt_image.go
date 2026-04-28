package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenAI Images API:
//   POST https://api.openai.com/v1/images/generations
//   Authorization: Bearer <OPENAI_API_KEY>
//   Body:  { model, prompt, n, size, quality, response_format }
//   Resp:  { created, data: [{ url | b64_json, revised_prompt }] }
//
// We always request response_format="b64_json" so the bytes land in the same
// pipe as Nano Banana — no extra HTTP round-trip and no expiring URL.

const (
	defaultGPTImageBase    = "https://api.openai.com/v1"
	defaultGPTImageModel   = "gpt-image-1"
	envGPTImageAPIKey      = "OPENAI_API_KEY"
	envGPTImageAPIKeyAlt   = "WUPHF_GPT_IMAGE_API_KEY"
	envGPTImageBaseURL     = "WUPHF_GPT_IMAGE_BASE_URL"
	envGPTImageModel       = "WUPHF_GPT_IMAGE_MODEL"
)

type gptImage struct{}

func init() { Register(&gptImage{}) }

func (g *gptImage) Kind() Kind { return KindGPTImage }

func (g *gptImage) Status(_ context.Context) Status {
	apiKey := gptImageAPIKey()
	st := Status{
		Kind:             KindGPTImage,
		Label:            "ChatGPT Image",
		Blurb:            "OpenAI Images API — gpt-image-1/1.5/2 and dall-e-3. Strong photoreal + typography. Synchronous (no polling).",
		BaseURL:          gptImageBaseURL(),
		DefaultModel:     gptImageModel(),
		SupportedModels:  []string{"gpt-image-2", "gpt-image-1.5", "gpt-image-1", "gpt-image-1-mini", "dall-e-3"},
		SupportsImage:    true,
		SupportsVideo:    false,
		NeedsAPIKey:      true,
		APIKeySet:        apiKey != "",
		ImplementationOK: true,
	}
	st.Configured = st.APIKeySet
	st.Reachable = st.APIKeySet
	if !st.APIKeySet {
		st.SetupHint = "Paste an OpenAI API key in Settings → Image generation → ChatGPT Image. Get one at https://platform.openai.com/api-keys"
	}
	return st
}

func (g *gptImage) Generate(ctx context.Context, req Request) (Result, error) {
	apiKey := gptImageAPIKey()
	if apiKey == "" {
		return Result{}, fmt.Errorf("gpt-image: missing API key (set %s)", envGPTImageAPIKey)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = gptImageModel()
	}

	body := map[string]any{
		"model":  model,
		"prompt": req.Prompt,
		"size":   gptImageSize(req.Width, req.Height),
		"n":      1,
	}
	// dall-e-3 doesn't accept response_format=b64_json on the same path —
	// only the gpt-image-* models do. dall-e-3 always returns url. The b64
	// branch keeps the rest of the flow simple; for dall-e-3 we download.
	if !strings.HasPrefix(model, "dall-e") {
		body["response_format"] = "b64_json"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, fmt.Errorf("gpt-image: marshal: %w", err)
	}

	endpoint := strings.TrimRight(gptImageBaseURL(), "/") + "/images/generations"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("gpt-image: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	startedAt := time.Now()
	resp, err := HTTPClientWithTimeout().Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("gpt-image: post: %w", err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("gpt-image: HTTP %d: %s", resp.StatusCode, truncate(string(rawBody), 400))
	}

	var decoded struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		return Result{}, fmt.Errorf("gpt-image: decode: %w", err)
	}
	if decoded.Error.Message != "" {
		return Result{}, fmt.Errorf("gpt-image: %s (%s)", decoded.Error.Message, decoded.Error.Type)
	}
	if len(decoded.Data) == 0 {
		return Result{}, fmt.Errorf("gpt-image: empty data")
	}
	first := decoded.Data[0]

	var saved SaveResult
	switch {
	case first.B64JSON != "":
		saved, err = SavePNG(req.Prompt, []byte(first.B64JSON), true)
	case first.URL != "":
		bs, _, derr := downloadMedia(ctx, first.URL)
		if derr != nil {
			return Result{}, fmt.Errorf("gpt-image: fetch %s: %w", first.URL, derr)
		}
		saved, err = SavePNG(req.Prompt, bs, false)
	default:
		return Result{}, fmt.Errorf("gpt-image: response had neither url nor b64_json")
	}
	if err != nil {
		return Result{}, err
	}

	prompt := req.Prompt
	if first.RevisedPrompt != "" {
		prompt = first.RevisedPrompt
	}
	return Result{
		Provider:   KindGPTImage,
		Model:      model,
		PromptUsed: prompt,
		ImageURL:   saved.HTTPURL,
		MimeType:   "image/png",
		DurationMs: time.Since(startedAt).Milliseconds(),
	}, nil
}

// gptImageSize maps requested width/height to OpenAI's enum. Auto when
// neither is set.
func gptImageSize(w, h int) string {
	if w <= 0 || h <= 0 {
		return "auto"
	}
	r := float64(w) / float64(h)
	switch {
	case r > 1.4:
		return "1536x1024"
	case r > 0.85:
		return "1024x1024"
	default:
		return "1024x1536"
	}
}

func gptImageAPIKey() string {
	for _, env := range []string{envGPTImageAPIKey, envGPTImageAPIKeyAlt} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(configString("gpt-image", "api_key"))
}
func gptImageBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(envGPTImageBaseURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("gpt-image", "base_url")); v != "" {
		return v
	}
	return defaultGPTImageBase
}
func gptImageModel() string {
	if v := strings.TrimSpace(os.Getenv(envGPTImageModel)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("gpt-image", "model")); v != "" {
		return v
	}
	return defaultGPTImageModel
}
