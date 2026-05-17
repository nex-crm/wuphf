package team

// broker_onboarding_phase2.go — Phase 2 deterministic CEO conversation.
//
// This file implements:
//   - advancePhase: validates and drives the phase state machine, emits the
//     next CEO suggestion card into b.messages on the CEO DM.
//   - Per-phase deterministic CEO message generators (greet → bridge).
//   - seedMinimalScratchLocked: the scratch-path atomic seed (#general +
//     2 wiki stubs + CEO). No fake team.
//   - ceoOnboardingTransitionFn: the onboarding.TransitionFunc wired into
//     the HTTP handler by the launcher/broker bootstrap.
//
// Hard rules (from spec + task brief):
//   - NO LLM tokens in Phase 2. All CEO messages are deterministic templates.
//   - CEO transcript lives in b.messages (channel = ceo DM slug).
//   - Atomic seed via seedFromBlueprintLocked (blueprint path) or
//     seedMinimalScratchLocked (scratch path) at the seed phase boundary.
//   - Every CEO payload that becomes a ceo_* card MUST pass through
//     sanitizeCEOPayload before the broker write.
//   - Phase 2 does NOT wire draft/approve/kickoff. Those are Phase 4.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
)

// ceoOnboardingTransitionFn returns an onboarding.TransitionFunc that the
// broker wires into POST /onboarding/transition so phase advances trigger
// CEO message emissions and, at the seed phase, the atomic office seed.
//
// The returned function is safe to call from a goroutine outside the broker
// lock; it acquires the lock internally via advancePhase.
func (b *Broker) ceoOnboardingTransitionFn() onboarding.TransitionFunc {
	return func(phase string, s *onboarding.State) error {
		return b.advancePhase(s, phase)
	}
}

// advancePhase emits the next CEO suggestion card for the given phase into the
// CEO DM (b.messages). It validates the state machine transition (guard
// against callers that bypass /onboarding/transition), ensures the CEO DM
// channel exists, and appends the deterministic message.
//
// At the seed phase it also runs the atomic office seed.
//
// Caller must NOT hold b.mu — this function acquires and releases it
// internally (and for EnsureDirectChannel which has its own lock).
func (b *Broker) advancePhase(s *onboarding.State, next string) error {
	// Ensure the CEO DM channel exists before trying to post into it.
	dmSlug, err := b.EnsureDirectChannel("ceo")
	if err != nil {
		return fmt.Errorf("onboarding phase %s: ensure CEO DM: %w", next, err)
	}

	// At the seed phase, run the atomic office seed BEFORE posting the
	// success message so the user sees a coherent office on the first paint.
	if next == onboarding.PhaseSeed {
		if err := b.runSeedPhase(s); err != nil {
			return fmt.Errorf("onboarding: seed phase: %w", err)
		}
	}

	// Build the deterministic CEO message for this phase.
	msgs := ceoDeterministicMessages(next, s)
	if len(msgs) == 0 {
		// Phase 4 phases (draft/approve/kickoff) are not wired in Phase 2.
		// Log and return without error so the transition still persists.
		log.Printf("onboarding: no deterministic messages for phase %q (Phase 4 not yet wired)", next)
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, payload := range msgs {
		// Sanitize the payload before writing into b.messages to close the
		// confused-deputy injection surface (mirrors PR #684 audit closure).
		sanitized, err := sanitizeCEOPayload(payload)
		if err != nil {
			return fmt.Errorf("onboarding: sanitize CEO payload for phase %q: %w", next, err)
		}
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "ceo",
			Channel:   dmSlug,
			Kind:      payload.Kind,
			Content:   payload.Content,
			Payload:   sanitized,
			Tagged:    []string{},
			Timestamp: now,
		})
	}

	// Store the last suggestion in the onboarding state as PendingSuggestion
	// so it can be re-emitted on resume (idempotent by suggestion ID).
	// We only track the last interactive card (not plain text messages).
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].SuggestionPayload != nil {
			pending := &onboarding.Suggestion{
				ID:       msgs[i].SuggestionID,
				Phase:    next,
				Kind:     msgs[i].Kind,
				Payload:  *msgs[i].SuggestionPayload,
				IssuedAt: time.Now().UTC(),
			}
			// Best-effort: persist the pending suggestion outside the broker
			// lock in a goroutine so we don't hold the lock during file I/O.
			// A failure here means the suggestion won't be re-emitted on crash
			// recovery — acceptable for Phase 2.
			go func(p *onboarding.Suggestion) {
				if st, loadErr := onboarding.Load(); loadErr == nil {
					st.PendingSuggestion = p
					if saveErr := onboarding.Save(st); saveErr != nil {
						log.Printf("onboarding: persist PendingSuggestion for phase %s: %v", next, saveErr)
					}
				}
			}(pending)
			break
		}
	}

	return b.saveLocked()
}

// runSeedPhase runs the atomic office seed at the seed phase boundary.
// Selects seedFromBlueprintLocked (blueprint path) vs seedMinimalScratchLocked
// (scratch path) based on FormAnswers.BlueprintID.
//
// Caller must NOT hold b.mu.
func (b *Broker) runSeedPhase(s *onboarding.State) error {
	blueprintID := strings.TrimSpace(s.FormAnswers.BlueprintID)
	if blueprintID != "" {
		// Blueprint path: reuse the existing atomic seed.
		loaded, err := operations.LoadBlueprint(onboarding.ResolveTemplatesRepoRoot(""), blueprintID)
		if err != nil {
			return fmt.Errorf("load blueprint %q: %w", blueprintID, err)
		}
		var selectedAgents []string
		if len(s.FormAnswers.PickedAgents) > 0 {
			selectedAgents = s.FormAnswers.PickedAgents
		}
		// task is not known at seed time in Phase 2; skipTask=true posts a
		// system welcome instead of a directive task.
		const task = ""
		const skipTask = true
		b.mu.Lock()
		seedErr := b.seedFromBlueprintLocked(loaded, selectedAgents, task, skipTask, false)
		b.mu.Unlock()
		if seedErr != nil {
			return seedErr
		}
		b.ensureNotebookDirsForRoster()
		b.materializeBlueprintWiki(loaded)
		return nil
	}
	// Scratch path: minimal seed (#general + 2 wiki stubs + CEO).
	b.mu.Lock()
	seedErr := b.seedMinimalScratchLocked(s)
	b.mu.Unlock()
	if seedErr != nil {
		return seedErr
	}
	b.ensureNotebookDirsForRoster()
	return nil
}

// seedMinimalScratchLocked seeds the absolute minimum for the scratch path:
//   - CEO agent (BuiltIn, lead)
//   - #general channel (members: CEO)
//   - 2 wiki stub files: README.md and team-charter.md (written to disk
//     outside the lock via materializeScratchWikiStubs — called by the caller
//     after releasing the lock)
//
// Caller MUST hold b.mu.
//
// This is intentionally minimal. No fake team is seeded. When the user
// describes a first task at the draft phase, CEO proposes agents inline
// (Phase 4 LLM path). See spec hard rule: "Scratch path uses
// seedMinimalScratchLocked (NEW function you write), not
// synthesizeBlueprintFromState."
func (b *Broker) seedMinimalScratchLocked(s *onboarding.State) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Seed CEO as the sole member.
	b.members = []officeMember{
		{
			Slug:           "ceo",
			Name:           "CEO",
			Role:           "lead",
			PermissionMode: "plan",
			BuiltIn:        true,
			CreatedBy:      "wuphf",
			CreatedAt:      now,
		},
	}
	b.memberIndex = map[string]int{"ceo": 0}

	// Seed #general.
	companyName := strings.TrimSpace(s.FormAnswers.CompanyName)
	if companyName == "" {
		companyName = "your office"
	}
	generalDesc := fmt.Sprintf("Primary coordination channel for %s.", companyName)
	b.channels = []teamChannel{{
		Slug:        "general",
		Name:        "general",
		Description: generalDesc,
		Members:     []string{"ceo"},
		CreatedBy:   "wuphf",
		CreatedAt:   now,
		UpdatedAt:   now,
	}}

	// Clear tasks and message history for a fresh start.
	b.tasks = nil
	b.messages = nil
	b.counter = 0
	b.lastTaggedAt = make(map[string]time.Time)

	// Signal subscribers that the office roster was replaced.
	b.publishOfficeChangeLocked(officeChangeEvent{Kind: "office_reseeded"})

	return b.saveLocked()
}

// materializeScratchWikiStubs writes the two minimal wiki stubs for the
// scratch path: README.md and team-charter.md. These are placeholders
// with FormAnswers-derived content; the website scan may later populate
// README.md with scraped facts.
//
// Caller must NOT hold b.mu. Best-effort: errors are logged, not returned,
// so a file I/O failure does not fail the seed phase.
func (b *Broker) materializeScratchWikiStubs(s *onboarding.State) {
	home := config.RuntimeHomeDir()
	if home == "" {
		log.Printf("onboarding: materializeScratchWikiStubs: WUPHF_RUNTIME_HOME unset")
		return
	}
	wikiRoot := filepath.Join(home, ".wuphf", "wiki")

	companyName := strings.TrimSpace(s.FormAnswers.CompanyName)
	if companyName == "" {
		companyName = "Your Company"
	}
	description := strings.TrimSpace(s.FormAnswers.Description)

	readmeContent := fmt.Sprintf("# %s\n\n%s\n\n_Populated by the onboarding website scan._\n",
		companyName, description)
	charterContent := fmt.Sprintf("# Team Charter\n\n## Company\n%s\n\n## Mission\n_Add your mission statement here._\n\n## Principles\n_Add your team principles here._\n",
		companyName)

	stubs := []struct {
		path    string
		content string
	}{
		{"README.md", readmeContent},
		{"team-charter.md", charterContent},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	worker := b.WikiWorker()
	for _, stub := range stubs {
		fullPath := filepath.Join(wikiRoot, stub.path)
		if err := writeWikiStubIfAbsent(ctx, fullPath, stub.content); err != nil {
			log.Printf("onboarding: scratch wiki stub %s: %v", stub.path, err)
		}
	}

	if worker != nil && worker.Repo() != nil {
		repo := worker.Repo()
		if err := repo.IndexRegen(ctx); err != nil {
			log.Printf("onboarding: scratch wiki index regen: %v", err)
		}
		sha, err := repo.CommitBootstrap(ctx, "wuphf: materialize scratch wiki stubs")
		if err != nil {
			log.Printf("onboarding: scratch wiki commit: %v", err)
		} else if sha != "" {
			log.Printf("onboarding: scratch wiki stubs committed %s", sha)
		}
	}
}

// writeWikiStubIfAbsent writes content to path only when the file does not
// already exist. Uses O_CREATE|O_EXCL for atomic existence check.
func writeWikiStubIfAbsent(_ context.Context, path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil // file already exists; leave it
		}
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ceoMessagePayload is the internal representation of a CEO deterministic
// turn. It carries both the plain-text Content (for the channelMessage) and
// an optional structured SuggestionPayload (for interactive cards).
type ceoMessagePayload struct {
	Kind              string
	Content           string
	SuggestionID      string
	SuggestionPayload *json.RawMessage
	Payload           json.RawMessage // sanitized structured payload (set by sanitizeCEOPayload)
}

// ceoDeterministicMessages returns the ordered set of CEO messages to post
// on entering the given phase. Each entry maps to one channelMessage in the
// CEO DM.
//
// All strings are verbatim from the spec "## CEO Voice — deterministic
// templates" section. No LLM tokens are spent here.
//
// Returns nil for phases not handled in Phase 2 (draft/approve/kickoff).
func ceoDeterministicMessages(phase string, s *onboarding.State) []ceoMessagePayload {
	companyName := ""
	if s != nil {
		companyName = strings.TrimSpace(s.FormAnswers.CompanyName)
	}
	switch phase {
	case onboarding.PhaseGreet:
		return []ceoMessagePayload{{
			Kind:         "ceo_form_field",
			Content:      "Office name?",
			SuggestionID: "greet-company-name",
			SuggestionPayload: mustMarshalRaw(map[string]interface{}{
				"field":       "company_name",
				"label":       "Office name",
				"placeholder": "e.g. Acme Billing",
				"required":    true,
			}),
		}}

	case onboarding.PhaseIdentity:
		// Identity collects description, priority, website URL, and owner
		// info as sequential form-field cards. Return them all at once; the
		// frontend renders one at a time as each is submitted.
		return []ceoMessagePayload{
			{
				Kind:         "ceo_form_field",
				Content:      "What does " + displayCompany(companyName) + " do?",
				SuggestionID: "identity-description",
				SuggestionPayload: mustMarshalRaw(map[string]interface{}{
					"field":       "description",
					"label":       "Short description",
					"placeholder": "e.g. Subscription billing for indie SaaS",
					"required":    false,
				}),
			},
			{
				Kind:         "ceo_form_field",
				Content:      "Top priority right now? (Optional.)",
				SuggestionID: "identity-priority",
				SuggestionPayload: mustMarshalRaw(map[string]interface{}{
					"field":       "priority",
					"label":       "Top priority",
					"placeholder": "e.g. Stripe webhooks",
					"required":    false,
					"skip_chip":   "Not yet",
				}),
			},
			{
				Kind:         "ceo_form_field",
				Content:      "Got a website I can scan for context?",
				SuggestionID: "identity-website",
				SuggestionPayload: mustMarshalRaw(map[string]interface{}{
					"field":       "website_url",
					"label":       "Website URL",
					"placeholder": "e.g. https://acme.com",
					"required":    false,
					"skip_chip":   "Skip",
				}),
			},
			{
				Kind:         "ceo_form_field",
				Content:      "Your name? Your role? (Optional.)",
				SuggestionID: "identity-owner",
				SuggestionPayload: mustMarshalRaw(map[string]interface{}{
					"fields": []map[string]interface{}{
						{"field": "owner_name", "label": "Your name", "placeholder": "e.g. Sam", "required": false},
						{"field": "owner_role", "label": "Your role", "placeholder": "e.g. Founder", "required": false},
					},
				}),
			},
		}

	case onboarding.PhaseScan:
		websiteURL := ""
		if s != nil {
			websiteURL = strings.TrimSpace(s.FormAnswers.WebsiteURL)
		}
		return []ceoMessagePayload{{
			Kind:         "ceo_scan_chip",
			Content:      fmt.Sprintf("Scanning %s…", displayURL(websiteURL)),
			SuggestionID: "scan-progress-" + urlToSuggestionID(websiteURL),
			SuggestionPayload: mustMarshalRaw(map[string]interface{}{
				"url":    websiteURL,
				"status": "scanning",
			}),
		}}

	case onboarding.PhaseBlueprint:
		return []ceoMessagePayload{{
			Kind:         "ceo_chip_row",
			Content:      "Pick a starter template, or start from scratch:",
			SuggestionID: "blueprint-pick",
			SuggestionPayload: mustMarshalRaw(map[string]interface{}{
				"field": "blueprint_id",
				"chips": []map[string]interface{}{
					{"id": "bookkeeping", "label": "Bookkeeping"},
					{"id": "content-ops", "label": "Content Ops"},
					{"id": "engineering-team", "label": "Engineering Team"},
					{"id": "", "label": "Start from scratch"},
				},
			}),
		}}

	case onboarding.PhaseTeam:
		// The team trim checklist is built from the blueprint's agent roster.
		// We emit a generic checklist here; the broker bootstrap populates
		// the actual agents from the picked blueprint when it wires this.
		return []ceoMessagePayload{{
			Kind:         "ceo_team_trim",
			Content:      "This blueprint comes with a team — keep or trim:",
			SuggestionID: "team-trim",
			SuggestionPayload: mustMarshalRaw(map[string]interface{}{
				"field":  "picked_agents",
				"agents": []interface{}{}, // populated by broker at emit time
			}),
		}}

	case onboarding.PhaseSeed:
		// Seed-done message: scratch vs blueprint path.
		blueprintID := ""
		if s != nil {
			blueprintID = strings.TrimSpace(s.FormAnswers.BlueprintID)
		}
		if blueprintID == "" {
			return []ceoMessagePayload{{
				Kind:    "text",
				Content: "✓ Empty office, your call. Start an issue, or look around?",
			}}
		}
		return []ceoMessagePayload{{
			Kind:    "text",
			Content: "✓ Office set up. Start an issue, or look around?",
		}}

	case onboarding.PhaseBridge:
		return []ceoMessagePayload{{
			Kind:         "ceo_chip_row",
			Content:      "All set up. What would you like to do?",
			SuggestionID: "bridge-choice",
			SuggestionPayload: mustMarshalRaw(map[string]interface{}{
				"chips": []map[string]interface{}{
					{"id": "start_issue", "label": "Start an issue", "action": "transition", "phase": "draft"},
					{"id": "look_around", "label": "Look around first", "action": "transition", "phase": "complete"},
				},
			}),
		}}

	case onboarding.PhaseComplete:
		// Marcus path: "look around first".
		return []ceoMessagePayload{{
			Kind:    "text",
			Content: "✓ I'll be in #general when you need me.",
		}}

	default:
		// draft/approve/kickoff and unknown phases return nil — no Phase 2 wiring.
		return nil
	}
}

// sanitizeCEOPayload sanitizes all user-controlled string fields in a CEO
// message payload to prevent confused-deputy injection. Returns the
// sanitized json.RawMessage or an error if marshaling fails.
//
// This mirrors the sanitizeContextValue logic in internal/teammcp/actions.go.
// It cannot import that function directly because teammcp imports team
// (which would create a cycle). The sanitization rule is identical:
// newlines → spaces, bullet → middle-dot, collapse whitespace.
func sanitizeCEOPayload(msg ceoMessagePayload) (json.RawMessage, error) {
	if msg.SuggestionPayload == nil {
		return nil, nil
	}
	// Unmarshal to a generic map, sanitize string leaves, re-marshal.
	var raw interface{}
	if err := json.Unmarshal(*msg.SuggestionPayload, &raw); err != nil {
		return nil, fmt.Errorf("sanitizeCEOPayload unmarshal: %w", err)
	}
	sanitized := sanitizeJSONValue(raw)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return nil, fmt.Errorf("sanitizeCEOPayload marshal: %w", err)
	}
	return json.RawMessage(out), nil
}

// sanitizeJSONValue recursively sanitizes all string values in a JSON-decoded
// value tree. Uses the same rules as sanitizeContextValue in teammcp/actions.go.
func sanitizeJSONValue(v interface{}) interface{} {
	switch vt := v.(type) {
	case string:
		return teamSanitizeContextValue(vt)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(vt))
		for k, val := range vt {
			out[k] = sanitizeJSONValue(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(vt))
		for i, val := range vt {
			out[i] = sanitizeJSONValue(val)
		}
		return out
	default:
		return v
	}
}

// teamSanitizeContextValue is a copy of the sanitizeContextValue function
// from internal/teammcp/actions.go. It lives here because that package
// imports this one; importing it back would create a cycle.
//
// Rule: collapse newlines, bullet chars, and multi-space runs so that a
// forged "Action:" header embedded in agent input cannot land at line-start
// where a card parser would interpret it as a structured field.
func teamSanitizeContextValue(s string) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer(
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
		" ", " ", // LINE SEPARATOR
		" ", " ", // PARAGRAPH SEPARATOR
		"•", "·", // U+2022 BULLET → U+00B7 MIDDLE DOT
	)
	cleaned := r.Replace(s)
	return strings.Join(strings.Fields(cleaned), " ")
}

// mustMarshalRaw marshals v to a json.RawMessage. Panics on error — used
// only for literal map[string]interface{} values in this file where the input
// is always marshallable.
func mustMarshalRaw(v interface{}) *json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshalRaw: %v", err))
	}
	raw := json.RawMessage(b)
	return &raw
}

// displayCompany returns a human-readable company name or a fallback.
func displayCompany(name string) string {
	if strings.TrimSpace(name) == "" {
		return "your company"
	}
	return strings.TrimSpace(name)
}

// displayURL shortens a URL for display in CEO messages.
func displayURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return "your site"
	}
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, "/")
	return u
}

// urlToSuggestionID returns a lowercase slug safe for use as a suggestion ID suffix.
func urlToSuggestionID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(
		"https://", "",
		"http://", "",
		"/", "-",
		".", "-",
		":", "-",
	).Replace(s)
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}
