package team

import (
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
)

// Defaults + state normalization. Owns:
//   - default office members + channels (loaded from the runtime
//     manifest under repoRootForRuntimeDefaults, with fallback to
//     company.DefaultManifest)
//   - isDefaultChannelState / isDefaultOfficeMemberState — used by
//     saveLocked's "zero-state" branch to detect "nothing worth
//     persisting" and remove the state file
//   - normalizeChannelSlug / normalizeActorSlug — ToLower + replace
//     space/_ with - (preserving the __ DM separator)
//   - ensureDefaultChannelsLocked / ensureDefaultOfficeMembersLocked
//     — idempotent recovery hooks; only seed defaults when state is
//     truly empty
//   - normalizeLoadedStateLocked — the post-load fixup pass that
//     reconciles legacy data shapes (un-slugged channels, missing
//     defaults, deduplicated members, request lifecycle re-scheduling)
//   - reconcileOrphanedBlockedTasksLocked — one-shot migration for
//     tasks left blocked by a since-terminated parent

func defaultOfficeMembers() []officeMember {
	now := time.Now().UTC().Format(time.RFC3339)
	manifest, err := company.LoadRuntimeManifest(repoRootForRuntimeDefaults())
	if err != nil || len(manifest.Members) == 0 {
		manifest = company.DefaultManifest()
	}
	members := make([]officeMember, 0, len(manifest.Members))
	for _, cfg := range manifest.Members {
		builtIn := cfg.System || cfg.Slug == manifest.Lead || cfg.Slug == "ceo"
		members = append(members, memberFromSpec(cfg, "wuphf", now, builtIn))
	}
	return members
}

func defaultOfficeMemberSlugs() []string {
	members := defaultOfficeMembers()
	slugs := make([]string, 0, len(members))
	for _, member := range members {
		slugs = append(slugs, member.Slug)
	}
	return slugs
}

func defaultTeamChannels() []teamChannel {
	now := time.Now().UTC().Format(time.RFC3339)
	manifest, err := company.LoadRuntimeManifest(repoRootForRuntimeDefaults())
	if err != nil || len(manifest.Channels) == 0 {
		manifest = company.DefaultManifest()
	}
	channels := make([]teamChannel, 0, len(manifest.Channels))
	for _, channel := range manifest.Channels {
		tc := teamChannel{
			Slug:        channel.Slug,
			Name:        channel.Name,
			Description: channel.Description,
			Members:     append([]string(nil), channel.Members...),
			Disabled:    append([]string(nil), channel.Disabled...),
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if channel.Surface != nil {
			tc.Surface = &channelSurface{
				Provider:    channel.Surface.Provider,
				RemoteID:    channel.Surface.RemoteID,
				RemoteTitle: channel.Surface.RemoteTitle,
				Mode:        channel.Surface.Mode,
				BotTokenEnv: channel.Surface.BotTokenEnv,
			}
		}
		channels = append(channels, tc)
	}
	return channels
}

func repoRootForRuntimeDefaults() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func isDefaultChannelState(channels []teamChannel) bool {
	defaults := defaultTeamChannels()
	if len(channels) != len(defaults) {
		return false
	}
	for i := range defaults {
		if channels[i].Slug != defaults[i].Slug || channels[i].Name != defaults[i].Name || channels[i].Description != defaults[i].Description {
			return false
		}
		if strings.Join(channels[i].Members, ",") != strings.Join(defaults[i].Members, ",") {
			return false
		}
		if strings.Join(channels[i].Disabled, ",") != strings.Join(defaults[i].Disabled, ",") {
			return false
		}
	}
	return true
}

func isDefaultOfficeMemberState(members []officeMember) bool {
	defaults := defaultOfficeMembers()
	if len(members) != len(defaults) {
		return false
	}
	for i := range defaults {
		if members[i].Slug != defaults[i].Slug || members[i].Name != defaults[i].Name || members[i].Role != defaults[i].Role {
			return false
		}
	}
	return true
}

func normalizeChannelSlug(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	slug = strings.TrimLeft(slug, "#")
	slug = strings.ReplaceAll(slug, " ", "-")
	// Preserve "__" (DM slug separator) before replacing single underscores.
	const placeholder = "\x00"
	slug = strings.ReplaceAll(slug, "__", placeholder)
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = strings.ReplaceAll(slug, placeholder, "__")
	if slug == "" {
		return "general"
	}
	return slug
}

func normalizeActorSlug(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	return slug
}

func (b *Broker) ensureDefaultChannelsLocked() {
	if len(b.channels) == 0 {
		b.channels = defaultTeamChannels()
		return
	}
	hasGeneral := false
	for _, ch := range b.channels {
		if ch.Slug == "general" {
			hasGeneral = true
			break
		}
	}
	if !hasGeneral {
		b.channels = append(defaultTeamChannels(), b.channels...)
		return
	}
	// Merge surface metadata from manifest into existing channels
	// (handles case where state was saved without surfaces by an older binary)
	defaults := defaultTeamChannels()
	for _, def := range defaults {
		if def.Surface == nil {
			continue
		}
		found := false
		for i := range b.channels {
			if b.channels[i].Slug == def.Slug {
				if b.channels[i].Surface == nil {
					b.channels[i].Surface = def.Surface
				}
				found = true
				break
			}
		}
		if !found {
			b.channels = append(b.channels, def)
		}
	}
}

// ensureDefaultOfficeMembersLocked seeds the DefaultManifest roster ONLY when
// no members exist. Prior implementation appended any missing default slug to
// a non-empty roster, which caused ceo/planner/executor/reviewer to leak back
// into blueprint-seeded teams (e.g. niche-crm) on every Broker.Load(). The
// function is called from broker init and post-load normalization as a true
// recovery hook: if state was corrupted or never seeded, fall back to defaults.
func (b *Broker) ensureDefaultOfficeMembersLocked() {
	if len(b.members) > 0 {
		return
	}
	b.members = defaultOfficeMembers()
}

func (b *Broker) normalizeLoadedStateLocked() {
	b.sessionMode = NormalizeSessionMode(b.sessionMode)
	b.oneOnOneAgent = NormalizeOneOnOneAgent(b.oneOnOneAgent)
	if b.findMemberLocked(b.oneOnOneAgent) == nil {
		b.oneOnOneAgent = DefaultOneOnOneAgent
	}
	seenMembers := make(map[string]struct{}, len(b.members))
	normalizedMembers := make([]officeMember, 0, len(b.members))
	for _, member := range b.members {
		member.Slug = normalizeChannelSlug(member.Slug)
		if member.Slug == "" {
			continue
		}
		if _, ok := seenMembers[member.Slug]; ok {
			continue
		}
		seenMembers[member.Slug] = struct{}{}
		member.Name = strings.TrimSpace(member.Name)
		if member.Name == "" {
			member.Name = humanizeSlug(member.Slug)
		}
		member.Role = strings.TrimSpace(member.Role)
		if member.Role == "" {
			member.Role = member.Name
		}
		member.BuiltIn = member.Slug == "ceo"
		member.Expertise = normalizeStringList(member.Expertise)
		member.AllowedTools = normalizeStringList(member.AllowedTools)
		normalizedMembers = append(normalizedMembers, member)
	}
	b.members = normalizedMembers
	for i := range b.channels {
		b.channels[i].Slug = normalizeChannelSlug(b.channels[i].Slug)
		if strings.TrimSpace(b.channels[i].Name) == "" {
			b.channels[i].Name = b.channels[i].Slug
		}
		if strings.TrimSpace(b.channels[i].Description) == "" {
			b.channels[i].Description = defaultTeamChannelDescription(b.channels[i].Slug, b.channels[i].Name)
		}
		if b.channels[i].Slug == "general" && len(b.channels[i].Members) < len(b.members) {
			// Re-populate general channel with all office members.
			// This fixes stale state where only CEO survived a previous normalization.
			allSlugs := make([]string, 0, len(b.members))
			for _, m := range b.members {
				allSlugs = append(allSlugs, m.Slug)
			}
			b.channels[i].Members = allSlugs
		}
		filteredMembers := make([]string, 0, len(b.channels[i].Members))
		for _, slug := range uniqueSlugs(b.channels[i].Members) {
			if b.findMemberLocked(slug) != nil {
				filteredMembers = append(filteredMembers, slug)
			}
		}
		b.channels[i].Members = uniqueSlugs(append([]string{"ceo"}, filteredMembers...))
		filteredDisabled := make([]string, 0, len(b.channels[i].Disabled))
		for _, slug := range uniqueSlugs(b.channels[i].Disabled) {
			if slug == "ceo" {
				continue
			}
			if b.findMemberLocked(slug) != nil && containsString(b.channels[i].Members, slug) {
				filteredDisabled = append(filteredDisabled, slug)
			}
		}
		b.channels[i].Disabled = filteredDisabled
	}
	for i := range b.messages {
		if strings.TrimSpace(b.messages[i].Channel) == "" {
			b.messages[i].Channel = "general"
		}
	}
	for i := range b.agentIssues {
		issueChannel := normalizeChannelSlug(channel.MigrateDMSlugString(b.agentIssues[i].Channel))
		if issueChannel == "" {
			issueChannel = "general"
		}
		b.agentIssues[i].Channel = issueChannel
		if strings.TrimSpace(b.agentIssues[i].UpdatedAt) == "" {
			b.agentIssues[i].UpdatedAt = b.agentIssues[i].CreatedAt
		}
		if b.agentIssues[i].Count <= 0 {
			b.agentIssues[i].Count = 1
		}
	}
	for i := range b.tasks {
		if strings.TrimSpace(b.tasks[i].Channel) == "" {
			b.tasks[i].Channel = "general"
		}
	}
	for i := range b.requests {
		if strings.TrimSpace(b.requests[i].Channel) == "" {
			b.requests[i].Channel = "general"
		}
		if strings.TrimSpace(b.requests[i].Kind) == "" {
			b.requests[i].Kind = "choice"
		}
		if strings.TrimSpace(b.requests[i].Status) == "" {
			if b.requests[i].Answered != nil {
				b.requests[i].Status = "answered"
			} else {
				b.requests[i].Status = "pending"
			}
		}
		if requestIsHumanInterview(b.requests[i]) {
			b.requests[i].Blocking = false
			b.requests[i].Required = false
		} else if b.requests[i].Blocking {
			b.requests[i].Blocking = true
		}
		if strings.TrimSpace(b.requests[i].UpdatedAt) == "" {
			b.requests[i].UpdatedAt = b.requests[i].CreatedAt
		}
		b.scheduleRequestLifecycleLocked(&b.requests[i])
	}
	for i := range b.tasks {
		if strings.TrimSpace(b.tasks[i].Channel) == "" {
			b.tasks[i].Channel = "general"
		}
		normalizeTaskPlan(&b.tasks[i])
		syncTaskMemoryWorkflow(&b.tasks[i], "")
		b.ensureTaskOwnerChannelMembershipLocked(b.tasks[i].Channel, b.tasks[i].Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&b.tasks[i])
		b.scheduleTaskLifecycleLocked(&b.tasks[i])
		_ = b.syncTaskWorktreeLocked(&b.tasks[i])
	}
	b.reconcileOrphanedBlockedTasksLocked()
	b.pendingInterview = firstBlockingRequest(b.requests)
}

// reconcileOrphanedBlockedTasksLocked unblocks tasks whose dependencies
// have all reached a terminal status (done/completed/canceled/cancelled)
// but who never received the unblock notification because the parent
// terminated under the pre-fix semantics where only Status="done" fired
// unblockDependentsLocked. This is a one-shot migration: tasks blocked
// by a still-active dependency are left alone. Idempotent — running it
// twice has no effect since the second pass finds nothing blocked.
//
// Caller must hold b.mu. Called from normalizeLoadedStateLocked so it
// runs once per broker boot against persisted state.
func (b *Broker) reconcileOrphanedBlockedTasksLocked() {
	for i := range b.tasks {
		t := &b.tasks[i]
		if !t.Blocked || t.Status != "blocked" {
			continue
		}
		if b.hasUnresolvedDepsLocked(t) {
			continue
		}
		t.Blocked = false
		t.Status = "in_progress"
		t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.appendActionLocked("task_unblocked", "office", t.Channel, "system",
			truncateSummary("Reconciled: parent dep terminated while task was blocked", 140), t.ID)
	}
}
