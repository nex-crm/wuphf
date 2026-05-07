package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/team"
)

// joinSubmit posts a JSON invite-acceptance to the share server. Tests use
// this to exercise the same path the React JoinPage takes.
func joinSubmit(t *testing.T, client *http.Client, baseURL, token, displayName string) *http.Response {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	body := []byte("{}")
	if displayName != "" {
		raw, err := json.Marshal(map[string]string{"display_name": displayName})
		if err != nil {
			t.Fatalf("marshal join body: %v", err)
		}
		body = raw
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/join/"+token, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build join request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("join submit: %v", err)
	}
	return resp
}

func TestValidateShareIPAllowsTailscaleByDefault(t *testing.T) {
	ip := net.ParseIP("100.82.14.6")
	if err := validateShareIP(ip, shareOptions{}); err != nil {
		t.Fatalf("validate tailscale ip: %v", err)
	}
}

func TestValidateShareIPBlocksLANWithoutExplicitOverride(t *testing.T) {
	ip := net.ParseIP("192.168.1.20")
	if err := validateShareIP(ip, shareOptions{}); err == nil {
		t.Fatalf("validate LAN without override returned nil")
	}
	if err := validateShareIP(ip, shareOptions{unsafeLAN: true}); err != nil {
		t.Fatalf("validate LAN with override: %v", err)
	}
}

func TestValidateShareIPBlocksPublicByDefault(t *testing.T) {
	ip := net.ParseIP("8.8.8.8")
	if err := validateShareIP(ip, shareOptions{}); err == nil {
		t.Fatalf("validate public ip returned nil")
	}
}

func TestValidateShareIPAllowsPublicWithUnsafeOverride(t *testing.T) {
	ip := net.ParseIP("8.8.8.8")
	if err := validateShareIP(ip, shareOptions{unsafePublicBind: true}); err != nil {
		t.Fatalf("validate public ip with unsafe override: %v", err)
	}
}

func TestValidateShareIPAllowsWireGuardPrivateViaResolverOverride(t *testing.T) {
	ip := net.ParseIP("10.13.0.7")
	if err := validateShareIP(ip, shareOptions{unsafeLAN: true}); err != nil {
		t.Fatalf("validate wireguard-style private ip: %v", err)
	}
}

func TestShareJoinFlowEndToEnd(t *testing.T) {
	b := team.NewBrokerAt(t.TempDir() + "/broker-state.json")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	invite, err := createShareInvite("http://"+b.Addr(), b.Token())
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	joined := false
	shareSrv := httptest.NewServer(newShareHandler("http://"+b.Addr(), b.Token(), func() {
		joined = true
	}))
	t.Cleanup(shareSrv.Close)

	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noFollow.Get(shareSrv.URL + "/join/" + invite.Token)
	if err != nil {
		t.Fatalf("join request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("join GET status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/?invite="+invite.Token {
		t.Fatalf("join GET redirect = %q, want /?invite=%s", loc, invite.Token)
	}
	resp = joinSubmit(t, nil, shareSrv.URL, invite.Token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join status = %d, want 200", resp.StatusCode)
	}
	var submitBody struct {
		OK       bool   `json:"ok"`
		Redirect string `json:"redirect"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&submitBody); err != nil {
		t.Fatalf("decode join response: %v", err)
	}
	if !submitBody.OK || submitBody.Redirect != "/#/channels/general" {
		t.Fatalf("unexpected join body: %+v", submitBody)
	}
	if !joined {
		t.Fatalf("join callback was not called")
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("join did not set a session cookie")
	}

	req, err := http.NewRequest(http.MethodGet, shareSrv.URL+"/api/humans/me", nil)
	if err != nil {
		t.Fatalf("build me request: %v", err)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("me request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status = %d body=%s", resp.StatusCode, string(body))
	}
	if !containsAll(string(body), `"display_name":"Team member"`, `"human_slug":"team-member"`) {
		t.Fatalf("unexpected me body: %s", string(body))
	}
}

func TestShareJoinInvalidInviteReturnsGone(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/humans/invites/accept" {
			t.Fatalf("unexpected broker request: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, `{"error":"invite_not_found"}`, http.StatusGone)
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, "broker-token", nil))
	t.Cleanup(shareSrv.Close)

	resp := joinSubmit(t, nil, shareSrv.URL, "missing-token", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("join status = %d, want 410 body=%s", resp.StatusCode, string(body))
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Error != "invite_expired_or_used" {
		t.Fatalf("error code = %q, want invite_expired_or_used", errBody.Error)
	}
}

func TestShareJoinMalformedBodyReturnsInvalidRequest(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("broker should not be called for malformed body: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, "broker-token", nil))
	t.Cleanup(shareSrv.Close)

	req, err := http.NewRequest(http.MethodPost, shareSrv.URL+"/join/abc", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("build malformed request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("malformed request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, string(body))
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Error != "invalid_request" {
		t.Fatalf("error code = %q, want invalid_request", errBody.Error)
	}
}

func TestShareJoinRejectsOversizedBody(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("broker should not be called for oversized body: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, "broker-token", nil))
	t.Cleanup(shareSrv.Close)

	// Construct a body larger than the 8 KiB cap. The decoder should error
	// out before ever reaching the broker, so an unauthenticated invite link
	// cannot be used to stream gigabytes into the share handler.
	huge := strings.Repeat("a", 16<<10)
	body := `{"display_name":"` + huge + `"}`
	req, err := http.NewRequest(http.MethodPost, shareSrv.URL+"/join/abc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build oversized request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("oversized request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Error != "invalid_request" {
		t.Fatalf("error code = %q, want invalid_request", errBody.Error)
	}
}

func TestShareJoinRejectsUnknownFields(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("broker should not be called when unknown fields are present: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, "broker-token", nil))
	t.Cleanup(shareSrv.Close)

	body := `{"display_name":"Maya","admin":true}`
	req, err := http.NewRequest(http.MethodPost, shareSrv.URL+"/join/abc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unknown-field request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestShareJoinBrokerFailureReturnsBadGateway(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/humans/invites/accept" {
			t.Fatalf("unexpected broker request: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, `{"error":"temporarily_unavailable"}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, "broker-token", nil))
	t.Cleanup(shareSrv.Close)

	resp := joinSubmit(t, nil, shareSrv.URL, "retry-token", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("join status = %d, want 502 body=%s", resp.StatusCode, string(body))
	}
}

func TestShareProxyDoesNotExposeBrokerToken(t *testing.T) {
	b := team.NewBrokerAt(t.TempDir() + "/broker-state.json")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	invite, err := createShareInvite("http://"+b.Addr(), b.Token())
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	shareSrv := httptest.NewServer(newShareHandler("http://"+b.Addr(), b.Token(), nil))
	t.Cleanup(shareSrv.Close)

	joinResp := joinSubmit(t, nil, shareSrv.URL, invite.Token, "")
	_ = joinResp.Body.Close()
	cookies := joinResp.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("join did not set a session cookie")
	}

	for _, path := range []string{"/api/web-token", "/api/notebook/list"} {
		req, err := http.NewRequest(http.MethodGet, shareSrv.URL+path, nil)
		if err != nil {
			t.Fatalf("build %s request: %v", path, err)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403 body=%s", path, resp.StatusCode, string(body))
		}
		if strings.Contains(string(body), b.Token()) {
			t.Fatalf("share proxy leaked broker token in %s body: %s", path, string(body))
		}
	}
}

func TestShareProxyOnboardingStateOnlyAllowsReadMethods(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/humans/me" {
			t.Fatalf("unexpected broker request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"human_slug":"team-member"}`))
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, "broker-token", nil))
	t.Cleanup(shareSrv.Close)

	for _, tc := range []struct {
		method string
		want   int
	}{
		{http.MethodGet, http.StatusOK},
		{http.MethodHead, http.StatusOK},
		{http.MethodPost, http.StatusMethodNotAllowed},
	} {
		t.Run(tc.method, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, shareSrv.URL+"/api/onboarding/state", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.AddCookie(&http.Cookie{Name: shareCookieName, Value: "valid-session"})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("state request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("state status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestShareProxyDoesNotForwardBrokerToken(t *testing.T) {
	const brokerToken = "host-secret-token"
	var proxiedAuth string
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/humans/me":
			if _, err := r.Cookie(shareCookieName); err != nil {
				t.Fatalf("session check did not include cookie: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"human_slug":"team-member"}`))
		case "/messages":
			proxiedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[]}`))
		default:
			t.Fatalf("unexpected broker path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(broker.Close)

	shareSrv := httptest.NewServer(newShareHandler(broker.URL, brokerToken, nil))
	t.Cleanup(shareSrv.Close)

	req, err := http.NewRequest(http.MethodGet, shareSrv.URL+"/api/messages", nil)
	if err != nil {
		t.Fatalf("build proxied request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: shareCookieName, Value: "valid-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied status = %d, want 200", resp.StatusCode)
	}
	if proxiedAuth != "" {
		t.Fatalf("share proxy forwarded Authorization = %q, want empty", proxiedAuth)
	}
}

func TestWebShareControllerClearInviteLocked(t *testing.T) {
	c := &webShareController{
		running:   true,
		inviteURL: "http://example.test/join/old",
		expiresAt: "2026-05-05T00:00:00Z",
	}

	c.clearInviteLocked()
	status := c.statusLocked()
	if status.InviteURL != "" || status.ExpiresAt != "" {
		t.Fatalf("status retained invite metadata: %+v", status)
	}
}

func TestShareProxyStampsHumanActor(t *testing.T) {
	b := team.NewBrokerAt(t.TempDir() + "/broker-state.json")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	invite, err := createShareInvite("http://"+b.Addr(), b.Token())
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	shareSrv := httptest.NewServer(newShareHandler("http://"+b.Addr(), b.Token(), nil))
	t.Cleanup(shareSrv.Close)

	joinResp := joinSubmit(t, nil, shareSrv.URL, invite.Token, "")
	_ = joinResp.Body.Close()
	cookies := joinResp.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("join did not set a session cookie")
	}

	postJSONWithCookies := func(path, body string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, shareSrv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build %s request: %v", path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request: %v", path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, resp.StatusCode, string(raw))
		}
	}

	postJSONWithCookies("/api/messages", `{"from":"you","channel":"general","content":"team member hello"}`)
	postJSONWithCookies("/api/actions", `{"kind":"manual","actor":"you","summary":"team member did work"}`)

	messagesReq, err := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/messages?channel=general", nil)
	if err != nil {
		t.Fatalf("build messages request: %v", err)
	}
	messagesReq.Header.Set("Authorization", "Bearer "+b.Token())
	messagesResp, err := http.DefaultClient.Do(messagesReq)
	if err != nil {
		t.Fatalf("messages request: %v", err)
	}
	defer messagesResp.Body.Close()
	var messagesPayload struct {
		Messages []struct {
			From    string `json:"from"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(messagesResp.Body).Decode(&messagesPayload); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	foundMessage := false
	for _, msg := range messagesPayload.Messages {
		if msg.Content == "team member hello" {
			foundMessage = true
			if msg.From != "human:team-member" {
				t.Fatalf("message from = %q, want human:team-member", msg.From)
			}
		}
	}
	if !foundMessage {
		t.Fatalf("posted team member message not found: %+v", messagesPayload.Messages)
	}

	actionsReq, err := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/actions", nil)
	if err != nil {
		t.Fatalf("build actions request: %v", err)
	}
	actionsReq.Header.Set("Authorization", "Bearer "+b.Token())
	actionsResp, err := http.DefaultClient.Do(actionsReq)
	if err != nil {
		t.Fatalf("actions request: %v", err)
	}
	defer actionsResp.Body.Close()
	var actionsPayload struct {
		Actions []struct {
			Actor   string `json:"actor"`
			Summary string `json:"summary"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(actionsResp.Body).Decode(&actionsPayload); err != nil {
		t.Fatalf("decode actions: %v", err)
	}
	foundAction := false
	for _, action := range actionsPayload.Actions {
		if action.Summary == "team member did work" {
			foundAction = true
			if action.Actor != "human:team-member" {
				t.Fatalf("action actor = %q, want human:team-member", action.Actor)
			}
		}
	}
	if !foundAction {
		t.Fatalf("posted team member action not found: %+v", actionsPayload.Actions)
	}
}

func containsAll(s string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
}

// TestWebShareControllerIssueInviteUsesAdapter confirms the controller routes
// invite creation through the registered ShareTransport when an in-process
// broker handle is available. The test broker is never started as an HTTP
// server, so any fallback to createShareInvite would fail with a transport
// error — a successful URL therefore proves the adapter path executed.
func TestWebShareControllerIssueInviteUsesAdapter(t *testing.T) {
	b := team.NewBrokerAt(t.TempDir() + "/broker-state.json")
	st := team.NewShareTransport(b, team.RelativeJoinURL)
	b.SetShareTransport(st)

	c := newWebShareController(7891)
	c.SetBroker(b)

	// brokerTokenFn deliberately errors so the test fails loudly if the
	// adapter branch silently regresses into the HTTP fallback. The adapter
	// path must never call this fn.
	tokenFn := func() (string, error) {
		t.Fatal("brokerTokenFn invoked: adapter path must not read the broker token")
		return "", nil
	}
	c.mu.Lock()
	url, expiresAt, err := c.issueInviteLocked(context.Background(), "10.0.0.5", 7891, "http://broker.invalid", tokenFn)
	c.mu.Unlock()
	if err != nil {
		t.Fatalf("issueInviteLocked: %v", err)
	}

	wantPrefix := "http://10.0.0.5:7891/join/"
	if !strings.HasPrefix(url, wantPrefix) {
		t.Errorf("invite URL = %q, want prefix %q (controller did not honor SetURLBuilder)", url, wantPrefix)
	}
	if url == wantPrefix {
		t.Error("invite URL has empty token")
	}
	if expiresAt == "" {
		t.Error("ExpiresAt empty: broker did not return invite metadata via adapter")
	}
}

// TestWebShareControllerIssueInviteFallsBackToHTTP confirms that when no
// in-process broker handle is available (the standalone `wuphf share`
// subcommand path), issueInviteLocked uses the HTTP createShareInvite call.
// We verify by pointing the controller at an httptest server that mimics the
// broker's POST /humans/invites response shape.
func TestWebShareControllerIssueInviteFallsBackToHTTP(t *testing.T) {
	const wantToken = "fallback-token-xyz"
	const wantExpiresAt = "2026-05-06T00:00:00Z"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/humans/invites" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + wantToken + `","invite":{"id":"inv-1","expires_at":"` + wantExpiresAt + `"}}`))
	}))
	t.Cleanup(srv.Close)

	c := newWebShareController(7891)
	// Intentionally no SetBroker — exercises the HTTP fallback branch.
	tokenFn := func() (string, error) { return "broker-token", nil }
	c.mu.Lock()
	url, expiresAt, err := c.issueInviteLocked(context.Background(), "192.168.1.10", 7891, srv.URL, tokenFn)
	c.mu.Unlock()
	if err != nil {
		t.Fatalf("issueInviteLocked: %v", err)
	}

	want := "http://192.168.1.10:7891/join/" + wantToken
	if url != want {
		t.Errorf("invite URL = %q, want %q", url, want)
	}
	if expiresAt != wantExpiresAt {
		t.Errorf("expiresAt = %q, want %q", expiresAt, wantExpiresAt)
	}
}

// TestWebShareControllerIssueInviteHTTPFallbackPropagatesTokenError confirms
// that when the adapter handle is absent and the lazy token getter fails, the
// error is surfaced rather than swallowed. Locks in the contract that
// issueInviteLocked treats brokerTokenFn errors as terminal for the HTTP path.
func TestWebShareControllerIssueInviteHTTPFallbackPropagatesTokenError(t *testing.T) {
	c := newWebShareController(7891)
	tokenErr := errors.New("token file unreadable")
	tokenFn := func() (string, error) { return "", tokenErr }
	c.mu.Lock()
	_, _, err := c.issueInviteLocked(context.Background(), "10.0.0.5", 7891, "http://broker.invalid", tokenFn)
	c.mu.Unlock()
	if !errors.Is(err, tokenErr) {
		t.Fatalf("issueInviteLocked error: got %v, want %v wrapped", err, tokenErr)
	}
}
