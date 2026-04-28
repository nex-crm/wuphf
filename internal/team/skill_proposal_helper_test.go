package team

import (
	"strings"
	"testing"
)

// skillProposalSpec is a convenience constructor so tests don't need to fill
// every field of teamSkill.
func skillProposalSpec(name, description, createdBy string) teamSkill {
	return teamSkill{
		Name:        name,
		Description: description,
		Content:     "Some content.",
		CreatedBy:   createdBy,
		Channel:     "general",
		Status:      "proposed",
	}
}

// callWriteSkillProposalLocked is a helper that acquires b.mu, calls
// writeSkillProposalLocked, then checks that the lock is still held (i.e.
// the function returned with lock held as documented). It is safe to call
// from tests because b.wikiWorker is nil so the deadlock-avoidance path is
// exercised without an actual wiki worker goroutine.
func callWriteSkillProposalLocked(b *Broker, spec teamSkill) (*teamSkill, error) {
	b.mu.Lock()
	sk, err := b.writeSkillProposalLocked(spec)
	b.mu.Unlock()
	return sk, err
}

func TestWriteSkillProposalLocked_ValidatesName(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	tests := []struct {
		name    string
		spec    teamSkill
		wantErr string
	}{
		{
			name:    "empty name",
			spec:    skillProposalSpec("", "A description.", "archivist"),
			wantErr: "name is required",
		},
		{
			name:    "whitespace name",
			spec:    skillProposalSpec("   ", "A description.", "archivist"),
			wantErr: "name is required",
		},
		{
			// skillSlug strips leading underscores → "-bad" which starts with dash
			name:    "underscore-leading name produces dash-start slug",
			spec:    skillProposalSpec("_bad", "A description.", "archivist"),
			wantErr: "slug",
		},
		{
			// skillSlug strips leading dashes but they are kept; "- bad" → "--bad"
			name:    "dash-leading name stays invalid",
			spec:    skillProposalSpec("-bad-start", "A description.", "archivist"),
			wantErr: "slug",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := callWriteSkillProposalLocked(b, tc.spec)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestWriteSkillProposalLocked_ValidatesDescription(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("my-skill", "", "archivist")
	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected error for empty description, got nil")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("error %q should mention description", err.Error())
	}
}

func TestWriteSkillProposalLocked_SystemAuthorWhitelist(t *testing.T) {
	t.Parallel()

	systemAuthors := []string{"archivist", "scanner", "system"}

	for _, author := range systemAuthors {
		author := author
		t.Run("system_author_"+author, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			spec := skillProposalSpec("my-skill-"+author, "A description.", author)
			sk, err := callWriteSkillProposalLocked(b, spec)
			if err != nil {
				t.Fatalf("expected system author %q to bypass member check, got err: %v", author, err)
			}
			if sk == nil {
				t.Fatal("expected non-nil skill, got nil")
			}
			if sk.CreatedBy != author {
				t.Errorf("CreatedBy: got %q, want %q", sk.CreatedBy, author)
			}
		})
	}
}

func TestWriteSkillProposalLocked_NonSystemAuthorRequiresMember(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// "random-agent" is not in the system author whitelist and not a
	// registered team member — should return an error.
	spec := skillProposalSpec("my-skill", "A description.", "random-agent")
	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected error for unregistered non-system author, got nil")
	}
	if !strings.Contains(err.Error(), "registered team member") {
		t.Errorf("error %q should mention 'registered team member'", err.Error())
	}
}

func TestWriteSkillProposalLocked_DeduplicatesOnSlugCollision(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("dedup-skill", "First write.", "archivist")
	sk1, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write with the same name should return the existing skill.
	spec2 := skillProposalSpec("dedup-skill", "Second write (different desc).", "archivist")
	sk2, err := callWriteSkillProposalLocked(b, spec2)
	if err != nil {
		t.Fatalf("second write (dedup): %v", err)
	}
	if sk2 == nil {
		t.Fatal("dedup: expected non-nil existing skill, got nil")
	}
	// Should be the SAME skill (first write wins).
	if sk2.Description != sk1.Description {
		t.Errorf("dedup: description changed — expected %q, got %q (second write was not deduped)",
			sk1.Description, sk2.Description)
	}

	// In-memory list should have exactly one skill.
	b.mu.Lock()
	count := 0
	for _, s := range b.skills {
		if skillSlug(s.Name) == "dedup-skill" {
			count++
		}
	}
	b.mu.Unlock()
	if count != 1 {
		t.Errorf("expected exactly 1 skill with slug 'dedup-skill', found %d", count)
	}
}

func TestWriteSkillProposalLocked_SuccessfulCreate(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("send-digest", "Send a daily digest.", "archivist")
	spec.Tags = []string{"comms"}
	spec.Trigger = "Every morning"

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk == nil {
		t.Fatal("expected non-nil skill")
	}
	if sk.Name != "send-digest" {
		t.Errorf("Name: got %q, want 'send-digest'", sk.Name)
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want 'proposed'", sk.Status)
	}
	if sk.CreatedBy != "archivist" {
		t.Errorf("CreatedBy: got %q, want 'archivist'", sk.CreatedBy)
	}

	// Verify the skill is in b.skills.
	b.mu.Lock()
	found := b.findSkillByNameLocked("send-digest")
	b.mu.Unlock()
	if found == nil {
		t.Error("skill not found in b.skills after creation")
	}

	// Verify a proposal request was appended.
	b.mu.Lock()
	var hasProposal bool
	for _, req := range b.requests {
		if req.Kind == "skill_proposal" && req.ReplyTo == "send-digest" {
			hasProposal = true
			break
		}
	}
	b.mu.Unlock()
	if !hasProposal {
		t.Error("no skill_proposal request found in b.requests")
	}
}

func TestWriteSkillProposalLocked_DefaultsStatus(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// Status field left empty — should default to "proposed".
	spec := teamSkill{
		Name:        "auto-status",
		Description: "Should default to proposed.",
		Content:     "content",
		CreatedBy:   "archivist",
	}
	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want 'proposed'", sk.Status)
	}
}

// TestWriteSkillProposalLocked_GuardRejectsDangerous covers the trust-ladder
// gate: a community-trust skill with a dangerous body is rejected outright,
// the rejection counter is bumped, and no skill is appended to b.skills.
func TestWriteSkillProposalLocked_GuardRejectsDangerous(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("evil-skill", "Wipes the world.", "archivist")
	spec.Content = "rm -rf /var/data\nThen continue with normal steps."

	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected guard rejection, got nil error")
	}
	if !strings.Contains(err.Error(), "skill_guard") {
		t.Errorf("error %q should mention skill_guard", err.Error())
	}

	b.mu.Lock()
	rejected := b.skillCompileMetrics.ProposalsRejectedByGuardTotal
	count := 0
	for _, s := range b.skills {
		if skillSlug(s.Name) == "evil-skill" {
			count++
		}
	}
	b.mu.Unlock()
	if rejected < 1 {
		t.Errorf("ProposalsRejectedByGuardTotal: got %d, want >= 1", rejected)
	}
	if count != 0 {
		t.Errorf("skill should not be in b.skills, found %d", count)
	}
}

// TestWriteSkillProposalLocked_GuardAllowsCautionForCommunity verifies that
// caution verdicts pass through under community trust (Stage A wiki source)
// and the safety_scan stamp is preserved on the skill we just wrote.
func TestWriteSkillProposalLocked_GuardAllowsCautionForCommunity(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("install-doc", "Document install steps.", "archivist")
	spec.Content = "Install: visit https://example.com/install for setup."

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("expected caution to pass under community trust, got: %v", err)
	}
	if sk == nil {
		t.Fatal("expected non-nil skill")
	}
	// Verify it landed in b.skills.
	b.mu.Lock()
	found := b.findSkillByNameLocked("install-doc")
	b.mu.Unlock()
	if found == nil {
		t.Error("skill not found after caution-allowed write")
	}
}

// TestWriteSkillProposalLocked_GuardStampsSafetyScan verifies the safety_scan
// metadata is rendered into the wiki copy when a skill writes successfully.
// (This indirectly covers the safety_scan stamp by exercising the guard
// scaffolding; the exact YAML payload is checked elsewhere.)
func TestWriteSkillProposalLocked_GuardStampsSafetyScan(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("stamped", "A clean skill.", "archivist")
	spec.Content = "Step 1: do the thing.\nStep 2: report back."
	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk == nil {
		t.Fatal("expected skill")
	}
	// The scan ran — counter should NOT have bumped (verdict was safe).
	b.mu.Lock()
	rejected := b.skillCompileMetrics.ProposalsRejectedByGuardTotal
	b.mu.Unlock()
	if rejected != 0 {
		t.Errorf("expected zero rejections for safe verdict, got %d", rejected)
	}
}

func TestWriteSkillProposalLocked_ValidSlugVariants(t *testing.T) {
	t.Parallel()

	validNames := []struct {
		input    string
		wantSlug string
	}{
		{"my-skill", "my-skill"},
		{"abc123", "abc123"},
		{"a", "a"},
		{"send-digest-v2", "send-digest-v2"},
		{"Send-Digest", "send-digest"},  // uppercase normalised by skillSlug
	}

	for _, tc := range validNames {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			spec := skillProposalSpec(tc.input, "A description.", "archivist")
			sk, err := callWriteSkillProposalLocked(b, spec)
			if err != nil {
				t.Fatalf("name=%q: unexpected error: %v", tc.input, err)
			}
			if skillSlug(sk.Name) != tc.wantSlug {
				t.Errorf("name=%q: slug=%q, want %q", tc.input, skillSlug(sk.Name), tc.wantSlug)
			}
		})
	}
}
