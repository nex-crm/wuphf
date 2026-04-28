package embedding

// openai.go is an OpenAI-compatible embeddings client. It works against
// OpenAI's hosted API (default), Azure OpenAI, Mistral's compat shim, and
// any local OpenAI-compat server (mlx-lm, llama.cpp, vLLM, …) by setting
// OPENAI_BASE_URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com"
	defaultOpenAIModel   = "text-embedding-3-small"
	// defaultOpenAIDim is the published vector dimension of
	// text-embedding-3-small. Larger models (text-embedding-3-large) use
	// 3072 dims; we pin the small model by default for cost/perf.
	defaultOpenAIDim     = 1536
	defaultOpenAITimeout = 30 * time.Second
)

// openAIProvider is the canonical OpenAI-compatible embeddings client.
// Concurrent-safe: the http.Client is internally locked and there is no
// mutable state beyond construction-time fields.
type openAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client
}

// newOpenAIProvider constructs a provider configured from environment
// variables:
//
//	OPENAI_API_KEY            — bearer token (required; empty key falls back to stub)
//	OPENAI_BASE_URL           — overrides the API host (e.g. http://localhost:8080)
//	WUPHF_EMBEDDING_MODEL     — overrides the model (default text-embedding-3-small)
//	WUPHF_EMBEDDING_DIMENSION — overrides the expected dimension (default 1536)
//	WUPHF_EMBEDDING_TIMEOUT   — Go duration string (default 30s)
func newOpenAIProvider(apiKey string) Provider {
	if strings.TrimSpace(apiKey) == "" {
		return NewStubProvider()
	}
	return &openAIProvider{
		apiKey:     apiKey,
		baseURL:    openAIBaseURLFromEnv(),
		model:      openAIModelFromEnv(),
		dimension:  openAIDimensionFromEnv(),
		httpClient: &http.Client{Timeout: openAITimeoutFromEnv()},
	}
}

// Name is a stable identifier of the form "openai-<model>" so cache rows
// stay valid across model changes.
func (p *openAIProvider) Name() string { return "openai-" + p.model }

// Dimension returns the expected vector length. Mismatches between the
// configured dimension and a returned vector raise an error from Embed so
// the cluster layer's sanity checks fire.
func (p *openAIProvider) Dimension() int { return p.dimension }

// Embed sends a single-text request and returns a unit-normalised vector.
// OpenAI documents that text-embedding-3-* already returns unit vectors;
// we re-normalise defensively in case a compat server lies.
func (p *openAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("embedding: empty text")
	}
	out, err := p.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, fmt.Errorf("embedding: openai: expected 1 vector got %d", len(out))
	}
	return out[0], nil
}

// EmbedBatch posts every input in a single request. OpenAI's embeddings
// endpoint accepts up to 2048 inputs per call; we don't shard here —
// callers control batch size.
func (p *openAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			return nil, fmt.Errorf("embedding: empty text at index %d", i)
		}
	}
	return p.embedBatch(ctx, texts)
}

// openAIRequest is the wire format for POST /v1/embeddings. We always send
// the model so an upstream can reject unsupported choices with a 400
// rather than silently returning a different model's vectors.
type openAIRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
}

// openAIResponse mirrors the OpenAI compat response. We tolerate trailing
// fields by keeping json:"-"-friendly tags only on the bits we read.
type openAIResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// embedBatch performs the round-trip. It validates dimensions, re-orders
// the response by index (just in case the server returns out of order),
// and L2-normalises every output.
func (p *openAIProvider) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(openAIRequest{
		Model:          p.model,
		Input:          texts,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("embedding: openai: marshal: %w", err)
	}

	endpoint := strings.TrimRight(p.baseURL, "/") + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: openai: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embedding: openai: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embedding: openai: decode: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: openai: response length mismatch (got %d want %d)", len(parsed.Data), len(texts))
	}

	out := make([][]float32, len(texts))
	for _, row := range parsed.Data {
		if row.Index < 0 || row.Index >= len(texts) {
			return nil, fmt.Errorf("embedding: openai: out-of-range index %d", row.Index)
		}
		if len(row.Embedding) != p.dimension {
			return nil, fmt.Errorf("embedding: openai: dimension mismatch (got %d want %d)", len(row.Embedding), p.dimension)
		}
		out[row.Index] = L2Normalise(row.Embedding)
	}
	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("embedding: openai: missing vector at index %d", i)
		}
	}
	return out, nil
}

// ── env helpers ───────────────────────────────────────────────────────────

func openAIBaseURLFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); v != "" {
		return v
	}
	return defaultOpenAIBaseURL
}

func openAIModelFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_EMBEDDING_MODEL")); v != "" {
		return v
	}
	return defaultOpenAIModel
}

func openAIDimensionFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("WUPHF_EMBEDDING_DIMENSION"))
	if raw == "" {
		return defaultOpenAIDim
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return defaultOpenAIDim
	}
	return n
}

func openAITimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_EMBEDDING_TIMEOUT"))
	if raw == "" {
		return defaultOpenAITimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultOpenAITimeout
	}
	return d
}
