package main

import (
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

func containsAll(s string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
}
