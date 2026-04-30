package main

import (
	"testing"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func freshCacheStore() *channelRenderCacheStore {
	return &channelRenderCacheStore{
		mainLines: make(map[uint64][]channelui.RenderedLine),
		sidebars:  make(map[uint64]string),
		markdown:  make(map[uint64]string),
		threaded:  make(map[uint64][]channelui.ThreadedMessage),
		blocks:    make(map[uint64][]channelui.RenderedLine),
	}
}

func TestCacheStoreMainLinesRoundTripIsClone(t *testing.T) {
	c := freshCacheStore()
	original := []channelui.RenderedLine{{Text: "one"}, {Text: "two"}}
	c.putMainLines(42, original)
	got, ok := c.getMainLines(42)
	if !ok || len(got) != 2 || got[0].Text != "one" {
		t.Fatalf("expected cached lines, got %v ok=%v", got, ok)
	}
	got[0].Text = "mutated"
	again, _ := c.getMainLines(42)
	if again[0].Text != "one" {
		t.Fatalf("cache must return a clone; mutation leaked: %v", again)
	}
	original[1].Text = "leaked"
	stored, _ := c.getMainLines(42)
	if stored[1].Text != "two" {
		t.Fatalf("cache must clone on put too; leaked: %v", stored)
	}
}

func TestCacheStoreMissReturnsFalse(t *testing.T) {
	c := freshCacheStore()
	if _, ok := c.getMainLines(7); ok {
		t.Fatalf("missing key should report ok=false")
	}
	if _, ok := c.getSidebar(7); ok {
		t.Fatalf("missing sidebar key should report ok=false")
	}
	if _, ok := c.getMarkdown(7); ok {
		t.Fatalf("missing markdown key should report ok=false")
	}
	if _, ok := c.getThreaded(7); ok {
		t.Fatalf("missing threaded key should report ok=false")
	}
	if _, ok := c.getViewportBlock(7); ok {
		t.Fatalf("missing viewport block key should report ok=false")
	}
}

func TestCacheStoreMainLinesEvictsAtLimit(t *testing.T) {
	c := freshCacheStore()
	for i := 0; i < mainLinesCacheLimit; i++ {
		c.putMainLines(uint64(i), []channelui.RenderedLine{{Text: "x"}})
	}
	if len(c.mainLines) != mainLinesCacheLimit {
		t.Fatalf("expected store at limit, got %d", len(c.mainLines))
	}
	// Insertion at the limit triggers a wipe before storing the new key.
	c.putMainLines(9999, []channelui.RenderedLine{{Text: "fresh"}})
	if _, ok := c.getMainLines(0); ok {
		t.Fatalf("oldest entry should have been evicted on overflow")
	}
	if got, ok := c.getMainLines(9999); !ok || got[0].Text != "fresh" {
		t.Fatalf("newest entry should remain after wipe, got %v ok=%v", got, ok)
	}
}

func TestCacheStoreSidebarRoundTrip(t *testing.T) {
	c := freshCacheStore()
	c.putSidebar(1, "rendered")
	got, ok := c.getSidebar(1)
	if !ok || got != "rendered" {
		t.Fatalf("expected sidebar hit, got %q ok=%v", got, ok)
	}
}

func TestCacheStoreMarkdownEvictionAndRetrieval(t *testing.T) {
	c := freshCacheStore()
	for i := 0; i < markdownCacheLimit; i++ {
		c.putMarkdown(uint64(i), "x")
	}
	c.putMarkdown(99999, "newest")
	if _, ok := c.getMarkdown(0); ok {
		t.Fatalf("oldest markdown entry should be evicted")
	}
	got, ok := c.getMarkdown(99999)
	if !ok || got != "newest" {
		t.Fatalf("newest markdown entry should be present, got %q ok=%v", got, ok)
	}
}

func TestCacheStoreThreadedClones(t *testing.T) {
	c := freshCacheStore()
	original := []channelui.ThreadedMessage{{Message: channelui.BrokerMessage{ID: "a"}, Depth: 0}}
	c.putThreaded(1, original)
	got, ok := c.getThreaded(1)
	if !ok || got[0].Message.ID != "a" {
		t.Fatalf("expected cached threaded message, got %v ok=%v", got, ok)
	}
	got[0].Message.ID = "mutated"
	again, _ := c.getThreaded(1)
	if again[0].Message.ID != "a" {
		t.Fatalf("threaded cache must return a clone")
	}
}

func TestMarkdownCacheKeyDistinguishesWidthAndContent(t *testing.T) {
	a := markdownCacheKey(80, "hello")
	b := markdownCacheKey(80, "hello")
	c := markdownCacheKey(80, "world")
	d := markdownCacheKey(120, "hello")
	if a != b {
		t.Fatalf("identical inputs must yield identical keys")
	}
	if a == c {
		t.Fatalf("different content must yield different key")
	}
	if a == d {
		t.Fatalf("different width must yield different key")
	}
}

func TestStateHasherStableForSameInputs(t *testing.T) {
	build := func() uint64 {
		h := newStateHasher()
		h.add("x", "y")
		h.addInt(7)
		h.addInt64(42)
		h.addBool(true)
		h.addMessages([]channelui.BrokerMessage{{ID: "m1", From: "fe", Content: "hi"}})
		h.addMembers([]channelui.Member{{Slug: "fe", Name: "Frontend"}})
		h.addChannels([]channelui.ChannelInfo{{Slug: "office", Name: "Office", Members: []string{"fe"}}})
		h.addTasks([]channelui.Task{{ID: "t1", Title: "Ship"}})
		h.addActions([]channelui.Action{{ID: "a1", Kind: "k", Summary: "did it"}})
		h.addRequests([]channelui.Interview{{ID: "r1", Question: "?"}})
		h.addDecisions([]channelui.Decision{{ID: "d1", Summary: "yes"}})
		h.addSignals([]channelui.Signal{{ID: "s1", Content: "noise"}})
		h.addWatchdogs([]channelui.Watchdog{{ID: "w1", Summary: "alert"}})
		h.addScheduler([]channelui.SchedulerJob{{Slug: "j1", Label: "Hourly"}})
		h.addExpandedThreads(map[string]bool{"a": true, "b": false, "c": true})
		return h.sum()
	}
	first := build()
	second := build()
	if first != second {
		t.Fatalf("state hasher must be deterministic for identical inputs (got %d vs %d)", first, second)
	}
}

func TestStateHasherChangesWithMessages(t *testing.T) {
	h1 := newStateHasher()
	h1.addMessages([]channelui.BrokerMessage{{ID: "m1", From: "fe", Content: "hello"}})

	h2 := newStateHasher()
	h2.addMessages([]channelui.BrokerMessage{{ID: "m1", From: "fe", Content: "goodbye"}})

	if h1.sum() == h2.sum() {
		t.Fatalf("changing message content must change hash")
	}
}

func TestStateHasherExpandedThreadsOrderInsensitive(t *testing.T) {
	h1 := newStateHasher()
	h1.addExpandedThreads(map[string]bool{"a": true, "b": true, "c": true})

	h2 := newStateHasher()
	h2.addExpandedThreads(map[string]bool{"c": true, "a": true, "b": true})

	if h1.sum() != h2.sum() {
		t.Fatalf("expanded thread set hash must be order-independent")
	}
}

func TestStateHasherIgnoresUnexpandedThreads(t *testing.T) {
	h1 := newStateHasher()
	h1.addExpandedThreads(map[string]bool{"a": true})

	h2 := newStateHasher()
	h2.addExpandedThreads(map[string]bool{"a": true, "b": false, "c": false})

	if h1.sum() != h2.sum() {
		t.Fatalf("unexpanded threads must not affect hash")
	}
}

func TestCloneRenderedLinesIndependentOfSource(t *testing.T) {
	src := []channelui.RenderedLine{{Text: "a"}, {Text: "b"}}
	clone := channelui.CloneRenderedLines(src)
	clone[0].Text = "mutated"
	if src[0].Text != "a" {
		t.Fatalf("clone must not share storage with source")
	}
	if got := channelui.CloneRenderedLines(nil); got != nil {
		t.Fatalf("nil input should clone to nil")
	}
}

func TestCloneThreadedMessagesIndependentOfSource(t *testing.T) {
	src := []channelui.ThreadedMessage{{Message: channelui.BrokerMessage{ID: "x"}}}
	clone := channelui.CloneThreadedMessages(src)
	clone[0].Message.ID = "mutated"
	if src[0].Message.ID != "x" {
		t.Fatalf("clone must not share storage with source")
	}
	if got := channelui.CloneThreadedMessages(nil); got != nil {
		t.Fatalf("nil input should clone to nil")
	}
}
