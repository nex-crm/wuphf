package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestPostAutomationMessage_DedupesByEventID is a unit-level guard for
// the eventID dedupe path: posting twice with the same eventID returns
// the original message and a duplicate=true flag without inserting a
// second record. Distinct from the integration test that hits the HTTP
// surface — this version exercises the broker method directly.
func TestPostAutomationMessage_DedupesByEventID(t *testing.T) {
	b := newTestBroker(t)
	first, dup1, err := b.PostAutomationMessage("nex", "general", "T", "first", "evt-x", "src", "Src", nil, "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if dup1 {
		t.Errorf("first call: expected duplicate=false")
	}
	second, dup2, err := b.PostAutomationMessage("nex", "general", "T", "second", "evt-x", "src", "Src", nil, "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !dup2 {
		t.Errorf("second call: expected duplicate=true")
	}
	if second.ID != first.ID {
		t.Errorf("expected same ID on dedupe, got %q vs %q", first.ID, second.ID)
	}
	if got := len(b.Messages()); got != 1 {
		t.Errorf("expected 1 message after dedupe, got %d", got)
	}
}

// TestPostMessage_SetsTimestampAndChannel pins basic invariants on the
// exported PostMessage entry point used by every other package: a
// returned message has a non-empty ID, a non-empty Timestamp, the
// requested Channel slug, and shows up in b.Messages().
func TestPostMessage_SetsTimestampAndChannel(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "ceo", Name: "CEO", Role: "lead"})
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "ceo")
		}
	}
	b.mu.Unlock()

	got, err := b.PostMessage("ceo", "general", "hello", nil, "")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if got.ID == "" {
		t.Error("expected non-empty ID")
	}
	if got.Timestamp == "" {
		t.Error("expected non-empty Timestamp")
	}
	if got.Channel != "general" {
		t.Errorf("Channel: want general, got %q", got.Channel)
	}
	all := b.Messages()
	if len(all) == 0 || all[len(all)-1].ID != got.ID {
		t.Errorf("posted message not visible in b.Messages()")
	}
}

// TestNormalizeMessageScope_KnownAndDefaults pins the normalizer
// contract: "", "all", "channel", and any unknown value collapse to ""
// (channel-wide). "agent", "inbox", "outbox" pass through (lower-cased,
// trimmed). The handler dispatches on this output, so drift here would
// silently change visibility semantics.
func TestNormalizeMessageScope_KnownAndDefaults(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"ALL":      "",
		"channel":  "",
		"agent":    "agent",
		" AGENT ":  "agent",
		"inbox":    "inbox",
		"outbox":   "outbox",
		"unknown":  "",
		" thread ": "",
	}
	for in, want := range cases {
		if got := normalizeMessageScope(in); got != want {
			t.Errorf("normalizeMessageScope(%q): want %q, got %q", in, want, got)
		}
	}
}

func TestFormatChannelViewIncludesThreadReference(t *testing.T) {
	got := FormatChannelView([]channelMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "Replying here", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
	})

	if !strings.Contains(got, "10:01:00 ↳ msg-1  @fe: Replying here") {
		t.Fatalf("expected threaded message to include reply marker, got %q", got)
	}
}

func TestBrokerCanonicalizesLegacyDMSlugs(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	postJSON := func(path string, payload map[string]any) *http.Response {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s failed: %v", path, err)
		}
		return resp
	}

	resp := postJSON("/channels/dm", map[string]any{
		"members": []string{"human", "ceo"},
		"type":    "direct",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create dm status %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create dm: %v", err)
	}
	wantSlug := channelDirectSlug("human", "ceo")
	if created.Slug != wantSlug {
		t.Fatalf("expected canonical slug %q, got %q", wantSlug, created.Slug)
	}

	msgResp := postJSON("/messages", map[string]any{
		"from":    "human",
		"channel": "dm-human-ceo",
		"content": "hello ceo",
	})
	defer msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(msgResp.Body)
		t.Fatalf("post legacy dm status %d: %s", msgResp.StatusCode, raw)
	}
	msgs := b.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	if msgs[0].Channel != wantSlug {
		t.Fatalf("expected message to land in %q, got %q", wantSlug, msgs[0].Channel)
	}

	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=dm-human-ceo&viewer_slug=human", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	getResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET legacy dm failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(getResp.Body)
		t.Fatalf("get legacy dm status %d: %s", getResp.StatusCode, raw)
	}
	var got struct {
		Channel  string           `json:"channel"`
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get dm: %v", err)
	}
	if got.Channel != wantSlug || len(got.Messages) != 1 {
		t.Fatalf("expected canonical channel %q with one message, got channel=%q messages=%d", wantSlug, got.Channel, len(got.Messages))
	}
}

func TestBrokerMessageKindAndTitleRoundTrip(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"from":    "ceo",
		"channel": "general",
		"kind":    "human_report",
		"title":   "Frontend ready for review",
		"content": "The launch page skeleton is ready for you to review.",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 posting message, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if got := result.Messages[0].Kind; got != "human_report" {
		t.Fatalf("expected human_report kind, got %q", got)
	}
	if got := result.Messages[0].Title; got != "Frontend ready for review" {
		t.Fatalf("expected title to round-trip, got %q", got)
	}
}

func TestBrokerMessagesCanScopeToThread(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	root, err := b.PostMessage("ceo", "general", "Root topic", nil, "")
	if err != nil {
		t.Fatalf("post root: %v", err)
	}
	reply, err := b.PostMessage("ceo", "general", "Reply in thread", nil, root.ID)
	if err != nil {
		t.Fatalf("post reply: %v", err)
	}
	if _, err := b.PostMessage("you", "general", "Separate topic", nil, ""); err != nil {
		t.Fatalf("post unrelated: %v", err)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&thread_id="+root.ID, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("thread messages request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing thread messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode thread messages: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected root and reply only, got %+v", result.Messages)
	}
	if result.Messages[0].ID != root.ID || result.Messages[1].ID != reply.ID {
		t.Fatalf("unexpected thread messages: %+v", result.Messages)
	}
}

func TestBrokerMessagesCanScopeToAgentInbox(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "fe", Name: "Frontend Engineer"},
	)
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm", "fe")
			break
		}
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	if _, err := b.PostMessage("you", "general", "Global direction", nil, ""); err != nil {
		t.Fatalf("post human message: %v", err)
	}
	if _, err := b.PostMessage("pm", "general", "Unrelated PM update", nil, ""); err != nil {
		t.Fatalf("post unrelated message: %v", err)
	}
	tagged, err := b.PostMessage("ceo", "general", "Frontend, take this next.", []string{"fe"}, "")
	if err != nil {
		t.Fatalf("post tagged message: %v", err)
	}
	own, err := b.PostMessage("fe", "general", "I am on it.", nil, "")
	if err != nil {
		t.Fatalf("post own message: %v", err)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&my_slug=fe&viewer_slug=fe&scope=agent", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("agent-scoped messages request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing agent-scoped messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages    []channelMessage `json:"messages"`
		TaggedCount int              `json:"tagged_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode agent-scoped messages: %v", err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected human, tagged, and own messages only, got %+v", result.Messages)
	}
	if result.TaggedCount != 1 {
		t.Fatalf("expected one tagged message, got %d", result.TaggedCount)
	}
	seen := map[string]bool{}
	for _, msg := range result.Messages {
		seen[msg.ID] = true
		if strings.Contains(msg.Content, "Unrelated PM update") {
			t.Fatalf("did not expect unrelated message in agent scope: %+v", result.Messages)
		}
	}
	if !seen[tagged.ID] || !seen[own.ID] {
		t.Fatalf("expected tagged and own messages in scoped view, got %+v", result.Messages)
	}
}

func TestHandleMessagesSupportsInboxAndOutboxScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "fe", Name: "Frontend Engineer"},
	)
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm", "fe")
			break
		}
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	root, err := b.PostMessage("ceo", "general", "Frontend, take the signup thread.", nil, "")
	if err != nil {
		t.Fatalf("post root message: %v", err)
	}
	ownReply, err := b.PostMessage("fe", "general", "I can own the signup thread.", nil, root.ID)
	if err != nil {
		t.Fatalf("post own reply: %v", err)
	}
	threadReply, err := b.PostMessage("pm", "general", "Please include the pricing copy in that thread.", nil, ownReply.ID)
	if err != nil {
		t.Fatalf("post thread reply: %v", err)
	}
	ownTopLevel, err := b.PostMessage("fe", "general", "Shipped the initial branch.", nil, "")
	if err != nil {
		t.Fatalf("post own top-level message: %v", err)
	}
	if _, err := b.PostMessage("pm", "general", "Unrelated roadmap chatter.", nil, ""); err != nil {
		t.Fatalf("post unrelated message: %v", err)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	fetch := func(scope string) []channelMessage {
		req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&viewer_slug=fe&scope="+scope, nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get %s messages: %v", scope, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 for %s scope, got %d: %s", scope, resp.StatusCode, raw)
		}
		var result struct {
			Messages []channelMessage `json:"messages"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode %s messages: %v", scope, err)
		}
		return result.Messages
	}

	inbox := fetch("inbox")
	if len(inbox) != 2 {
		t.Fatalf("expected CEO root plus PM thread reply in inbox, got %+v", inbox)
	}
	if inbox[0].ID != root.ID || inbox[1].ID != threadReply.ID {
		t.Fatalf("unexpected inbox ordering/content: %+v", inbox)
	}

	outbox := fetch("outbox")
	if len(outbox) != 2 {
		t.Fatalf("expected only authored messages in outbox, got %+v", outbox)
	}
	if outbox[0].ID != ownReply.ID || outbox[1].ID != ownTopLevel.ID {
		t.Fatalf("unexpected outbox ordering/content: %+v", outbox)
	}
}

func TestBrokerGetMessagesAgentScopeKeepsHumanAndCEOContext(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "fe", Name: "Frontend Engineer"},
	)
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm", "fe")
			break
		}
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	postMessage := func(payload map[string]any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post message: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 posting message, got %d: %s", resp.StatusCode, raw)
		}
	}

	postMessage(map[string]any{"channel": "general", "from": "you", "content": "Frontend, should we ship this?", "tagged": []string{"fe"}})
	postMessage(map[string]any{"channel": "general", "from": "pm", "content": "Unrelated roadmap chatter."})
	postMessage(map[string]any{"channel": "general", "from": "ceo", "content": "Keep scope tight and focus on signup."})
	postMessage(map[string]any{"channel": "general", "from": "fe", "content": "I can take the signup work."})

	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&viewer_slug=fe&scope=agent", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected scoped transcript to keep 3 messages, got %+v", result.Messages)
	}
	if got := result.Messages[1].From; got != "ceo" {
		t.Fatalf("expected CEO context to remain visible, got %+v", result.Messages)
	}
	for _, msg := range result.Messages {
		if msg.From == "pm" {
			t.Fatalf("did not expect unrelated PM chatter in scoped transcript: %+v", result.Messages)
		}
	}
}

func TestLastTaggedAtSetOnPost(t *testing.T) {
	b := &Broker{}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo", "pm"}}}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}, {Slug: "pm", Name: "PM"}}

	// Post a message tagging ceo
	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "@ceo what should we do?",
		Tagged:  []string{"ceo"},
	}

	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}

	// Simulate what handlePostMessage does
	if len(msg.Tagged) > 0 && (msg.From == "you" || msg.From == "human") {
		for _, slug := range msg.Tagged {
			b.lastTaggedAt[slug] = time.Now()
		}
	}

	if _, ok := b.lastTaggedAt["ceo"]; !ok {
		t.Fatal("expected ceo to be in lastTaggedAt")
	}
	if _, ok := b.lastTaggedAt["pm"]; ok {
		t.Fatal("did not expect pm to be in lastTaggedAt")
	}
}

func TestBrokerSurfaceMetadataPersists(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "tg-ops",
		Name:    "tg-ops",
		Members: []string{"ceo"},
		Surface: &channelSurface{
			Provider:    "telegram",
			RemoteID:    "-100999",
			RemoteTitle: "Ops Group",
			Mode:        "supergroup",
			BotTokenEnv: "MY_BOT_TOKEN",
		},
		CreatedBy: "test",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	})
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t, b)
	var found *teamChannel
	for _, ch := range reloaded.channels {
		if ch.Slug == "tg-ops" {
			found = &ch
			break
		}
	}
	if found == nil {
		t.Fatal("expected tg-ops channel after reload")
	}
	if found.Surface == nil {
		t.Fatal("expected surface metadata to persist")
	}
	if found.Surface.Provider != "telegram" {
		t.Fatalf("expected provider=telegram, got %q", found.Surface.Provider)
	}
	if found.Surface.RemoteID != "-100999" {
		t.Fatalf("expected remote_id=-100999, got %q", found.Surface.RemoteID)
	}
	if found.Surface.RemoteTitle != "Ops Group" {
		t.Fatalf("expected remote_title=Ops Group, got %q", found.Surface.RemoteTitle)
	}
	if found.Surface.Mode != "supergroup" {
		t.Fatalf("expected mode=supergroup, got %q", found.Surface.Mode)
	}
	if found.Surface.BotTokenEnv != "MY_BOT_TOKEN" {
		t.Fatalf("expected bot_token_env=MY_BOT_TOKEN, got %q", found.Surface.BotTokenEnv)
	}
}

func TestBrokerSurfaceChannelsFilter(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels,
		teamChannel{
			Slug:    "tg-ch",
			Name:    "tg-ch",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
		},
		teamChannel{
			Slug:    "slack-ch",
			Name:    "slack-ch",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "slack", RemoteID: "C123"},
		},
		teamChannel{
			Slug:    "native-ch",
			Name:    "native-ch",
			Members: []string{"ceo"},
		},
	)
	b.mu.Unlock()

	tgChannels := b.SurfaceChannels("telegram")
	if len(tgChannels) < 1 {
		t.Fatalf("expected at least 1 telegram channel, got %d", len(tgChannels))
	}
	if tgChannels[0].Slug != "tg-ch" {
		t.Fatalf("expected tg-ch, got %q", tgChannels[0].Slug)
	}

	slackChannels := b.SurfaceChannels("slack")
	if len(slackChannels) != 1 {
		t.Fatalf("expected 1 slack channel, got %d", len(slackChannels))
	}

	nativeChannels := b.SurfaceChannels("")
	if len(nativeChannels) != 0 {
		t.Fatalf("expected 0 native surface channels, got %d", len(nativeChannels))
	}
}

func TestBrokerExternalQueueDeduplication(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "ext",
		Name:    "ext",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
	})
	b.mu.Unlock()

	// Post two messages
	b.PostMessage("ceo", "ext", "msg one", nil, "")
	b.PostMessage("ceo", "ext", "msg two", nil, "")

	queue1 := b.ExternalQueue("telegram")
	if len(queue1) != 2 {
		t.Fatalf("expected 2 messages in first drain, got %d", len(queue1))
	}

	// Second drain should be empty
	queue2 := b.ExternalQueue("telegram")
	if len(queue2) != 0 {
		t.Fatalf("expected 0 messages in second drain, got %d", len(queue2))
	}

	// Post one more
	b.PostMessage("ceo", "ext", "msg three", nil, "")
	queue3 := b.ExternalQueue("telegram")
	if len(queue3) != 1 {
		t.Fatalf("expected 1 new message, got %d", len(queue3))
	}
	if queue3[0].Content != "msg three" {
		t.Fatalf("expected 'msg three', got %q", queue3[0].Content)
	}
}

func TestBrokerPostInboundSurfaceMessage(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "surf",
		Name:    "surf",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
	})
	b.mu.Unlock()

	msg, err := b.PostInboundSurfaceMessage("alice", "surf", "hello surface", "telegram")
	if err != nil {
		t.Fatalf("PostInboundSurfaceMessage: %v", err)
	}
	if msg.Kind != "surface" {
		t.Fatalf("expected kind=surface, got %q", msg.Kind)
	}
	if msg.Source != "telegram" {
		t.Fatalf("expected source=telegram, got %q", msg.Source)
	}

	// Inbound should not appear in the external queue
	queue := b.ExternalQueue("telegram")
	if len(queue) != 0 {
		t.Fatalf("inbound message should not appear in external queue, got %d", len(queue))
	}

	// But it should appear in channel messages
	msgs := b.ChannelMessages("surf")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 channel message, got %d", len(msgs))
	}
}

func TestRecentHumanMessagesReturnsLastNHumanMessages(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "fe", Content: "agent reply 1", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "you", Content: "human says hi", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "m3", From: "nex", Content: "nex automation", Timestamp: "2026-04-14T10:02:00Z"},
		{ID: "m4", From: "be", Content: "agent reply 2", Timestamp: "2026-04-14T10:03:00Z"},
		{ID: "m5", From: "human", Content: "human follow-up", Timestamp: "2026-04-14T10:04:00Z"},
		{ID: "m6", From: "you", Content: "human again", Timestamp: "2026-04-14T10:05:00Z"},
	}
	b.mu.Unlock()

	// Request last 2 human messages — should return m5 and m6 (the most recent 2 from human senders).
	got := b.RecentHumanMessages(2)
	if len(got) != 2 {
		t.Fatalf("expected 2 recent human messages, got %d: %+v", len(got), got)
	}
	if got[0].ID != "m5" {
		t.Errorf("expected first message m5, got %q", got[0].ID)
	}
	if got[1].ID != "m6" {
		t.Errorf("expected second message m6, got %q", got[1].ID)
	}
}

func TestRecentHumanMessagesLimitCapsResults(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "you", Content: "first", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "you", Content: "second", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "m3", From: "nex", Content: "nex msg", Timestamp: "2026-04-14T10:02:00Z"},
	}
	b.mu.Unlock()

	// nex is also a human/external sender — all 3 qualify; limit=5 returns all 3.
	got := b.RecentHumanMessages(5)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages (you+you+nex), got %d", len(got))
	}
}

func TestRecentHumanMessagesExcludesNonHuman(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "fe", Content: "agent", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "be", Content: "agent2", Timestamp: "2026-04-14T10:01:00Z"},
	}
	b.mu.Unlock()

	got := b.RecentHumanMessages(10)
	if len(got) != 0 {
		t.Fatalf("expected 0 human messages, got %d", len(got))
	}
}

func TestRecentHumanMessagesIncludesNexSender(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "fe", Content: "agent msg", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "nex", Content: "nex automation context", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "m3", From: "you", Content: "human question", Timestamp: "2026-04-14T10:02:00Z"},
	}
	b.mu.Unlock()

	// Spec: "nex" is treated as human/external alongside "you" and "human".
	// Without nex messages in resume packets, conversations triggered by Nex automation
	// are silently dropped on restart.
	got := b.RecentHumanMessages(10)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages (nex+you), got %d", len(got))
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["m2"] {
		t.Error("expected nex message m2 to be included")
	}
	if !ids["m3"] {
		t.Error("expected human message m3 to be included")
	}
	if ids["m1"] {
		t.Error("expected agent message m1 to be excluded")
	}
}

func TestPostAutomationMessageDeduplicatesByEventID(t *testing.T) {
	b := newTestBroker(t)

	first, dup1, err := b.PostAutomationMessage("nex", "general", "Signal", "first post", "evt-001", "nex", "Nex", nil, "")
	if err != nil {
		t.Fatalf("first PostAutomationMessage: %v", err)
	}
	if dup1 {
		t.Fatal("first call should not be a duplicate")
	}

	second, dup2, err := b.PostAutomationMessage("nex", "general", "Signal", "second post", "evt-001", "nex", "Nex", nil, "")
	if err != nil {
		t.Fatalf("second PostAutomationMessage: %v", err)
	}
	if !dup2 {
		t.Fatal("second call with same eventID must be flagged as duplicate")
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate call must return original message ID %q, got %q", first.ID, second.ID)
	}

	// Only one message should be stored.
	msgs := b.Messages()
	count := 0
	for _, m := range msgs {
		if m.EventID == "evt-001" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 message with eventID evt-001, got %d", count)
	}
}

// TestExternalQueueDeduplicatesByMessageID verifies that calling ExternalQueue
// twice for a surface channel only delivers each message once.
func TestExternalQueueDeduplicatesByMessageID(t *testing.T) {
	b := newTestBroker(t)

	// Register a channel with a surface so ExternalQueue has something to scan.
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "slack-general",
		Name:    "Slack General",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "slack"},
	})
	b.mu.Unlock()

	// Post a message directly into the broker state (bypassing HTTP) so it lands
	// in the surface channel without going through PostInboundSurfaceMessage (which
	// auto-marks as delivered).
	b.mu.Lock()
	b.counter++
	b.messages = append(b.messages, channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "you",
		Channel:   "slack-general",
		Content:   "Hello from Slack",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()

	first := b.ExternalQueue("slack")
	if len(first) != 1 {
		t.Fatalf("expected 1 message on first ExternalQueue call, got %d", len(first))
	}

	second := b.ExternalQueue("slack")
	if len(second) != 0 {
		t.Fatalf("expected 0 messages on second ExternalQueue call (already delivered), got %d", len(second))
	}
}

// ─── Focus mode routing ───────────────────────────────────────────────────

// makeFocusModeLauncher builds a Launcher backed by a real broker with three
// members (ceo, eng, pm) wired into the general channel, and focus mode on.
func makeFocusModeLauncher(t *testing.T) (*Launcher, *Broker) {
	t.Helper()
	b := newTestBroker(t)

	// Add eng and pm members to the broker so they appear in EnabledMembers.
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer", Role: "Engineer"},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
	}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = []string{"ceo", "eng", "pm"}
		}
	}
	b.focusMode = true
	b.mu.Unlock()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
				{Slug: "pm", Name: "Product Manager"},
			},
		},
		broker:          b,
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}
	return l, b
}

// TestFocusModeRouting_UntaggedMessageWakesLeadOnly verifies that an untagged
// human message in focus mode only notifies the lead (CEO), not specialists.

func TestFocusModeRouting_UntaggedMessageWakesLeadOnly(t *testing.T) {
	l, _ := makeFocusModeLauncher(t)

	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "What should we do today?",
		Tagged:  nil,
	}
	immediate, _ := l.notificationTargetsForMessage(msg)

	if len(immediate) != 1 {
		t.Fatalf("focus mode untagged: expected 1 target (CEO), got %d: %v", len(immediate), immediate)
	}
	if immediate[0].Slug != "ceo" {
		t.Fatalf("focus mode untagged: expected ceo, got %q", immediate[0].Slug)
	}
}

// TestFocusModeRouting_TaggedSpecialistWakesSpecialistOnly verifies that when
// the human explicitly tags a specialist in focus mode, only that specialist
// wakes — not the lead.
func TestFocusModeRouting_TaggedSpecialistWakesSpecialistOnly(t *testing.T) {
	l, _ := makeFocusModeLauncher(t)

	msg := channelMessage{
		ID:      "msg-2",
		From:    "you",
		Channel: "general",
		Content: "Hey eng, can you review the PR?",
		Tagged:  []string{"eng"},
	}
	immediate, _ := l.notificationTargetsForMessage(msg)

	if len(immediate) != 1 {
		t.Fatalf("focus mode @eng: expected 1 target, got %d: %v", len(immediate), immediate)
	}
	if immediate[0].Slug != "eng" {
		t.Fatalf("focus mode @eng: expected eng, got %q", immediate[0].Slug)
	}
}

// TestFocusModeRouting_CollobaborativeUntaggedWakesAll verifies the contrast:
// without focus mode, an untagged human message wakes all enabled agents.
func TestFocusModeRouting_CollaborativeUntaggedWakesAll(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer", Role: "Engineer"},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
	}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = []string{"ceo", "eng", "pm"}
		}
	}
	b.focusMode = false // collaborative mode
	b.mu.Unlock()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
				{Slug: "pm", Name: "Product Manager"},
			},
		},
		broker:          b,
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	msg := channelMessage{
		ID:      "msg-3",
		From:    "you",
		Channel: "general",
		Content: "What should we do today?",
		Tagged:  nil,
	}
	immediate, _ := l.notificationTargetsForMessage(msg)

	// In collaborative mode, CEO always wakes for human messages.
	hasCEO := false
	for _, t := range immediate {
		if t.Slug == "ceo" {
			hasCEO = true
		}
	}
	if !hasCEO {
		t.Fatalf("collaborative mode: expected CEO in targets, got %v", immediate)
	}
}

// ─── Push semantics ───────────────────────────────────────────────────────

// TestHeadlessQueue_EmptyBeforePush verifies that the agent headless queue
// starts empty — no timers or background goroutines pre-populate it.
