package team

// End-to-end broker-restart integration test. Exercises the full
// save-side path for two unrelated mutations (custom channel + custom
// office member) via HTTP handlers, then reloads the state from disk
// via reloadedBroker(t) and asserts both mutations survived.
//
// The existing TestBrokerPersistsAndReloadsState covers in-memory
// saveLocked → reloadedBroker. This test covers the shape we actually
// ship: a caller POSTs to an HTTP handler, the handler triggers
// saveLocked as a side effect, the state file lands on disk, and a
// fresh broker on the same path sees the mutation. This is the seam
// Track A most easily silently breaks (e.g. the handler's closure
// captures a stale statePath, writes to the wrong file, the next
// construction starts fresh and the test passes in isolation but the
// user's real state is gone).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

func TestBrokerStatePersistsAcrossReload_ChannelAndMember(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}

	post := func(path string, body any) *http.Response {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s: %v", path, err)
		}
		req, err := http.NewRequest(http.MethodPost, "http://"+b.Addr()+path, bytes.NewReader(data))
		if err != nil {
			t.Fatalf("NewRequest %s: %v", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		return resp
	}

	// Create a custom channel. Explicitly empty Members so the handler
	// doesn't try to validate against defaults; "ceo" is auto-prepended
	// by the handler anyway.
	channelBody := map[string]any{
		"action":      "create",
		"slug":        "persistence-test",
		"name":        "Persistence Test",
		"description": "Created by TestBrokerStatePersistsAcrossReload",
		"created_by":  "ceo",
	}
	if resp := post("/channels", channelBody); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("POST /channels: status %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Create a custom member on claude-code (the default provider; no
	// openclaw bridge required). This exercises a different saveLocked
	// call site than the channel handler.
	memberBody := map[string]any{
		"action": "create",
		"slug":   "persistence-pm",
		"name":   "Persistence PM",
		"role":   "Product Manager",
		"provider": map[string]any{
			"kind":  provider.KindClaudeCode,
			"model": "claude-sonnet-4-5",
		},
	}
	if resp := post("/office-members", memberBody); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("POST /office-members: status %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Stop the original broker. The HTTP handlers should have already
	// persisted both mutations; Stop just drains goroutines and closes
	// the server.
	b.Stop()

	// Fresh broker on the same path, loads from disk. reloadedBroker
	// constructs a Broker and explicitly calls loadState — the only
	// way to opt back into disk read under the test-mode gate.
	reloaded := reloadedBroker(t, b)

	// Channel round-tripped.
	reloaded.mu.Lock()
	channels := append([]teamChannel(nil), reloaded.channels...)
	m := reloaded.findMemberLocked("persistence-pm")
	reloaded.mu.Unlock()

	foundChannel := false
	for _, c := range channels {
		if c.Slug == "persistence-test" {
			foundChannel = true
			if c.Description != "Created by TestBrokerStatePersistsAcrossReload" {
				t.Errorf("channel description lost: %+v", c)
			}
			break
		}
	}
	if !foundChannel {
		t.Errorf("channel persistence-test missing after reload; got channels: %v", channelSlugs(channels))
	}

	// Member round-tripped.
	if m == nil {
		t.Fatalf("member persistence-pm missing after reload")
	}
	if m.Role != "Product Manager" {
		t.Errorf("member role lost: got %q", m.Role)
	}
	if m.Provider.Kind != provider.KindClaudeCode {
		t.Errorf("member provider kind lost: got %q", m.Provider.Kind)
	}
}

// channelSlugs is a test-only helper so failure messages show which
// channels a reloaded broker actually has (saves a round-trip when
// debugging a persistence regression).
func channelSlugs(chs []teamChannel) []string {
	out := make([]string, 0, len(chs))
	for _, c := range chs {
		out = append(out, c.Slug)
	}
	return out
}
