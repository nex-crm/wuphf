package team

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/company"
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

// Regression: manifest lists members not yet adopted in the broker. Before
// the fix, createTelegramChannel passed all manifest slugs to
// createChannelLocked, which rejected any unadopted slug with a 404. After
// the fix, unadopted slugs are silently dropped and the connect succeeds.
func TestCreateTelegramChannelSkipsUnadoptedManifestMembers(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()

	// Broker has only "ceo". The manifest lists two additional desired-state
	// members that haven't connected yet — use neutral slugs so the test
	// isn't coupled to any particular agent set.
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", BuiltIn: true}}
	b.memberIndex = nil // force lazy rebuild on next findMemberLocked
	b.mu.Unlock()

	manifestPath := filepath.Join(t.TempDir(), "company.json")
	raw, err := json.Marshal(company.Manifest{
		Lead: "ceo",
		Members: []company.MemberSpec{
			{Slug: "agent-a"},
			{Slug: "agent-b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WUPHF_COMPANY_FILE", manifestPath)

	// Must succeed despite the unadopted manifest members.
	ch, err := b.createTelegramChannel("standup", "Standup", 100, "group")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("got nil channel")
	}

	// Only the adopted "ceo" must be present. Checking exact set catches a
	// broken path that returns an empty list.
	if len(ch.Members) != 1 || ch.Members[0] != "ceo" {
		t.Fatalf("expected channel members [ceo], got %v", ch.Members)
	}
}

func TestCreateTelegramChannelSurfacesManifestReadError(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()

	manifestPath := filepath.Join(t.TempDir(), "company.json")
	if err := os.WriteFile(manifestPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WUPHF_COMPANY_FILE", manifestPath)

	_, err := b.createTelegramChannel("standup", "Standup", 100, "group")
	if err == nil {
		t.Fatal("expected manifest read error, got nil")
	}
	if !strings.Contains(err.Error(), "load company manifest") {
		t.Fatalf("expected manifest load context, got %v", err)
	}
}
