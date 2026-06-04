package team

// broker_migration_channels.go — Phase 6 one-shot migration that folds every
// legacy free-standing channel (and any DM-shaped channel) into an archived
// Task that OWNS it, mirroring the Backup & Migration system task that owns
// #general (see broker_system_tasks.go).
//
// Why: the product is now pure task-scoped — the only navigable chat surface is
// a task's channel ("Channel" tab renders task.Channel). A channel with no
// owning task is unreachable in the UI, so on upgrade its message history would
// be stranded. The user invariant (2026-06-04): "no more DMs; every chat
// channel is a task channel, or else no chat possible." This migration makes
// every legacy channel a task channel by minting an archived owning task,
// preserving all history with ZERO message rewrites (the channel slug is
// unchanged; only a task is added).
//
// Safe + idempotent: a re-run finds the slug already owned and skips. Archived
// tasks are never GC-pruned (isTerminalTask is Approved-only, broker_gc.go), so
// the fold survives saves. Runs once per Broker via MigrateLegacyChannelsOnce.

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// legacyChannelMigrationOnce ensures migrateLegacyChannelsIntoArchivedTasksLocked
// runs at most once per Broker pointer (mirrors lifecycleMigrationOnce).
var legacyChannelMigrationOnce sync.Map // *Broker -> *sync.Once

// archivedChannelTaskIDPrefix is the stable, reserved ID prefix for tasks minted
// to own a folded legacy channel. Never produced by the ID counter.
const archivedChannelTaskIDPrefix = "task-archived-"

// MigrateLegacyChannelsOnce is the broker startup entry point for the Phase 6
// channel fold. Safe to call from any number of init hooks; the underlying
// migration runs exactly once per Broker pointer. Acquires b.mu internally.
func (b *Broker) MigrateLegacyChannelsOnce() {
	if b == nil {
		return
	}
	val, _ := legacyChannelMigrationOnce.LoadOrStore(b, &sync.Once{})
	once := val.(*sync.Once)
	once.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.migrateLegacyChannelsIntoArchivedTasksLocked()
	})
}

// migrateLegacyChannelsIntoArchivedTasksLocked mints one archived owning Task
// for every channel slug that has persisted message history but is not already
// owned by a task. Caller MUST hold b.mu.
func (b *Broker) migrateLegacyChannelsIntoArchivedTasksLocked() {
	if b == nil {
		return
	}

	// owned = every slug already owned by a task. #general is always owned by
	// the Backup & Migration system task (seeded idempotently by
	// ensureBackupMigrationTaskLocked); guard it explicitly so it is never
	// double-folded even on a code path where that task is not seeded yet.
	owned := make(map[string]bool, len(b.tasks)+1)
	usedTaskIDs := make(map[string]bool, len(b.tasks))
	for i := range b.tasks {
		if s := normalizeChannelSlug(b.tasks[i].Channel); s != "" {
			owned[s] = true
		}
		usedTaskIDs[b.tasks[i].ID] = true
	}
	owned["general"] = true

	// A slug is foldable only if it carries at least one persisted message —
	// an empty channel has nothing to preserve and would just add Archive
	// clutter.
	hasHistory := make(map[string]bool)
	for i := range b.messages {
		if s := normalizeChannelSlug(b.messages[i].Channel); s != "" {
			hasHistory[s] = true
		}
	}

	// Candidate slugs in deterministic order: declared channels first (so office
	// channels keep their configured Name), then any message-only slugs (DMs
	// whose channel record lives in channelStore, or channels deleted while
	// their messages remained). Iterating b.messages in insertion order (rather
	// than the hasHistory map) keeps minted IDs deterministic across runs.
	candidates := make([]string, 0, len(b.channels)+len(hasHistory))
	seen := make(map[string]bool)
	addCandidate := func(raw string) {
		s := normalizeChannelSlug(raw)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		candidates = append(candidates, s)
	}
	for i := range b.channels {
		addCandidate(b.channels[i].Slug)
	}
	for i := range b.messages {
		addCandidate(b.messages[i].Channel)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, slug := range candidates {
		if owned[slug] {
			continue
		}
		// Trusted sender pseudo-slugs (system/nex/you/human/ceo/librarian) are
		// not real chat channels — never mint a task for them.
		if reservedChannelSlugs[slug] {
			continue
		}
		if !hasHistory[slug] {
			continue
		}

		taskID := uniqueArchivedChannelTaskID(slug, usedTaskIDs)
		usedTaskIDs[taskID] = true
		owned[slug] = true

		title, details := b.archivedChannelDescriptorLocked(slug)
		task := teamTask{
			ID:        taskID,
			Channel:   slug,
			Title:     title,
			Details:   details,
			Owner:     "",
			CreatedBy: "system",
			System:    true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		// Errors are intentionally ignored: LifecycleStateArchived is canonical
		// and derivedFieldsFor always resolves it (matches
		// ensureBackupMigrationTaskLocked).
		_ = b.applyLifecycleStateLocked(&task, LifecycleStateArchived)
		b.tasks = append(b.tasks, task)
	}
}

// archivedChannelDescriptorLocked returns the Title + Details for the archived
// task that owns a folded legacy channel. DM-shaped slugs get a person-centric
// label; office channels keep their configured display name. Caller holds b.mu.
func (b *Broker) archivedChannelDescriptorLocked(slug string) (title, details string) {
	if isDirectChannelSlug(slug) {
		who := humanizeSlug(dmCounterpartSlug(slug))
		if strings.TrimSpace(who) == "" {
			who = "an agent"
		}
		return "Chat with " + who,
			"Archived on upgrade: preserves your earlier 1:1 messages with " + who +
				" as a task. WUPHF no longer has standalone DMs — every conversation lives in a task."
	}
	name := slug
	if ch := b.findChannelLocked(slug); ch != nil && strings.TrimSpace(ch.Name) != "" {
		name = strings.TrimSpace(ch.Name)
	}
	display := "#" + strings.TrimPrefix(name, "#")
	return "Archived " + display,
		"Archived on upgrade: preserves the history of the legacy " + display +
			" channel so it stays reachable as a task."
}

// dmCounterpartSlug extracts the non-human participant from a DM channel slug
// (`<agent>__human`, `human__<agent>`, or the legacy `dm-<agent>` shape).
func dmCounterpartSlug(slug string) string {
	s := strings.TrimSpace(slug)
	if rest, ok := strings.CutPrefix(s, "dm-"); ok {
		return rest
	}
	parts := strings.Split(s, "__")
	for _, p := range parts {
		if p == "" || isHumanMessageSender(p) {
			continue
		}
		return p
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return s
}

// uniqueArchivedChannelTaskID builds a stable, collision-free task ID for a
// folded channel slug. Two distinct slugs that sanitize to the same segment get
// disambiguated with a numeric suffix.
func uniqueArchivedChannelTaskID(slug string, used map[string]bool) string {
	base := archivedChannelTaskIDPrefix + sanitizeIDSegment(slug)
	id := base
	for n := 2; used[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

// sanitizeIDSegment lowercases slug and collapses every run of non
// [a-z0-9] characters to a single hyphen (so `dwight__human` -> `dwight-human`),
// trimming leading/trailing hyphens. Falls back to "channel" when empty.
func sanitizeIDSegment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var out strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			out.WriteByte('-')
			prevDash = true
		}
	}
	trimmed := strings.Trim(out.String(), "-")
	if trimmed == "" {
		return "channel"
	}
	return trimmed
}
