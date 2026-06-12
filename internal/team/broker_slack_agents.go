package team

// broker_slack_agents.go registers FOREIGN Slack agents — bots that live in a
// bridged Slack channel but are not this office's own bot — as bridged office
// members, following the OpenClaw pattern: the office member roster is the
// source of truth, and the external identity (the bot's Slack user id) lives in
// the member's ProviderBinding (Kind == provider.KindSlack).
//
// Registration is the INGRESS allowlist for the Slack transport: routeInbound
// drops every bot-authored message unless the author's Slack user id is
// registered here (fail-closed — see slack_transport.go). It is also the
// outbound mention map: an office message that @mentions a registered agent's
// slug is rendered with a real <@U…> ping so the foreign bot actually wakes.
//
//	POST /slack/agents { user_id, name, slug? } → { slug, name, user_id, created }
//	GET  /slack/agents → { agents: [ { slug, name, user_id } ] }

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/nex-crm/wuphf/internal/provider"
)

type slackAgentRegisterRequest struct {
	UserID string `json:"user_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Slug   string `json:"slug,omitempty"`
}

type slackAgentView struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	UserID string `json:"user_id"`
}

// handleSlackAgents serves the foreign-agent registry: POST registers (or
// idempotently re-registers) a foreign Slack bot as a bridged member; GET lists
// the current registrations.
func (b *Broker) handleSlackAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"agents": b.slackAgents()})
	case http.MethodPost:
		var body slackAgentRegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		userID := strings.TrimSpace(body.UserID)
		if !isSlackUserID(userID) {
			http.Error(w, "slack user_id required (U… or W…)", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = userID
		}
		// normalizeChannelSlug falls back to "general" on empty input, so pick
		// the non-empty source BEFORE normalizing (same gotcha as channel
		// routing — see normalizeChannelSlug).
		raw := strings.TrimSpace(body.Slug)
		if raw == "" {
			raw = name
		}
		slug := normalizeChannelSlug(raw)
		if slug == "" || slug == "general" {
			http.Error(w, "could not derive a usable slug; pass slug explicitly", http.StatusBadRequest)
			return
		}
		created, err := b.RegisterSlackAgent(slug, name, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"slug": slug, "name": name, "user_id": userID, "created": created,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// RegisterSlackAgent registers a foreign Slack bot as a bridged office member
// keyed by its Slack user id. Idempotent for an identical (slug, user id) pair;
// any other collision — the slug names a non-slack member, the slug is bound to
// a different Slack user, or the Slack user is already registered under another
// slug — is rejected so attribution stays one-to-one. Returns created=false on
// an idempotent re-registration.
func (b *Broker) RegisterSlackAgent(slug, name, userID string) (created bool, err error) {
	if slug == "" || slug == "general" {
		return false, fmt.Errorf("slug %q is reserved", slug)
	}
	if !isSlackUserID(userID) {
		return false, fmt.Errorf("invalid slack user id %q", userID)
	}
	// Serialize with every other member mutation so the conflict check below
	// and the create/bind steps are atomic against concurrent registrations
	// (same outer lock handleOfficeMembers mutations take).
	b.officeMemberMutationMu.Lock()
	defer b.officeMemberMutationMu.Unlock()
	if conflictErr := b.slackAgentConflict(slug, userID); conflictErr != nil {
		return false, conflictErr
	}
	existed := b.memberExists(slug)
	if err := b.EnsureBridgedMember(slug, name, "slack"); err != nil {
		return false, fmt.Errorf("register slack agent %q: %w", slug, err)
	}
	if err := b.SetMemberProvider(slug, provider.ProviderBinding{
		Kind:  provider.KindSlack,
		Slack: &provider.SlackProviderBinding{UserID: userID},
	}); err != nil {
		return false, fmt.Errorf("bind slack agent %q: %w", slug, err)
	}
	// Make the agent visible in every bridged room, not just #general (which
	// EnsureBridgedMember already handles). Best-effort consistency: membership
	// is presentation; inbound routing keys on the registry, not on membership.
	if err := b.ensureMemberInSlackChannels(slug); err != nil {
		return false, fmt.Errorf("join slack channels for %q: %w", slug, err)
	}
	return !existed, nil
}

// slackAgentConflict checks the one-to-one invariants between office slugs and
// Slack user ids under the broker lock. A nil return means slug+userID is free
// or already bound to exactly this pair.
func (b *Broker) slackAgentConflict(slug, userID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.members {
		m := &b.members[i]
		boundID := ""
		if m.Provider.Kind == provider.KindSlack && m.Provider.Slack != nil {
			boundID = m.Provider.Slack.UserID
		}
		if m.Slug == slug {
			if boundID == userID {
				return nil // idempotent re-registration
			}
			if boundID != "" {
				return fmt.Errorf("slug %q already bound to slack user %s", slug, boundID)
			}
			return fmt.Errorf("slug %q already names a non-slack member", slug)
		}
		if boundID == userID {
			return fmt.Errorf("slack user %s already registered as %q", userID, m.Slug)
		}
	}
	return nil
}

// memberExists reports whether slug names a current office member.
func (b *Broker) memberExists(slug string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.findMemberLocked(slug) != nil
}

// ensureMemberInSlackChannels appends slug to the member list of every channel
// carrying a "slack" surface, so agents registered after a channel was bridged
// still show up in that room.
func (b *Broker) ensureMemberInSlackChannels(slug string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	changed := false
	for i := range b.channels {
		ch := &b.channels[i]
		if ch.Surface == nil || ch.Surface.Provider != "slack" {
			continue
		}
		if !containsString(ch.Members, slug) {
			ch.Members = append(ch.Members, slug)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return b.saveLocked()
}

// slackAgents returns the registered foreign agents (members whose provider
// binding is Kind == slack), for the GET listing.
func (b *Broker) slackAgents() []slackAgentView {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []slackAgentView{}
	for i := range b.members {
		m := &b.members[i]
		if m.Provider.Kind != provider.KindSlack || m.Provider.Slack == nil {
			continue
		}
		out = append(out, slackAgentView{Slug: m.Slug, Name: m.Name, UserID: m.Provider.Slack.UserID})
	}
	return out
}

// MemberDisplayNames returns slug → display name for every office member.
// The Slack renderer uses it to turn office-internal "@slug" tokens — which
// mean nothing to real Slack users — into plain display names.
func (b *Broker) MemberDisplayNames() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]string, len(b.members))
	for i := range b.members {
		m := &b.members[i]
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = m.Slug
		}
		out[m.Slug] = name
	}
	return out
}

// SlackAgentSlugByUserID resolves a Slack user id to the office slug of a
// registered foreign agent, or "" when the id is not registered. This is the
// transport's inbound allowlist lookup — an empty return means "drop".
func (b *Broker) SlackAgentSlugByUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.members {
		m := &b.members[i]
		if m.Provider.Kind == provider.KindSlack && m.Provider.Slack != nil && m.Provider.Slack.UserID == userID {
			return m.Slug
		}
	}
	return ""
}

// SlackAgentUserIDBySlug resolves an office slug to its registered Slack user
// id, or "" when slug is not a registered foreign agent. This is the outbound
// mention map — the ONLY source a real <@U…> ping may be built from (never
// message text).
func (b *Broker) SlackAgentUserIDBySlug(slug string) string {
	// Empty-check BEFORE normalizing: normalizeChannelSlug("") == "general".
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	slug = normalizeChannelSlug(slug)
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil || m.Provider.Kind != provider.KindSlack || m.Provider.Slack == nil {
		return ""
	}
	return m.Provider.Slack.UserID
}

// isSlackUserID reports whether s is a well-formed Slack user id (U…) or an
// Enterprise Grid workspace user id (W…): the prefix followed only by
// uppercase alphanumerics. Strict on charset because the id is stored, listed
// via GET /slack/agents, and embedded in outbound <@…> mentions.
func isSlackUserID(s string) bool {
	if len(s) < 2 {
		return false
	}
	switch s[0] {
	case 'U', 'W':
	default:
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
