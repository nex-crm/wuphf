package main

import (
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
)

// Epicenter tests for sidebar state extracted to channel_sidebar_state.go.
// Each test exercises one decision in the sidebar projection or cursor
// machinery; a failure points at exactly one method.

func TestSidebarItems_OneOnOneReturnsNil(t *testing.T) {
	m := channelModel{
		sessionMode:   string(team.SessionModeOneOnOne),
		oneOnOneAgent: "ceo",
		channels:      []channelui.ChannelInfo{{Slug: "general"}},
	}

	if got := m.sidebarItems(); got != nil {
		t.Fatalf("1:1 mode should hide sidebar, got %d items", len(got))
	}
}

func TestSidebarItems_PrependsChannelsBeforeApps(t *testing.T) {
	m := channelModel{
		channels: []channelui.ChannelInfo{
			{Slug: "general", Name: "general"},
			{Slug: "engineering", Name: "engineering"},
		},
	}

	items := m.sidebarItems()

	if len(items) < 3 {
		t.Fatalf("expected channels + apps, got %d items", len(items))
	}
	if items[0].Kind != "channel" || items[0].Value != "general" {
		t.Fatalf("first item should be #general, got %+v", items[0])
	}
	if items[1].Kind != "channel" || items[1].Value != "engineering" {
		t.Fatalf("second item should be #engineering, got %+v", items[1])
	}
	// Everything after the channels must be apps.
	for i, it := range items[2:] {
		if it.Kind != "app" {
			t.Fatalf("items[%d] expected kind=app, got %q", i+2, it.Kind)
		}
	}
}

func TestChannelSidebarItems_FallsBackToGeneralWhenEmpty(t *testing.T) {
	m := channelModel{}

	items := m.channelSidebarItems()

	if len(items) != 1 || items[0].Value != "general" {
		t.Fatalf("empty channel list should fall back to #general, got %+v", items)
	}
}

func TestClampSidebarCursor_ClampsToZeroWhenEmpty(t *testing.T) {
	m := channelModel{
		sessionMode:   string(team.SessionModeOneOnOne), // forces sidebarItems() to return nil
		sidebarCursor: 99,
	}

	m.clampSidebarCursor()

	if m.sidebarCursor != 0 {
		t.Fatalf("expected cursor=0 when no items, got %d", m.sidebarCursor)
	}
}

func TestClampSidebarCursor_ClampsToLastItem(t *testing.T) {
	m := channelModel{
		channels:      []channelui.ChannelInfo{{Slug: "general"}},
		sidebarCursor: 999,
	}

	m.clampSidebarCursor()

	items := m.sidebarItems()
	want := len(items) - 1
	if m.sidebarCursor != want {
		t.Fatalf("expected cursor clamped to %d, got %d", want, m.sidebarCursor)
	}
}

func TestClampSidebarCursor_ClampsToZeroFromNegative(t *testing.T) {
	m := channelModel{
		channels:      []channelui.ChannelInfo{{Slug: "general"}},
		sidebarCursor: -5,
	}

	m.clampSidebarCursor()

	if m.sidebarCursor != 0 {
		t.Fatalf("expected cursor=0 from negative, got %d", m.sidebarCursor)
	}
}

func TestSetSidebarCursorForItem_FindsMatch(t *testing.T) {
	m := channelModel{
		channels: []channelui.ChannelInfo{
			{Slug: "general"},
			{Slug: "engineering"},
		},
	}

	m.setSidebarCursorForItem(sidebarItem{Kind: "channel", Value: "engineering"})

	if m.sidebarCursor != 1 {
		t.Fatalf("expected cursor=1 for #engineering, got %d", m.sidebarCursor)
	}
}

func TestSyncSidebarCursorToActive_PrefersChannelMatchInMessagesApp(t *testing.T) {
	m := channelModel{
		channels: []channelui.ChannelInfo{
			{Slug: "general"},
			{Slug: "engineering"},
		},
		activeChannel: "engineering",
		activeApp:     channelui.OfficeAppMessages,
	}

	m.syncSidebarCursorToActive()

	if m.sidebarCursor != 1 {
		t.Fatalf("expected cursor on #engineering channel item, got %d", m.sidebarCursor)
	}
}
