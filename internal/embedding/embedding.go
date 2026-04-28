// Package embedding provides a pluggable text-embedding interface used by
// Stage B notebook semantic clustering. The interface is intentionally small
// (Embed / EmbedBatch / Dimension / Name) so callers can swap providers
// without changing call sites.
//
// # Provider selection
//
// NewDefault inspects the environment and returns the highest-priority
// provider that is configured. Order:
//  1. ANTHROPIC_API_KEY → Voyage AI (Anthropic does not ship a native
//     embeddings API today; voyage-3-large is the recommended companion).
//     This currently routes through the OpenAI-compatible Voyage endpoint.
//  2. OPENAI_API_KEY → OpenAI text-embedding-3-small (1536 dims). Works
//     against any OpenAI-compatible endpoint via OPENAI_BASE_URL.
//  3. else → local stub provider (deterministic hash-based pseudo-vectors,
//     32 dims). Stub vectors are NOT semantically meaningful — they only
//     guarantee determinism for tests / dev environments without API keys.
//
// # Cost model
//
// At time of writing (2026-04-28):
//
//	OpenAI text-embedding-3-small  : $0.02 / 1M tokens (≈ $0.00000002 / token)
//	Voyage voyage-3-large          : $0.18 / 1M tokens
//
// We approximate token count as runes / 4. Real tokenisers will diverge,
// but the figure is good enough for telemetry — the goal is "did we just
// spend a dollar" awareness, not invoice-grade accounting.
//
// # Cache
//
// Embeddings are stable for a (text, model) pair, so callers should cache.
// The package ships a JSONL append-only cache (Cache in cache.go) keyed by
// SHA-256 of the input text. Default cache file is
//
//	$WUPHF_HOME/.wuphf/cache/embeddings.jsonl
//
// (where $WUPHF_HOME is resolved via internal/config.RuntimeHomeDir).
//
// # Determinism
//
// Each provider returns L2-normalised vectors so cosine similarity is the
// dot product. This keeps the clustering layer simple and avoids
// per-comparison normalisation.
package embedding

import (
	"context"
	"math"
	"os"
	"strings"
)

// Provider returns a vector for a given text. Stateless — embeddings are
// not cached at this layer; callers cache per their needs (see Cache in
// cache.go).
type Provider interface {
	// Embed returns a unit-length vector for text. Errors on empty text
	// or provider failure. Implementations must respect ctx.Done().
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch is a batched form. Implementations may parallelise or
	// call Embed in a loop. The output slice must match the input slice
	// length and ordering.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension is the fixed length of returned vectors. Used for sanity
	// checks at the cluster layer and to size cache rows.
	Dimension() int

	// Name is a stable identifier used for logging and the cache "model"
	// field. Examples: "anthropic-voyage-3-large", "openai-text-embedding-3-small",
	// "local-stub".
	Name() string
}

// Cosine returns the cosine similarity of two vectors. Both must be the
// same length. Returns 1.0 for identical, 0.0 for orthogonal, -1.0 for
// opposite. Returns 0 when either side is empty or lengths differ.
//
// Implementations of Provider return unit-length vectors, so this reduces
// to the dot product. We still divide by the L2 norms here so the helper
// is correct on hand-crafted inputs (e.g. tests).
func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// L2Normalise returns a copy of v scaled so its Euclidean norm is 1. A
// zero vector is returned as-is (no division by zero). Used by every
// provider so callers get unit vectors regardless of upstream conventions.
func L2Normalise(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		out := make([]float32, len(v))
		copy(out, v)
		return out
	}
	norm := math.Sqrt(sum)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

// NewDefault returns a Provider based on environment variables. See the
// package doc comment for selection order. The returned provider is safe
// for concurrent use.
//
// NewDefault never returns nil — the stub provider is the floor.
func NewDefault() Provider {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return newAnthropicProvider(k)
	}
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return newOpenAIProvider(k)
	}
	return NewStubProvider()
}
