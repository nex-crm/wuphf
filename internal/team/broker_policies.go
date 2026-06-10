package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrPolicyRuleEmpty is the sentinel returned by RecordPolicy when the
// caller passes a blank rule. Callers (HTTP handlers) match on it via
// errors.Is to map the validation case to 400 — a sentinel keeps that
// dispatch from drifting if the underlying message ever changes.
var ErrPolicyRuleEmpty = errors.New("rule cannot be empty")

// RecordPolicy adds a new active policy or reactivates an existing one.
// Deduplicates by case-insensitive rule text — re-recording the same
// rule (any casing) returns the original record with Active flipped
// back on rather than minting a duplicate. The policy applies to ALL
// agents; use RecordPolicyScoped for per-agent assignment.
func (b *Broker) RecordPolicy(source, rule string) (officePolicy, error) {
	return b.RecordPolicyScoped(source, rule, nil)
}

// RecordPolicyScoped is RecordPolicy with per-agent assignment (B3).
// agents nil/empty means the policy applies to every agent. When the rule
// text matches an existing policy (normalized whitespace, case-insensitive),
// the existing record is reactivated and its scope is WIDENED: nil on
// either side wins (all agents), otherwise the union of both lists. A
// re-compiled or re-stated rule must never silently narrow who it governs.
func (b *Broker) RecordPolicyScoped(source, rule string, agents []string) (officePolicy, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return officePolicy{}, ErrPolicyRuleEmpty
	}
	agents = normalizePolicyAgents(agents)
	normalized := normalizePolicyRuleText(rule)
	b.mu.Lock()
	defer b.mu.Unlock()
	// Dedupe: don't add the same rule twice.
	for i, p := range b.policies {
		if normalizePolicyRuleText(p.Rule) == normalized {
			prevActive := b.policies[i].Active
			prevAgents := b.policies[i].Agents
			b.policies[i].Active = true
			b.policies[i].Agents = mergePolicyAgents(prevAgents, agents)
			if err := b.saveLocked(); err != nil {
				// Roll back the flips so the in-memory state stays
				// consistent with what's persisted on disk.
				b.policies[i].Active = prevActive
				b.policies[i].Agents = prevAgents
				return officePolicy{}, fmt.Errorf("persist policy: %w", err)
			}
			return b.policies[i], nil
		}
	}
	p := newOfficePolicy(source, rule)
	p.Agents = agents
	b.policies = append(b.policies, p)
	if err := b.saveLocked(); err != nil {
		// Roll back the in-memory append so the caller doesn't see a
		// policy on a subsequent ListPolicies that won't survive restart.
		b.policies = b.policies[:len(b.policies)-1]
		return officePolicy{}, fmt.Errorf("persist policy: %w", err)
	}
	return p, nil
}

// mergePolicyAgents widens two assignment scopes. Nil means "all agents",
// so nil on either side dominates; otherwise the union, normalized.
func mergePolicyAgents(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	return normalizePolicyAgents(append(append([]string(nil), a...), b...))
}

// ListPolicies returns all active policies.
func (b *Broker) ListPolicies() []officePolicy {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officePolicy, 0, len(b.policies))
	for _, p := range b.policies {
		if p.Active {
			out = append(out, p)
		}
	}
	return out
}

// policyByIDLocked returns the policy with the given ID or nil. Caller
// must hold b.mu.
func (b *Broker) policyByIDLocked(id string) *officePolicy {
	for i := range b.policies {
		if b.policies[i].ID == id {
			return &b.policies[i]
		}
	}
	return nil
}

// policyRequestMaxBodyBytes caps incoming policy rule bodies. A
// single rule + source string fits in well under 4 KiB; cap higher to
// allow longer human-typed rationale, but reject anything that's
// clearly not a policy.
const policyRequestMaxBodyBytes = 16 << 10

func (b *Broker) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		out := make([]officePolicy, 0, len(b.policies))
		for _, p := range b.policies {
			if p.Active {
				out = append(out, p)
			}
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"policies": out})

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, policyRequestMaxBodyBytes)
		defer r.Body.Close()
		var body struct {
			Source string `json:"source"`
			Rule   string `json:"rule"`
			// Agents optionally scopes the policy to specific agent
			// slugs. Empty/omitted = all agents — the default for
			// human chat feedback (core-loop step 11).
			Agents []string `json:"agents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Rule) == "" {
			http.Error(w, "rule is required", http.StatusBadRequest)
			return
		}
		p, err := b.RecordPolicyScoped(body.Source, body.Rule, body.Agents)
		if err != nil {
			// Validation vs persistence: ErrPolicyRuleEmpty is the only
			// validation-class error; anything else is a persistence
			// failure. Match on the sentinel rather than the message
			// text so a future copy edit can't break this dispatch.
			if errors.Is(err, ErrPolicyRuleEmpty) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p)

	case http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/policies/")
		id = strings.TrimSpace(id)
		if id == "" || id == "/policies" {
			// Parse from body
			var body struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			id = strings.TrimSpace(body.ID)
		}
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		flipped := false
		for i, p := range b.policies {
			if p.ID == id {
				b.policies[i].Active = false
				flipped = true
				break
			}
		}
		if flipped {
			if err := b.saveLocked(); err != nil {
				b.mu.Unlock()
				http.Error(w, "failed to persist policy delete", http.StatusInternalServerError)
				return
			}
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePoliciesSubpath routes /policies/{id}[/verb]. Supported:
//
//	DELETE /policies/{id}          — deactivate (delegates to handlePolicies)
//	POST   /policies/{id}/assign   — add an agent slug to the policy's scope
//	POST   /policies/{id}/unassign — remove an agent slug from the scope
//
// assign/unassign mirror the skills enable-for/disable-for pattern
// (skill_crud_endpoints.go). Scope semantics differ from skills in one
// deliberate way: nil/empty Agents means ALL agents, so
//   - assign on an all-agents policy is a no-op (it already applies);
//   - unassign on an all-agents policy materializes the current roster
//     minus that agent (the "disable for X" intent);
//   - unassigning the LAST agent is rejected — an empty list would flip
//     the policy back to all-agents silently. Deactivate it instead.
func (b *Broker) handlePoliciesSubpath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/policies/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.Error(w, "policy id required", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := strings.TrimSpace(parts[0])
	verb := ""
	if len(parts) == 2 {
		verb = strings.TrimSpace(parts[1])
	}

	if verb == "" && r.Method == http.MethodDelete {
		// Path-style delete: the existing handler already parses the id
		// from the URL, so delegate for one canonical deactivate path.
		b.handlePolicies(w, r)
		return
	}
	if verb != "assign" && verb != "unassign" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, policyRequestMaxBodyBytes)
	defer r.Body.Close()
	var body struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	agent := strings.ToLower(strings.TrimSpace(body.Agent))
	if agent == "" {
		http.Error(w, "agent required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	p := b.policyByIDLocked(id)
	if p == nil {
		b.mu.Unlock()
		http.Error(w, "policy not found", http.StatusNotFound)
		return
	}
	prevAgents := p.Agents
	switch verb {
	case "assign":
		if b.findMemberLocked(agent) == nil {
			b.mu.Unlock()
			http.Error(w, "agent not in roster", http.StatusBadRequest)
			return
		}
		if len(p.Agents) > 0 {
			p.Agents = normalizePolicyAgents(append(append([]string(nil), p.Agents...), agent))
		}
		// len==0 → already applies to all agents; idempotent no-op.
	case "unassign":
		scope := p.Agents
		if len(scope) == 0 {
			// All-agents policy: materialize the roster so "everyone
			// except this agent" is representable.
			scope = b.allMemberSlugsLocked()
		}
		filtered := make([]string, 0, len(scope))
		for _, slug := range scope {
			if slug != agent {
				filtered = append(filtered, slug)
			}
		}
		if len(filtered) == 0 {
			b.mu.Unlock()
			http.Error(w, "unassigning the last agent would re-broadcast the policy to everyone; deactivate the policy instead", http.StatusConflict)
			return
		}
		p.Agents = normalizePolicyAgents(filtered)
	}
	out := *p
	var saveErr error
	if !policyAgentsEqual(prevAgents, p.Agents) {
		if saveErr = b.saveLocked(); saveErr != nil {
			p.Agents = prevAgents
		}
	}
	b.mu.Unlock()
	if saveErr != nil {
		http.Error(w, "failed to persist policy assignment", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// policyAgentsEqual reports element-wise equality (both already normalized).
func policyAgentsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
