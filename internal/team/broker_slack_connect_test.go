package team

import (
	"errors"
	"testing"
)

func TestCreateSlackChannelBindsSurface(t *testing.T) {
	b := newTestBroker(t)

	ch, err := b.createSlackChannel("C0123", "general")
	if err != nil {
		t.Fatalf("createSlackChannel: %v", err)
	}
	if ch.Slug != "slack-general" {
		t.Fatalf("slug = %q, want slack-general", ch.Slug)
	}
	if ch.Surface == nil || ch.Surface.Provider != "slack" || ch.Surface.RemoteID != "C0123" {
		t.Fatalf("surface = %+v, want slack/C0123", ch.Surface)
	}

	// The transport discovers it via SurfaceChannels("slack").
	found := false
	for _, sc := range b.SurfaceChannels("slack") {
		if sc.Surface != nil && sc.Surface.RemoteID == "C0123" {
			found = true
		}
	}
	if !found {
		t.Fatal("SurfaceChannels(slack) should include the connected channel")
	}
}

func TestCreateSlackChannelIdempotentAndConflict(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.createSlackChannel("C0123", "general"); err != nil {
		t.Fatalf("first connect: %v", err)
	}

	// Reconnecting the same channel id to the same slug is idempotent.
	if _, err := b.createSlackChannel("C0123", "general"); !errors.Is(err, errChannelAlreadyExists) {
		t.Fatalf("reconnect should be idempotent, got %v", err)
	}

	// A different channel id on the same slug is a conflict.
	if _, err := b.createSlackChannel("C9999", "general"); !errors.Is(err, errSlackChannelAlreadyBridges) {
		t.Fatalf("conflicting bind should error, got %v", err)
	}
}

func TestIsSlackChannelIDAndSlug(t *testing.T) {
	for _, id := range []string{"C0123", "G0456"} {
		if !isSlackChannelID(id) {
			t.Fatalf("%q should be a valid slack channel id", id)
		}
	}
	for _, id := range []string{"", "U0123", "x"} {
		if isSlackChannelID(id) {
			t.Fatalf("%q should not be a valid slack channel id", id)
		}
	}
	if got := slackChannelSlug("Revenue Ops"); got != "slack-revenue-ops" {
		t.Fatalf("slug = %q, want slack-revenue-ops", got)
	}
	if got := slackChannelSlug("slack-foo"); got != "slack-foo" {
		t.Fatalf("slug = %q, want slack-foo (no double prefix)", got)
	}
}
