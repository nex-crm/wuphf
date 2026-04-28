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
