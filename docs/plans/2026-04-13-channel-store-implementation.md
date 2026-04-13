# Channel Store & DM Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `dm-*` string prefix hack with a proper channel system using Mattermost-aligned types, UUID IDs (internal), slug-based external API, per-member read cursors, and extracted message storage.

**Architecture:** New `internal/channel` package owns Channel, ChannelMember, and Message data models with in-memory indexes. Broker embeds `channel.Store` and delegates all channel/message operations to it. Existing `internal/chat/` is deleted. DM slugs change from `dm-{agent}` to deterministic `{a}__{b}` pairs. All 50+ `dm-` prefix references across 13 files are replaced with `channel.Type == "D"` checks.

**Tech Stack:** Go 1.25+ (per go.mod), `github.com/google/uuid`, SHA1 for group slugs, JSON persistence embedded in broker-state.json.

**Note:** All `go test` commands assume working directory is the worktree root (`/Users/najmuzzaman/Documents/nex/WUPHF/.worktrees/debug-dm-specialists/`). The Store API adds `IncrementMentionsForTagged` beyond what the spec defines, needed for public channel @mention semantics.

**Spec:** `docs/specs/2026-04-13-channel-store-dm-redesign.md`

---

## File Structure

```
internal/channel/           (NEW — sole authority for channels, members, messages)
├── types.go                Channel, ChannelMember, ChannelType, ChannelFilter, Message
├── slug.go                 DirectSlug, GroupSlug
├── slug_test.go            Determinism + ordering tests
├── store.go                Store struct, CRUD, DM/group ops, member ops, indexes
├── store_test.go           Full coverage of Store methods
├── message.go              AppendMessage, ChannelMessages, ThreadMessages, IncrementMentions
├── message_test.go         Message storage + mention semantics tests
├── cursor.go               MarkRead, MarkProcessed, GetMember cursor ops
├── cursor_test.go          Read cursor tests
├── migration.go            Boot migration from dm-* to new format
└── migration_test.go       Migration idempotency tests

internal/chat/              (DELETED — replaced by internal/channel)

internal/team/broker.go     (MODIFIED — embed channel.Store, delegate channel/message ops)
internal/team/launcher.go   (MODIFIED — DM detection via Store, work packet DM preamble)
internal/teammcp/server.go  (MODIFIED — add team_dm_open tool)
web/index.html              (MODIFIED — replace dm-* prefix checks with channel type checks)
cmd/wuphf/channel.go        (MODIFIED — TUI channel switching via channel types)
```

---

### Task 1: Create `internal/channel` types and slug generation

**Files:**
- Create: `internal/channel/types.go`
- Create: `internal/channel/slug.go`
- Create: `internal/channel/slug_test.go`

- [ ] **Step 1: Write slug determinism tests**

```go
// internal/channel/slug_test.go
package channel

import "testing"

func TestDirectSlugIsSorted(t *testing.T) {
	// Same pair, different order → same slug
	if DirectSlug("engineering", "human") != DirectSlug("human", "engineering") {
		t.Error("DirectSlug must be order-independent")
	}
}

func TestDirectSlugFormat(t *testing.T) {
	slug := DirectSlug("human", "engineering")
	if slug != "engineering__human" {
		t.Errorf("expected engineering__human, got %s", slug)
	}
}

func TestGroupSlugDeterministic(t *testing.T) {
	a := GroupSlug([]string{"human", "engineering", "design"})
	b := GroupSlug([]string{"design", "human", "engineering"})
	if a != b {
		t.Error("GroupSlug must be order-independent")
	}
	if len(a) != 40 {
		t.Errorf("expected 40-char SHA1 hex, got %d chars", len(a))
	}
}

func TestDirectSlugDoesNotCollideWithGroup(t *testing.T) {
	d := DirectSlug("human", "engineering")
	g := GroupSlug([]string{"human", "engineering"})
	if d == g {
		t.Error("direct and group slugs must not collide")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run TestDirect`
Expected: FAIL — package does not exist yet

- [ ] **Step 3: Implement types.go**

```go
// internal/channel/types.go
package channel

// ChannelType identifies the kind of channel (Mattermost-aligned).
type ChannelType string

const (
	ChannelTypePublic  ChannelType = "O" // Public channels (general, engineering)
	ChannelTypeDirect  ChannelType = "D" // 1:1 DMs (human + one agent)
	ChannelTypeGroup   ChannelType = "G" // Group DMs (human + N agents)
)

// Channel represents a communication channel.
type Channel struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	Type        ChannelType `json:"type"`
	CreatedBy   string      `json:"created_by,omitempty"`
	CreatedAt   string      `json:"created_at,omitempty"`
	UpdatedAt   string      `json:"updated_at,omitempty"`
	LastPostAt  string      `json:"last_post_at,omitempty"`
	Description string      `json:"description,omitempty"`
}

// ChannelMember tracks a member's relationship to a channel.
type ChannelMember struct {
	ChannelID       string `json:"channel_id"`
	Slug            string `json:"slug"`
	Role            string `json:"role,omitempty"`
	LastReadID      string `json:"last_read_id,omitempty"`
	LastProcessedID string `json:"last_processed_id,omitempty"`
	MentionCount    int    `json:"mention_count"`
	NotifyLevel     string `json:"notify_level"`
	JoinedAt        string `json:"joined_at,omitempty"`
}

// ChannelFilter constrains Store.List results.
type ChannelFilter struct {
	Type   ChannelType // empty = all
	Member string      // empty = all; set = only channels containing this member
}

// Message represents a single message in a channel.
// Moved from internal/team/broker.go channelMessage.
type Message struct {
	ID          string            `json:"id"`
	From        string            `json:"from"`
	Channel     string            `json:"channel,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Source      string            `json:"source,omitempty"`
	SourceLabel string            `json:"source_label,omitempty"`
	EventID     string            `json:"event_id,omitempty"`
	Title       string            `json:"title,omitempty"`
	Content     string            `json:"content"`
	Tagged      []string          `json:"tagged"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Timestamp   string            `json:"timestamp"`
	Reactions   []MessageReaction `json:"reactions,omitempty"`
}

// MessageReaction represents a reaction to a message.
type MessageReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}
```

- [ ] **Step 4: Implement slug.go**

```go
// internal/channel/slug.go
package channel

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"sort"
)

// DirectSlug returns a deterministic lookup key for a 1:1 DM.
// The two slugs are sorted lexicographically and joined with "__".
func DirectSlug(a, b string) string {
	if a > b {
		return b + "__" + a
	}
	return a + "__" + b
}

// GroupSlug returns a deterministic lookup key for a group DM.
// SHA1 hash of sorted member slugs (Mattermost-aligned).
func GroupSlug(members []string) string {
	sorted := make([]string, len(members))
	copy(sorted, members)
	sort.Strings(sorted)
	h := sha1.New()
	for _, m := range sorted {
		io.WriteString(h, m)
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestDirect|TestGroup"`
Expected: PASS (all 4 tests)

- [ ] **Step 6: Commit**

```bash
git add internal/channel/types.go internal/channel/slug.go internal/channel/slug_test.go
git commit -m "feat(channel): add types, slug generation with Mattermost-aligned channel model"
```

---

### Task 2: Implement Store — channel CRUD + indexes

**Files:**
- Create: `internal/channel/store.go`
- Create: `internal/channel/store_test.go`

- [ ] **Step 1: Write failing tests for Store CRUD**

```go
// internal/channel/store_test.go
package channel

import (
	"testing"
)

func TestStoreCreateAndGet(t *testing.T) {
	s := NewStore()
	ch, err := s.Create(Channel{Name: "general", Slug: "general", Type: ChannelTypePublic})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.ID == "" {
		t.Error("expected UUID to be generated")
	}
	got, ok := s.Get(ch.ID)
	if !ok || got.Slug != "general" {
		t.Error("Get by ID failed")
	}
	got2, ok := s.GetBySlug("general")
	if !ok || got2.ID != ch.ID {
		t.Error("GetBySlug failed")
	}
}

func TestStoreCreateRejectsDuplicateSlug(t *testing.T) {
	s := NewStore()
	s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	_, err := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	if err == nil {
		t.Error("expected error on duplicate slug")
	}
}

func TestStoreDelete(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "temp", Type: ChannelTypePublic})
	if err := s.Delete(ch.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, ok := s.Get(ch.ID)
	if ok {
		t.Error("channel should be gone after delete")
	}
}

func TestStoreListFilterByType(t *testing.T) {
	s := NewStore()
	s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.Create(Channel{Slug: "human__eng", Type: ChannelTypeDirect})
	pubs := s.List(ChannelFilter{Type: ChannelTypePublic})
	if len(pubs) != 1 || pubs[0].Slug != "general" {
		t.Errorf("expected 1 public channel, got %d", len(pubs))
	}
	dms := s.List(ChannelFilter{Type: ChannelTypeDirect})
	if len(dms) != 1 {
		t.Errorf("expected 1 direct channel, got %d", len(dms))
	}
}

func TestStoreGetOrCreateDirect(t *testing.T) {
	s := NewStore()
	ch1, err := s.GetOrCreateDirect("human", "engineering")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if ch1.Type != ChannelTypeDirect {
		t.Errorf("expected type D, got %s", ch1.Type)
	}
	if ch1.Slug != DirectSlug("human", "engineering") {
		t.Errorf("expected deterministic slug, got %s", ch1.Slug)
	}
	// Idempotent: second call returns same channel
	ch2, _ := s.GetOrCreateDirect("engineering", "human")
	if ch2.ID != ch1.ID {
		t.Error("GetOrCreateDirect should be idempotent")
	}
}

func TestStoreGetOrCreateDirectAddsMembersWithAllNotify(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	members := s.Members(ch.ID)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
	for _, m := range members {
		if m.NotifyLevel != "all" {
			t.Errorf("DM members should default to notify=all, got %s for %s", m.NotifyLevel, m.Slug)
		}
	}
}

func TestStoreGetOrCreateGroup(t *testing.T) {
	s := NewStore()
	ch, err := s.GetOrCreateGroup([]string{"human", "engineering", "design"}, "human")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if ch.Type != ChannelTypeGroup {
		t.Errorf("expected type G, got %s", ch.Type)
	}
	members := s.Members(ch.ID)
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestStore"`
Expected: FAIL — NewStore not defined

- [ ] **Step 3: Implement store.go**

Create `internal/channel/store.go` with:
- `NewStore()` constructor initializing empty slices and index maps
- `Create(ch Channel)` — generates UUID via `uuid.NewString()`, validates no duplicate slug, adds to slices + indexes, sets `CreatedAt`/`UpdatedAt`
- `Get(id)` — O(1) via `byID` map
- `GetBySlug(slug)` — O(1) via `bySlug` map
- `List(filter)` — iterate channels, filter by Type and/or Member (using `memberOf` index)
- `Delete(id)` — remove from slices + all three indexes, remove associated members
- `GetOrCreateDirect(a, b)` — compute `DirectSlug(a, b)`, check `bySlug`, create if missing with Type=D, auto-add both members with NotifyLevel="all"
- `GetOrCreateGroup(members, createdBy)` — compute `GroupSlug(members)`, check `bySlug`, create if missing with Type=G, auto-add all members with NotifyLevel="all"
- `FindDirectByMembers(a, b)` — `GetBySlug(DirectSlug(a, b))` + type check
- `OtherMember(channelID, slug)` — for type D, return the other member
- `AddMember(channelID, slug, notifyLevel)` — append to members slice, update `memberOf` index
- `RemoveMember(channelID, slug)` — remove from members slice + index
- `Members(channelID)` — filter members slice by channelID
- `IsMember(channelID, slug)` — check `memberOf` index
- `MemberChannels(slug)` — use `memberOf` index to return all channels
- `IsDirectMessage(channelID)` / `IsGroupMessage(channelID)` — type check via `byID`
- `MarshalJSON()` / `UnmarshalJSON()` — serialize `{channels, members, messages, counter}`, rebuild indexes on unmarshal
- `rebuildIndexes()` — called after UnmarshalJSON, populates byID, bySlug, memberOf

Index maps:
```go
byID     map[string]*Channel    // channel.ID → *Channel
bySlug   map[string]*Channel    // channel.Slug → *Channel
memberOf map[string][]string    // member slug → []channel IDs
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestStore"`
Expected: PASS (all 7 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/channel/store.go internal/channel/store_test.go
git commit -m "feat(channel): implement Store with CRUD, DM/group creation, member management, indexes"
```

---

### Task 3: Implement message storage and mention semantics

**Files:**
- Create: `internal/channel/message.go`
- Create: `internal/channel/message_test.go`

- [ ] **Step 1: Write failing tests for message storage**

```go
// internal/channel/message_test.go
package channel

import "testing"

func TestAppendAndRetrieveMessages(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.AppendMessage(Message{ID: "msg-1", From: "human", Channel: ch.Slug, Content: "hello"})
	msgs := s.ChannelMessages(ch.Slug)
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestIncrementMentionsDM(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	// Human sends message → engineering's mention count goes up
	s.AppendMessage(Message{ID: "msg-1", From: "human", Channel: ch.Slug, Content: "hey"})
	s.IncrementMentions(ch.Slug, "human")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.MentionCount != 1 {
		t.Errorf("expected 1 mention for engineering, got %d", m.MentionCount)
	}
	// Sender's own count should not increase
	mh, _ := s.GetMember(ch.ID, "human")
	if mh.MentionCount != 0 {
		t.Errorf("sender should have 0 mentions, got %d", mh.MentionCount)
	}
}

func TestIncrementMentionsPublicChannel(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.AddMember(ch.ID, "human", "mention")
	s.AddMember(ch.ID, "engineering", "mention")
	// Public channel: IncrementMentions only bumps explicitly tagged members
	s.IncrementMentionsForTagged(ch.Slug, "human", []string{"engineering"})
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.MentionCount != 1 {
		t.Errorf("expected 1 mention for tagged member, got %d", m.MentionCount)
	}
}

func TestThreadMessages(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	s.AppendMessage(Message{ID: "msg-1", From: "human", Channel: ch.Slug, Content: "root"})
	s.AppendMessage(Message{ID: "msg-2", From: "engineering", Channel: ch.Slug, Content: "reply", ReplyTo: "msg-1"})
	s.AppendMessage(Message{ID: "msg-3", From: "human", Channel: ch.Slug, Content: "other thread"})
	thread := s.ThreadMessages(ch.Slug, "msg-1")
	if len(thread) != 2 {
		t.Errorf("expected 2 messages in thread, got %d", len(thread))
	}
}

func TestIncrementMentionsDeletedChannelIsNoOp(t *testing.T) {
	s := NewStore()
	// Incrementing mentions on a channel slug that doesn't exist should not panic
	s.IncrementMentions("nonexistent-channel", "human")
	// No error, no panic — silent skip per spec failure modes
}

func TestCounterIncrements(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	id1 := s.NextMessageID()
	s.AppendMessage(Message{ID: id1, From: "human", Channel: ch.Slug, Content: "first"})
	id2 := s.NextMessageID()
	if id1 == id2 {
		t.Error("counter should produce unique IDs")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestAppend|TestIncrement|TestThread|TestCounter"`
Expected: FAIL

- [ ] **Step 3: Implement message.go**

Create `internal/channel/message.go` with:
- `AppendMessage(msg Message)` — append to messages slice, update `LastPostAt` on channel
- `ChannelMessages(channelSlug string)` — filter messages by channel slug, return slice
- `ThreadMessages(channelSlug, threadID string)` — filter by channel + (ID == threadID || ReplyTo == threadID)
- `AllMessages()` — return copy of all messages
- `NextMessageID()` — increment counter, return `fmt.Sprintf("msg-%d", counter)`
- `IncrementMentions(channelSlug, senderSlug string)` — for DM/Group: bump MentionCount for all members except sender. Find channel by slug, check type.
- `IncrementMentionsForTagged(channelSlug, senderSlug string, tagged []string)` — for public channels: bump only members in `tagged` list

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestAppend|TestIncrement|TestThread|TestCounter"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/channel/message.go internal/channel/message_test.go
git commit -m "feat(channel): implement message storage with DM/channel mention semantics"
```

---

### Task 4: Implement read cursors

**Files:**
- Create: `internal/channel/cursor.go`
- Create: `internal/channel/cursor_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/channel/cursor_test.go
package channel

import "testing"

func TestMarkReadAdvancesCursor(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	s.MarkRead(ch.ID, "engineering", "msg-5")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.LastReadID != "msg-5" {
		t.Errorf("expected LastReadID=msg-5, got %s", m.LastReadID)
	}
}

func TestMarkProcessedAdvancesCursor(t *testing.T) {
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	s.MarkProcessed(ch.ID, "engineering", "msg-3")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.LastProcessedID != "msg-3" {
		t.Errorf("expected LastProcessedID=msg-3, got %s", m.LastProcessedID)
	}
}

func TestMarkProcessedNotAdvancedOnMissedCall(t *testing.T) {
	// Simulates agent crash: MarkRead advances but MarkProcessed is never called.
	// On next poll, LastProcessedID should still be empty, allowing retry.
	s := NewStore()
	ch, _ := s.GetOrCreateDirect("human", "engineering")
	s.MarkRead(ch.ID, "engineering", "msg-5")
	m, _ := s.GetMember(ch.ID, "engineering")
	if m.LastReadID != "msg-5" {
		t.Error("LastReadID should advance")
	}
	if m.LastProcessedID != "" {
		t.Error("LastProcessedID should remain empty until explicitly set")
	}
}

func TestMarkReadOnNonMemberIsNoOp(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	err := s.MarkRead(ch.ID, "nonexistent", "msg-1")
	if err == nil {
		t.Error("expected error marking read for non-member")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestMark"`
Expected: FAIL

- [ ] **Step 3: Implement cursor.go**

Create `internal/channel/cursor.go` with:
- `MarkRead(channelID, slug, messageID string) error` — find member, set LastReadID
- `MarkProcessed(channelID, slug, messageID string) error` — find member, set LastProcessedID, reset MentionCount to 0
- `GetMember(channelID, slug string) (*ChannelMember, bool)` — find member by channel+slug pair

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestMark"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/channel/cursor.go internal/channel/cursor_test.go
git commit -m "feat(channel): implement read cursors for unread tracking and double-post prevention"
```

---

### Task 5: Implement boot migration from `dm-*` format

**Files:**
- Create: `internal/channel/migration.go`
- Create: `internal/channel/migration_test.go`

- [ ] **Step 1: Write failing migration tests**

```go
// internal/channel/migration_test.go
package channel

import "testing"

func TestMigrateDMSlug(t *testing.T) {
	s := NewStore()
	// Simulate old broker state: channel with dm-* slug
	s.channels = append(s.channels, Channel{
		Slug:    "dm-engineering",
		Name:    "dm-engineering",
		Type:    "dm", // old type value
	})
	s.messages = append(s.messages, Message{
		ID: "msg-1", From: "human", Channel: "dm-engineering", Content: "hello",
	})
	s.rebuildIndexes()

	migrated := s.MigrateLegacyDM()
	if !migrated {
		t.Error("expected migration to occur")
	}

	// Channel should now have proper type and slug
	ch, ok := s.GetBySlug(DirectSlug("human", "engineering"))
	if !ok {
		t.Fatal("migrated channel not found by new slug")
	}
	if ch.Type != ChannelTypeDirect {
		t.Errorf("expected type D, got %s", ch.Type)
	}
	if ch.ID == "" {
		t.Error("expected UUID to be assigned")
	}

	// Messages should reference new slug
	msgs := s.ChannelMessages(ch.Slug)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message after migration, got %d", len(msgs))
	}

	// Members should be created
	members := s.Members(ch.ID)
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
}

func TestMigratePublicChannel(t *testing.T) {
	s := NewStore()
	s.channels = append(s.channels, Channel{
		Slug: "general",
		Name: "general",
	})
	s.rebuildIndexes()

	s.MigrateLegacyDM()

	ch, ok := s.GetBySlug("general")
	if !ok {
		t.Fatal("general channel should still exist")
	}
	if ch.Type != ChannelTypePublic {
		t.Errorf("expected type O, got %s", ch.Type)
	}
	if ch.ID == "" {
		t.Error("expected UUID to be assigned")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	s := NewStore()
	ch, _ := s.Create(Channel{Slug: "general", Type: ChannelTypePublic})
	originalID := ch.ID
	migrated := s.MigrateLegacyDM()
	if migrated {
		t.Error("migration should be no-op when channels already have UUIDs and proper types")
	}
	ch2, _ := s.GetBySlug("general")
	if ch2.ID != originalID {
		t.Error("UUID should not change on no-op migration")
	}
}

func TestMigrateTaskChannelReferences(t *testing.T) {
	old := "dm-engineering"
	expected := DirectSlug("human", "engineering")
	result := MigrateDMSlugString(old)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
	// Non-DM slugs should pass through unchanged
	if MigrateDMSlugString("general") != "general" {
		t.Error("non-DM slugs should pass through unchanged")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestMigrate"`
Expected: FAIL

- [ ] **Step 3: Implement migration.go**

Create `internal/channel/migration.go` with:
- `MigrateLegacyDM() bool` — iterate channels:
  - If slug starts with `"dm-"` and Type is not `"D"`: extract agent slug, compute `DirectSlug("human", agent)`, update slug + type + generate UUID, create members, update all messages referencing old slug
  - If Type is empty and slug doesn't start with `"dm-"`: set Type to `"O"`, generate UUID if missing
  - Return true if any changes made
  - Rebuild indexes after migration
- `MigrateDMSlugString(slug string) string` — exported helper for broker to use when migrating task/request channel refs. If slug starts with `"dm-"`, convert to `DirectSlug("human", trimmed)`. Otherwise return unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v -run "TestMigrate"`
Expected: PASS

- [ ] **Step 5: Run full channel package test suite**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/channel/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/channel/migration.go internal/channel/migration_test.go
git commit -m "feat(channel): implement boot migration from dm-* prefix to deterministic pair slugs"
```

---

### Task 6: Delete `internal/chat/` package

**Files:**
- Delete: `internal/chat/` (all files)
- Modify: any files that import `internal/chat/`

- [ ] **Step 1: Find all imports of internal/chat**

Run: `cd .worktrees/debug-dm-specialists && grep -r '"github.com/nex-crm/wuphf/internal/chat"' --include="*.go" -l`

- [ ] **Step 2: Remove or replace all imports**

For each file found: remove the import and any references to `chat.` types. The `channel` package now provides equivalent types.

- [ ] **Step 3: Delete the chat package**

Run: `rm -rf internal/chat/`

- [ ] **Step 4: Verify build compiles**

Run: `cd .worktrees/debug-dm-specialists && go build ./...`
Expected: Success (no broken imports)

- [ ] **Step 5: Run full test suite**

Run: `cd .worktrees/debug-dm-specialists && go test ./...`
Expected: ALL PASS (chat tests are gone, all other packages pass)

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: delete internal/chat/ — replaced by internal/channel/"
```

---

### Task 7: Integrate channel.Store into Broker

This is the largest task. The broker embeds `channel.Store`, delegates channel/message operations, and uses the Store for persistence within `broker-state.json`.

**Files:**
- Modify: `internal/team/broker.go`

- [ ] **Step 1: Add channel.Store field to Broker struct**

Add `channelStore *channel.Store` field to the `Broker` struct (around line 391). Import the channel package.

- [ ] **Step 2: Initialize Store in NewBroker()**

In the `NewBroker()` function, initialize: `channelStore: channel.NewStore()`.

- [ ] **Step 3: Update brokerState to embed channel store**

In the `brokerState` struct (line 343), replace `Messages []channelMessage` and `Channels []teamChannel` with:
```go
ChannelStore json.RawMessage `json:"channel_store,omitempty"`
```
Keep `Counter` in brokerState temporarily for backward compat during migration.

- [ ] **Step 4: Update loadState to unmarshal channel store**

In `loadState()` (line 1560):
- After unmarshaling `brokerState`, check if `state.ChannelStore` exists
- If yes: `json.Unmarshal(state.ChannelStore, b.channelStore)`
- If no (legacy state): load old `state.Messages` into store via migration, load old `state.Channels` into store, call `b.channelStore.MigrateLegacyDM()`, migrate task/request/surface channel refs using `channel.MigrateDMSlugString()` — iterate `b.tasks`, `b.requests`, and channel surface configs to replace any `dm-*` slugs with deterministic pair slugs

- [ ] **Step 5: Update saveLocked to marshal channel store**

In `saveLocked()` (line 1607):
- Replace `Messages: b.messages` with: marshal `b.channelStore` to `json.RawMessage` and assign to `state.ChannelStore`
- Remove `Channels: b.channels` assignment

- [ ] **Step 6: Replace IsDMSlug/DMSlugFor/DMTargetAgent with Store methods**

Search and replace across broker.go:
- `IsDMSlug(slug)` → `b.channelStore.IsDirectMessageBySlug(slug)` (add this helper to Store: check if slug matches a type-D channel)
- `DMSlugFor(agentSlug)` → `channel.DirectSlug("human", agentSlug)`
- `DMTargetAgent(slug)` → use `b.channelStore.OtherMember()` or extract from slug
- `ensureDMConversationLocked(slug)` → `b.channelStore.GetOrCreateDirect("human", agentSlug)`

Delete the `IsDMSlug`, `DMSlugFor`, `DMTargetAgent`, and `ensureDMConversationLocked` functions.

- [ ] **Step 7: Replace findChannelLocked with Store.GetBySlug**

All calls to `b.findChannelLocked(slug)` become `b.channelStore.GetBySlug(slug)`. Adapt return types (returns `*Channel, bool` instead of `*teamChannel`).

- [ ] **Step 8: Replace canAccessChannelLocked with Store.IsMember**

Update `canAccessChannelLocked` to delegate to `b.channelStore.IsMember(channelID, slug)` for the agent membership check. Keep the human/CEO universal-access shortcuts.

- [ ] **Step 9: Replace appendMessageLocked with Store.AppendMessage**

Replace `b.messages = append(b.messages, msg)` with `b.channelStore.AppendMessage(msg)`. The broker keeps `publishMessageLocked(msg)` for pub/sub — the Store doesn't know about subscribers.

- [ ] **Step 10: Replace ChannelMessages with Store.ChannelMessages**

Delegate `b.ChannelMessages(channel)` to `b.channelStore.ChannelMessages(channel)`.

- [ ] **Step 11: Update handlePostMessage to use Store**

In `handlePostMessage` (line 4001):
- Replace `b.findChannelLocked(channel)` + `ensureDMConversationLocked` with Store lookups
- Replace `b.appendMessageLocked(msg)` with `b.channelStore.AppendMessage(msg)` + `b.publishMessageLocked(msg)`
- Call `b.channelStore.IncrementMentions()` or `IncrementMentionsForTagged()` based on channel type

- [ ] **Step 12: Update handleGetMessages to use Store**

In `handleGetMessages` (line 4330):
- Replace channel lookup with Store
- Replace message filtering with `Store.ChannelMessages` + `Store.ThreadMessages`

- [ ] **Step 13: Add POST /channels/dm endpoint**

Add new HTTP handler:
```go
func (b *Broker) handleCreateDM(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Members []string `json:"members"`
		Type    string   `json:"type"` // "direct" or "group"
	}
	// Parse, validate, call Store.GetOrCreateDirect or GetOrCreateGroup
	// Return channel ID, slug, type, name, created flag
}
```
Register in HTTP mux alongside existing `/channels` endpoint.

- [ ] **Step 14: Remove teamChannel struct and related dead code**

Delete: `teamChannel` struct, `isDM()` method, `dmName()` from chat package references, `channelHasMemberLocked`, `enabledChannelMembersLocked` (replace with Store equivalents).

- [ ] **Step 15: Run full test suite**

Run: `cd .worktrees/debug-dm-specialists && go test ./...`
Expected: ALL PASS (some existing tests may need adaptation for new types)

- [ ] **Step 16: Commit**

```bash
git add internal/team/broker.go internal/channel/
git commit -m "refactor(broker): embed channel.Store, delegate channel/message ops, add POST /channels/dm"
```

---

### Task 8: Update launcher DM detection and work packet

**Files:**
- Modify: `internal/team/launcher.go`

- [ ] **Step 1: Update notificationTargetsForMessage**

Replace `IsDMSlug(ch)` check (line 664) with:
```go
if ch, ok := l.broker.channelStore.GetBySlug(normalizeChannelSlug(msg.Channel)); ok && ch.Type == channel.ChannelTypeDirect {
```
Replace `DMTargetAgent(ch)` with `l.broker.channelStore.OtherMember(ch.ID, msg.From)`.

- [ ] **Step 2: Update responseInstructionForTarget with DM preamble**

Replace the DM-aware branch we added in the bugfix (line 2237) with a richer version that uses `channel.Store`:
```go
if ch, ok := l.broker.channelStore.GetBySlug(normalizeChannelSlug(msg.Channel)); ok && ch.Type == channel.ChannelTypeDirect {
    other, _ := l.broker.channelStore.OtherMember(ch.ID, slug)
    if other != "" {
        return fmt.Sprintf("You are @%s. The human is messaging you directly in a DM. Respond helpfully from your domain expertise.", slug)
    }
}
```

- [ ] **Step 3: Update buildMessageWorkPacket with DM context header**

In `buildMessageWorkPacket` (line 2246), add DM context header before the work packet body:
```go
if ch, ok := l.broker.channelStore.GetBySlug(channelSlug); ok {
    switch ch.Type {
    case channel.ChannelTypeDirect:
        lines = append([]string{
            "Context: DIRECT MESSAGE",
            "This is a private 1:1 conversation with the human. Respond to every message.",
            "You do not need to coordinate with other agents.",
            "---",
        }, lines...)
    case channel.ChannelTypeGroup:
        members := l.broker.channelStore.Members(ch.ID)
        names := make([]string, 0, len(members))
        for _, m := range members {
            if m.Slug != slug {
                names = append(names, "@"+m.Slug)
            }
        }
        lines = append([]string{
            "Context: GROUP MESSAGE",
            fmt.Sprintf("This is a group conversation with: %s.", strings.Join(names, ", ")),
            "Respond to messages directed at you or within your expertise.",
            "---",
        }, lines...)
    }
}
```

- [ ] **Step 4: Update isDM checks throughout launcher.go**

Replace all `IsDMSlug(normalizeChannelSlug(msg.Channel))` calls with Store-based type checks. There are ~5 locations (lines 422, 664, 2237, and work packet construction).

- [ ] **Step 5: Run launcher tests**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/team/ -v -run "TestNotification|TestResponse|TestBuild"`
Expected: ALL PASS (adapt test setup to use channel.Store)

- [ ] **Step 6: Commit**

```bash
git add internal/team/launcher.go internal/team/launcher_test.go
git commit -m "refactor(launcher): use channel.Store for DM detection, add DM preamble to work packets"
```

---

### Task 9: Add team_dm_open MCP tool

**Files:**
- Modify: `internal/teammcp/server.go`

- [ ] **Step 1: Read current MCP tool registration pattern**

Run: `cd .worktrees/debug-dm-specialists && grep -n "team_broadcast\|registerTool\|addTool" internal/teammcp/server.go | head -20`

- [ ] **Step 2: Add team_dm_open tool**

Register a new MCP tool `team_dm_open` that:
- Takes `members` (string array) and `type` ("direct" or "group")
- Validates: at least one member must be "human" (no agent-to-agent DMs)
- Calls `broker.channelStore.GetOrCreateDirect()` or `GetOrCreateGroup()`
- Returns: `{channel_id, channel_slug, type, name, created}`

- [ ] **Step 3: Run MCP tests**

Run: `cd .worktrees/debug-dm-specialists && go test ./internal/teammcp/ -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/teammcp/server.go
git commit -m "feat(mcp): add team_dm_open tool for human-initiated DM channel creation"
```

---

### Task 10: Update web UI — replace `dm-*` prefix checks

**Files:**
- Modify: `web/index.html`

- [ ] **Step 1: Add channel type metadata to web UI**

Update the `/channels` API response handling to include `type` field on channel objects. Store channel metadata in a JS map keyed by slug.

- [ ] **Step 2: Replace all `dm-*` string prefix checks**

Search for every `indexOf('dm-') === 0` and `currentChannel.slice(3)` and replace:
- `currentChannel.indexOf('dm-') === 0` → `getChannelType(currentChannel) === 'D'`
- `switchChannel('dm-' + a.slug)` → `openDM(a.slug)` which calls `POST /channels/dm` then switches
- `currentChannel.slice(3)` → look up channel object for display name
- `currentChannel.replace('dm-', '')` → look up channel slug from metadata

Helper function:
```javascript
function getChannelType(slug) {
  var ch = channelMetadata[slug];
  return ch ? ch.type : 'O';
}

function openDM(agentSlug) {
  WuphfAPI.post('/channels/dm', { members: ['human', agentSlug], type: 'direct' })
    .then(function(resp) { switchChannel(resp.slug); });
}
```

- [ ] **Step 3: Update auto-tagging for DMs**

Replace the `dm-` prefix check in sendMessage and thread reply:
```javascript
if (getChannelType(currentChannel) === 'D') {
  var ch = channelMetadata[currentChannel];
  if (ch && ch.members) {
    ch.members.forEach(function(m) {
      if (m !== 'human' && m !== 'you' && tagged.indexOf(m) === -1) tagged.push(m);
    });
  }
}
```

- [ ] **Step 4: Update sidebar DM rendering**

Fetch DM channels from `/channels?type=dm` and render with proper display names instead of `dm-*` slugs.

- [ ] **Step 5: Verify in browser**

Run: `cd .worktrees/debug-dm-specialists && go run ./cmd/wuphf --web`
Open browser, test: open DM from sidebar, send message, verify agent responds, switch back to general.

- [ ] **Step 6: Commit**

```bash
git add web/index.html
git commit -m "refactor(web): replace dm-* prefix checks with channel type system"
```

---

### Task 11: Update TUI channel handling

**Files:**
- Modify: `cmd/wuphf/channel.go`

- [ ] **Step 1: Replace dm-* prefix checks in TUI**

Search `cmd/wuphf/channel.go` for `strings.HasPrefix(m.activeChannel, "dm-")` and replace with channel type lookups via the broker API.

Replace `m.activeChannel = "dm-" + target.Slug` with a call to resolve/create the DM channel and set `m.activeChannel` to the new deterministic slug.

- [ ] **Step 2: Update channel title display**

Replace:
```go
if strings.HasPrefix(m.activeChannel, "dm-") {
    slug := strings.TrimPrefix(m.activeChannel, "dm-")
    return "DM→" + slug
}
```
With a channel type lookup that displays the proper name.

- [ ] **Step 3: Run TUI tests**

Run: `cd .worktrees/debug-dm-specialists && go test ./cmd/wuphf/ -v -run "Channel|DM"`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/wuphf/channel.go
git commit -m "refactor(tui): replace dm-* prefix checks with channel type system"
```

---

### Task 12: Update existing tests + adapt UAT

**Files:**
- Modify: `internal/team/launcher_test.go`
- Modify: `internal/team/broker_test.go`
- Modify: `tests/uat/agent-sidebar-dm-e2e.sh`

- [ ] **Step 1: Update launcher tests to use channel.Store**

Update `TestNotificationTargetsForDMChannel`, `TestResponseInstructionForTargetDMChannelRespondsHelpfully`, and any other tests that reference `dm-*` slugs to use the new deterministic slug format and channel types.

- [ ] **Step 2: Update broker tests**

Update any broker tests that create DM channels with `dm-*` slugs to use the `POST /channels/dm` endpoint or Store directly.

- [ ] **Step 3: Adapt UAT script**

Update `tests/uat/agent-sidebar-dm-e2e.sh` to:
- Call `/channels/dm` to create DM channels instead of navigating to `dm-*` URLs
- Assert channel type is `D` in responses
- Verify the new deterministic slug format in screenshots

- [ ] **Step 4: Run full test suite**

Run: `cd .worktrees/debug-dm-specialists && go test ./... -count=1`
Expected: ALL PASS, 0 failures

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "test: adapt all tests for channel store migration, update UAT for new DM flow"
```

---

### Task 13: Final verification and cleanup

- [ ] **Step 1: Grep for any remaining `dm-` prefix references**

Run: `cd .worktrees/debug-dm-specialists && grep -rn '"dm-\|dm-\|IsDMSlug\|DMSlugFor\|DMTargetAgent\|ensureDMConversation' --include="*.go" --include="*.html" | grep -v _test.go | grep -v migration.go`

Expected: Zero results (all references eliminated except migration code and tests)

- [ ] **Step 2: Grep for any remaining `internal/chat` imports**

Run: `cd .worktrees/debug-dm-specialists && grep -rn 'internal/chat"' --include="*.go"`

Expected: Zero results

- [ ] **Step 3: Run full test suite one final time**

Run: `cd .worktrees/debug-dm-specialists && go test ./... -count=1 -race`
Expected: ALL PASS with race detector

- [ ] **Step 4: Verify line count reduction in broker.go**

Run: `wc -l internal/team/broker.go`
Expected: ~5000-5200 lines (down from 6469)

- [ ] **Step 5: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: final cleanup — remove dead dm-* references"
```
