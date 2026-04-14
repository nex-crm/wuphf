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
