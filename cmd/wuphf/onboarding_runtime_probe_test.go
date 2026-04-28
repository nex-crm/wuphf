package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProbeLocalRuntimeAcceptsOpenAIShape asserts the happy path: a 200 OK
// with `{"object":"list", "data":[…]}` is treated as a real runtime.
func TestProbeLocalRuntimeAcceptsOpenAIShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	if !probeLocalRuntime(context.Background(), &http.Client{Timeout: time.Second}, srv.URL) {
		t.Fatal("expected probeLocalRuntime to accept OpenAI-shaped 200 OK")
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
		"object_not_list":    `{"object":"chat.completion","data":[]}`,
		"missing_object_key": `{"data":[]}`,
		"empty":              ``,
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
	// 2 MB body that nominally starts with the OpenAI shape but never
	// terminates the JSON object — we want to confirm the limit kicks in
	// and the probe fails gracefully (returns false) instead of hanging
	// or buffering.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		const chunk = `{"object":"list","data":[`
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
