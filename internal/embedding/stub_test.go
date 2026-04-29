package embedding

import (
	"context"
	"testing"
)

func TestStubProvider_Deterministic(t *testing.T) {
	p := NewStubProvider()
	ctx := context.Background()
	got1, err := p.Embed(ctx, "deploy prod pipeline smoke tests")
	if err != nil {
		t.Fatalf("first embed: %v", err)
	}
	got2, err := p.Embed(ctx, "deploy prod pipeline smoke tests")
	if err != nil {
		t.Fatalf("second embed: %v", err)
	}
	if len(got1) != len(got2) || len(got1) != stubDim {
		t.Fatalf("dimension: got %d/%d want %d", len(got1), len(got2), stubDim)
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Errorf("non-deterministic at index %d: %v vs %v", i, got1[i], got2[i])
		}
	}
}

func TestStubProvider_DistinctTexts(t *testing.T) {
	p := NewStubProvider()
	ctx := context.Background()
	a, err := p.Embed(ctx, "deploy prod pipeline smoke tests")
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := p.Embed(ctx, "marketing launch eyebrow copy rewrite")
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	identical := true
	for i := range a {
		if a[i] != b[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Error("stub vectors collided across distinct texts — hash buckets too small")
	}
}

func TestStubProvider_EmptyText(t *testing.T) {
	p := NewStubProvider()
	if _, err := p.Embed(context.Background(), ""); err == nil {
		t.Error("empty text should error")
	}
	if _, err := p.Embed(context.Background(), "   "); err == nil {
		t.Error("whitespace-only text should error")
	}
}

func TestStubProvider_BatchPreservesOrder(t *testing.T) {
	p := NewStubProvider()
	texts := []string{"first", "second", "third"}
	out, err := p.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(out) != len(texts) {
		t.Fatalf("len: got %d want %d", len(out), len(texts))
	}
	for i, text := range texts {
		single, err := p.Embed(context.Background(), text)
		if err != nil {
			t.Fatalf("single %d: %v", i, err)
		}
		for j := range single {
			if out[i][j] != single[j] {
				t.Errorf("batch row %d differs at %d: batch=%v single=%v", i, j, out[i][j], single[j])
			}
		}
	}
}

func TestStubProvider_OverlappingTextsClusterTogether(t *testing.T) {
	// The stub is hash-based, but two texts that share the same
	// vocabulary should still produce vectors with a high cosine
	// similarity. This test pins the stub's "good enough for tests"
	// guarantee — if it ever drops, the notebook scanner test stops
	// being a real signal.
	p := NewStubProvider()
	ctx := context.Background()
	a, _ := p.Embed(ctx, "deploy prod pipeline smoke tests toggle flipping")
	b, _ := p.Embed(ctx, "deploy prod pipeline smoke tests toggle flipping today")
	c, _ := p.Embed(ctx, "marketing launch eyebrow copy rewrite founders")
	if Cosine(a, b) < 0.8 {
		t.Errorf("near-identical texts: cosine %v want >=0.8", Cosine(a, b))
	}
	if Cosine(a, c) > 0.5 {
		t.Errorf("dissimilar texts: cosine %v want <=0.5", Cosine(a, c))
	}
}

func TestStubProvider_Metadata(t *testing.T) {
	p := NewStubProvider()
	if p.Name() != "local-stub" {
		t.Errorf("name: got %q want local-stub", p.Name())
	}
	if p.Dimension() != stubDim {
		t.Errorf("dimension: got %d want %d", p.Dimension(), stubDim)
	}
}

func TestNewDefault_FallsBackToStub(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	got := NewDefault()
	if got.Name() != "local-stub" {
		t.Errorf("default with no keys: got %q want local-stub", got.Name())
	}
}

// TestNewDefault_AnthropicOnlyDoesNotLeakToVoyage guards against the
// security blocker that was caught in PR review: an ANTHROPIC_API_KEY
// alone must NOT be silently forwarded to api.voyageai.com as a Voyage
// bearer. Voyage is a separate company and the key is invalid there
// anyway — but more importantly it would be a cross-provider credential
// leak. The user must explicitly set VOYAGE_API_KEY (or OPENAI_API_KEY)
// to enable real embeddings.
func TestNewDefault_AnthropicOnlyDoesNotLeakToVoyage(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-shouldnotleak")
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	got := NewDefault()
	if got.Name() != "local-stub" {
		t.Fatalf("anthropic-only must fall back to stub, got %q", got.Name())
	}
}

// TestNewDefault_VoyageKeyEnablesVoyage confirms the explicit opt-in
// path still works.
func TestNewDefault_VoyageKeyEnablesVoyage(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "voyage-real-key")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	got := NewDefault()
	if got.Name() != "voyage-"+defaultVoyageModel {
		t.Fatalf("voyage key set: got %q want voyage-%s", got.Name(), defaultVoyageModel)
	}
}

// TestEmbeddingDimensionOverride distinguishes "user explicitly set" from
// "default kicked in". The Voyage provider relied on this distinction to
// pick its 1024 default vs. honouring a user-set 1536 (e.g. for
// voyage-code-2). Earlier code compared
// `openAIDimensionFromEnv() == defaultOpenAIDim` (1536), which was true for
// BOTH "unset" and "explicitly 1536", silently clobbering Voyage users on
// 1536-dim models down to 1024.
func TestEmbeddingDimensionOverride(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("WUPHF_EMBEDDING_DIMENSION", "")
		n, ok := embeddingDimensionOverride()
		if ok {
			t.Errorf("unset must return ok=false, got n=%d ok=true", n)
		}
	})
	t.Run("explicit 1536 is honoured", func(t *testing.T) {
		t.Setenv("WUPHF_EMBEDDING_DIMENSION", "1536")
		n, ok := embeddingDimensionOverride()
		if !ok || n != 1536 {
			t.Errorf("explicit 1536 must return ok=true n=1536, got n=%d ok=%v", n, ok)
		}
	})
	t.Run("explicit 512 is honoured", func(t *testing.T) {
		t.Setenv("WUPHF_EMBEDDING_DIMENSION", "512")
		n, ok := embeddingDimensionOverride()
		if !ok || n != 512 {
			t.Errorf("explicit 512: got n=%d ok=%v", n, ok)
		}
	})
	t.Run("garbage falls back to default", func(t *testing.T) {
		t.Setenv("WUPHF_EMBEDDING_DIMENSION", "not-a-number")
		_, ok := embeddingDimensionOverride()
		if ok {
			t.Errorf("garbage must return ok=false")
		}
	})
}

// TestNewVoyageProvider_HonoursExplicitDimension pins the Voyage path: a
// user on voyage-code-2 with WUPHF_EMBEDDING_DIMENSION=1536 must get a
// 1536-dim provider, not 1024. We don't make a real HTTP call; the
// provider's Dimension() reflects what would be sent.
func TestNewVoyageProvider_HonoursExplicitDimension(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "voyage-real")
	t.Setenv("WUPHF_EMBEDDING_MODEL", "voyage-code-2")
	t.Setenv("WUPHF_EMBEDDING_DIMENSION", "1536")
	got := newVoyageProvider()
	if got.Dimension() != 1536 {
		t.Fatalf("voyage with explicit 1536: got dim=%d want 1536", got.Dimension())
	}
}
