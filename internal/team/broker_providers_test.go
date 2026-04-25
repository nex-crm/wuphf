package team

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/provider"
)

func TestMemberFromSpec_CopiesProvider(t *testing.T) {
	spec := company.MemberSpec{
		Slug:     "pm-bot",
		Name:     "PM Bot",
		Provider: provider.ProviderBinding{Kind: provider.KindCodex, Model: "gpt-5.4"},
	}
	m := memberFromSpec(spec, "test", "2026-04-16T00:00:00Z", false)
	if m.Slug != "pm-bot" || m.Name != "PM Bot" {
		t.Fatalf("unexpected member: %+v", m)
	}
	if m.Provider.Kind != provider.KindCodex || m.Provider.Model != "gpt-5.4" {
		t.Fatalf("provider not copied: %+v", m.Provider)
	}
}

func TestHandleOfficeMembers_CreateWithProvider(t *testing.T) {
	b, ts, token := newBrokerHTTPTest(t)
	defer ts.Close()

	body := map[string]any{
		"action": "create",
		"slug":   "pm-openclaw",
		"name":   "PM OpenClaw",
		"provider": map[string]any{
			"kind":  provider.KindOpenclaw,
			"model": "openai-codex/gpt-5.4",
			"openclaw": map[string]any{
				"session_key": "agent:test:pm",
				"agent_id":    "main",
			},
		},
	}
	resp := doBrokerPost(t, ts, token, "/office-members", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d", resp.StatusCode)
	}

	// Verify persisted shape via findMemberLocked round-trip
	b.mu.Lock()
	m := b.findMemberLocked("pm-openclaw")
	b.mu.Unlock()
	if m == nil {
		t.Fatal("member not found after create")
	}
	if m.Provider.Kind != provider.KindOpenclaw {
		t.Fatalf("provider kind=%q, want %q", m.Provider.Kind, provider.KindOpenclaw)
	}
	if m.Provider.Openclaw == nil || m.Provider.Openclaw.SessionKey != "agent:test:pm" {
		t.Fatalf("openclaw binding lost: %+v", m.Provider.Openclaw)
	}
}

func TestHandleOfficeMembers_UpdateSwitchesProvider(t *testing.T) {
	b, ts, token := newBrokerHTTPTest(t)
	defer ts.Close()

	// Create on codex
	create := map[string]any{
		"action":   "create",
		"slug":     "switcher",
		"name":     "Switcher",
		"provider": map[string]any{"kind": provider.KindCodex, "model": "gpt-5.4"},
	}
	if r := doBrokerPost(t, ts, token, "/office-members", create); r.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d", r.StatusCode)
	}

	// Update to openclaw
	update := map[string]any{
		"action": "update",
		"slug":   "switcher",
		"provider": map[string]any{
			"kind":  provider.KindOpenclaw,
			"model": "openai-codex/gpt-5.4",
			"openclaw": map[string]any{
				"session_key": "agent:test:switcher",
			},
		},
	}
	if r := doBrokerPost(t, ts, token, "/office-members", update); r.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d", r.StatusCode)
	}

	b.mu.Lock()
	m := b.findMemberLocked("switcher")
	b.mu.Unlock()
	if m.Provider.Kind != provider.KindOpenclaw {
		t.Fatalf("provider not switched: %q", m.Provider.Kind)
	}
	if m.Provider.Openclaw == nil || m.Provider.Openclaw.SessionKey != "agent:test:switcher" {
		t.Fatalf("new openclaw binding missing: %+v", m.Provider.Openclaw)
	}
}

func TestHandleOfficeMembers_InvalidProviderKind(t *testing.T) {
	_, ts, token := newBrokerHTTPTest(t)
	defer ts.Close()

	body := map[string]any{
		"action":   "create",
		"slug":     "bad-provider",
		"name":     "Bad Provider",
		"provider": map[string]any{"kind": "gemini"},
	}
	resp := doBrokerPost(t, ts, token, "/office-members", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), "claude-code") {
		t.Fatalf("error should list valid kinds, got %q", buf.String())
	}
}

func TestProviderFieldSurvivesBrokerReload(t *testing.T) {
	tmpDir := t.TempDir()
	setBrokerStatePathForTest(t, func() string { return filepath.Join(tmpDir, "broker-state.json") })

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug: "persist-test",
		Name: "Persist Test",
		Provider: provider.ProviderBinding{
			Kind:     provider.KindOpenclaw,
			Model:    "openai-codex/gpt-5.4",
			Openclaw: &provider.OpenclawProviderBinding{SessionKey: "agent:test:persist"},
		},
	})
	b.memberIndex[b.members[len(b.members)-1].Slug] = len(b.members) - 1
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t)
	reloaded.mu.Lock()
	got := reloaded.findMemberLocked("persist-test")
	reloaded.mu.Unlock()
	if got == nil {
		t.Fatal("member did not survive reload")
	}
	if got.Provider.Kind != provider.KindOpenclaw {
		t.Fatalf("kind not persisted: %q", got.Provider.Kind)
	}
	if got.Provider.Openclaw == nil || got.Provider.Openclaw.SessionKey != "agent:test:persist" {
		t.Fatalf("openclaw block not persisted: %+v", got.Provider.Openclaw)
	}
}

func TestRebuildMemberIndex_AfterRemove(t *testing.T) {
	b := NewBroker()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.members = []officeMember{
		{Slug: "a", Name: "A"},
		{Slug: "b", Name: "B"},
		{Slug: "c", Name: "C"},
	}
	b.rebuildMemberIndexLocked()

	// Remove "b"
	filtered := b.members[:0]
	for _, m := range b.members {
		if m.Slug != "b" {
			filtered = append(filtered, m)
		}
	}
	b.members = filtered
	b.rebuildMemberIndexLocked()

	if got := b.findMemberLocked("b"); got != nil {
		t.Fatal("removed member still found")
	}
	if got := b.findMemberLocked("c"); got == nil || got.Name != "C" {
		t.Fatalf("shift-after-remove lost C: %+v", got)
	}
	if got := b.findMemberLocked("a"); got == nil || got.Name != "A" {
		t.Fatalf("A lookup broken after rebuild: %+v", got)
	}
}

// newBrokerHTTPTest spins up a broker with its HTTP handler attached to an
// httptest server, returning the broker, the server, and the auth token.
func newBrokerHTTPTest(t *testing.T) (*Broker, *httptest.Server, string) {
	t.Helper()
	tmpDir := t.TempDir()
	setBrokerStatePathForTest(t, func() string { return filepath.Join(tmpDir, "broker-state.json") })

	b := NewBroker()
	// Attach a fake bridge so handleOfficeMembers can exercise openclaw
	// create/update/remove paths without dialing a real gateway.
	fake := newFakeOC()
	bridge := NewOpenclawBridge(b, fake, nil)
	b.AttachOpenclawBridge(bridge)

	mux := http.NewServeMux()
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	ts := httptest.NewServer(mux)
	return b, ts, b.Token()
}

func doBrokerPost(t *testing.T, ts *httptest.Server, token, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}
