package team

import (
	"errors"
	"testing"
)

// Regression for the title-collision bug: SlugifyTelegramTitle is title-only,
// so two distinct Telegram chats with the same display name collide on slug.
// Without this guard the second connect would return the existing channel as
// "success" even though the channel still pointed at the first chat —
// messages would route to the wrong conversation. Asserts the second connect
// reports a conflict instead of silently rebinding.
func TestCreateTelegramChannelRejectsTitleCollision(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()

	if _, err := b.createTelegramChannel("standup", "Standup", 100, "group"); err != nil {
		t.Fatalf("first connect: unexpected error: %v", err)
	}

	// Same title (and therefore same slug) but a different chat id.
	_, err := b.createTelegramChannel("standup", "Standup", 200, "group")
	if err == nil {
		t.Fatal("second connect with colliding title: expected error, got nil")
	}
	if !errors.Is(err, errChannelAlreadyBridges) {
		t.Fatalf("second connect with colliding title: expected errChannelAlreadyBridges, got %v",
			err)
	}

	// Same chat id is idempotent — the user just asked for the same chat
	// twice. Should return the existing channel + the errChannelAlreadyExists
	// sentinel so the handler can fall through to a 200 response.
	ch, err := b.createTelegramChannel("standup", "Standup", 100, "group")
	if !errors.Is(err, errChannelAlreadyExists) {
		t.Fatalf("idempotent re-connect: expected errChannelAlreadyExists, got %v", err)
	}
	if ch == nil || ch.Slug != "standup" {
		t.Fatalf("idempotent re-connect: expected existing channel, got %+v", ch)
	}
}

// Regression for the "existing channel has nil/non-telegram surface"
// branch of the title-collision guard. A user-created or hand-edited channel
// that happens to share a slug with the new Telegram chat must NOT be
// silently rebound to a Telegram surface — that would make a non-telegram
// channel start receiving messages from a remote chat. Asserts that case
// yields errChannelAlreadyBridges (409) like the cross-chat collision does.
func TestCreateTelegramChannelRejectsNonTelegramSurface(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()

	// Seed a channel with no surface (mimics a hand-created channel).
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "standup",
		Name:    "Standup",
		Members: []string{"ceo"},
	})
	b.mu.Unlock()

	_, err := b.createTelegramChannel("standup", "Standup", 100, "group")
	if err == nil {
		t.Fatal("connect into non-telegram channel: expected error, got nil")
	}
	if !errors.Is(err, errChannelAlreadyBridges) {
		t.Fatalf("connect into non-telegram channel: expected errChannelAlreadyBridges, got %v", err)
	}
	if ch := b.findChannelLocked("standup"); ch == nil || ch.Surface != nil {
		t.Fatalf("connect into non-telegram channel: surface mutated: %+v", ch)
	}
}
