package team

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// Bug reproduced by scripts/debug-tagging/run.sh with HIRE_SLUG=qa-spec:
//
//   1. Human tags @qa-spec in #general.
//   2. Notification routes correctly to qa-spec (PR #218's fix).
//   3. qa-spec's headless turn fires and attempts to post a reply.
//   4. Broker rejects the reply with 403 "channel access denied" because
//      handleOfficeMembers action=create does not add the new member to
//      #general.Members, and canAccessChannelLocked requires membership
//      for every non-lead sender.
//   5. User sees nothing. Symptom: "no response comes back."
//
// PR #218 fixed reads (notification targeting). This test covers the write
// side (reply posting) which is still broken on main.

func newBrokerWithPackChannels(t *testing.T, packAgents []agent.AgentConfig) *Broker {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	b := NewBroker()
	b.mu.Lock()
	// Seed pack-like roster.
	members := make([]officeMember, 0, len(packAgents))
	for _, cfg := range packAgents {
		members = append(members, officeMember{Slug: cfg.Slug, Name: cfg.Name, Role: cfg.Name})
	}
	b.members = members
	b.memberIndex = map[string]int{}
	for i, m := range b.members {
		b.memberIndex[m.Slug] = i
	}
	// Seed #general with every pack member (mirrors the pack-launch auto-fill
	// in normalizeLoadedStateLocked) and #engineering with a scoped subset
	// (a realistic topical channel that the human may have restricted).
	packSlugs := make([]string, 0, len(members))
	for _, m := range members {
		packSlugs = append(packSlugs, m.Slug)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: packSlugs, CreatedAt: now, UpdatedAt: now},
		{Slug: "engineering", Name: "engineering", Members: []string{"ceo"}, CreatedAt: now, UpdatedAt: now},
		// A DM channel that must NOT receive the new hire.
		{Slug: "dm-human-ceo", Name: "DM: CEO", Type: "dm", Members: []string{"ceo"}, CreatedAt: now, UpdatedAt: now},
	}
	b.mu.Unlock()
	return b
}

// Bug A — state-level: after POST /office-members action=create, the new
// slug MUST be a member of every non-DM channel. Skips DM channels: those
// encode the target agent in the slug and have their own membership gate.
// Also asserts UpdatedAt moved forward so SSE-refreshing UIs see the roster
// change, and asserts a channel_updated event fires per mutated channel.
func TestWizardHire_AddsNewMemberToAllNonDMChannels(t *testing.T) {
	b := newBrokerWithPackChannels(t, []agent.AgentConfig{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "pm", Name: "Product Manager"},
	})
	b.mu.Lock()
	b.token = "test-token"
	// Capture pre-hire UpdatedAt values so we can assert forward movement.
	preUpdated := map[string]string{}
	for _, ch := range b.channels {
		preUpdated[ch.Slug] = ch.UpdatedAt
	}
	b.mu.Unlock()

	events, unsubscribe := b.SubscribeOfficeChanges(16)
	defer unsubscribe()

	mux := http.NewServeMux()
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Ensure at least 1 second of wall-clock passes between seed and hire so
	// the RFC3339 timestamp compare can move forward even on systems with
	// 1s resolution.
	time.Sleep(1100 * time.Millisecond)

	body, _ := json.Marshal(map[string]any{
		"action": "create",
		"slug":   "qa-spec",
		"name":   "QA Specialist",
	})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL+"/office-members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("hire: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hire: status=%d", resp.StatusCode)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// #general already contained qa-spec? No, seeded without it. Must be added.
	general := b.findChannelLocked("general")
	if general == nil || !containsString(general.Members, "qa-spec") {
		t.Fatalf("general must contain qa-spec after hire; got members=%v", general.Members)
	}
	if general.UpdatedAt == preUpdated["general"] {
		t.Fatalf("general.UpdatedAt did not advance (%q); SSE subscribers will not see the roster change", general.UpdatedAt)
	}

	// #engineering was a scoped channel (only CEO). The "add to every non-DM
	// channel" policy means qa-spec must be added here too.
	eng := b.findChannelLocked("engineering")
	if eng == nil || !containsString(eng.Members, "qa-spec") {
		t.Fatalf("engineering must contain qa-spec after hire (non-DM policy); got members=%v", eng.Members)
	}
	if eng.UpdatedAt == preUpdated["engineering"] {
		t.Fatalf("engineering.UpdatedAt did not advance; SSE subscribers will not see the roster change")
	}

	// DM channel must NOT be touched.
	dm := b.findChannelLocked("dm-human-ceo")
	if dm == nil {
		t.Fatalf("dm channel disappeared")
	}
	if containsString(dm.Members, "qa-spec") {
		t.Fatalf("DM channel should not auto-include wizard-hired member; got members=%v", dm.Members)
	}
	if dm.UpdatedAt != preUpdated["dm-human-ceo"] {
		t.Fatalf("DM channel UpdatedAt changed unexpectedly (pre=%q post=%q)", preUpdated["dm-human-ceo"], dm.UpdatedAt)
	}

	// Event side of the fix: SSE subscribers must see one channel_updated per
	// mutated channel plus the existing member_created. Drain with a brief
	// timeout so the goroutine-delivered events land.
	seenMemberCreated := false
	updatedSlugs := map[string]bool{}
	deadline := time.After(250 * time.Millisecond)
drain:
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				break drain
			}
			switch evt.Kind {
			case "member_created":
				if evt.Slug == "qa-spec" {
					seenMemberCreated = true
				}
			case "channel_updated":
				updatedSlugs[evt.Slug] = true
			}
		case <-deadline:
			break drain
		}
	}
	if !seenMemberCreated {
		t.Fatalf("expected member_created event for qa-spec")
	}
	if !updatedSlugs["general"] || !updatedSlugs["engineering"] {
		t.Fatalf("expected channel_updated events for general and engineering; got %v", updatedSlugs)
	}
	if updatedSlugs["dm-human-ceo"] {
		t.Fatalf("no channel_updated event should fire for the untouched DM channel")
	}
}

// Bug A' — stale Disabled entry from a prior lifecycle must be cleared on
// re-hire. Belt+braces: normalizeLoadedStateLocked already filters orphan
// Disabled entries on load, and the remove branch clears both Members and
// Disabled, so this is defensive. The test pins the invariant so a future
// state-rebuild path that forgets it doesn't silently leave a new hire muted.
func TestWizardHire_ClearsStaleDisabledEntryFromPriorLifecycle(t *testing.T) {
	b := newBrokerWithPackChannels(t, []agent.AgentConfig{{Slug: "ceo", Name: "CEO"}})
	b.mu.Lock()
	b.token = "test-token"
	// Simulate a leftover disabled entry for the slug we're about to hire.
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Disabled = []string{"qa-spec"}
		}
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"action": "create", "slug": "qa-spec", "name": "QA"})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL+"/office-members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("hire: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hire: status=%d", resp.StatusCode)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	general := b.findChannelLocked("general")
	if general == nil {
		t.Fatalf("general channel missing")
	}
	if containsString(general.Disabled, "qa-spec") {
		t.Fatalf("stale Disabled entry for qa-spec survived the re-hire; members=%v disabled=%v", general.Members, general.Disabled)
	}
	if !containsString(general.Members, "qa-spec") {
		t.Fatalf("qa-spec must be in Members after hire; got %v", general.Members)
	}
}

// Bug A” — action: "remove" must reverse the channel-membership side effect
// of action: "create". Without this, a removed slug stays in every channel's
// Members list, wastes screen space in the UI, and risks reviving a ghost
// member on state reload. The existing remove branch already handles this
// but we pin it so a future refactor of the create side can't break it.
func TestWizardHire_RemoveReversesChannelMembership(t *testing.T) {
	b := newBrokerWithPackChannels(t, []agent.AgentConfig{{Slug: "ceo", Name: "CEO"}})
	b.mu.Lock()
	b.token = "test-token"
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	post := func(payload map[string]any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL+"/office-members", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("post: status=%d payload=%v", resp.StatusCode, payload)
		}
	}

	post(map[string]any{"action": "create", "slug": "qa-spec", "name": "QA"})
	post(map[string]any{"action": "remove", "slug": "qa-spec"})

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.channels {
		if containsString(ch.Members, "qa-spec") {
			t.Fatalf("qa-spec lingered in #%s.members after remove: %v", ch.Slug, ch.Members)
		}
		if containsString(ch.Disabled, "qa-spec") {
			t.Fatalf("qa-spec lingered in #%s.disabled after remove: %v", ch.Slug, ch.Disabled)
		}
	}
}

// Bug B — end-to-end: drive the exact HTTP flow the browser uses.
//
//  1. Start broker with CEO + PM (pack) plus #general seeded.
//  2. POST /office-members action=create { slug: "qa-spec" }.
//  3. POST /messages { from: "qa-spec", channel: "general", content: "…" }.
//     Today: 403 "channel access denied".
//     Expected: 200 with a message id.
func TestBug_WizardHiredSpecialist_ReplyEndToEnd_HTTPFlow(t *testing.T) {
	b := newBrokerWithPackChannels(t, []agent.AgentConfig{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "pm", Name: "Product Manager"},
	})
	// Bypass auth for the test — we're exercising access control, not tokens.
	b.mu.Lock()
	b.token = "test-token"
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	mux.HandleFunc("/messages", b.requireAuth(b.handleMessages))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	do := func(method, path string, body any) (*http.Response, []byte) {
		t.Helper()
		var r *http.Request
		if body == nil {
			r, _ = http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
		} else {
			buf, _ := json.Marshal(body)
			r, _ = http.NewRequestWithContext(context.Background(), method, srv.URL+path, bytes.NewReader(buf))
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatalf("http %s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		return resp, buf[:n]
	}

	// 1) Hire qa-spec via the same endpoint the web wizard uses.
	hireResp, hireBody := do("POST", "/office-members", map[string]any{
		"action": "create",
		"slug":   "qa-spec",
		"name":   "QA Specialist",
		"role":   "QA",
	})
	if hireResp.StatusCode != http.StatusOK {
		t.Fatalf("hire failed: status=%d body=%s", hireResp.StatusCode, hireBody)
	}

	// 2) qa-spec posts a reply to #general — this is what headless dispatch does
	//    at the end of a turn, and is what fails today with 403.
	replyResp, replyBody := do("POST", "/messages", map[string]any{
		"from":    "qa-spec",
		"channel": "general",
		"content": "Ack — qa-spec reply to #general after wizard-hire",
	})
	if replyResp.StatusCode != http.StatusOK {
		t.Fatalf("bug reproduced: wizard-hired qa-spec cannot post reply to #general. "+
			"status=%d body=%s — this is why the user sees 'no response comes back' "+
			"after tagging a specialist added via the web wizard.",
			replyResp.StatusCode, strings.TrimSpace(string(replyBody)))
	}
}
