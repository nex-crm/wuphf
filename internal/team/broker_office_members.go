package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// Office member CRUD. Extracted from broker_office_channels.go's
// 392-line handleOfficeMembers monolith — the TODO at the original
// site asked for this split: parser/applier separation so each action
// (create, update, remove) is reviewable in isolation.
//
// The HTTP entrypoint (handleOfficeMembers) only routes by method + action.
// Each mutation helper takes b.mu only for in-memory reads/writes; OpenClaw and
// persistence I/O run outside the broker lock.

type officeMemberListEntry struct {
	officeMember
	Status       string `json:"status,omitempty"`
	Activity     string `json:"activity,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Task         string `json:"task,omitempty"`
	LiveActivity string `json:"liveActivity,omitempty"`
	LastTime     string `json:"lastTime,omitempty"`
	// Online + LastSeenAt are presence fields populated from b.memberPresence
	// (broker_presence.go), distinct from Status/Activity above which describe
	// "is the agent processing right now". Online tracks "does the adapter
	// still have a live session for this slug"; LastSeenAt is preserved on
	// flip-off so the UI can render "last seen 5m ago" for offline members.
	//
	// Online has no omitempty: detached members must serialize as `online:false`
	// so clients can reliably distinguish "offline" from "field missing because
	// the member has no presence record at all". LastSeenAt keeps omitempty
	// because empty-string is a real distinct state ("we have never seen this
	// member online") that the UI renders differently from a stale timestamp.
	Online     bool   `json:"online"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

type officeMemberMutationBody struct {
	Action         string                    `json:"action"`
	Slug           string                    `json:"slug"`
	Name           string                    `json:"name"`
	Role           string                    `json:"role"`
	Expertise      []string                  `json:"expertise"`
	Personality    string                    `json:"personality"`
	PermissionMode string                    `json:"permission_mode"`
	AllowedTools   []string                  `json:"allowed_tools"`
	CreatedBy      string                    `json:"created_by"`
	Provider       *provider.ProviderBinding `json:"provider,omitempty"`
}

type officeMemberMutationResult struct {
	payload            map[string]any
	events             []officeChangeEvent
	write              brokerStateWrite
	ensureNotebookDirs bool
}

type officeMemberMutationError struct {
	status  int
	message string
}

func newOfficeMemberMutationError(status int, message string) *officeMemberMutationError {
	return &officeMemberMutationError{status: status, message: message}
}

func (b *Broker) handleOfficeMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.serveOfficeMemberList(w)
	case http.MethodPost:
		b.serveOfficeMemberMutation(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) serveOfficeMemberList(w http.ResponseWriter) {
	b.mu.Lock()
	now := time.Now()
	members := make([]officeMemberListEntry, 0, len(b.members))
	for _, member := range b.members {
		entry := officeMemberListEntry{officeMember: cloneOfficeMemberForRead(member)}
		if snapshot, ok := b.activity[member.Slug]; ok {
			entry.Status = snapshot.Status
			entry.Activity = snapshot.Activity
			entry.Detail = snapshot.Detail
			entry.LiveActivity = snapshot.Detail
			entry.Task = snapshot.Detail
			entry.LastTime = snapshot.LastTime
		}
		if entry.Status == "" && b.lastTaggedAt != nil {
			if taggedAt, ok := b.lastTaggedAt[member.Slug]; ok && now.Sub(taggedAt) < 60*time.Second {
				entry.Status = "active"
				entry.Activity = "queued"
				entry.Detail = "active"
				entry.LiveActivity = "active"
				entry.Task = "active"
				entry.LastTime = taggedAt.UTC().Format(time.RFC3339)
			}
		}
		if entry.Status == "" {
			entry.Status = "idle"
		}
		if entry.Activity == "" {
			entry.Activity = "idle"
		}
		if presence, ok := b.presenceForSlugLocked(member.Slug); ok {
			entry.Online = presence.Online
			entry.LastSeenAt = presence.LastSeenAt.UTC().Format(time.RFC3339)
		}
		members = append(members, entry)
	}
	humanHasPosted := b.humanHasPosted
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	// meta is a sibling object on /office-members so the frontend's existing
	// members poll can read durable office-level signals without an extra
	// request. humanHasPosted drives the first-run "tag @agent" nudge —
	// see the Broker.humanHasPosted field doc for the lifecycle.
	_ = json.NewEncoder(w).Encode(map[string]any{
		"members": members,
		"meta": map[string]any{
			"humanHasPosted": humanHasPosted,
		},
	})
}

func (b *Broker) OfficeMembers() []officeMember {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeMember, len(b.members))
	for i, member := range b.members {
		out[i] = cloneOfficeMemberForRead(member)
	}
	return out
}

func cloneOfficeMemberForRead(member officeMember) officeMember {
	clone := member
	if len(member.Expertise) > 0 {
		clone.Expertise = append([]string(nil), member.Expertise...)
	}
	if len(member.AllowedTools) > 0 {
		clone.AllowedTools = append([]string(nil), member.AllowedTools...)
	}
	if member.Provider.Openclaw != nil {
		openclaw := *member.Provider.Openclaw
		clone.Provider.Openclaw = &openclaw
	}
	return clone
}

func (b *Broker) serveOfficeMemberMutation(w http.ResponseWriter, r *http.Request) {
	var body officeMemberMutationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	action := strings.TrimSpace(body.Action)
	slug := normalizeChannelSlug(body.Slug)
	if slug == "" {
		http.Error(w, "slug required", http.StatusBadRequest)
		return
	}

	b.officeMemberMutationMu.Lock()
	defer b.officeMemberMutationMu.Unlock()

	var result officeMemberMutationResult
	var mutationErr *officeMemberMutationError
	switch action {
	case "create":
		result, mutationErr = b.createOfficeMember(r, slug, body)
	case "update":
		result, mutationErr = b.updateOfficeMember(r, slug, body)
	case "remove":
		result, mutationErr = b.removeOfficeMember(r, slug)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if mutationErr != nil {
		http.Error(w, mutationErr.message, mutationErr.status)
		return
	}
	if err := b.writeBrokerState(result.write); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.publishOfficeChanges(result.events)
	if result.ensureNotebookDirs {
		b.ensureNotebookDirsForRoster()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result.payload)
}

func (b *Broker) publishOfficeChanges(events []officeChangeEvent) {
	if len(events) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, evt := range events {
		b.publishOfficeChangeLocked(evt)
	}
}

// createOfficeMember persists a new office member, registers any openclaw
// bridge subscription up front (so a half-configured member is never
// persisted), seeds the new hire into every non-DM channel's roster, and clears
// any stale Disabled entry from a prior lifecycle.
func (b *Broker) createOfficeMember(r *http.Request, slug string, body officeMemberMutationBody) (officeMemberMutationResult, *officeMemberMutationError) {
	now := time.Now().UTC().Format(time.RFC3339)
	var bridge *OpenclawBridge

	if body.Provider != nil {
		if err := provider.ValidateKind(body.Provider.Kind); err != nil {
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadRequest, err.Error())
		}
	}
	member := officeMember{
		Slug:           slug,
		Name:           strings.TrimSpace(body.Name),
		Role:           strings.TrimSpace(body.Role),
		Expertise:      normalizeStringList(body.Expertise),
		Personality:    strings.TrimSpace(body.Personality),
		PermissionMode: strings.TrimSpace(body.PermissionMode),
		AllowedTools:   normalizeStringList(body.AllowedTools),
		CreatedBy:      strings.TrimSpace(body.CreatedBy),
		CreatedAt:      now,
	}
	if body.Provider != nil {
		member.Provider = *body.Provider
	}
	applyOfficeMemberDefaults(&member)

	// For openclaw agents, reach the gateway BEFORE we persist: if the
	// caller didn't supply a session key, auto-create one; either way,
	// attach the bridge subscription. If the gateway is unreachable we
	// fail the whole create so we don't persist a half-configured
	// member that can't actually talk.
	if member.Provider.Kind == provider.KindOpenclaw {
		if member.Provider.Openclaw == nil {
			member.Provider.Openclaw = &provider.OpenclawProviderBinding{}
		}
		b.mu.Lock()
		alreadyExists := b.findMemberLocked(slug) != nil
		bridge = b.openclawBridgeLocked()
		b.mu.Unlock()
		if alreadyExists {
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusConflict, "member already exists")
		}
		if bridge == nil {
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusServiceUnavailable, "openclaw bridge not active; cannot create openclaw member")
		}
		if member.Provider.Openclaw.SessionKey == "" {
			agentID := member.Provider.Openclaw.AgentID
			if agentID == "" {
				agentID = "main"
			}
			label := fmt.Sprintf("wuphf-%s-%d", slug, time.Now().UnixNano())
			key, err := bridge.CreateSession(r.Context(), agentID, label)
			if err != nil {
				return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadGateway, fmt.Sprintf("openclaw sessions.create: %v", err))
			}
			member.Provider.Openclaw.SessionKey = key
		}
		if err := bridge.AttachSlugAndSubscribe(r.Context(), slug, member.Provider.Openclaw.SessionKey); err != nil {
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadGateway, fmt.Sprintf("openclaw subscribe: %v", err))
		}
	} else {
		b.mu.Lock()
		alreadyExists := b.findMemberLocked(slug) != nil
		b.mu.Unlock()
		if alreadyExists {
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusConflict, "member already exists")
		}
	}

	b.mu.Lock()
	if b.findMemberLocked(slug) != nil {
		b.mu.Unlock()
		if member.Provider.Kind == provider.KindOpenclaw && bridge != nil && member.Provider.Openclaw != nil {
			if err := bridge.DetachSession(r.Context(), slug, member.Provider.Openclaw.SessionKey); err != nil {
				go bridge.postSystemMessage(fmt.Sprintf("agent %q create-conflict: detach warning: %v", slug, err))
			}
		}
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusConflict, "member already exists")
	}
	b.members = append(b.members, member)
	b.memberIndex[member.Slug] = len(b.members) - 1
	// Add the new hire to every non-DM channel's Members list so they
	// can actually POST replies. canAccessChannelLocked enforces
	// ch.Members for every non-CEO agent sender; without this, a
	// wizard-hired specialist can be tagged and dispatched but its
	// reply is 403'd with "channel access denied" and the user sees
	// nothing. DM channels are intentionally skipped — DMs encode
	// the target agent in the slug and go through a different
	// membership gate.
	//
	// Policy note: this is broader than normalizeLoadedStateLocked's
	// seed (which only fills #general). A wizard hire joins every
	// topical channel by default; admins can narrow via
	// /channel-members action=remove afterwards. The rationale is
	// that an office member who can't post to any non-default
	// channel without a second configuration step violates the
	// principle of least surprise — the hire UI does not surface a
	// channel-scope picker, so the implicit default has to be
	// "office-wide."
	//
	// We also clear any stale Disabled entry for this slug. A fresh
	// hire shouldn't inherit a mute left over from a prior lifecycle.
	updatedChannels := make([]string, 0, len(b.channels))
	for i := range b.channels {
		if b.channels[i].isDM() {
			continue
		}
		mutated := false
		if !containsString(b.channels[i].Members, slug) {
			b.channels[i].Members = uniqueSlugs(append(b.channels[i].Members, slug))
			mutated = true
		}
		if containsString(b.channels[i].Disabled, slug) {
			// Allocate a fresh slice instead of reusing the
			// backing array via [:0]+append. The in-place form
			// is safe but reads as if it could clobber the
			// range — readability over one extra alloc on a
			// rare re-hire path.
			next := make([]string, 0, len(b.channels[i].Disabled))
			for _, d := range b.channels[i].Disabled {
				if d != slug {
					next = append(next, d)
				}
			}
			b.channels[i].Disabled = next
			mutated = true
		}
		if mutated {
			b.channels[i].UpdatedAt = now
			updatedChannels = append(updatedChannels, b.channels[i].Slug)
		}
	}
	write, err := b.prepareBrokerStateWriteLocked()
	if err != nil {
		b.mu.Unlock()
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusInternalServerError, "failed to persist broker state")
	}
	b.mu.Unlock()
	events := []officeChangeEvent{{Kind: "member_created", Slug: slug}}
	// Notify SSE subscribers that these channels' rosters changed so
	// the UI sidebar refreshes without requiring a separate trigger.
	for _, chSlug := range updatedChannels {
		events = append(events, officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
	}
	return officeMemberMutationResult{
		payload:            map[string]any{"member": member},
		events:             events,
		write:              write,
		ensureNotebookDirs: true,
	}, nil
}

// updateOfficeMember applies a partial update to an existing office member.
// Provider switches reconcile the openclaw bridge subscription before mutating
// member.Provider so an attach failure preserves the previous state.
func (b *Broker) updateOfficeMember(r *http.Request, slug string, body officeMemberMutationBody) (officeMemberMutationResult, *officeMemberMutationError) {
	var oldBinding provider.ProviderBinding
	var newBinding provider.ProviderBinding
	var providerChanged bool
	var bridge *OpenclawBridge

	b.mu.Lock()
	member := b.findMemberLocked(slug)
	if member == nil {
		b.mu.Unlock()
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusNotFound, "member not found")
	}
	if body.Provider != nil {
		if err := provider.ValidateKind(body.Provider.Kind); err != nil {
			b.mu.Unlock()
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadRequest, err.Error())
		}
		oldBinding = member.Provider
		newBinding = *body.Provider
		providerChanged = true
		bridge = b.openclawBridgeLocked()
	}
	b.mu.Unlock()

	if providerChanged {
		fromOpenclaw := oldBinding.Kind == provider.KindOpenclaw
		toOpenclaw := newBinding.Kind == provider.KindOpenclaw
		oldSessionKey := ""
		if oldBinding.Openclaw != nil {
			oldSessionKey = oldBinding.Openclaw.SessionKey
		}
		newSessionKey := ""

		if toOpenclaw {
			if bridge == nil {
				return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusServiceUnavailable, "openclaw bridge not active; cannot switch agent to openclaw")
			}
			if newBinding.Openclaw == nil {
				newBinding.Openclaw = &provider.OpenclawProviderBinding{}
			}
			if newBinding.Openclaw.SessionKey == "" {
				agentID := newBinding.Openclaw.AgentID
				if agentID == "" {
					agentID = "main"
				}
				label := fmt.Sprintf("wuphf-%s-%d", slug, time.Now().UnixNano())
				key, err := bridge.CreateSession(r.Context(), agentID, label)
				if err != nil {
					return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadGateway, fmt.Sprintf("openclaw sessions.create: %v", err))
				}
				newBinding.Openclaw.SessionKey = key
			}
			newSessionKey = newBinding.Openclaw.SessionKey
		}

		// Attach BEFORE detaching the old session so an attach failure
		// preserves the previous subscription rather than leaving the
		// agent silently disconnected. Order matters for openclaw→
		// openclaw swaps in particular: detach-first plus a failed
		// attach would return 502 with member.Provider still pointing
		// at the old binding but no live subscription on the gateway.
		if toOpenclaw {
			if err := bridge.AttachSlugAndSubscribe(r.Context(), slug, newBinding.Openclaw.SessionKey); err != nil {
				return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadGateway, fmt.Sprintf("openclaw subscribe: %v", err))
			}
		}

		if fromOpenclaw && bridge != nil && oldSessionKey != "" {
			// Detach old session from subscriptions. Best-effort; log via
			// the bridge's own system-message channel on failure. The
			// daemon-side session lingers (no sessions.end method); user
			// can prune via the OpenClaw CLI if they care.
			if err := bridge.DetachSession(r.Context(), slug, oldSessionKey); err != nil {
				go bridge.postSystemMessage(fmt.Sprintf("agent %q provider-switch: detach warning: %v", slug, err))
			}
		}

		if toOpenclaw && newSessionKey == "" {
			return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadGateway, "openclaw subscribe: missing session key")
		}
	}

	b.mu.Lock()
	member = b.findMemberLocked(slug)
	if member == nil {
		b.mu.Unlock()
		if providerChanged && newBinding.Kind == provider.KindOpenclaw && bridge != nil && newBinding.Openclaw != nil {
			if err := bridge.DetachSession(r.Context(), slug, newBinding.Openclaw.SessionKey); err != nil {
				go bridge.postSystemMessage(fmt.Sprintf("agent %q update-conflict: detach warning: %v", slug, err))
			}
		}
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusNotFound, "member not found")
	}
	if body.Name != "" {
		member.Name = strings.TrimSpace(body.Name)
	}
	if body.Role != "" {
		member.Role = strings.TrimSpace(body.Role)
	}
	if body.Expertise != nil {
		member.Expertise = normalizeStringList(body.Expertise)
	}
	if body.Personality != "" {
		member.Personality = strings.TrimSpace(body.Personality)
	}
	if body.PermissionMode != "" {
		member.PermissionMode = strings.TrimSpace(body.PermissionMode)
	}
	if body.AllowedTools != nil {
		member.AllowedTools = normalizeStringList(body.AllowedTools)
	}
	if providerChanged {
		member.Provider = newBinding
	}
	applyOfficeMemberDefaults(member)
	write, err := b.prepareBrokerStateWriteLocked()
	if err != nil {
		b.mu.Unlock()
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusInternalServerError, "failed to persist broker state")
	}
	responseMember := *member
	b.mu.Unlock()
	// Match the create/remove paths so SSE subscribers learn about updated
	// member metadata (provider switch, name changes, channel reassignment)
	// instead of waiting for a full reload.
	return officeMemberMutationResult{
		payload: map[string]any{"member": responseMember},
		events:  []officeChangeEvent{{Kind: "member_updated", Slug: slug}},
		write:   write,
	}, nil
}

// removeOfficeMember deletes an office member, releases owned tasks, removes
// the slug from all non-DM channels' Members + Disabled lists, and best-effort
// detaches any openclaw subscription.
func (b *Broker) removeOfficeMember(r *http.Request, slug string) (officeMemberMutationResult, *officeMemberMutationError) {
	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	member := b.findMemberLocked(slug)
	if member == nil {
		b.mu.Unlock()
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusNotFound, "member not found")
	}
	if member.BuiltIn || slug == "ceo" {
		b.mu.Unlock()
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusBadRequest, "cannot remove built-in member")
	}
	memberSnapshot := *member
	bridge := b.openclawBridgeLocked()
	b.mu.Unlock()

	// If the member was bridged to OpenClaw, unsubscribe from the
	// gateway. Best-effort: member removal must succeed even when
	// the gateway is unreachable. We do NOT call sessions.end because
	// the current daemon doesn't expose that method — the session
	// lingers daemon-side and the user can clean it up via the
	// OpenClaw CLI if they want to reclaim the slot.
	if memberSnapshot.Provider.Kind == provider.KindOpenclaw && bridge != nil {
		if err := bridge.DetachSlugAndUnsubscribe(r.Context(), memberSnapshot.Slug); err != nil {
			go bridge.postSystemMessage(fmt.Sprintf("agent %q removed: detach warning: %v", memberSnapshot.Slug, err))
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	member = b.findMemberLocked(slug)
	if member == nil {
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusNotFound, "member not found")
	}
	filteredMembers := b.members[:0]
	for _, existing := range b.members {
		if existing.Slug != slug {
			filteredMembers = append(filteredMembers, existing)
		}
	}
	b.members = filteredMembers
	b.rebuildMemberIndexLocked()
	// Symmetry with action:create — skip DM channels (they encode
	// their target in the slug and go through a different
	// membership gate) and emit a channel_updated event per
	// actually-mutated channel so SSE subscribers refresh the
	// roster. Without this, the UI sidebar gets a half-signal
	// lifecycle (create emits channel_updated, remove does not).
	removedChannels := make([]string, 0, len(b.channels))
	for i := range b.channels {
		if b.channels[i].isDM() {
			continue
		}
		mutated := false
		if containsString(b.channels[i].Members, slug) {
			next := make([]string, 0, len(b.channels[i].Members))
			for _, existing := range b.channels[i].Members {
				if existing != slug {
					next = append(next, existing)
				}
			}
			b.channels[i].Members = next
			mutated = true
		}
		if containsString(b.channels[i].Disabled, slug) {
			next := make([]string, 0, len(b.channels[i].Disabled))
			for _, existing := range b.channels[i].Disabled {
				if existing != slug {
					next = append(next, existing)
				}
			}
			b.channels[i].Disabled = next
			mutated = true
		}
		if mutated {
			b.channels[i].UpdatedAt = now
			removedChannels = append(removedChannels, b.channels[i].Slug)
		}
	}
	for i := range b.tasks {
		if b.tasks[i].Owner == slug {
			b.tasks[i].Owner = ""
			b.tasks[i].status = "open"
			b.tasks[i].UpdatedAt = now
		}
	}
	write, err := b.prepareBrokerStateWriteLocked()
	if err != nil {
		return officeMemberMutationResult{}, newOfficeMemberMutationError(http.StatusInternalServerError, "failed to persist broker state")
	}
	events := []officeChangeEvent{{Kind: "member_removed", Slug: slug}}
	for _, chSlug := range removedChannels {
		events = append(events, officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
	}
	return officeMemberMutationResult{
		payload: map[string]any{"ok": true},
		events:  events,
		write:   write,
	}, nil
}
