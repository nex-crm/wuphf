package agent

import "testing"

func TestTemplateLookup(t *testing.T) {
	cases := []struct {
		slug string
		name string
	}{
		{"seo-agent", "SEO Analyst"},
		{"lead-gen", "Lead Generator"},
		{"enrichment", "Data Enricher"},
		{"research", "Research Analyst"},
		{"customer-success", "Customer Success"},
		{"team-lead", "Team Lead"},
		{"founding-agent", "Team Lead"},
	}

	for _, tc := range cases {
		cfg, ok := Templates[tc.slug]
		if !ok {
			t.Errorf("template %q not found", tc.slug)
			continue
		}
		if cfg.Name != tc.name {
			t.Errorf("template %q: got name %q, want %q", tc.slug, cfg.Name, tc.name)
		}
		if len(cfg.Expertise) == 0 {
			t.Errorf("template %q: expertise is empty", tc.slug)
		}
		if len(cfg.Tools) == 0 {
			t.Errorf("template %q: tools is empty", tc.slug)
		}
	}
}

func TestTemplateCount(t *testing.T) {
	if len(Templates) != 7 {
		t.Errorf("expected 7 templates, got %d", len(Templates))
	}
}
