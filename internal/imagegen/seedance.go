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

// Seedance is reached through MuAPI (api.muapi.ai). Pattern:
//   POST /api/v1/<model-endpoint>           x-api-key header
//        body { prompt, aspect_ratio, duration, quality, [images_list] }
//        → { request_id }
//   GET  /api/v1/predictions/{request_id}/result    x-api-key header
//        → { status, outputs: [...], error }
//
// Status enum: pending | processing | completed | failed.
// Output URLs live in `outputs[]` — entries can be raw URL strings OR
// `{outputs: [url, ...]}` dicts (per the muapi-cli source). We walk both.

const (
	defaultSeedanceBase          = "https://api.muapi.ai/api/v1"
	defaultSeedanceModelEndpoint = "seedance-pro-text-to-video"
	envSeedanceAPIKey            = "MUAPI_API_KEY"
	envSeedanceAPIKeyAlt         = "WUPHF_SEEDANCE_API_KEY"
	envSeedanceBaseURL           = "WUPHF_SEEDANCE_BASE_URL"
	envSeedanceModel             = "WUPHF_SEEDANCE_MODEL"
	seedancePollInterval         = 3 * time.Second
	seedancePollTimeout          = 10 * time.Minute // video can run long
)

type seedance struct{}

func init() { Register(&seedance{}) }

func (s *seedance) Kind() Kind { return KindSeedance }

func (s *seedance) Status(_ context.Context) Status {
	apiKey := seedanceAPIKey()
	st := Status{
		Kind:         KindSeedance,
		Label:        "Seedance 2",
		Blurb:        "ByteDance Seedance via MuAPI — text-to-video and image-to-video. Async; we poll predictions/<id>/result every 3s up to 10m.",
		BaseURL:      seedanceBaseURL(),
		DefaultModel: seedanceModel(),
		SupportedModels: []string{
			"seedance-pro-text-to-video",
			"seedance-pro-image-to-video",
			"seedance-lite-text-to-video",
			"seedance-lite-image-to-video",
			"seedance-v2.0-t2v",
			"seedance-v2.0-i2v",
		},
		SupportsImage:    false,
		SupportsVideo:    true,
		NeedsAPIKey:      true,
		APIKeySet:        apiKey != "",
		ImplementationOK: true,
	}
	st.Configured = st.APIKeySet
	st.Reachable = st.APIKeySet
	if !st.APIKeySet {
		st.SetupHint = "Get a key from MuAPI (https://muapi.ai) — Seedance is exposed there. Paste in Settings → Image generation → Seedance 2."
	}
	return st
}

func (s *seedance) Generate(ctx context.Context, req Request) (Result, error) {
	apiKey := seedanceAPIKey()
	if apiKey == "" {
		return Result{}, fmt.Errorf("seedance: missing API key (set %s)", envSeedanceAPIKey)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = seedanceModel()
	}
	model = strings.Trim(model, "/")

	// Body: text-to-video uses `prompt`; image-to-video adds `images_list`.
	body := map[string]any{
		"prompt":       req.Prompt,
		"aspect_ratio": aspectRatioFor(req.Width, req.Height),
		"quality":      "high",
		"duration":     5,
	}
	if req.ReferenceImage != "" {
		body["images_list"] = []string{req.ReferenceImage}
	}
	for k, v := range req.Extra {
		body[k] = v
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, fmt.Errorf("seedance: marshal: %w", err)
	}

	startedAt := time.Now()
	endpoint := strings.TrimRight(seedanceBaseURL(), "/") + "/" + model
	submitReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("seedance: build submit: %w", err)
	}
	submitReq.Header.Set("Content-Type", "application/json")
	submitReq.Header.Set("x-api-key", apiKey)

	submitResp, err := HTTPClientWithTimeout().Do(submitReq)
	if err != nil {
		return Result{}, fmt.Errorf("seedance: submit: %w", err)
	}
	submitBody, _ := io.ReadAll(submitResp.Body)
	_ = submitResp.Body.Close()
	if submitResp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("seedance: submit HTTP %d: %s", submitResp.StatusCode, truncate(string(submitBody), 400))
	}

	var submitDecoded struct {
		RequestID string `json:"request_id"`
		ID        string `json:"id"`
		Status    string `json:"status"`
		Error     struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(submitBody, &submitDecoded); err != nil {
		return Result{}, fmt.Errorf("seedance: decode submit: %w (body: %s)", err, truncate(string(submitBody), 200))
	}
	if submitDecoded.Error.Message != "" {
		return Result{}, fmt.Errorf("seedance: %s", submitDecoded.Error.Message)
	}
	requestID := submitDecoded.RequestID
	if requestID == "" {
		requestID = submitDecoded.ID
	}
	if requestID == "" {
		return Result{}, fmt.Errorf("seedance: submit returned no request_id (body: %s)", truncate(string(submitBody), 200))
	}

	// Poll predictions/<id>/result.
	resultURL, mimeType, err := seedanceWaitForResult(ctx, apiKey, requestID)
	if err != nil {
		return Result{}, err
	}

	// Download bytes.
	bs, fetchedMime, err := downloadMedia(ctx, resultURL)
	if err != nil {
		return Result{}, fmt.Errorf("seedance: fetch %s: %w", resultURL, err)
	}
	if mimeType == "" {
		mimeType = fetchedMime
	}
	saved, err := SavePNG(req.Prompt, bs, false)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Provider:      KindSeedance,
		Model:         model,
		PromptUsed:    req.Prompt,
		ImageURL:      saved.HTTPURL,
		MimeType:      mimeType,
		DurationMs:    time.Since(startedAt).Milliseconds(),
		ProviderRefID: requestID,
	}, nil
}

// seedanceWaitForResult polls /predictions/<id>/result until completed.
// Returns the FIRST output URL from outputs[], plus a guess at mime type.
func seedanceWaitForResult(ctx context.Context, apiKey, requestID string) (string, string, error) {
	pollURL := strings.TrimRight(seedanceBaseURL(), "/") + "/predictions/" + requestID + "/result"
	deadline := time.Now().Add(seedancePollTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(seedancePollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("x-api-key", apiKey)
		resp, err := HTTPClientWithTimeout().Do(req)
		if err != nil {
			return "", "", fmt.Errorf("seedance: poll: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return "", "", fmt.Errorf("seedance: poll HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		// outputs[] is heterogeneous: each entry may be a raw URL string,
		// a {url} dict, or a {outputs: [url, ...]} nested dict.
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return "", "", fmt.Errorf("seedance: decode poll: %w", err)
		}
		status, _ := raw["status"].(string)
		switch strings.ToLower(status) {
		case "completed", "complete", "succeeded", "done":
			urls := extractMuAPIOutputs(raw)
			if len(urls) == 0 {
				return "", "", fmt.Errorf("seedance: completed but outputs empty (body: %s)", truncate(string(body), 200))
			}
			mime := "video/mp4"
			if isImageURL(urls[0]) {
				mime = "image/png"
			}
			return urls[0], mime, nil
		case "failed", "error", "cancelled", "canceled":
			msg := ""
			if errMap, ok := raw["error"].(map[string]any); ok {
				if m, _ := errMap["message"].(string); m != "" {
					msg = m
				}
			}
			if msg == "" {
				msg = fmt.Sprintf("status=%s", status)
			}
			return "", "", fmt.Errorf("seedance: %s", msg)
		}
		// pending / processing / unknown — keep polling.
	}
	return "", "", fmt.Errorf("seedance: poll timed out after %s", seedancePollTimeout)
}

// extractMuAPIOutputs walks outputs[] which may contain string URLs OR
// {outputs:[...]} / {url:...} dicts (matches muapi-cli's _extract_output_urls).
func extractMuAPIOutputs(raw map[string]any) []string {
	out := []string{}
	walk := func(v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				out = append(out, t)
			}
		case map[string]any:
			if u, ok := t["url"].(string); ok && u != "" {
				out = append(out, u)
			}
			if inner, ok := t["outputs"].([]any); ok {
				for _, x := range inner {
					if s, ok := x.(string); ok && s != "" {
						out = append(out, s)
					}
				}
			}
		}
	}
	if outputs, ok := raw["outputs"].([]any); ok {
		for _, o := range outputs {
			walk(o)
		}
	}
	// Some MuAPI responses also drop the URL at top-level `url` or `result.url`.
	if u, ok := raw["url"].(string); ok && u != "" {
		out = append(out, u)
	}
	if result, ok := raw["result"].(map[string]any); ok {
		if u, ok := result["url"].(string); ok && u != "" {
			out = append(out, u)
		}
	}
	return out
}

func isImageURL(u string) bool {
	lower := strings.ToLower(u)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func seedanceAPIKey() string {
	for _, env := range []string{envSeedanceAPIKey, envSeedanceAPIKeyAlt} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(configString("seedance", "api_key"))
}
func seedanceBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(envSeedanceBaseURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("seedance", "base_url")); v != "" {
		return v
	}
	return defaultSeedanceBase
}
func seedanceModel() string {
	if v := strings.TrimSpace(os.Getenv(envSeedanceModel)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("seedance", "model")); v != "" {
		return v
	}
	return defaultSeedanceModelEndpoint
}
