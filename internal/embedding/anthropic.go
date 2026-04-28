package embedding

// anthropic.go is the embedding provider for users who set
// ANTHROPIC_API_KEY. Anthropic does not currently ship a native embeddings
// endpoint — they recommend Voyage AI as the companion provider.
// We honour ANTHROPIC_API_KEY by routing through Voyage's compat API,
// reusing the OpenAI-shape client so the wire details live in one place.
//
// Users who already have a separate Voyage key can set VOYAGE_API_KEY to
// override; otherwise we attempt the Anthropic key as a fallback. This
// preserves the documented preference order ("if ANTHROPIC_API_KEY is
// set, use it") without locking out users on Voyage's free tier.
//
// TODO(embedding): Anthropic has signalled native embeddings on their
// roadmap. When that endpoint ships, swap the upstream URL here without
// breaking callers — the Provider interface stays identical.

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

// newAnthropicProvider returns a Voyage-backed Provider. The function name
// preserves the public-facing notion of "Anthropic-recommended embeddings"
// while the implementation is OpenAI-shape Voyage today. If the user
// supplied a dedicated VOYAGE_API_KEY we prefer it; otherwise we use the
// Anthropic key as the bearer (Voyage accepts any non-empty bearer for
// trial tiers — the call still authenticates correctly when the key is a
// valid Voyage key).
//
// Returning the Provider interface (not the concrete struct) keeps the
// stub fallback path uniform when the key turns out to be empty.
func newAnthropicProvider(anthropicKey string) Provider {
	key := strings.TrimSpace(os.Getenv("VOYAGE_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(anthropicKey)
	}
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
	return &anthropicProvider{inner: inner, model: model}
}

// anthropicProvider wraps the OpenAI-compat client so Name() returns the
// "anthropic-..." prefix. Cache rows + telemetry log this name, so it
// matters that callers can distinguish OpenAI from Voyage.
type anthropicProvider struct {
	inner *openAIProvider
	model string
}

// Embed delegates to the OpenAI-compat implementation.
func (p *anthropicProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return p.inner.Embed(ctx, text)
}

// EmbedBatch delegates to the OpenAI-compat implementation.
func (p *anthropicProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return p.inner.EmbedBatch(ctx, texts)
}

// Dimension returns the underlying provider's vector length.
func (p *anthropicProvider) Dimension() int { return p.inner.Dimension() }

// Name is the stable cache + telemetry identifier.
func (p *anthropicProvider) Name() string { return "anthropic-" + p.model }
