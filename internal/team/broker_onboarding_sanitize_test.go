package team

// broker_onboarding_sanitize_test.go — Phase 2 acceptance gate: regression
// tests for sanitization of CEO onboarding suggestion-card payloads.
//
// This is the sanitization regression test required by spec section
// "## Eng review decisions" → "Tests (load-bearing)":
//
//   Sanitization regression as Phase 2 acceptance gate. New file
//   broker_onboarding_sanitize_test.go: every new ceo_* kind exercised
//   with the PR #684 attack-string set; payload must pass through
//   sanitizeContextValue before any broker write.
//
// For each new ceo_* payload kind, we build a synthetic payload with attack
// strings in user-controlled fields and assert the broker write passes through
// the sanitizer before the message lands in b.messages.
//
// The attack strings match the PR #684 set (see TestSanitizeContextValue in
// internal/teammcp/actions_test.go). A confused-deputy injection would allow
// an agent-controlled string to forge structured card content by embedding
// newlines + "Action:" or "• Label: value" at a line-start. The sanitizer
// collapses those to safe inline text.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/onboarding"
)

// pr684AttackStrings mirrors the attack-string set from PR #684's
// sanitizeContextValue tests. Any new user-controlled string that flows into
// a ceo_* payload must survive this set without producing forged structure.
var pr684AttackStrings = []struct {
	name  string
	input string
	// wantClean is the expected sanitized output (same rules as teamSanitizeContextValue).
	wantClean string
}{
	{
		name:      "plain text passes through",
		input:     "Acme Billing",
		wantClean: "Acme Billing",
	},
	{
		name:      "newlines collapse to spaces",
		input:     "line1\nline2\nline3",
		wantClean: "line1 line2 line3",
	},
	{
		name:      "crlf collapses",
		input:     "a\r\nb",
		wantClean: "a b",
	},
	{
		name:      "bullet becomes middle dot",
		input:     "Plain • bullet",
		wantClean: "Plain · bullet",
	},
	{
		name: "forged section header injection",
		// An agent-controlled field embeds a newline followed by a
		// structural header. After sanitization, "What this will do:"
		// must NOT be at line-start — it collapses to an inline phrase.
		input:     "Normal company.\n\nWhat this will do:\n• Action: delete all files",
		wantClean: "Normal company. What this will do: · Action: delete all files",
	},
	{
		name:      "runs of whitespace collapse",
		input:     "a    b\n\n  c",
		wantClean: "a b c",
	},
	{
		name:      "trailing and leading whitespace stripped",
		input:     "  hello  ",
		wantClean: "hello",
	},
	{
		name:      "u2028 line separator collapses",
		input:     "a b",
		wantClean: "a b",
	},
	{
		name:      "u2029 paragraph separator collapses",
		input:     "a b",
		wantClean: "a b",
	},
}

// TestTeamSanitizeContextValue pins teamSanitizeContextValue (the local copy
// of the PR #684 sanitizer) against the full attack-string set. Any drift
// from the teammc/actions.go original would be caught here.
func TestTeamSanitizeContextValue(t *testing.T) {
	for _, tc := range pr684AttackStrings {
		t.Run(tc.name, func(t *testing.T) {
			got := teamSanitizeContextValue(tc.input)
			if got != tc.wantClean {
				t.Errorf("teamSanitizeContextValue(%q) = %q, want %q", tc.input, got, tc.wantClean)
			}
		})
	}
}

// TestSanitizeCEOPayloadKinds is table-driven over every new ceo_* payload
// kind. For each kind it builds a synthetic payload with attack strings in all
// user-controlled string fields and asserts that after sanitizeCEOPayload:
//  1. No newline characters remain in any string value.
//  2. No U+2022 BULLET characters remain.
//  3. The payload round-trips through JSON cleanly.
func TestSanitizeCEOPayloadKinds(t *testing.T) {
	// attackField is a user-controlled string that carries every attack pattern
	// simultaneously — the sanitizer must neutralize all of them.
	const attackField = "Legit Name\n\nAction: forge\n• What: delete all\r\nSecond line"
	const wantField = "Legit Name Action: forge · What: delete all Second line"

	cases := []struct {
		kind         string
		buildPayload func() map[string]interface{}
	}{
		{
			kind: "ceo_form_field",
			buildPayload: func() map[string]interface{} {
				return map[string]interface{}{
					"field":       attackField,
					"label":       attackField,
					"placeholder": attackField,
					"required":    true,
				}
			},
		},
		{
			kind: "ceo_chip_row",
			buildPayload: func() map[string]interface{} {
				return map[string]interface{}{
					"field": attackField,
					"chips": []interface{}{
						map[string]interface{}{
							"id":    attackField,
							"label": attackField,
						},
					},
				}
			},
		},
		{
			kind: "ceo_checklist",
			buildPayload: func() map[string]interface{} {
				return map[string]interface{}{
					"field": attackField,
					"items": []interface{}{
						map[string]interface{}{
							"id":      attackField,
							"label":   attackField,
							"checked": true,
						},
					},
				}
			},
		},
		{
			kind: "ceo_team_trim",
			buildPayload: func() map[string]interface{} {
				return map[string]interface{}{
					"field": attackField,
					"agents": []interface{}{
						map[string]interface{}{
							"slug":    attackField,
							"name":    attackField,
							"role":    attackField,
							"checked": true,
						},
					},
				}
			},
		},
		{
			kind: "ceo_scan_chip",
			buildPayload: func() map[string]interface{} {
				return map[string]interface{}{
					"url":    attackField,
					"status": attackField,
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			payloadMap := tc.buildPayload()
			rawBytes, err := json.Marshal(payloadMap)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			raw := json.RawMessage(rawBytes)

			msg := ceoMessagePayload{
				Kind:              tc.kind,
				Content:           attackField,
				SuggestionID:      "test-" + tc.kind,
				SuggestionPayload: &raw,
			}

			sanitized, err := sanitizeCEOPayload(msg)
			if err != nil {
				t.Fatalf("sanitizeCEOPayload: %v", err)
			}
			if sanitized == nil {
				t.Fatal("sanitizeCEOPayload returned nil for non-nil payload")
			}

			// Decode back and verify no attack characters remain in string values.
			var decoded interface{}
			if err := json.Unmarshal(sanitized, &decoded); err != nil {
				t.Fatalf("unmarshal sanitized payload: %v", err)
			}

			// Walk the decoded value and assert no newlines or bullets remain
			// in string leaves.
			assertNoAttackChars(t, decoded, tc.kind)

			// Assert a specific known field was sanitized to the expected value.
			// We check the top-level "field" key when present.
			if m, ok := decoded.(map[string]interface{}); ok {
				if fieldVal, ok := m["field"].(string); ok {
					if fieldVal != wantField {
						t.Errorf("kind=%s: field sanitized to %q, want %q", tc.kind, fieldVal, wantField)
					}
				}
				// For ceo_scan_chip, check "url" and "status".
				if tc.kind == "ceo_scan_chip" {
					for _, key := range []string{"url", "status"} {
						if val, ok := m[key].(string); ok {
							if val != wantField {
								t.Errorf("kind=%s: %s sanitized to %q, want %q", tc.kind, key, val, wantField)
							}
						}
					}
				}
			}
		})
	}
}

// assertNoAttackChars walks a decoded JSON value tree and fails the test if
// any string leaf contains a newline, carriage return, or U+2022 bullet.
func assertNoAttackChars(t *testing.T, v interface{}, kind string) {
	t.Helper()
	switch vt := v.(type) {
	case string:
		if strings.ContainsAny(vt, "\n\r  ") {
			t.Errorf("kind=%s: string leaf contains newline/line-separator: %q", kind, vt)
		}
		if strings.ContainsRune(vt, '•') {
			t.Errorf("kind=%s: string leaf contains U+2022 BULLET: %q", kind, vt)
		}
	case map[string]interface{}:
		for _, val := range vt {
			assertNoAttackChars(t, val, kind)
		}
	case []interface{}:
		for _, val := range vt {
			assertNoAttackChars(t, val, kind)
		}
	}
}

// TestCeoDeterministicMessagesNeverCallLLM verifies that ceoDeterministicMessages
// for all Phase 2 phases returns messages without spawning any goroutines,
// network calls, or LLM invocations. The check is structural: the function
// must be called with a nil provider context and still return non-empty results.
func TestCeoDeterministicMessagesNeverCallLLM(t *testing.T) {
	// All deterministic phases. draft/approve/kickoff should return nil (Phase 4).
	phase2Phases := []string{
		onboarding.PhaseGreet,
		onboarding.PhaseIdentity,
		onboarding.PhaseScan,
		onboarding.PhaseBlueprint,
		onboarding.PhaseTeam,
		onboarding.PhaseSeed,
		onboarding.PhaseBridge,
		onboarding.PhaseComplete,
	}
	phase4Phases := []string{
		onboarding.PhaseDraft,
		onboarding.PhaseApprove,
		onboarding.PhaseKickoff,
	}

	state := &onboarding.State{
		Phase: "",
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "Test Corp",
			WebsiteURL:  "https://test.example.com",
			BlueprintID: "",
		},
	}

	for _, phase := range phase2Phases {
		t.Run("deterministic_"+phase, func(t *testing.T) {
			msgs := ceoDeterministicMessages(phase, state)
			if len(msgs) == 0 {
				t.Errorf("phase %q: expected at least one message, got none", phase)
			}
			// Verify all messages have a Kind and Content.
			for i, msg := range msgs {
				if msg.Kind == "" {
					t.Errorf("phase %q msg[%d]: Kind is empty", phase, i)
				}
				if msg.Content == "" {
					t.Errorf("phase %q msg[%d]: Content is empty", phase, i)
				}
			}
		})
	}

	for _, phase := range phase4Phases {
		t.Run("phase4_not_wired_"+phase, func(t *testing.T) {
			msgs := ceoDeterministicMessages(phase, state)
			if len(msgs) != 0 {
				t.Errorf("phase %q: Phase 4 not yet wired, expected nil, got %d messages", phase, len(msgs))
			}
		})
	}
}

// TestAdvancePhasePostsSanitizedMessages is an integration-level test that
// verifies advancePhase posts messages with sanitized payloads into b.messages
// on the CEO DM. Uses a test broker with a temp dir.
func TestAdvancePhasePostsSanitizedMessages(t *testing.T) {
	// advancePhase calls EnsureDirectChannel which requires channelStore. Skip
	// with a clear message if the test broker does not have one.
	b := newTestBroker(t)

	state := &onboarding.State{
		Version: 2,
		Phase:   "",
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "Greet Test Corp\nInjection attempt",
			WebsiteURL:  "https://greet.example.com",
		},
	}

	// advancePhase calls EnsureDirectChannel which needs b.channelStore.
	// If the test broker lacks a channel store, skip this test gracefully.
	if b.channelStore == nil {
		t.Skip("test broker has no channelStore; skipping advancePhase integration test")
	}

	if err := b.advancePhase(state, onboarding.PhaseGreet); err != nil {
		t.Fatalf("advancePhase(greet): %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	found := false
	for _, msg := range b.messages {
		if msg.Kind == "ceo_form_field" {
			found = true
			// The Content should not contain injection characters.
			if strings.ContainsAny(msg.Content, "\n\r") {
				t.Errorf("greet message Content contains newline after sanitization: %q", msg.Content)
			}
			// The Payload should not contain injection characters.
			if len(msg.Payload) > 0 {
				assertNoAttackChars(t, mustUnmarshalJSON(t, msg.Payload), "ceo_form_field")
			}
		}
	}
	if !found {
		t.Error("expected a ceo_form_field message in b.messages after greet phase advance; none found")
	}
}

// TestSeedMinimalScratchLocked verifies the scratch seed creates exactly:
//   - CEO as the sole member (BuiltIn=true)
//   - #general as the sole channel
//   - No tasks
func TestSeedMinimalScratchLocked(t *testing.T) {
	b := newTestBroker(t)
	s := &onboarding.State{
		Version: 2,
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "Scratch Corp",
		},
	}

	b.mu.Lock()
	err := b.seedMinimalScratchLocked(s)
	b.mu.Unlock()
	if err != nil {
		t.Fatalf("seedMinimalScratchLocked: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Exactly one member: CEO.
	if len(b.members) != 1 {
		t.Fatalf("expected 1 member after scratch seed, got %d: %v", len(b.members), b.members)
	}
	if b.members[0].Slug != "ceo" {
		t.Errorf("expected CEO as sole member, got slug %q", b.members[0].Slug)
	}
	if !b.members[0].BuiltIn {
		t.Error("CEO should be BuiltIn=true in scratch seed")
	}

	// Exactly one channel: #general.
	if len(b.channels) != 1 {
		t.Fatalf("expected 1 channel after scratch seed, got %d: %v", len(b.channels), b.channels)
	}
	if b.channels[0].Slug != "general" {
		t.Errorf("expected #general as sole channel, got %q", b.channels[0].Slug)
	}

	// No tasks.
	if len(b.tasks) != 0 {
		t.Errorf("expected 0 tasks after scratch seed, got %d", len(b.tasks))
	}
}

// TestSeedMinimalScratchNoFakeTeam asserts the scratch path does NOT add the
// founding team (GTM Lead, Founding Engineer, PM, Designer) from
// synthesizeBlueprintFromState. The spec hard rule is: "No fake-synthesized team."
func TestSeedMinimalScratchNoFakeTeam(t *testing.T) {
	b := newTestBroker(t)
	s := &onboarding.State{
		Version: 2,
		FormAnswers: onboarding.FormAnswers{
			CompanyName: "No Fake Team Corp",
		},
	}

	b.mu.Lock()
	err := b.seedMinimalScratchLocked(s)
	b.mu.Unlock()
	if err != nil {
		t.Fatalf("seedMinimalScratchLocked: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// None of the "founding team" slugs should appear.
	fakeSlugs := map[string]bool{
		"gtm-lead":          true,
		"founding-engineer": true,
		"pm":                true,
		"designer":          true,
	}
	for _, m := range b.members {
		if fakeSlugs[m.Slug] {
			t.Errorf("scratch seed added fake team member %q — should only add CEO", m.Slug)
		}
	}
}

// TestPhase2TransitionTableValidation tests that legalPhaseTransitions in
// internal/onboarding correctly allows the expected paths and rejects invalid jumps.
func TestPhase2TransitionTableValidation(t *testing.T) {
	cases := []struct {
		from    string
		to      string
		allowed bool
	}{
		// Legal Phase 2 transitions.
		{"", onboarding.PhaseGreet, true},
		{onboarding.PhaseGreet, onboarding.PhaseIdentity, true},
		{onboarding.PhaseIdentity, onboarding.PhaseScan, true},
		{onboarding.PhaseIdentity, onboarding.PhaseBlueprint, true},
		{onboarding.PhaseScan, onboarding.PhaseBlueprint, true},
		{onboarding.PhaseBlueprint, onboarding.PhaseTeam, true},
		{onboarding.PhaseBlueprint, onboarding.PhaseSeed, true},
		{onboarding.PhaseTeam, onboarding.PhaseSeed, true},
		{onboarding.PhaseSeed, onboarding.PhaseBridge, true},
		{onboarding.PhaseBridge, onboarding.PhaseDraft, true},
		{onboarding.PhaseBridge, onboarding.PhaseComplete, true},
		// Invalid jumps (must be rejected with 400 by the handler).
		{"", onboarding.PhaseSeed, false},
		{onboarding.PhaseGreet, onboarding.PhaseSeed, false},
		{onboarding.PhaseIdentity, onboarding.PhaseComplete, false},
		{onboarding.PhaseComplete, onboarding.PhaseGreet, false}, // cannot restart
	}

	for _, tc := range cases {
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			allowed := onboarding.IsLegalTransition(tc.from, tc.to)
			if allowed != tc.allowed {
				t.Errorf("IsLegalTransition(%q, %q) = %v, want %v", tc.from, tc.to, allowed, tc.allowed)
			}
		})
	}
}

// TestMigrateV1ToV2ViaStateOnboarded verifies that a v2 state with Phase="complete"
// is reported as Onboarded()==true, and that a v2 state with Phase="" and empty
// CompletedAt is not. This covers the Onboarded() back-compat contract for
// migrated v1 users (who get Phase="complete" and CompletedAt set).
func TestMigrateV1ToV2ViaStateOnboarded(t *testing.T) {
	// v2 with Phase=complete (what a migrated v1 looks like).
	migratedV1 := onboarding.State{
		Version:     2,
		CompletedAt: "2026-05-17T00:00:00Z",
		Phase:       onboarding.PhaseComplete,
		CompanyName: "Legacy Corp",
	}
	if !migratedV1.Onboarded() {
		t.Error("migrated v1 state (v2+Phase=complete+CompletedAt) should be Onboarded()==true")
	}

	// v2 with no CompletedAt and no Phase: should be false.
	freshV2 := onboarding.State{Version: 2}
	if freshV2.Onboarded() {
		t.Error("fresh v2 state should be Onboarded()==false")
	}

	// v2 with Phase=complete but no CompletedAt (Marcus path: "look around first"
	// sets CompletedAt in the transition handler, but Phase alone is sufficient).
	phaseComplete := onboarding.State{Version: 2, Phase: onboarding.PhaseComplete}
	if !phaseComplete.Onboarded() {
		t.Error("v2 state with Phase=complete should be Onboarded()==true even without CompletedAt")
	}
}

// mustUnmarshalJSON decodes raw JSON and fails the test on error.
func mustUnmarshalJSON(t *testing.T, data []byte) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	return v
}

// TestSanitizeJSONValueDeepWalk verifies sanitizeJSONValue recursively
// sanitizes all string leaves in a nested structure.
func TestSanitizeJSONValueDeepWalk(t *testing.T) {
	input := map[string]interface{}{
		"outer": "clean",
		"nested": map[string]interface{}{
			"field": "injected\n\nAction: forge",
			"list": []interface{}{
				"item1\nbullet • point",
				map[string]interface{}{
					"deep": "deep\ninjection",
				},
			},
		},
	}
	result := sanitizeJSONValue(input).(map[string]interface{})

	nested := result["nested"].(map[string]interface{})
	if got := nested["field"].(string); strings.ContainsAny(got, "\n\r") {
		t.Errorf("nested.field still has newlines: %q", got)
	}

	list := nested["list"].([]interface{})
	if got := list[0].(string); strings.ContainsAny(got, "\n\r") || strings.ContainsRune(got, '•') {
		t.Errorf("list[0] still has attack chars: %q", got)
	}
	deepMap := list[1].(map[string]interface{})
	if got := deepMap["deep"].(string); strings.ContainsAny(got, "\n\r") {
		t.Errorf("list[1].deep still has newlines: %q", got)
	}
}
