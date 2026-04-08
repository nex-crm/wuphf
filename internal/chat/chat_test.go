package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestChannelCRUD(t *testing.T) {
	dir := tempDir(t)
	cm, err := NewChannelManagerAt(filepath.Join(dir, "channels.json"))
	if err != nil {
		t.Fatalf("NewChannelManagerAt: %v", err)
	}

	// Create
	ch, err := cm.Create("dev", ChannelPublic, []string{"alice", "bob"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.Name != "dev" || ch.Type != ChannelPublic {
		t.Fatalf("unexpected channel: %+v", ch)
	}

	// List
	list := cm.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(list))
	}

	// Get
	got, ok := cm.Get(ch.ID)
	if !ok || got.Name != "dev" {
		t.Fatalf("Get failed: ok=%v, got=%+v", ok, got)
	}

	// Delete
	if err := cm.Delete(ch.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := cm.Get(ch.ID); ok {
		t.Fatal("channel still exists after delete")
	}
}

func TestChannelPersistence(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "channels.json")

	cm1, err := NewChannelManagerAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cm1.Create("persist-test", ChannelPublic, nil); err != nil {
		t.Fatal(err)
	}

	// Reload from disk.
	cm2, err := NewChannelManagerAt(path)
	if err != nil {
		t.Fatal(err)
	}
	list := cm2.List()
	if len(list) != 1 || list[0].Name != "persist-test" {
		t.Fatalf("persistence failed: %+v", list)
	}
}

func TestDMCreation(t *testing.T) {
	dir := tempDir(t)
	cm, err := NewChannelManagerAt(filepath.Join(dir, "channels.json"))
	if err != nil {
		t.Fatal(err)
	}

	// First call creates.
	ch1, err := cm.GetOrCreateDM("alice", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if ch1.Type != ChannelDirect {
		t.Fatalf("expected direct, got %s", ch1.Type)
	}

	// Second call (reversed order) returns same channel.
	ch2, err := cm.GetOrCreateDM("bob", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if ch1.ID != ch2.ID {
		t.Fatalf("expected same channel ID, got %q vs %q", ch1.ID, ch2.ID)
	}
}

func TestMessagePersistence(t *testing.T) {
	dir := tempDir(t)
	ms := NewMessageStoreAt(dir)

	channelID := "test-channel-1"
	msg, err := ms.Send(channelID, "alice", "Alice", "hello world", MsgText)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg.Content != "hello world" || msg.SenderSlug != "alice" {
		t.Fatalf("unexpected msg: %+v", msg)
	}

	// Send a second message.
	if _, err := ms.Send(channelID, "bob", "Bob", "hi alice", MsgText); err != nil {
		t.Fatal(err)
	}

	// List all.
	msgs, err := ms.List(channelID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// List with limit.
	msgs, err = ms.List(channelID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hi alice" {
		t.Fatalf("limit=1 returned unexpected: %+v", msgs)
	}

	// Verify file exists on disk.
	path := filepath.Join(dir, "msg-"+channelID+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("message file missing: %v", err)
	}
}

func TestSystemMessages(t *testing.T) {
	dir := tempDir(t)
	ms := NewMessageStoreAt(dir)

	channelID := "sys-test"
	msg, err := ms.SendSystem(channelID, "agent joined the channel")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgSystem || msg.SenderSlug != "system" {
		t.Fatalf("unexpected system msg: %+v", msg)
	}

	msgs, err := ms.List(channelID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Type != MsgSystem {
		t.Fatalf("expected 1 system message, got %+v", msgs)
	}
}

func TestRouterMentionRouting(t *testing.T) {
	dir := tempDir(t)
	cm, err := NewChannelManagerAt(filepath.Join(dir, "channels.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms := NewMessageStoreAt(dir)
	r := NewRouter(cm, ms)

	// Message with @mention should create a DM channel.
	result, err := r.Route("alice", "Alice", "hey @bob check this out")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mentions) != 1 || result.Mentions[0] != "bob" {
		t.Fatalf("expected mention of bob, got %v", result.Mentions)
	}

	// Verify DM channel was created.
	ch, ok := cm.Get(result.ChannelID)
	if !ok {
		t.Fatal("channel not found")
	}
	if ch.Type != ChannelDirect {
		t.Fatalf("expected direct channel, got %s", ch.Type)
	}

	// Verify message was stored.
	msgs, err := ms.List(result.ChannelID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hey @bob check this out" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
}

func TestRouterGeneralChannel(t *testing.T) {
	dir := tempDir(t)
	cm, err := NewChannelManagerAt(filepath.Join(dir, "channels.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms := NewMessageStoreAt(dir)
	r := NewRouter(cm, ms)

	// Message without mentions goes to general.
	result, err := r.Route("alice", "Alice", "hello everyone")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mentions) != 0 {
		t.Fatalf("expected no mentions, got %v", result.Mentions)
	}

	ch, ok := cm.Get(result.ChannelID)
	if !ok {
		t.Fatal("general channel not found")
	}
	if ch.Name != "general" || ch.Type != ChannelPublic {
		t.Fatalf("expected general public channel, got %+v", ch)
	}
}

func TestInjectOfficeInsight(t *testing.T) {
	dir := tempDir(t)
	cm, err := NewChannelManagerAt(filepath.Join(dir, "channels.json"))
	if err != nil {
		t.Fatal(err)
	}
	ms := NewMessageStoreAt(dir)
	r := NewRouter(cm, ms)

	ch, err := cm.Create("general", ChannelPublic, nil)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := r.InjectOfficeInsight(ch.ID, "Archived decisions and blockers.")
	if err != nil {
		t.Fatalf("InjectOfficeInsight: %v", err)
	}
	if msg.SenderSlug != "office-insight" {
		t.Fatalf("expected office-insight sender, got %q", msg.SenderSlug)
	}
	if msg.SenderName != "Office Insight" {
		t.Fatalf("expected Office Insight sender name, got %q", msg.SenderName)
	}
	if msg.Type != MsgSystem {
		t.Fatalf("expected system message type, got %q", msg.Type)
	}
}

func TestParseMentions(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello @bob", []string{"bob"}},
		{"@alice and @bob", []string{"alice", "bob"}},
		{"@alice @alice", []string{"alice"}}, // dedup
		{"no mentions here", nil},
		{"@bob, @charlie!", []string{"bob", "charlie"}}, // strip punctuation
		{"@", nil}, // empty mention
	}
	for _, tt := range tests {
		got := parseMentions(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseMentions(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseMentions(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
