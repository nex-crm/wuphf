package team

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rtStub is a canned RoundTripper so the test never touches the network. It also
// records the outbound request so we can assert the real key is forwarded and
// the body carries the configured model.
type rtStub struct {
	status  int
	body    string
	gotAuth string
	gotBody string
}

func (s *rtStub) RoundTrip(req *http.Request) (*http.Response, error) {
	s.gotAuth = req.Header.Get("Authorization")
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		s.gotBody = string(b)
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func withRealtimeStub(t *testing.T, stub *rtStub) {
	t.Helper()
	prev := realtimeHTTPClient
	realtimeHTTPClient = &http.Client{Transport: stub}
	t.Cleanup(func() { realtimeHTTPClient = prev })
}

func TestHandleRealtimeSession_MintsEphemeralKey(t *testing.T) {
	t.Setenv("WUPHF_OPENAI_API_KEY", "sk-real-secret")
	t.Setenv("WUPHF_REALTIME_MODEL", "gpt-realtime")
	stub := &rtStub{status: 200, body: `{"value":"ek_abc","expires_at":999}`}
	withRealtimeStub(t, stub)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/realtime/session", nil)
	(&Broker{}).handleRealtimeSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	// The real key is forwarded to OpenAI but must NOT appear in the response.
	if stub.gotAuth != "Bearer sk-real-secret" {
		t.Fatalf("upstream auth: got %q", stub.gotAuth)
	}
	if strings.Contains(rec.Body.String(), "sk-real-secret") {
		t.Fatalf("response leaked the real key: %s", rec.Body.String())
	}
	for _, want := range []string{`"ephemeral_key":"ek_abc"`, `"gpt-realtime"`, `realtime/calls`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("response missing %q: %s", want, rec.Body.String())
		}
	}
	if !strings.Contains(stub.gotBody, `"gpt-realtime"`) {
		t.Fatalf("mint request missing model: %s", stub.gotBody)
	}
}

func TestHandleRealtimeSession_NoKeyFallsBack(t *testing.T) {
	// Empty env + an empty runtime home so Load() finds no config key.
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/realtime/session", nil)
	(&Broker{}).handleRealtimeSession(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
}

func TestMintRealtimeEphemeralKey_AcceptsPreviewShape(t *testing.T) {
	stub := &rtStub{status: 200, body: `{"client_secret":{"value":"ek_pre","expires_at":5}}`}
	withRealtimeStub(t, stub)

	ek, exp, err := mintRealtimeEphemeralKey(t.Context(), "sk-x", "gpt-realtime")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if ek != "ek_pre" || exp != 5 {
		t.Fatalf("parsed: got (%q,%d), want (ek_pre,5)", ek, exp)
	}
}

func TestMintRealtimeEphemeralKey_ErrorsOnNon2xx(t *testing.T) {
	withRealtimeStub(t, &rtStub{status: 401, body: `{"error":"bad key"}`})
	if _, _, err := mintRealtimeEphemeralKey(t.Context(), "sk-x", "gpt-realtime"); err == nil {
		t.Fatal("expected error on 401")
	}
}
