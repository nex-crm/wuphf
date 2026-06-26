package gbrain

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// withEmbeddingSeams overrides the package-level test seams and restores them
// when the test finishes, so embedding selection can be exercised without a
// real OpenAI key, ollama binary, or gbrain subprocess.
func withEmbeddingSeams(t *testing.T, openAIKey string, ollamaModel string) {
	t.Helper()
	prevKey := selectOpenAIKey
	prevOllama := ollamaEmbeddingModel
	selectOpenAIKey = func() string { return openAIKey }
	ollamaEmbeddingModel = func() string { return ollamaModel }
	t.Cleanup(func() {
		selectOpenAIKey = prevKey
		ollamaEmbeddingModel = prevOllama
	})
}

func TestSelectEmbeddingModelPrefersOpenAI(t *testing.T) {
	withEmbeddingSeams(t, "sk-test", "nomic-embed-text")
	if got := SelectEmbeddingModel(); got != openAIEmbeddingModel {
		t.Fatalf("expected OpenAI to win, got %q", got)
	}
	if !EmbeddingAvailable() {
		t.Fatal("expected EmbeddingAvailable true with OpenAI key")
	}
}

func TestSelectEmbeddingModelFallsBackToOllama(t *testing.T) {
	withEmbeddingSeams(t, "", "nomic-embed-text")
	if got := SelectEmbeddingModel(); got != "ollama:nomic-embed-text" {
		t.Fatalf("expected ollama fallback, got %q", got)
	}
	if !EmbeddingAvailable() {
		t.Fatal("expected EmbeddingAvailable true with ollama model")
	}
}

func TestSelectEmbeddingModelEmptyWhenNoProvider(t *testing.T) {
	// No OpenAI key, no ollama embedder (Anthropic-only world): "" / keyword.
	withEmbeddingSeams(t, "", "")
	if got := SelectEmbeddingModel(); got != "" {
		t.Fatalf("expected empty selection, got %q", got)
	}
	if EmbeddingAvailable() {
		t.Fatal("expected EmbeddingAvailable false with no embedder")
	}
}

func TestParseOllamaEmbeddingModel(t *testing.T) {
	tests := []struct {
		name    string
		listing string
		want    string
	}{
		{
			name:    "prefers nomic and strips latest tag",
			listing: "NAME                    ID    SIZE\nllama3:latest           abc   4GB\nnomic-embed-text:latest def   274MB\n",
			want:    "nomic-embed-text",
		},
		{
			name:    "falls back to any embed model",
			listing: "NAME            ID    SIZE\nmxbai-embed-large:v1 ghi   600MB\n",
			want:    "mxbai-embed-large:v1",
		},
		{
			name:    "none when no embed model",
			listing: "NAME      ID    SIZE\nllama3    abc   4GB\n",
			want:    "",
		},
		{
			name:    "empty listing",
			listing: "",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseOllamaEmbeddingModel(tc.listing); got != tc.want {
				t.Fatalf("parseOllamaEmbeddingModel = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeRunner records the gbrain commands EnsureBrain issues and returns scripted
// output/errors, so idempotency can be asserted without a real gbrain.
type fakeRunner struct {
	calls   [][]string
	out     string
	err     error
	noBrain bool
}

func (f *fakeRunner) run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	// `init` always succeeds in the fake; only `config get` reflects the
	// no-brain state so EnsureBrain's idempotency branch can be exercised.
	if len(args) > 0 && args[0] == "init" {
		return "", nil
	}
	if f.noBrain {
		return "", errors.New("Refusing to read config: No brain configured. Run: gbrain init")
	}
	return f.out, f.err
}

func withRunner(t *testing.T, r *fakeRunner) {
	t.Helper()
	prev := runGBrain
	runGBrain = r.run
	t.Cleanup(func() { runGBrain = prev })
}

func TestBrainConfiguredDetectsMissingBrain(t *testing.T) {
	r := &fakeRunner{noBrain: true}
	withRunner(t, r)
	if BrainConfigured(context.Background()) {
		t.Fatal("expected BrainConfigured false when no brain exists")
	}
}

func TestBrainConfiguredTrueWhenBrainExists(t *testing.T) {
	r := &fakeRunner{out: "openai:text-embedding-3-large"}
	withRunner(t, r)
	if !BrainConfigured(context.Background()) {
		t.Fatal("expected BrainConfigured true when config get succeeds")
	}
}

func TestEnsureBrainIdempotentWhenConfigured(t *testing.T) {
	// Brain already exists: EnsureBrain must NOT init, only read the model.
	r := &fakeRunner{out: "ollama:nomic-embed-text"}
	withRunner(t, r)
	withEmbeddingSeams(t, "", "nomic-embed-text")

	model, err := EnsureBrain(context.Background())
	if err != nil {
		t.Fatalf("EnsureBrain: %v", err)
	}
	if model != "ollama:nomic-embed-text" {
		t.Fatalf("expected existing model returned, got %q", model)
	}
	for _, call := range r.calls {
		if len(call) > 0 && call[0] == "init" {
			t.Fatalf("EnsureBrain must not init an existing brain, ran: %v", r.calls)
		}
	}
}

func TestEnsureBrainInitsWithSelectedModelWhenAbsent(t *testing.T) {
	r := &fakeRunner{noBrain: true}
	withRunner(t, r)
	withEmbeddingSeams(t, "sk-test", "")

	model, err := EnsureBrain(context.Background())
	if err != nil {
		t.Fatalf("EnsureBrain: %v", err)
	}
	if model != openAIEmbeddingModel {
		t.Fatalf("expected selected model %q, got %q", openAIEmbeddingModel, model)
	}
	wantInit := []string{"init", "--pglite", "--embedding-model", openAIEmbeddingModel}
	if !containsCall(r.calls, wantInit) {
		t.Fatalf("expected init call %v, got %v", wantInit, r.calls)
	}
}

func TestEnsureBrainInitsKeywordOnlyWhenNoEmbedder(t *testing.T) {
	r := &fakeRunner{noBrain: true}
	withRunner(t, r)
	withEmbeddingSeams(t, "", "")

	model, err := EnsureBrain(context.Background())
	if err != nil {
		t.Fatalf("EnsureBrain: %v", err)
	}
	if model != "" {
		t.Fatalf("expected empty model for keyword-only, got %q", model)
	}
	wantInit := []string{"init", "--pglite", "--no-embedding"}
	if !containsCall(r.calls, wantInit) {
		t.Fatalf("expected keyword-only init %v, got %v", wantInit, r.calls)
	}
}

func containsCall(calls [][]string, want []string) bool {
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			return true
		}
	}
	return false
}
