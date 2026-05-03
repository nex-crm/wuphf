package embedding

// stub.go is a deterministic, hash-based pseudo-embedding provider. It
// is the floor returned by NewDefault when no API key is configured, and
// it is the provider tests use to exercise the cluster + scanner paths
// without making network calls.
//
// CRITICAL: stub vectors are NOT semantically meaningful. Two semantically
// identical sentences with different vocabularies will get unrelated
// vectors. The only guarantee is determinism: the same input string
// produces the same vector across runs and platforms.
//
// The vectors are produced by:
//  1. tokenising the input on non-alphanumeric runes (lowercase),
//  2. hashing each token to a stable [0..stubDim) bucket,
//  3. summing 1.0 into that bucket for every token occurrence,
//  4. L2-normalising the resulting vector.
//
// This is just enough structure that texts which share many tokens score
// higher cosine similarity than texts that don't, which is what the
// notebook scanner's contract expects.

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
)

// stubDim is the fixed dimension of stub vectors. 32 is small enough to
// keep test goroutine memory negligible and large enough that the bucket
// hashes don't collide for the small notebook corpora tests use.
const stubDim = 32

// stubProvider is the concrete type returned by NewStubProvider. It is
// stateless and safe for concurrent use.
type stubProvider struct{}

// NewStubProvider returns the deterministic hash-based provider. Exposed
// (capitalised) so tests in other packages can dependency-inject it
// without paying for a real API call.
func NewStubProvider() Provider { return &stubProvider{} }

// Name is "local-stub" so cache rows + log lines clearly mark stub data.
func (p *stubProvider) Name() string { return "local-stub" }

// Dimension is the fixed vector length.
func (p *stubProvider) Dimension() int { return stubDim }

// Embed tokenises text the same way the notebook scanner does, hashes
// each token into a fixed-dim bucket, sums occurrences, and L2-normalises
// the result. Returns an error on empty text so callers can't smuggle
// a zero-vector through to the cluster layer.
func (p *stubProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("embedding: stub: empty text")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v := make([]float32, stubDim)
	tokens := stubTokenise(text)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		// Tokenless text (e.g. all punctuation) still needs a deterministic
		// vector so callers don't see different errors for empty vs. punct.
		// We seed with a single bucket so the output is non-zero.
		idx := stubBucket(text)
		v[idx] = 1
		return L2Normalise(v), nil
	}
	for _, tok := range tokens {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		idx := stubBucket(tok)
		v[idx]++
	}
	return L2Normalise(v), nil
}

// EmbedBatch runs Embed in a loop. We don't parallelise because the work
// is microseconds — a goroutine pool would cost more than it saves.
func (p *stubProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := p.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// stubTokenise lowercases and splits on non-alphanumeric runes. Mirrors
// tokenizeForCluster in internal/team/notebook_signal_scanner.go so a
// stub-provider scan produces clusters comparable to the Jaccard path.
// Stop-words are intentionally NOT removed here — the cluster scoring
// step handles stop-word noise via cosine threshold tuning.
func stubTokenise(s string) []string {
	var out []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tok := current.String()
		current.Reset()
		if len(tok) <= 1 {
			return
		}
		out = append(out, tok)
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// stubBucket hashes a string into a [0..stubDim) bucket via FNV-1a.
// Stable across Go versions and CPU architectures.
func stubBucket(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() % uint32(stubDim))
}
