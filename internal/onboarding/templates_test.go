package onboarding

import "testing"

func TestDefaultTemplatesReturnsFiveItems(t *testing.T) {
	templates := DefaultTemplates()
	if len(templates) != 5 {
		t.Fatalf("DefaultTemplates: got %d items, want 5", len(templates))
	}
}

func TestDefaultTemplatesNonEmptyFields(t *testing.T) {
	for _, tmpl := range DefaultTemplates() {
		if tmpl.ID == "" {
			t.Errorf("template %+v: ID must not be empty", tmpl)
		}
		if tmpl.Title == "" {
			t.Errorf("template %q: Title must not be empty", tmpl.ID)
		}
		if tmpl.OwnerSlug == "" {
			t.Errorf("template %q: OwnerSlug must not be empty", tmpl.ID)
		}
		if tmpl.Description == "" {
			t.Errorf("template %q: Description must not be empty", tmpl.ID)
		}
	}
}

func TestDefaultTemplatesExpectedIDs(t *testing.T) {
	wantIDs := []string{"landing", "repo", "spec", "readme", "audit"}
	templates := DefaultTemplates()
	for i, want := range wantIDs {
		if templates[i].ID != want {
			t.Errorf("templates[%d].ID: got %q, want %q", i, templates[i].ID, want)
		}
	}
}

func TestDefaultTemplatesOwnerSlugs(t *testing.T) {
	// Verify the expected owner distribution: eng×2, pm×2, ceo×1.
	counts := map[string]int{}
	for _, tmpl := range DefaultTemplates() {
		counts[tmpl.OwnerSlug]++
	}
	if counts["eng"] != 2 {
		t.Errorf("expected 2 eng templates, got %d", counts["eng"])
	}
	if counts["pm"] != 2 {
		t.Errorf("expected 2 pm templates, got %d", counts["pm"])
	}
	if counts["ceo"] != 1 {
		t.Errorf("expected 1 ceo template, got %d", counts["ceo"])
	}
}

func TestDefaultTemplatesUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, tmpl := range DefaultTemplates() {
		if seen[tmpl.ID] {
			t.Errorf("duplicate template ID: %q", tmpl.ID)
		}
		seen[tmpl.ID] = true
	}
}

func TestRevOpsTemplatesReturnsFiveItems(t *testing.T) {
	templates := RevOpsTemplates()
	if len(templates) != 5 {
		t.Fatalf("RevOpsTemplates: got %d items, want 5", len(templates))
	}
}

func TestRevOpsTemplatesExpectedIDs(t *testing.T) {
	wantIDs := []string{"pipeline_audit", "meeting_prep", "revive_closed_lost", "score_inbound", "stalled_deals"}
	templates := RevOpsTemplates()
	for i, want := range wantIDs {
		if templates[i].ID != want {
			t.Errorf("revops templates[%d].ID: got %q, want %q", i, templates[i].ID, want)
		}
	}
}

func TestRevOpsTemplatesNonEmptyFields(t *testing.T) {
	for _, tmpl := range RevOpsTemplates() {
		if tmpl.ID == "" || tmpl.Title == "" || tmpl.Description == "" || tmpl.OwnerSlug == "" {
			t.Errorf("revops template %+v: all fields must be non-empty", tmpl)
		}
	}
}

func TestRevOpsTemplatesOwnerSlugsMatchPack(t *testing.T) {
	// Every owner slug should be one of the slugs present in the RevOps pack.
	validSlugs := map[string]bool{"ceo": true, "ops-lead": true, "ae": true, "sdr": true, "analyst": true}
	for _, tmpl := range RevOpsTemplates() {
		if !validSlugs[tmpl.OwnerSlug] {
			t.Errorf("revops template %q: OwnerSlug %q not in RevOps pack", tmpl.ID, tmpl.OwnerSlug)
		}
	}
}

func TestTemplatesForPackRouting(t *testing.T) {
	cases := []struct {
		slug    string
		firstID string
	}{
		{"", "landing"},                    // fallback to default
		{"founding-team", "landing"},       // explicit default
		{"revops", "pipeline_audit"},       // revops-specific
		{"unknown-pack", "landing"},        // unknown falls through to default
	}
	for _, tc := range cases {
		got := TemplatesForPack(tc.slug)
		if len(got) == 0 || got[0].ID != tc.firstID {
			t.Errorf("TemplatesForPack(%q): first ID got %q, want %q", tc.slug, got[0].ID, tc.firstID)
		}
	}
}
