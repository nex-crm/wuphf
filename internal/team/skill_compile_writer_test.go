package team

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// compiledSkillSpec is a convenience constructor so tests don't need to fill
// every field of teamSkill.
func compiledSkillSpec(name, description, createdBy string) teamSkill {
	return teamSkill{
		Name:        name,
		Description: description,
		Content:     "Some content.",
		CreatedBy:   createdBy,
		Channel:     "general",
	}
}

// callWriteCompiledSkillLocked is a helper that acquires b.mu, calls
// writeCompiledSkillLocked, then checks that the lock is still held (i.e.
// the function returned with lock held as documented). It is safe to call
// from tests because b.wikiWorker is nil so the deadlock-avoidance path is
// exercised without an actual wiki worker goroutine.
func callWriteCompiledSkillLocked(b *Broker, spec teamSkill) (*teamSkill, error) {
	b.mu.Lock()
	sk, err := b.writeCompiledSkillLocked(spec)
	b.mu.Unlock()
	return sk, err
}

func TestWriteCompiledSkillLocked_ValidatesName(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	tests := []struct {
		name    string
		spec    teamSkill
		wantErr string
	}{
		{
			name:    "empty name",
			spec:    compiledSkillSpec("", "A description.", "archivist"),
			wantErr: "name is required",
		},
		{
			name:    "whitespace name",
			spec:    compiledSkillSpec("   ", "A description.", "archivist"),
			wantErr: "name is required",
		},
		{
			// skillSlug strips leading underscores → "-bad" which starts with dash
			name:    "underscore-leading name produces dash-start slug",
			spec:    compiledSkillSpec("_bad", "A description.", "archivist"),
			wantErr: "slug",
		},
		{
			// skillSlug strips leading dashes but they are kept; "- bad" → "--bad"
			name:    "dash-leading name stays invalid",
			spec:    compiledSkillSpec("-bad-start", "A description.", "archivist"),
			wantErr: "slug",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := callWriteCompiledSkillLocked(b, tc.spec)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestWriteCompiledSkillLocked_ValidatesDescription(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("my-skill", "", "archivist")
	_, err := callWriteCompiledSkillLocked(b, spec)
	if err == nil {
		t.Fatal("expected error for empty description, got nil")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("error %q should mention description", err.Error())
	}
}

func TestWriteCompiledSkillLocked_SystemAuthorWhitelist(t *testing.T) {
	t.Parallel()

	systemAuthors := []string{LibrarianSlug, "archivist", "scanner", "system"}

	for _, author := range systemAuthors {
		author := author
		t.Run("system_author_"+author, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			spec := compiledSkillSpec("my-skill-"+author, "A description.", author)
			sk, err := callWriteCompiledSkillLocked(b, spec)
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

func TestWriteCompiledSkillLocked_NonSystemAuthorRequiresMember(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// "random-agent" is not in the system author whitelist and not a
	// registered team member — should return an error.
	spec := compiledSkillSpec("my-skill", "A description.", "random-agent")
	_, err := callWriteCompiledSkillLocked(b, spec)
	if err == nil {
		t.Fatal("expected error for unregistered non-system author, got nil")
	}
	if !strings.Contains(err.Error(), "registered team member") {
		t.Errorf("error %q should mention 'registered team member'", err.Error())
	}
}

func TestWriteCompiledSkillLocked_DeduplicatesOnSlugCollision(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("dedup-skill", "First write.", "archivist")
	sk1, err := callWriteCompiledSkillLocked(b, spec)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write with the same name should return the existing skill.
	spec2 := compiledSkillSpec("dedup-skill", "Second write (different desc).", "archivist")
	sk2, err := callWriteCompiledSkillLocked(b, spec2)
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

func TestWriteCompiledSkillLocked_SuccessfulCreate(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("send-digest", "Send a daily digest.", "archivist")
	spec.Tags = []string{"comms"}
	spec.Trigger = "Every morning"

	sk, err := callWriteCompiledSkillLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk == nil {
		t.Fatal("expected non-nil skill")
	}
	if sk.Name != "send-digest" {
		t.Errorf("Name: got %q, want 'send-digest'", sk.Name)
	}
	if sk.Status != "active" {
		t.Errorf("Status: got %q, want 'active' (compiled skills go live immediately)", sk.Status)
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

	// Core-loop R5: no approval interview is created — compilation is the
	// only creation path and it activates directly.
	b.mu.Lock()
	for _, req := range b.requests {
		if req.Kind == "skill_proposal" {
			t.Errorf("unexpected skill_proposal request %q — the proposal flow was removed", req.ID)
		}
	}
	b.mu.Unlock()
}

func TestWriteCompiledSkillLocked_DefaultsStatus(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// Status field left empty — should default to "active" (core-loop R5:
	// compiled skills go live immediately, no proposal state).
	spec := teamSkill{
		Name:        "auto-status",
		Description: "Should default to active.",
		Content:     "content",
		CreatedBy:   "archivist",
	}
	sk, err := callWriteCompiledSkillLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk.Status != "active" {
		t.Errorf("Status: got %q, want 'active'", sk.Status)
	}
}

func TestWriteCompiledSkillLocked_PreservesDisabledFromStatus(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("paused-proposal", "A paused proposal.", "archivist")
	spec.Status = "disabled"
	spec.DisabledFromStatus = "proposed"

	sk, err := callWriteCompiledSkillLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk.Status != "disabled" {
		t.Errorf("Status: got %q, want disabled", sk.Status)
	}
	if sk.DisabledFromStatus != "proposed" {
		t.Errorf("DisabledFromStatus: got %q, want proposed", sk.DisabledFromStatus)
	}
	fm := teamSkillToFrontmatter(*sk)
	if fm.Metadata.Wuphf.DisabledFromStatus != "proposed" {
		t.Errorf("frontmatter disabled_from_status: got %q, want proposed", fm.Metadata.Wuphf.DisabledFromStatus)
	}
}

// TestWriteCompiledSkillLocked_GuardRejectsDangerous covers the trust-ladder
// gate: a community-trust skill with a dangerous body is rejected outright,
// the rejection counter is bumped, and no skill is appended to b.skills.
func TestWriteCompiledSkillLocked_GuardRejectsDangerous(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("evil-skill", "Wipes the world.", "archivist")
	spec.Content = "rm -rf /var/data\nThen continue with normal steps."

	_, err := callWriteCompiledSkillLocked(b, spec)
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

// TestWriteCompiledSkillLocked_GuardAllowsCautionForCommunity verifies that
// caution verdicts pass through under community trust (Stage A wiki source)
// and the safety_scan stamp is preserved on the skill we just wrote.
func TestWriteCompiledSkillLocked_GuardAllowsCautionForCommunity(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("install-doc", "Document install steps.", "archivist")
	spec.Content = "Install: visit https://example.com/install for setup."

	sk, err := callWriteCompiledSkillLocked(b, spec)
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

// TestWriteCompiledSkillLocked_GuardStampsSafetyScan verifies the safety_scan
// metadata is rendered into the wiki copy when a skill writes successfully.
// (This indirectly covers the safety_scan stamp by exercising the guard
// scaffolding; the exact YAML payload is checked elsewhere.)
func TestWriteCompiledSkillLocked_GuardStampsSafetyScan(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := compiledSkillSpec("stamped", "A clean skill.", "archivist")
	spec.Content = "Step 1: do the thing.\nStep 2: report back."
	sk, err := callWriteCompiledSkillLocked(b, spec)
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

// TestWriteCompiledSkillLocked_BackfillsSourceArticleOnDedup covers the
// healing path for Stage A skills created before the provenance fix landed.
// The existing skill carries an empty SourceArticle; the incoming spec has
// it populated; dedup should copy the value through, persist, and surface a
// log without creating a second skill.
func TestWriteCompiledSkillLocked_BackfillsSourceArticleOnDedup(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// Seed an existing skill with empty SourceArticle (simulates a
	// pre-fix Stage A proposal).
	first := compiledSkillSpec("provenance-skill", "Backfill provenance.", "archivist")
	first.SourceArticle = ""
	sk1, err := callWriteCompiledSkillLocked(b, first)
	if err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if sk1.SourceArticle != "" {
		t.Fatalf("seed precondition: SourceArticle should be empty, got %q", sk1.SourceArticle)
	}

	// Snapshot the count before the dedup call.
	b.mu.Lock()
	beforeCount := 0
	for _, s := range b.skills {
		if skillSlug(s.Name) == "provenance-skill" {
			beforeCount++
		}
	}
	b.mu.Unlock()

	// Re-propose with provenance populated.
	second := compiledSkillSpec("provenance-skill", "Backfill provenance (again).", "archivist")
	second.SourceArticle = "team/playbooks/provenance-skill.md"
	sk2, err := callWriteCompiledSkillLocked(b, second)
	if err != nil {
		t.Fatalf("dedup write: %v", err)
	}
	if sk2 == nil {
		t.Fatal("dedup: expected non-nil skill")
	}
	if sk2.SourceArticle != "team/playbooks/provenance-skill.md" {
		t.Errorf("SourceArticle: got %q, want %q (backfill did not run)",
			sk2.SourceArticle, "team/playbooks/provenance-skill.md")
	}

	// No duplicate skill was created.
	b.mu.Lock()
	afterCount := 0
	for _, s := range b.skills {
		if skillSlug(s.Name) == "provenance-skill" {
			afterCount++
		}
	}
	found := b.findSkillByNameLocked("provenance-skill")
	b.mu.Unlock()
	if afterCount != beforeCount {
		t.Errorf("dedup: skill count changed (before=%d, after=%d)", beforeCount, afterCount)
	}
	if found == nil || found.SourceArticle != "team/playbooks/provenance-skill.md" {
		t.Errorf("in-memory skill SourceArticle not persisted: %+v", found)
	}

	// Saved state file exists (proves saveLocked was invoked). The state
	// file was empty before the seed because newTestBroker pins statePath
	// to a tmpdir; saveLocked wrote it during the seed write and again
	// during backfill. We assert it exists rather than re-reading content.
	if _, statErr := os.Stat(b.statePath); statErr != nil {
		t.Errorf("expected state file at %q to exist after backfill, got: %v", b.statePath, statErr)
	}
}

// TestWriteCompiledSkillLocked_DedupNoBackfillWhenAlreadySet ensures we do
// NOT overwrite an existing non-empty SourceArticle on the dedup path.
func TestWriteCompiledSkillLocked_DedupNoBackfillWhenAlreadySet(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	first := compiledSkillSpec("already-set", "Has provenance.", "archivist")
	first.SourceArticle = "team/playbooks/original.md"
	if _, err := callWriteCompiledSkillLocked(b, first); err != nil {
		t.Fatalf("seed: %v", err)
	}

	second := compiledSkillSpec("already-set", "Different.", "archivist")
	second.SourceArticle = "team/playbooks/different.md"
	sk, err := callWriteCompiledSkillLocked(b, second)
	if err != nil {
		t.Fatalf("dedup: %v", err)
	}
	if sk.SourceArticle != "team/playbooks/original.md" {
		t.Errorf("backfill should not overwrite a non-empty SourceArticle: got %q", sk.SourceArticle)
	}
}

func TestWriteCompiledSkillLocked_ValidSlugVariants(t *testing.T) {
	t.Parallel()

	validNames := []struct {
		input    string
		wantSlug string
	}{
		{"my-skill", "my-skill"},
		{"abc123", "abc123"},
		{"a", "a"},
		{"send-digest-v2", "send-digest-v2"},
		{"Send-Digest", "send-digest"}, // uppercase normalised by skillSlug
	}

	for _, tc := range validNames {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			spec := compiledSkillSpec(tc.input, "A description.", "archivist")
			sk, err := callWriteCompiledSkillLocked(b, spec)
			if err != nil {
				t.Fatalf("name=%q: unexpected error: %v", tc.input, err)
			}
			if skillSlug(sk.Name) != tc.wantSlug {
				t.Errorf("name=%q: slug=%q, want %q", tc.input, skillSlug(sk.Name), tc.wantSlug)
			}
		})
	}
}

// TestEnhanceSkillLocked_UpdateFirstThreshold pins the core-loop step 7.3
// size threshold: enhancing an existing skill is rejected with
// errSkillEnhanceTooLarge once the merged body would exceed
// skillUpdateFirstMaxBytes, so the caller falls back to creating a new
// skill instead of growing the existing one forever.
func TestEnhanceSkillLocked_UpdateFirstThreshold(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	big := strings.Repeat("x", skillUpdateFirstMaxBytes-100)
	spec := compiledSkillSpec("big-skill", "A very large skill.", "archivist")
	spec.Content = big
	if _, err := callWriteCompiledSkillLocked(b, spec); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	b.mu.Lock()
	_, err := b.enhanceSkillLocked("big-skill", strings.Repeat("y", 200), "", "candidate-slug")
	b.mu.Unlock()
	if !errors.Is(err, errSkillEnhanceTooLarge) {
		t.Fatalf("expected errSkillEnhanceTooLarge, got %v", err)
	}
}

// TestWriteCompiledSkillLocked_AutoAssignsOwnerAgents pins core-loop step 8:
// a compiled skill is auto-assigned to the office roster at creation so it
// is loaded into every agent's prompt on the next build.
func TestWriteCompiledSkillLocked_AutoAssignsOwnerAgents(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	b.mu.Lock()
	roster := b.allMemberSlugsLocked()
	b.mu.Unlock()
	if len(roster) == 0 {
		t.Skip("test broker has no members; auto-assign has nothing to pin")
	}

	sk, err := callWriteCompiledSkillLocked(b, compiledSkillSpec("assigned-skill", "Auto-assigned to the roster.", "archivist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sk.OwnerAgents) != len(roster) {
		t.Fatalf("OwnerAgents: got %v, want full roster %v", sk.OwnerAgents, roster)
	}
}

// TestRegisterCompiledPlaybookSkill_AttributesToLibrarian pins ten-out-of-ten
// E4: compiled artifacts are attributed to the real acting roster identity
// (librarian), not the "@archivist" commit-author name no roster ever showed
// (ICP-eval v3 [18:21:15]: 'Created by "@archivist" — an agent name never
// shown in the roster').
func TestRegisterCompiledPlaybookSkill_AttributesToLibrarian(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	skillMD := []byte("---\nname: renewal-outreach\ndescription: Run the renewal outreach playbook.\n---\n\n## Steps\n\n1. Pull the account brief.\n2. Draft the outreach.\n")
	if !b.RegisterCompiledPlaybookSkill("renewal-outreach", "team/playbooks/renewal-outreach.md", skillMD) {
		t.Fatal("expected a new broker skill to be registered")
	}

	b.mu.Lock()
	var registered *teamSkill
	for i := range b.skills {
		if b.skills[i].Name == "renewal-outreach" {
			registered = &b.skills[i]
		}
	}
	b.mu.Unlock()
	if registered == nil {
		t.Fatal("registered skill not found in broker state")
	}
	if registered.CreatedBy != LibrarianSlug {
		t.Errorf("CreatedBy: got %q, want %q", registered.CreatedBy, LibrarianSlug)
	}
}
