package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient builds a Client pointed at the given test server URL.
func newTestClient(serverURL, apiKey string) *Client {
	c := NewClient(apiKey)
	c.BaseURL = serverURL
	return c
}

func TestIsAuthenticated(t *testing.T) {
	c := NewClient("")
	if c.IsAuthenticated() {
		t.Fatal("expected unauthenticated")
	}
	c.SetAPIKey("key123")
	if !c.IsAuthenticated() {
		t.Fatal("expected authenticated after SetAPIKey")
	}
}

func TestGet_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "test-key")
	_, err := Get[map[string]string](c, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("expected 'Bearer test-key', got %q", gotAuth)
	}
}

func TestGet_AuthError_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "bad-key")
	_, err := Get[map[string]any](c, "", 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*AuthError); !ok {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestGet_AuthError_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "bad-key")
	_, err := Get[map[string]any](c, "", 0)
	var ae *AuthError
	if !errorAs(err, &ae) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestGet_RateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "key")
	_, err := Get[map[string]any](c, "", 0)
	rle, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 30*time.Second {
		t.Fatalf("expected 30s retry-after, got %s", rle.RetryAfter)
	}
}

func TestGet_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "key")
	_, err := Get[map[string]any](c, "", 0)
	se, ok := err.(*ServerError)
	if !ok {
		t.Fatalf("expected *ServerError, got %T: %v", err, err)
	}
	if se.Status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", se.Status)
	}
}

func TestPost_SendsBody(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "key")
	_, err := Post[map[string]string](c, "", map[string]string{"hello": "world"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["hello"] != "world" {
		t.Fatalf("unexpected body: %v", gotBody)
	}
}

func TestGetRaw_ReturnsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("plain text"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "key")
	got, err := c.GetRaw("", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain text" {
		t.Fatalf("expected 'plain text', got %q", got)
	}
}

// TestRegisterRequest_RoundTrips locks in the JSON shape of the legacy
// RegisterRequest struct for any external callers still marshaling it.
// The HTTP Register() method itself has been removed — nex-cli owns
// registration now (see internal/nex.Register).
func TestRegisterRequest_RoundTrips(t *testing.T) {
	payload := RegisterRequest{
		Email:       "test@example.com",
		Name:        "Test",
		CompanyName: "Acme",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal RegisterRequest: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal RegisterRequest: %v", err)
	}
	if got["email"] != "test@example.com" {
		t.Fatalf("email: got %q", got["email"])
	}
	if got["name"] != "Test" {
		t.Fatalf("name: got %q", got["name"])
	}
	if got["company_name"] != "Acme" {
		t.Fatalf("company_name: got %q", got["company_name"])
	}
}

// errorAs is a thin wrapper to avoid importing errors in tests.
func errorAs(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case **AuthError:
		if ae, ok := err.(*AuthError); ok {
			*t = ae
			return true
		}
	case **RateLimitError:
		if rle, ok := err.(*RateLimitError); ok {
			*t = rle
			return true
		}
	case **ServerError:
		if se, ok := err.(*ServerError); ok {
			*t = se
			return true
		}
	}
	return false
}
