package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/team"
)

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

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(shareSrv.URL + "/join/" + invite.Token)
	if err != nil {
		t.Fatalf("join request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("join status = %d, want 302", resp.StatusCode)
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
	if !containsAll(string(body), `"display_name":"Co-founder"`, `"human_slug":"co-founder"`) {
		t.Fatalf("unexpected me body: %s", string(body))
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

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	joinResp, err := client.Get(shareSrv.URL + "/join/" + invite.Token)
	if err != nil {
		t.Fatalf("join request: %v", err)
	}
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
			_, _ = w.Write([]byte(`{"human_slug":"co-founder"}`))
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

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	joinResp, err := client.Get(shareSrv.URL + "/join/" + invite.Token)
	if err != nil {
		t.Fatalf("join request: %v", err)
	}
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

	postJSONWithCookies("/api/messages", `{"from":"you","channel":"general","content":"cofounder hello"}`)
	postJSONWithCookies("/api/actions", `{"kind":"manual","actor":"you","summary":"cofounder did work"}`)

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
		if msg.Content == "cofounder hello" {
			foundMessage = true
			if msg.From != "human:co-founder" {
				t.Fatalf("message from = %q, want human:co-founder", msg.From)
			}
		}
	}
	if !foundMessage {
		t.Fatalf("posted cofounder message not found: %+v", messagesPayload.Messages)
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
		if action.Summary == "cofounder did work" {
			foundAction = true
			if action.Actor != "human:co-founder" {
				t.Fatalf("action actor = %q, want human:co-founder", action.Actor)
			}
		}
	}
	if !foundAction {
		t.Fatalf("posted cofounder action not found: %+v", actionsPayload.Actions)
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
