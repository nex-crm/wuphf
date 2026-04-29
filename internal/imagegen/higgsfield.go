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

// Higgsfield API: https://docs.higgsfield.ai
//
// Auth:  Authorization: Key <api_key>:<api_key_secret>
// Base:  https://platform.higgsfield.ai
// Submit:  POST /{model_id}            body {prompt, aspect_ratio, resolution, ...}
// Status:  GET  /requests/{id}/status
// Cancel:  POST /requests/{id}/cancel
//
// Response shape (immediate or after polling):
//   {
//     "status": "completed",
//     "request_id": "...",
//     "status_url":  "...",
//     "cancel_url":  "...",
//     "images": [{"url": "..."}],
//     "video":  {"url": "..."}
//   }
//
// Some renders complete sync (the response is already status=completed);
// longer renders return status=queued and require polling status_url
// until completed.

const (
	defaultHiggsfieldBase    = "https://platform.higgsfield.ai"
	defaultHiggsfieldModelID = "higgsfield-ai/soul/standard"
	envHiggsfieldAPIKey      = "HIGGSFIELD_API_KEY"
	envHiggsfieldBaseURL     = "WUPHF_HIGGSFIELD_BASE_URL"
	envHiggsfieldModel       = "WUPHF_HIGGSFIELD_MODEL"

	higgsfieldPollInterval = 4 * time.Second
	higgsfieldPollTimeout  = 5 * time.Minute
)

type higgsfield struct{}

func init() { Register(&higgsfield{}) }

func (h *higgsfield) Kind() Kind { return KindHiggsfield }

func (h *higgsfield) Status(_ context.Context) Status {
	apiKey := higgsfieldAPIKey()
	st := Status{
		Kind:             KindHiggsfield,
		Label:            "Higgsfield",
		Blurb:            "Higgsfield Soul + cinematic video. Auth uses paired key:secret on the platform.higgsfield.ai endpoint.",
		BaseURL:          higgsfieldBaseURL(),
		DefaultModel:     higgsfieldModelID(),
		SupportedModels:  []string{"higgsfield-ai/soul/standard", "higgsfield-ai/soul/pro"},
		SupportsImage:    true,
		SupportsVideo:    true,
		NeedsAPIKey:      true,
		APIKeySet:        apiKey != "",
		ImplementationOK: true,
	}
	st.Configured = st.APIKeySet
	st.Reachable = st.APIKeySet
	if !st.APIKeySet {
		st.SetupHint = `Paste your Higgsfield credential in Settings → Image generation → Higgsfield as "<api_key>:<api_key_secret>" (one string with the colon — that's the literal Auth header value Higgsfield expects). Get the key pair at https://higgsfield.ai/account/api.`
	} else if !strings.Contains(apiKey, ":") {
		st.Configured = false
		st.SetupHint = `Higgsfield key looks malformed — it must be "<api_key>:<api_key_secret>" (with a literal colon).`
	}
	return st
}

// higgsfieldResponse is the shape returned by both the submit POST and the
// status GET. Decoded loosely so a partial / progressive response (e.g. only
// status field populated mid-render) doesn't error out.
type higgsfieldResponse struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	StatusURL string `json:"status_url"`
	CancelURL string `json:"cancel_url"`
	Images    []struct {
		URL string `json:"url"`
	} `json:"images"`
	Video struct {
		URL string `json:"url"`
	} `json:"video"`
	// Higgsfield surfaces errors via either an explicit `error` object or
	// HTTP-level status. Decode both so the caller gets a useful message.
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func (h *higgsfield) Generate(ctx context.Context, req Request) (Result, error) {
	apiKey := higgsfieldAPIKey()
	if apiKey == "" {
		return Result{}, fmt.Errorf("higgsfield: missing API key (set %s or paste in Settings as key:secret)", envHiggsfieldAPIKey)
	}
	if !strings.Contains(apiKey, ":") {
		return Result{}, fmt.Errorf("higgsfield: API key must be in the format \"<api_key>:<api_key_secret>\" (with a literal colon)")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = higgsfieldModelID()
	}
	model = strings.Trim(model, "/")

	// Build the request body.
	body := map[string]any{
		"prompt": req.Prompt,
	}
	if req.NegativePrompt != "" {
		body["negative_prompt"] = req.NegativePrompt
	}
	body["aspect_ratio"] = aspectRatioFor(req.Width, req.Height)
	body["resolution"] = resolutionFor(req.Width, req.Height)
	for k, v := range req.Extra {
		body[k] = v
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, fmt.Errorf("higgsfield: marshal: %w", err)
	}

	endpoint := strings.TrimRight(higgsfieldBaseURL(), "/") + "/" + model
	startedAt := time.Now()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("higgsfield: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Key "+apiKey)

	resp, err := HTTPClientWithTimeout().Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("higgsfield: post: %w", err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("higgsfield: HTTP %d on %s: %s", resp.StatusCode, endpoint, truncate(string(rawBody), 400))
	}

	var decoded higgsfieldResponse
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		return Result{}, fmt.Errorf("higgsfield: decode submit response: %w (body: %s)", err, truncate(string(rawBody), 200))
	}

	// If submit didn't return completed inline, poll status_url until
	// completed or the poll budget runs out.
	if !isCompleted(decoded.Status) {
		decoded, err = pollHiggsfield(ctx, apiKey, &decoded)
		if err != nil {
			return Result{}, err
		}
	}

	if decoded.Error.Message != "" {
		return Result{}, fmt.Errorf("higgsfield: %s (%s)", decoded.Error.Message, decoded.Error.Code)
	}

	// Pick image first; fall back to video if this is a video render.
	mediaURL := ""
	switch {
	case len(decoded.Images) > 0 && decoded.Images[0].URL != "":
		mediaURL = decoded.Images[0].URL
	case decoded.Video.URL != "":
		mediaURL = decoded.Video.URL
	}
	if mediaURL == "" {
		return Result{}, fmt.Errorf("higgsfield: response had no image or video URL (status=%q)", decoded.Status)
	}

	// Download the bytes so the BoardRoom serves them locally.
	bytesData, mimeType, err := downloadMedia(ctx, mediaURL)
	if err != nil {
		return Result{}, fmt.Errorf("higgsfield: fetch %s: %w", mediaURL, err)
	}
	saved, err := SavePNG(req.Prompt, bytesData, false)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Provider:      KindHiggsfield,
		Model:         model,
		PromptUsed:    req.Prompt,
		ImageURL:      saved.HTTPURL,
		MimeType:      mimeType,
		DurationMs:    time.Since(startedAt).Milliseconds(),
		ProviderRefID: decoded.RequestID,
	}, nil
}

// pollHiggsfield walks status_url until the render is completed or fails.
func pollHiggsfield(ctx context.Context, apiKey string, last *higgsfieldResponse) (higgsfieldResponse, error) {
	statusURL := last.StatusURL
	if statusURL == "" {
		// Synthesize from request_id if the API didn't provide it.
		if last.RequestID == "" {
			return *last, fmt.Errorf("higgsfield: status %q with no request_id or status_url to poll", last.Status)
		}
		statusURL = strings.TrimRight(higgsfieldBaseURL(), "/") + "/requests/" + last.RequestID + "/status"
	}

	deadline := time.Now().Add(higgsfieldPollTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return *last, ctx.Err()
		case <-time.After(higgsfieldPollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return *last, fmt.Errorf("higgsfield: build status req: %w", err)
		}
		req.Header.Set("Authorization", "Key "+apiKey)

		resp, err := HTTPClientWithTimeout().Do(req)
		if err != nil {
			return *last, fmt.Errorf("higgsfield: status fetch: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return *last, fmt.Errorf("higgsfield: status HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		var next higgsfieldResponse
		if err := json.Unmarshal(body, &next); err != nil {
			return *last, fmt.Errorf("higgsfield: decode status response: %w", err)
		}
		if isCompleted(next.Status) {
			return next, nil
		}
		if isFailed(next.Status) {
			msg := next.Error.Message
			if msg == "" {
				msg = next.Detail
			}
			if msg == "" {
				msg = "render failed"
			}
			return next, fmt.Errorf("higgsfield: %s (status=%q)", msg, next.Status)
		}
		// otherwise still queued/running — keep polling.
	}
	return *last, fmt.Errorf("higgsfield: poll timed out after %s (last status=%q)", higgsfieldPollTimeout, last.Status)
}

func isCompleted(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "completed", "complete", "succeeded", "success", "done":
		return true
	}
	return false
}

func isFailed(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "failed", "error", "cancelled", "canceled":
		return true
	}
	return false
}

// downloadMedia GETs an absolute URL and returns the raw bytes + content type.
// Used for pulling the rendered image off Higgsfield's CDN.
func downloadMedia(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := HTTPClientWithTimeout().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	mt := resp.Header.Get("Content-Type")
	if mt == "" {
		mt = "image/png"
	}
	return body, mt, nil
}

// aspectRatioFor maps requested width/height to one of Higgsfield's
// supported aspect ratios. Defaults to 16:9 when neither is set.
func aspectRatioFor(w, h int) string {
	if w <= 0 || h <= 0 {
		return "16:9"
	}
	r := float64(w) / float64(h)
	switch {
	case r > 1.6:
		return "16:9"
	case r > 1.2:
		return "4:3"
	case r > 0.85:
		return "1:1"
	case r > 0.65:
		return "3:4"
	default:
		return "9:16"
	}
}

// resolutionFor maps the larger of width/height to Higgsfield's resolution
// enum. Default 720p (free-tier safe).
func resolutionFor(w, h int) string {
	pixels := w
	if h > pixels {
		pixels = h
	}
	switch {
	case pixels >= 1920:
		return "1080p"
	case pixels >= 1280:
		return "720p"
	default:
		return "720p"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ── env / config resolution ──

func higgsfieldAPIKey() string {
	if v := strings.TrimSpace(os.Getenv(envHiggsfieldAPIKey)); v != "" {
		return v
	}
	return strings.TrimSpace(configString("higgsfield", "api_key"))
}

func higgsfieldBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(envHiggsfieldBaseURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("higgsfield", "base_url")); v != "" {
		return v
	}
	return defaultHiggsfieldBase
}

func higgsfieldModelID() string {
	if v := strings.TrimSpace(os.Getenv(envHiggsfieldModel)); v != "" {
		return v
	}
	if v := strings.TrimSpace(configString("higgsfield", "model")); v != "" {
		return v
	}
	return defaultHiggsfieldModelID
}
