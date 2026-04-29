package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProbeLocalRuntimeAcceptsOpenAIShape asserts the happy path: a 200
// OK with `{"object":"list", "data":[…]}` is treated as a real runtime.
func TestProbeLocalRuntimeAcceptsOpenAIShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen2.5-coder:7b","object":"model"}]}`))
	}))
	defer srv.Close()

	if !probeLocalRuntime(context.Background(), &http.Client{Timeout: time.Second}, srv.URL) {
		t.Fatal("expected probeLocalRuntime to accept OpenAI-shaped 200 OK")
	}
}

// TestProbeLocalRuntimeAcceptsEmptyData covers the "freshly installed
// ollama with no models pulled yet" case. The daemon is up, /v1/models
// returns `{"object":"list","data":[]}`, and we MUST treat it as a real
// runtime — rejecting it would silently break the most common ollama
// first-run state. The flip side (an unrelated server happening to
// return `data:[]` is exotic enough to accept the false positive risk).
func TestProbeLocalRuntimeAcceptsEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	if !probeLocalRuntime(context.Background(), &http.Client{Timeout: time.Second}, srv.URL) {
		t.Fatal("freshly-installed ollama (data:[]) must be treated as a real runtime")
	}
}

// TestProbeLocalRuntimeRejectsArbitrary200 covers the load-bearing case:
// a 200 OK from any other dev server on port 8080 (Spring Boot, devserver
// index, generic JSON) must not satisfy the probe. Without the body
// validation, mlx-lm's port collision turns into a bad --provider hint.
func TestProbeLocalRuntimeRejectsArbitrary200(t *testing.T) {
	cases := map[string]string{
		"html":               `<!doctype html><html><body>Hello</body></html>`,
		"unrelated_json":     `{"status":"ok"}`,
		"object_not_list":    `{"object":"chat.completion","data":[{"id":"x"}]}`,
		"missing_object_key": `{"data":[{"id":"x"}]}`,
		"empty":              ``,
		// `data` key missing entirely — distinguishes a real OpenAI-shaped
		// response (which always includes the key, even if the array is
		// empty) from an unrelated server returning a partial JSON shape.
		"object_list_no_data_key": `{"object":"list"}`,
		// Explicit null — same intent: not a real /v1/models response.
		"object_list_data_null": `{"object":"list","data":null}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			body := body
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			if probeLocalRuntime(context.Background(), &http.Client{Timeout: time.Second}, srv.URL) {
				t.Fatalf("probeLocalRuntime accepted non-OpenAI body %q", name)
			}
		})
	}
}

// TestProbeLocalRuntimeRejectsNon2xx asserts the status check still fires
// before the body decode.
func TestProbeLocalRuntimeRejectsNon2xx(t *testing.T) {
	for _, status := range []int{301, 401, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := status
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(s)
				_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			}))
			defer srv.Close()
			if probeLocalRuntime(context.Background(), &http.Client{Timeout: time.Second}, srv.URL) {
				t.Fatalf("probeLocalRuntime accepted status %d", s)
			}
		})
	}
}

// TestProbeLocalRuntimeBoundedReadSize asserts the LimitReader cap so a
// misbehaving server can't stream megabytes into the wizard's hot path.
func TestProbeLocalRuntimeBoundedReadSize(t *testing.T) {
	// 256KB body that nominally starts with the OpenAI shape but never
	// terminates the JSON object — we want to confirm the LimitReader
	// 64KB cap kicks in and the probe fails gracefully (returns false)
	// instead of hanging or buffering megabytes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		const chunk = `{"object":"list","data":[{"id":"x"`
		_, _ = w.Write([]byte(chunk))
		// Pad with garbage past the 64KB cap.
		blob := make([]byte, 256*1024)
		for i := range blob {
			blob[i] = 'x'
		}
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	done := make(chan bool, 1)
	go func() {
		done <- probeLocalRuntime(context.Background(), &http.Client{Timeout: 2 * time.Second}, srv.URL)
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("probe accepted a 256KB unterminated body")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not return within 3s on oversized body")
	}
}
