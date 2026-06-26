package team

// broker_external_replay_test.go pins the restart-replay regression: messages
// loaded from persisted state must never re-enter an external surface's
// outbound queue. externalDelivered is in-memory only, so before loadState
// seeded it, every broker restart re-queued the whole channel history for
// Slack/Telegram delivery (observed live as a 315-message flood + rate-limit
// storm on boot).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalQueueDoesNotReplayPersistedHistoryAfterRestart(t *testing.T) {
	withDiskLoad(t)

	state := brokerState{
		Channels: []teamChannel{{
			Slug:    "slack-office",
			Name:    "slack-office",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "slack", RemoteID: "C0123"},
		}},
		Messages: []channelMessage{
			{ID: "m1", From: "ceo", Channel: "slack-office", Content: "old message 1", Timestamp: "2026-06-10T00:00:00Z"},
			{ID: "m2", From: "ceo", Channel: "slack-office", Content: "old message 2", Timestamp: "2026-06-10T00:01:00Z"},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	path := filepath.Join(t.TempDir(), "broker-state.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// Boot exactly as production does — loadState runs in the constructor.
	b := NewBrokerAt(path)

	if replayed := b.ExternalQueue("slack"); len(replayed) != 0 {
		t.Fatalf("restart replayed %d persisted message(s) to the external queue: %+v", len(replayed), replayed)
	}

	// New messages posted after boot still flow out.
	b.mu.Lock()
	b.messages = append(b.messages, channelMessage{
		ID: "m3", From: "ceo", Channel: "slack-office", Content: "fresh message", Timestamp: "2026-06-12T00:00:00Z",
	})
	b.mu.Unlock()

	fresh := b.ExternalQueue("slack")
	if len(fresh) != 1 || fresh[0].ID != "m3" {
		t.Fatalf("expected exactly the fresh message queued, got %+v", fresh)
	}
}
