# Channel Store & DM Redesign

**Date:** 2026-04-13
**Branch:** debug/dm-specialist-agents
**Status:** Draft — pending implementation plan

## Problem

WUPHF's DM system uses a `"dm-{agent-slug}"` string prefix hack. `IsDMSlug()` checks `strings.HasPrefix(slug, "dm-")`. This causes:

1. Agents don't know they're in a DM — they get "stay quiet unless tagged" instructions even in private conversations
2. No proper channel types — DMs, group DMs, and public channels are all the same struct differentiated by a string prefix
3. No read cursors — agents re-process old messages, causing double-posting
4. No member management — DM members are inferred from the slug, not tracked
5. Two competing channel models (`teamChannel` in broker.go, `Channel` in internal/chat/) with neither being authoritative

50+ references to the `"dm-"` prefix across 13 files.

## Solution

Extract channel management into `internal/channel/` with Mattermost-aligned data structures. Proper channel types (Direct, Group, Public), UUID channel IDs, deterministic DM creation, per-member read cursors, and notification levels.

## Reference Architecture

Mattermost's channel system (github.com/mattermost/mattermost):
- `ChannelType` enum: `"O"` (public), `"P"` (private), `"D"` (direct), `"G"` (group)
- DM channel name: `{smaller_user_id}__{larger_user_id}` (deterministic, sorted)
- Group channel name: SHA1 hash of sorted member IDs
- Channel + ChannelMember are separate models, composite key `(ChannelId, UserId)`
- DM detection: `channel.Type == ChannelTypeDirect` (never string prefix)
- In DMs, every message from the other user is a mention

## Architecture Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Channel duplication | Kill `internal/chat/`, build `internal/channel/` as sole truth | Two competing models is a mess. One authoritative source. |
| 2 | Persistence | Shared in broker-state.json. ChannelStore owns model, broker owns save. | No split-brain risk. One file, one save operation. |
| 3 | Message references | **REVISED (Codex):** UUID internal, slugs external | UUIDs for dedup/identity inside the Store. Slugs remain the API/UI/MCP wire format. ~60% less migration surface. |
| 4 | DM discovery | Explicit `team_dm_open` MCP tool + `/channels/dm` HTTP endpoint. Human-only. | No agent-to-agent DMs. Agents work openly in channels. |
| 5 | Agent DM context | Explicit DM preamble in work packets | Agents know they're in a private 1:1. Respond to everything. |
| 6 | Broker scope | Extract channels + messages together | Natural boundary. Channel without messages is half an abstraction. |
| 7 | Test strategy | Full unit tests + adapt existing UAT | 27 test gaps. Unit tests for channel package, adapt UAT for UUID flows. |
| 8 | Lookup performance | In-memory index maps (byID, bySlug, memberIndex) | O(1) on hot paths. ~20 lines, maintained on write. |
| 9 | Group DMs | **REVISED (Codex):** Type enum + data model only, defer routing/UI | G exists in enum and Store. Routing, notifications, UI only handle D for now. |
| 10 | Read cursors | Keep cursors + add launcher notification dedup | Cursors needed for unread tracking. Double-posting fix also requires launcher dedup. |
| 11 | Migration scope | **REVISED (Codex):** All entities, not just messages | Channels, messages, tasks, requests, surfaces. dm-* slugs become deterministic pair slugs. |

## Data Model

### ChannelType

```go
type ChannelType string

const (
    ChannelTypePublic ChannelType = "O"  // Public channels (general, engineering)
    ChannelTypeDirect ChannelType = "D"  // 1:1 DMs (human + one agent)
    ChannelTypeGroup  ChannelType = "G"  // Group DMs (human + N agents)
)
```

### Channel

```go
type Channel struct {
    ID          string      `json:"id"`            // UUID v4
    Name        string      `json:"name"`          // Display name
    Slug        string      `json:"slug"`          // Lookup key (deterministic for D/G)
    Type        ChannelType `json:"type"`          // O, D, or G
    CreatedBy   string      `json:"created_by"`
    CreatedAt   string      `json:"created_at"`
    UpdatedAt   string      `json:"updated_at"`
    LastPostAt  string      `json:"last_post_at,omitempty"`
    Description string      `json:"description,omitempty"` // Public channels only
}
```

### ChannelMember

```go
type ChannelMember struct {
    ChannelID       string `json:"channel_id"`
    Slug            string `json:"slug"`
    Role            string `json:"role,omitempty"`     // "owner", "member"
    LastReadID      string `json:"last_read_id"`       // Last message seen (typing indicator)
    LastProcessedID string `json:"last_processed_id"`  // Last message acted on (prevents double-posting)
    MentionCount    int    `json:"mention_count"`
    NotifyLevel     string `json:"notify_level"`       // "all", "mention", "none"
    JoinedAt        string `json:"joined_at"`
}
```

### Slug Generation

```
DM:    DirectSlug("engineering", "human") → "engineering__human" (sorted)
Group: GroupSlug(["human","engineering","design"]) → SHA1 hex (40 chars)
```

### Mention Semantics

- **DM (type D):** Every message from the other member increments MentionCount. Like Mattermost.
- **Group DM (type G):** Every message from any other member increments MentionCount.
- **Public channel (type O):** Only explicit @mentions increment MentionCount.

### NotifyLevel Defaults

- DM/Group: `"all"` — every message is a notification
- Public channel: `"mention"` — only @tags

## Store API

```
internal/channel/
├── types.go        # Channel, ChannelMember, ChannelType, ChannelFilter
├── slug.go         # DirectSlug, GroupSlug
├── store.go        # Store struct, CRUD, DM/group ops, member ops, cursors
├── store_test.go   # Full coverage
├── slug_test.go    # Determinism tests
└── migration.go    # Boot migration from dm-* to new format
```

### Store struct

```go
type Store struct {
    channels []Channel
    members  []ChannelMember
    // In-memory indexes (maintained on write)
    byID     map[string]*Channel        // channel UUID → Channel
    bySlug   map[string]*Channel        // channel slug → Channel
    memberOf map[string][]string         // member slug → []channel IDs
    mu       sync.RWMutex
}
```

### Key methods

```
// Lifecycle (broker calls these for persistence)
func (s *Store) MarshalJSON() ([]byte, error)
func (s *Store) UnmarshalJSON(data []byte) error

// Channel CRUD
func (s *Store) Create(ch Channel) (*Channel, error)
func (s *Store) Get(id string) (*Channel, bool)
func (s *Store) GetBySlug(slug string) (*Channel, bool)
func (s *Store) List(filter ChannelFilter) []Channel
func (s *Store) Delete(id string) error

// DM operations
func (s *Store) GetOrCreateDirect(memberA, memberB string) (*Channel, error)
func (s *Store) GetOrCreateGroup(members []string, createdBy string) (*Channel, error)
func (s *Store) FindDirectByMembers(a, b string) (*Channel, bool)
func (s *Store) OtherMember(channelID, slug string) (string, bool)

// Member operations
func (s *Store) AddMember(channelID, slug, notifyLevel string) error
func (s *Store) RemoveMember(channelID, slug string) error
func (s *Store) Members(channelID string) []ChannelMember
func (s *Store) IsMember(channelID, slug string) bool
func (s *Store) MemberChannels(slug string) []Channel

// Read cursor / notifications
func (s *Store) MarkRead(channelID, slug, messageID string) error
func (s *Store) MarkProcessed(channelID, slug, messageID string) error
func (s *Store) IncrementMentions(channelID, senderSlug string) error
func (s *Store) GetMember(channelID, slug string) (*ChannelMember, bool)

// Message storage (moved from broker)
func (s *Store) AppendMessage(msg Message) error
func (s *Store) ChannelMessages(channelID string) []Message
func (s *Store) ThreadMessages(channelID, threadID string) []Message

// Queries
func (s *Store) IsDirectMessage(channelID string) bool
func (s *Store) IsGroupMessage(channelID string) bool
```

## Broker Integration

```
┌─────────────────────────────────────────────────┐
│                    Broker                        │
│                                                  │
│  ┌──────────────┐  ┌─────────────────────────┐  │
│  │ HTTP Router   │  │ Pub/Sub (subscribers)    │  │
│  │ /messages     │  │ messageSubscribers       │  │
│  │ /channels     │  │ actionSubscribers        │  │
│  │ /channels/dm  │  │                          │  │
│  └──────┬───────┘  └────────────┬─────────────┘  │
│         │                       │                 │
│         ▼                       │                 │
│  ┌──────────────────────────────▼──────────────┐  │
│  │           channel.Store                      │  │
│  │  channels []Channel                          │  │
│  │  members  []ChannelMember                    │  │
│  │  messages []Message (moved from broker)       │  │
│  │  byID / bySlug / memberOf indexes            │  │
│  └──────────────────────────────────────────────┘  │
│                                                  │
│  broker-state.json                               │
│  {                                               │
│    "channel_store": { ... },  ← Store's JSON     │
│    "tasks": [...],                               │
│    "requests": [...],                            │
│    ...                                           │
│  }                                               │
└─────────────────────────────────────────────────┘
```

The broker embeds `channel.Store` and serializes it as `"channel_store"` within broker-state.json. On load, it unmarshals the store subsection. On save, it marshals the full state including the store.

## Agent Work Packet

When an agent receives a DM notification, the work packet includes:

```
Context: DIRECT MESSAGE
This is a private 1:1 conversation with the human. Respond to every message.
You do not need to coordinate with other agents. This conversation is between
you and the human only.

---
[New from @human]: <message content>
You are @engineering. Respond helpfully from your domain expertise.
Reply via team_broadcast with my_slug "engineering", channel "<UUID>", reply_to_id "<msg-id>".
```

For group DMs:

```
Context: GROUP MESSAGE
This is a group conversation with: @human, @design.
Respond to messages directed at you or within your expertise.

---
[New from @human]: <message content>
...
```

For public channels, the existing work packet format is preserved (no context header).

## MCP Tool: team_dm_open

New tool available to agents (restricted: human-initiated only via web/TUI):

```json
{
  "name": "team_dm_open",
  "description": "Open or find a direct message channel",
  "parameters": {
    "members": ["human", "engineering"],
    "type": "direct"  // or "group"
  },
  "returns": {
    "channel_id": "<UUID>",
    "channel_slug": "engineering__human",
    "type": "D",
    "created": false  // true if newly created
  }
}
```

Agent-to-agent DM attempts are rejected. All agent-to-agent communication must happen in public channels where the human has observability.

## HTTP Endpoint: POST /channels/dm

```
POST /channels/dm
{
  "members": ["human", "engineering"],
  "type": "direct"
}

Response:
{
  "id": "<UUID>",
  "slug": "engineering__human",
  "type": "D",
  "name": "DM with Engineering",
  "created": false
}
```

## Migration

On boot, if broker-state.json contains channels with `"dm-"` prefix slugs:

1. For each `dm-{agent}` channel:
   - Generate UUID
   - Set `Type = "D"`
   - Set `Slug = DirectSlug("human", agent)`
   - Create ChannelMember entries for both with `NotifyLevel = "all"`
2. For each message referencing the old slug:
   - Update `msg.Channel` to the new UUID
3. For public channels (general, engineering, etc.):
   - Generate UUID
   - Set `Type = "O"`
   - Preserve existing slug
4. Save migrated state

Migration is idempotent: if channels already have UUIDs, skip.

## Web UI Changes

All `dm-*` string prefix checks replaced:
- `currentChannel.indexOf('dm-') === 0` → channel object lookup, `channel.type === 'D'`
- `switchChannel('dm-' + slug)` → `openDM(agentSlug)` which calls `/channels/dm` then switches by UUID
- `currentChannel.slice(3)` → `channel.slug` or `channel.name`
- Sidebar DM list fetched from `/channels?type=dm` with proper channel objects

## TUI Changes

- `strings.HasPrefix(m.activeChannel, "dm-")` → channel type check via store
- Channel switcher uses UUIDs internally, displays names
- Ctrl+D returns to general channel (by UUID, not hardcoded slug)

## NOT in Scope

- **Private channels (type P)**: Mattermost has them but WUPHF doesn't need non-DM private channels yet. The type enum reserves the slot.
- **Channel permissions/roles**: ChannelMember has a Role field but no RBAC enforcement. All members are equal.
- **Message editing/deletion**: Not part of this redesign.
- **Channel archiving**: No soft-delete. Channels are created or removed.
- **SessionModeOneOnOne changes**: Kept as a separate launch mode per architecture decision. Not touched.
- **Typing indicators sync with read state**: Data model supports it (LastReadID), but UI implementation deferred.

## What Already Exists

| Component | Current location | Reused? |
|-----------|-----------------|---------|
| `teamChannel` struct | `broker.go:182` | Replaced by `channel.Channel` |
| `ensureDMConversationLocked` | `broker.go:1972` | Logic moves to `Store.GetOrCreateDirect` |
| `IsDMSlug/DMSlugFor/DMTargetAgent` | `broker.go:199-216` | Deleted. Replaced by `channel.Type == "D"` |
| `internal/chat/` package | `internal/chat/` | Deleted entirely |
| `canAccessChannelLocked` | `broker.go:2307` | Delegates to `Store.IsMember` |
| `appendMessageLocked` | `broker.go` | Moves to `Store.AppendMessage` |
| `handleGetMessages` | `broker.go:4330` | Broker keeps HTTP handler, delegates to Store |
| `channelHasMemberLocked` | `broker.go` | Replaced by `Store.IsMember` |
| `notificationTargetsForMessage` DM branch | `launcher.go:664` | Updated to use `Store.IsDirectMessage` |

## Failure Modes

| Codepath | Failure | Test? | Handling? | User impact |
|----------|---------|-------|-----------|-------------|
| Boot migration | Corrupt broker-state.json | Needed | Needed | Office won't start |
| GetOrCreateDirect | Race condition (two concurrent creates) | Needed | Mutex protects | None (idempotent) |
| UUID generation | Collision | Extremely unlikely | UUID v4 uniqueness | None |
| MarkProcessed | Agent crashes mid-processing | Needed | Cursor not advanced, agent retries | Possible double-post |
| IncrementMentions | Message to deleted channel | Needed | Check channel exists | Silent skip |
| Web UI switchChannel | UUID not found in sidebar | Needed | Graceful fallback | Channel not openable |
