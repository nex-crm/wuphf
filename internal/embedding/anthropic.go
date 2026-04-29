package embedding

// anthropic.go is the Voyage AI embedding provider, opt-in via VOYAGE_API_KEY.
// Anthropic does not ship a native embeddings endpoint at time of writing
// (2026-04-28); voyage-3-large is their recommended companion. To avoid
// silently shipping ANTHROPIC_API_KEY to api.voyageai.com (third-party), we
// require an explicit VOYAGE_API_KEY here — see newVoyageProvider.
//
// TODO(embedding): when Anthropic ships native embeddings, add a real
// anthropic provider that uses ANTHROPIC_API_KEY against the Anthropic host.

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultVoyageBaseURL = "https://api.voyageai.com"
	defaultVoyageModel   = "voyage-3-large"
	// defaultVoyageDim is the published vector dimension of voyage-3-large.
	// voyage-3-lite is 512; voyage-code-2 is 1536. Override with
	// WUPHF_EMBEDDING_DIMENSION when switching models.
	defaultVoyageDim = 1024
)

// newVoyageProvider returns a Voyage-backed Provider when VOYAGE_API_KEY is
// set. Returns the stub when the key is empty (caller is responsible for
// only invoking this when they want Voyage).
//
// We deliberately do NOT accept ANTHROPIC_API_KEY as a fallback bearer:
// Voyage is a separate company and shipping the user's Anthropic key to
// api.voyageai.com would be a cross-provider credential leak.
func newVoyageProvider() Provider {
	key := strings.TrimSpace(os.Getenv("VOYAGE_API_KEY"))
	if key == "" {
		return NewStubProvider()
	}

	baseURL := strings.TrimSpace(os.Getenv("VOYAGE_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultVoyageBaseURL
	}

	model := strings.TrimSpace(os.Getenv("WUPHF_EMBEDDING_MODEL"))
	if model == "" {
		model = defaultVoyageModel
	}

	dim := openAIDimensionFromEnv()
	if dim == defaultOpenAIDim {
		// User did not override → use Voyage's default dim, not OpenAI's.
		dim = defaultVoyageDim
	}

	timeout := openAITimeoutFromEnv()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	inner := &openAIProvider{
		apiKey:     key,
		baseURL:    baseURL,
		model:      model,
		dimension:  dim,
		httpClient: &http.Client{Timeout: timeout},
	}
	return &voyageProvider{inner: inner, model: model}
}

// voyageProvider wraps the OpenAI-compat client so Name() returns the
// "voyage-..." prefix. Cache rows + telemetry log this name, so it
// matters that callers can distinguish OpenAI from Voyage.
type voyageProvider struct {
	inner *openAIProvider
	model string
}

// Embed delegates to the OpenAI-compat implementation.
func (p *voyageProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return p.inner.Embed(ctx, text)
}

// EmbedBatch delegates to the OpenAI-compat implementation.
func (p *voyageProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return p.inner.EmbedBatch(ctx, texts)
}

// Dimension returns the underlying provider's vector length.
func (p *voyageProvider) Dimension() int { return p.inner.Dimension() }

// Name is the stable cache + telemetry identifier.
func (p *voyageProvider) Name() string { return "voyage-" + p.model }
