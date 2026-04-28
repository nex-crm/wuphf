package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withOpenAIServer spins up a controllable httptest server that mimics
// the OpenAI embeddings API. The handler closure can override the
// default response by returning a non-empty body / status; otherwise the
// test gets a 200 with a synthetic 1536-dim vector per input.
func withOpenAIServer(t *testing.T, handler func(r *http.Request) (status int, body any)) (*httptest.Server, *openAIProvider) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		status, body := handler(r)
		w.Header().Set("Content-Type", "application/json")
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)

	provider := &openAIProvider{
		apiKey:     "test-key",
		baseURL:    server.URL,
		model:      "text-embedding-3-small",
		dimension:  defaultOpenAIDim,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	return server, provider
}

// fixedVector returns a synthetic 1536-dim unit vector with `at` set to
// 1.0 and the rest zero. Cheap way to produce distinguishable rows in
// the canned response.
func fixedVector(at int) []float32 {
	v := make([]float32, defaultOpenAIDim)
	if at >= 0 && at < len(v) {
		v[at] = 1.0
	}
	return v
}

func TestOpenAIProvider_ParsesCannedResponse(t *testing.T) {
	_, provider := withOpenAIServer(t, func(_ *http.Request) (int, any) {
		return http.StatusOK, openAIResponse{
			Model: "text-embedding-3-small",
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Index: 0, Embedding: fixedVector(0)},
			},
		}
	})

	got, err := provider.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(got) != defaultOpenAIDim {
		t.Errorf("dim: got %d want %d", len(got), defaultOpenAIDim)
	}
	if got[0] == 0 {
		t.Error("expected first slot to be non-zero (distinguishable)")
	}
}

func TestOpenAIProvider_AuthHeader(t *testing.T) {
	var seen string
	_, provider := withOpenAIServer(t, func(r *http.Request) (int, any) {
		seen = r.Header.Get("Authorization")
		return http.StatusOK, openAIResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{{Index: 0, Embedding: fixedVector(0)}},
		}
	})

	if _, err := provider.Embed(context.Background(), "hello"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if seen != "Bearer test-key" {
		t.Errorf("auth header: got %q want %q", seen, "Bearer test-key")
	}
}

func TestOpenAIProvider_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Block long enough that the context-supplied deadline fires.
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	provider := &openAIProvider{
		apiKey:     "test-key",
		baseURL:    server.URL,
		model:      "text-embedding-3-small",
		dimension:  defaultOpenAIDim,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := provider.Embed(ctx, "hello")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected context-related error, got %v", err)
	}
}

func TestOpenAIProvider_NonOKStatus(t *testing.T) {
	_, provider := withOpenAIServer(t, func(_ *http.Request) (int, any) {
		return http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "bad key"}}
	})

	_, err := provider.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got %v", err)
	}
}

func TestOpenAIProvider_DimensionMismatch(t *testing.T) {
	_, provider := withOpenAIServer(t, func(_ *http.Request) (int, any) {
		return http.StatusOK, openAIResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{{Index: 0, Embedding: []float32{0.1, 0.2}}}, // wrong dim
		}
	})

	_, err := provider.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on dimension mismatch")
	}
	if !strings.Contains(err.Error(), "dimension") {
		t.Errorf("expected dimension error, got %v", err)
	}
}

func TestOpenAIProvider_BatchPreservesOrder(t *testing.T) {
	_, provider := withOpenAIServer(t, func(r *http.Request) (int, any) {
		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// Server returns the data out of order to verify the client
		// re-orders by index.
		data := []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			{Index: 1, Embedding: fixedVector(2)},
			{Index: 0, Embedding: fixedVector(1)},
		}
		return http.StatusOK, openAIResponse{Data: data}
	})

	got, err := provider.EmbedBatch(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2", len(got))
	}
	if got[0][1] == 0 || got[1][2] == 0 {
		t.Error("batch results were not re-ordered by index")
	}
}

func TestOpenAIProvider_EmptyText(t *testing.T) {
	provider := &openAIProvider{apiKey: "k", model: "m", dimension: defaultOpenAIDim, httpClient: &http.Client{}}
	if _, err := provider.Embed(context.Background(), ""); err == nil {
		t.Error("empty text should error before HTTP")
	}
}

func TestOpenAIProvider_RealEnvSkipped(t *testing.T) {
	// Ensures we don't accidentally hit the live API in unit-test mode.
	// If OPENAI_API_KEY is absent (the default in CI), this test is a
	// pure skip — it documents the contract.
	if k := strings.TrimSpace(envGet("OPENAI_API_KEY")); k != "" {
		t.Skip("OPENAI_API_KEY is set: a separate live-integration test should cover the real endpoint")
	}
	if got := NewDefault().Name(); got != "local-stub" {
		t.Errorf("expected stub fallback in CI, got %q", got)
	}
}

// envGet is a tiny indirection so the skip test reads cleanly without
// importing os at the top of the test file.
func envGet(k string) string { return getEnvForTest(k) }
