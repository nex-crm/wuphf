package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetectSkillClusters(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect", Status: "active"},
		{Name: "enrich-prospect-inbox", Description: "Full enrichment loop for an inbound prospect from inbox", Status: "active"},
		{Name: "bad-data-self-heal", Description: "Diagnose and resolve tasks blocked by unresolvable prospect data", Status: "active"},
	})

	b.mu.Lock()
	clusters := b.detectSkillClustersLocked()
	b.mu.Unlock()

	if len(clusters) == 0 {
		t.Fatal("expected at least one cluster for similar enrichment skills")
	}

	// The enrichment cluster should contain both enrich-prospect variants.
	found := false
	for _, c := range clusters {
		if c.Representative == "enrich-prospect" || c.Representative == "enrich-prospect-inbox" {
			found = true
			if len(c.Members) == 0 {
				t.Error("expected cluster to have members")
			}
		}
	}
	if !found {
		t.Error("expected a cluster containing enrichment skills")
	}
}

func TestDetectSkillClusters_NoClusterForUnrelated(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "bad-data-self-heal", Description: "Diagnose and resolve tasks blocked by unresolvable prospect data", Status: "active"},
		{Name: "ceo-session-resume", Description: "Restore office context at the start of each CEO session", Status: "active"},
	})

	b.mu.Lock()
	clusters := b.detectSkillClustersLocked()
	b.mu.Unlock()

	if len(clusters) != 0 {
		t.Errorf("expected no clusters for unrelated skills, got %d", len(clusters))
	}
}

func TestMergeSkillCluster(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect", Status: "active", Tags: []string{"enrichment"}},
		{Name: "enrich-prospect-inbox", Description: "Full enrichment loop from inbox", Status: "active", Tags: []string{"inbox"}},
	})

	b.mu.Lock()
	clusters := b.detectSkillClustersLocked()
	merged, archived, err := b.mergeSkillClusterLocked("enrich-prospect", clusters)
	b.mu.Unlock()

	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if merged != 1 {
		t.Errorf("expected 1 merged, got %d", merged)
	}
	if archived != 1 {
		t.Errorf("expected 1 archived, got %d", archived)
	}

	// Verify the target has RelatedSkills populated.
	b.mu.Lock()
	target := b.findSkillByNameLocked("enrich-prospect")
	b.mu.Unlock()

	if target == nil {
		t.Fatal("target skill not found after merge")
	}
	if len(target.RelatedSkills) == 0 {
		t.Error("expected RelatedSkills to be populated on target")
	}

	// Verify the merged skill is archived.
	b.mu.Lock()
	member := b.findSkillByNameIncludingArchivedLocked("enrich-prospect-inbox")
	b.mu.Unlock()

	if member == nil {
		t.Fatal("merged skill not found")
	}
	if member.Status != "archived" {
		t.Errorf("expected merged skill to be archived, got %q", member.Status)
	}

	// Verify tags were merged.
	hasInbox := false
	for _, tag := range target.Tags {
		if tag == "inbox" {
			hasInbox = true
		}
	}
	if !hasInbox {
		t.Error("expected target to inherit 'inbox' tag from merged skill")
	}
}

func TestMergeSkillCluster_TargetNotFound(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run", Status: "active"},
	})

	b.mu.Lock()
	clusters := b.detectSkillClustersLocked()
	_, _, err := b.mergeSkillClusterLocked("nonexistent", clusters)
	b.mu.Unlock()

	if err == nil {
		t.Error("expected error for nonexistent target")
	}
}

func TestHandleSkillConsolidate_DryRun(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect", Status: "active"},
		{Name: "enrich-prospect-inbox", Description: "Full enrichment loop from inbox for prospect", Status: "active"},
	})

	body := `{"dry_run": true}`
	req := httptest.NewRequest(http.MethodPost, "/skills/consolidate", strings.NewReader(body))
	w := httptest.NewRecorder()

	b.handleSkillConsolidate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp consolidateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Dry run should not archive anything.
	if resp.Merged != 0 || resp.Archived != 0 {
		t.Errorf("expected 0 merged/archived in dry run, got merged=%d archived=%d",
			resp.Merged, resp.Archived)
	}

	// But should detect clusters.
	if len(resp.Clusters) == 0 {
		t.Error("expected at least one cluster in dry run response")
	}
}

func TestMergeSkillContent(t *testing.T) {
	target := "Step 1: Do something\nStep 2: Do something else"
	member := "Step A: SaaS-specific workflow\nStep B: Include ARR metrics"

	merged := mergeSkillContent(target, member, "saas-pitch-deck")

	if !strings.Contains(merged, "Step 1: Do something") {
		t.Error("expected merged content to preserve target content")
	}
	if !strings.Contains(merged, "## Consolidated from saas-pitch-deck") {
		t.Error("expected consolidated section header")
	}
	if !strings.Contains(merged, "Step A: SaaS-specific workflow") {
		t.Error("expected merged content to include member content")
	}
}

func TestMergeSkillContent_IdenticalContent(t *testing.T) {
	content := "Same content"
	merged := mergeSkillContent(content, content, "dup-skill")

	if strings.Contains(merged, "Consolidated from") {
		t.Error("expected no consolidated section for identical content")
	}
}

func TestMergeSkillContent_EmptyMember(t *testing.T) {
	target := "Target content"
	merged := mergeSkillContent(target, "", "empty-skill")

	if merged != target {
		t.Errorf("expected unchanged target, got %q", merged)
	}
}

func TestMergeSkillCluster_MergesContent(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run", Content: "Generic enrichment steps", Status: "active"},
		{Name: "enrich-prospect-inbox", Description: "Full enrichment from inbox", Content: "Inbox-specific enrichment steps", Status: "active"},
	})

	b.mu.Lock()
	clusters := b.detectSkillClustersLocked()
	merged, _, err := b.mergeSkillClusterLocked("enrich-prospect", clusters)
	b.mu.Unlock()

	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if merged != 1 {
		t.Fatalf("expected 1 merged, got %d", merged)
	}

	b.mu.Lock()
	target := b.findSkillByNameLocked("enrich-prospect")
	b.mu.Unlock()

	if !strings.Contains(target.Content, "Inbox-specific enrichment steps") {
		t.Error("expected target content to include merged member content")
	}
	if !strings.Contains(target.Content, "Consolidated from") {
		t.Error("expected consolidated section header in target content")
	}
}

func TestAutoConsolidateSkillsIfNeeded(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect", Content: "Generic enrichment steps", Status: "active"},
		{Name: "enrich-prospect-inbox", Description: "Full enrichment loop from inbox for prospect", Content: "Inbox-specific steps", Status: "active"},
		{Name: "bad-data-self-heal", Description: "Diagnose and resolve unresolvable prospect data", Content: "Self-heal steps", Status: "active"},
	})

	b.mu.Lock()
	b.autoConsolidateSkillsIfNeeded()
	b.mu.Unlock()

	// Verify: the enrichment cluster was merged.
	b.mu.Lock()
	enrichTarget := b.findSkillByNameLocked("enrich-prospect")
	inboxSkill := b.findSkillByNameLocked("enrich-prospect-inbox")
	selfHeal := b.findSkillByNameLocked("bad-data-self-heal")
	b.mu.Unlock()

	if enrichTarget == nil {
		t.Fatal("expected enrich-prospect to still exist")
	}
	if strings.Contains(enrichTarget.Content, "Consolidated from") == false {
		// The inbox skill content should have been merged in.
		// Only check if there was actually a cluster detected.
		b.mu.Lock()
		inboxArchived := b.findSkillByNameIncludingArchivedLocked("enrich-prospect-inbox")
		b.mu.Unlock()
		if inboxArchived != nil && inboxArchived.Status == "archived" {
			t.Error("expected enrich-prospect content to include consolidated section")
		}
	}

	// The inbox skill should be archived (if cluster was detected).
	if inboxSkill != nil {
		t.Error("expected enrich-prospect-inbox to be archived (not visible in non-archived lookup)")
	}

	// Unrelated skill should be untouched.
	if selfHeal == nil {
		t.Fatal("expected bad-data-self-heal to still exist")
	}
	if selfHeal.Status != "active" {
		t.Errorf("expected bad-data-self-heal to remain active, got %q", selfHeal.Status)
	}
}

func TestAutoConsolidateSkillsIfNeeded_IdempotentWithNoOverlap(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "bad-data-self-heal", Description: "Diagnose unresolvable data", Content: "Steps", Status: "active"},
		{Name: "ceo-session-resume", Description: "Restore office context", Content: "Steps", Status: "active"},
	})

	b.mu.Lock()
	b.autoConsolidateSkillsIfNeeded()
	b.mu.Unlock()

	// Both skills should remain active and untouched.
	b.mu.Lock()
	s1 := b.findSkillByNameLocked("bad-data-self-heal")
	s2 := b.findSkillByNameLocked("ceo-session-resume")
	b.mu.Unlock()

	if s1 == nil || s1.Status != "active" {
		t.Error("expected bad-data-self-heal to remain active")
	}
	if s2 == nil || s2.Status != "active" {
		t.Error("expected ceo-session-resume to remain active")
	}
}
