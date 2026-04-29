package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// RecordPolicy adds a new active policy or reactivates an existing one.
// Deduplicates by case-insensitive rule text — re-recording the same
// rule (any casing) returns the original record with Active flipped
// back on rather than minting a duplicate.
func (b *Broker) RecordPolicy(source, rule string) (officePolicy, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return officePolicy{}, fmt.Errorf("rule cannot be empty")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Dedupe: don't add the same rule twice.
	for i, p := range b.policies {
		if strings.EqualFold(p.Rule, rule) {
			b.policies[i].Active = true
			if err := b.saveLocked(); err != nil {
				return officePolicy{}, fmt.Errorf("persist policy: %w", err)
			}
			return b.policies[i], nil
		}
	}
	p := newOfficePolicy(source, rule)
	b.policies = append(b.policies, p)
	if err := b.saveLocked(); err != nil {
		// Roll back the in-memory append so the caller doesn't see a
		// policy on a subsequent ListPolicies that won't survive restart.
		b.policies = b.policies[:len(b.policies)-1]
		return officePolicy{}, fmt.Errorf("persist policy: %w", err)
	}
	return p, nil
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
		p, err := b.RecordPolicy(body.Source, body.Rule)
		if err != nil {
			// "rule cannot be empty" is the only validation-class error
			// from RecordPolicy; anything else is a persistence failure.
			if strings.Contains(err.Error(), "rule cannot be empty") {
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
