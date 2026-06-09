package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// broker_action_grants.go owns scoped, revocable action grants — the "scoped
// grants" half of the trust model (per-action modal default + scoped grants).
// When a human clicks "Approve & always allow" on the approval modal, a grant is
// minted for ONE specific action by ONE agent on ONE platform. The resolver then
// returns `proceed` (skip the modal) for that exact (agent, platform, action_id)
// until the grant expires or is revoked. The scope is always a concrete
// action_id — never a wildcard — so a grant can never widen the blast radius
// beyond the single action the human actually saw and authorized.
//
// SECURITY (host-trust model — read before changing): grant CRUD is broker-
// token-gated, exactly like connect/disconnect/resolve. The broker token IS the
// host-trust boundary in this single-trust-domain OSS deployment: the owner's
// web app and the MCP server both present it (actor kind = broker), and human
// SESSION actors are restricted shared-link guests that withAuth already 403s
// off non-allowlisted routes. So an actor-kind == human gate would be BACKWARDS
// — it would reject the owner. The practical control is that no MCP tool reaches
// /integrations/grants, so an agent can only get here with a shell tool + the
// broker token — at which point it already owns the broker (it could likewise
// curl /requests/answer to approve its own card). Grants do raise the stakes
// over that pre-existing exposure because they are STANDING and SILENT, so as
// defense-in-depth every grant is capped to maxGrantTTL and the residual
// (whether grants deserve a stronger control than the broker token) is flagged
// for human review on the PR. Multi-tenant auth is out of scope for this repo.

type actionGrant struct {
	ID          string `json:"id"`
	AgentSlug   string `json:"agent_slug"`
	Platform    string `json:"platform"`
	ActionScope string `json:"action_scope"`
	Channel     string `json:"channel,omitempty"`
	IssueID     string `json:"issue_id,omitempty"`
	GrantedBy   string `json:"granted_by"`
	GrantedAt   string `json:"granted_at"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
}

func actionGrantAgentKey(agent string) string { return strings.ToLower(strings.TrimSpace(agent)) }
func actionGrantPlatformKey(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}
func actionGrantScopeKey(actionID string) string { return strings.ToLower(strings.TrimSpace(actionID)) }

// maxGrantTTL caps how long any standing grant can authorize a modal bypass.
// Even a grant minted with no expiry (or a far-future one) self-expires after
// this window — defense in depth so a forgotten or maliciously-created grant
// cannot silently bypass approval forever. Capped at mint time in addActionGrant.
const maxGrantTTL = 30 * 24 * time.Hour

// parseGrantTime parses a grant timestamp accepting both second and nanosecond
// RFC3339 precision (a client may send either), returning ok=false otherwise.
func parseGrantTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts, true
	}
	return time.Time{}, false
}

// actionGrantActive reports whether a grant currently authorizes actions: not
// revoked and not past expiry. An unparseable expiry is treated as expired —
// fail closed, never let a malformed grant silently authorize an action.
func actionGrantActive(g actionGrant, now time.Time) bool {
	if strings.TrimSpace(g.RevokedAt) != "" {
		return false
	}
	if exp := strings.TrimSpace(g.ExpiresAt); exp != "" {
		ts, ok := parseGrantTime(exp)
		if !ok || !now.Before(ts) {
			return false
		}
	}
	return true
}

// hasActiveActionGrant reports whether a non-revoked, non-expired grant covers
// EXACTLY this (agent, platform, action_id). The match is exact on all three —
// no wildcard, no platform-wide grant — so the resolver can only auto-approve
// the precise action a human pre-authorized. Locks b.mu.
func (b *Broker) hasActiveActionGrant(agent, platform, actionID string, now time.Time) bool {
	a := actionGrantAgentKey(agent)
	p := actionGrantPlatformKey(platform)
	s := actionGrantScopeKey(actionID)
	if a == "" || p == "" || s == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.actionGrants {
		g := b.actionGrants[i]
		if actionGrantAgentKey(g.AgentSlug) == a &&
			actionGrantPlatformKey(g.Platform) == p &&
			actionGrantScopeKey(g.ActionScope) == s &&
			actionGrantActive(g, now) {
			return true
		}
	}
	return false
}

func cloneActionGrants(in []actionGrant) []actionGrant {
	if len(in) == 0 {
		return nil
	}
	out := make([]actionGrant, len(in))
	copy(out, in)
	return out
}

// addActionGrant mints a grant, idempotent on (agent, platform, scope): an
// existing active grant for the same triple is returned rather than duplicated.
// Locks b.mu.
func (b *Broker) addActionGrant(g actionGrant) actionGrant {
	now := time.Now().UTC()
	g.AgentSlug = strings.TrimSpace(g.AgentSlug)
	g.Platform = strings.TrimSpace(g.Platform)
	g.ActionScope = strings.TrimSpace(g.ActionScope)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.actionGrants {
		existing := b.actionGrants[i]
		if actionGrantAgentKey(existing.AgentSlug) == actionGrantAgentKey(g.AgentSlug) &&
			actionGrantPlatformKey(existing.Platform) == actionGrantPlatformKey(g.Platform) &&
			actionGrantScopeKey(existing.ActionScope) == actionGrantScopeKey(g.ActionScope) &&
			actionGrantActive(existing, now) {
			return existing
		}
	}
	b.counter++
	g.ID = fmt.Sprintf("grant-%d", b.counter)
	g.GrantedAt = now.Format(time.RFC3339)
	if strings.TrimSpace(g.GrantedBy) == "" {
		g.GrantedBy = "human"
	}
	// Cap the expiry to maxGrantTTL: an empty/unparseable/too-far-future expiry
	// is clamped so no grant outlives the policy ceiling.
	ceiling := now.Add(maxGrantTTL)
	if exp, ok := parseGrantTime(g.ExpiresAt); !ok || exp.After(ceiling) {
		g.ExpiresAt = ceiling.Format(time.RFC3339)
	}
	b.actionGrants = append(b.actionGrants, g)
	b.appendActionLocked(
		"integration_grant_created", "office", normalizeChannelSlug(g.Channel), g.GrantedBy,
		truncateSummary(fmt.Sprintf("Always allow %s · %s on %s", g.AgentSlug, g.ActionScope, g.Platform), 140), g.ID,
	)
	_ = b.saveLocked()
	return g
}

// revokeActionGrant marks a grant revoked. Idempotent: revoking an already
// revoked grant is a no-op that still returns it. Locks b.mu.
func (b *Broker) revokeActionGrant(id, actor string) (actionGrant, bool) {
	id = strings.TrimSpace(id)
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.actionGrants {
		if b.actionGrants[i].ID != id {
			continue
		}
		if strings.TrimSpace(b.actionGrants[i].RevokedAt) == "" {
			b.actionGrants[i].RevokedAt = now
			b.appendActionLocked(
				"integration_grant_revoked", "office", normalizeChannelSlug(b.actionGrants[i].Channel), actor,
				truncateSummary("Revoked grant "+id, 140), id,
			)
			_ = b.saveLocked()
		}
		return b.actionGrants[i], true
	}
	return actionGrant{}, false
}

func (b *Broker) handleIntegrationGrants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleListActionGrants(w, r)
	case http.MethodPost:
		b.handleMutateActionGrant(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleListActionGrants(w http.ResponseWriter, r *http.Request) {
	includeAll := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all")), "true")
	now := time.Now().UTC()
	b.mu.Lock()
	out := make([]actionGrant, 0, len(b.actionGrants))
	for _, g := range b.actionGrants {
		if includeAll || actionGrantActive(g, now) {
			out = append(out, g)
		}
	}
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"grants": out})
}

func (b *Broker) handleMutateActionGrant(w http.ResponseWriter, r *http.Request) {
	// SECURITY: this endpoint is host-only by construction. The broker token is
	// the host-trust boundary (same as connect/disconnect/resolve), and human
	// SESSION actors (shared-link guests) are already 403'd off non-allowlisted
	// routes by withAuth. The guarantee that a prompt-injected AGENT cannot
	// grant itself a standing approval bypass is that NO MCP tool reaches this
	// endpoint — agents act only through the fixed teammcp tool surface, none of
	// which mints grants. Grants are created from the human-driven approval modal
	// (web → broker token) and revoked from the Integrations app.
	var body struct {
		Action      string `json:"action"`
		ID          string `json:"id"`
		AgentSlug   string `json:"agent_slug"`
		Platform    string `json:"platform"`
		ActionScope string `json:"action_scope"`
		ActionID    string `json:"action_id"`
		Channel     string `json:"channel"`
		IssueID     string `json:"issue_id"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	switch strings.ToLower(strings.TrimSpace(body.Action)) {
	case "", "grant", "create":
		scope := strings.TrimSpace(body.ActionScope)
		if scope == "" {
			scope = strings.TrimSpace(body.ActionID)
		}
		if scope == "" || strings.TrimSpace(body.AgentSlug) == "" || strings.TrimSpace(body.Platform) == "" {
			http.Error(w, "agent_slug, platform, and action_scope are required", http.StatusBadRequest)
			return
		}
		if strings.Contains(scope, "*") {
			http.Error(w, "action_scope must be a concrete action_id, not a wildcard", http.StatusBadRequest)
			return
		}
		grant := b.addActionGrant(actionGrant{
			AgentSlug:   strings.TrimSpace(body.AgentSlug),
			Platform:    strings.TrimSpace(body.Platform),
			ActionScope: scope,
			Channel:     strings.TrimSpace(body.Channel),
			IssueID:     strings.TrimSpace(body.IssueID),
			GrantedBy:   integrationRequestActor(r),
			ExpiresAt:   strings.TrimSpace(body.ExpiresAt),
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"grant": grant})
	case "revoke":
		id := strings.TrimSpace(body.ID)
		if id == "" {
			http.Error(w, "id is required to revoke", http.StatusBadRequest)
			return
		}
		grant, found := b.revokeActionGrant(id, integrationRequestActor(r))
		if !found {
			http.Error(w, "grant not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"grant": grant})
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}
