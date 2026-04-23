package team

import (
	"testing"
)

// TestClassifyQuery covers every QueryClass with at least four cases each.
// Table-driven, parallel per subtest.
func TestClassifyQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		query       string
		wantClass   QueryClass
		wantMinConf float64 // confidence must be >= this
	}{
		// ── status ────────────────────────────────────────────────────────────
		{
			name:        "status_what_does_sarah_do",
			query:       "What does Sarah do?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.7,
		},
		{
			name:        "status_what_is_role",
			query:       "What is Sarah's role at Acme?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.7,
		},
		{
			name:        "status_named_entity_present",
			query:       "Tell me about Acme Corp's Q2 pipeline",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.4,
		},
		{
			name:        "status_wikilink_explicit",
			query:       "What do we know about [[people/sarah-jones]]?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.9,
		},
		{
			name:        "status_job_title",
			query:       "What is the job title of David?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.7,
		},

		// ── relationship ──────────────────────────────────────────────────────
		{
			name:        "relationship_who_reports_to",
			query:       "Who reports to Sarah?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			name:        "relationship_who_works_for",
			query:       "Who works for Acme?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			name:        "relationship_who_manages",
			query:       "Who manages the enterprise accounts?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			name:        "relationship_who_leads",
			query:       "Who leads the platform team?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			name:        "relationship_who_runs",
			query:       "Who runs sales at Acme Corp?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},

		// ── multi_hop ─────────────────────────────────────────────────────────
		{
			name:        "multi_hop_who_at_acme",
			query:       "who at acme champions the Q2 pilot?",
			wantClass:   QueryClassMultiHop,
			wantMinConf: 0.8,
		},
		{
			name:        "multi_hop_who_at_entity_capitalized",
			query:       "Who at Globex drives enterprise sales?",
			wantClass:   QueryClassMultiHop,
			wantMinConf: 0.8,
		},
		{
			name:        "multi_hop_entity_at_entity",
			query:       "Which deal did Sarah Jones at Acme close?",
			wantClass:   QueryClassMultiHop,
			wantMinConf: 0.7,
		},
		{
			name:        "multi_hop_wikilink_at_pattern",
			query:       "who at [[companies/acme-corp]] owns the pilot?",
			wantClass:   QueryClassMultiHop,
			wantMinConf: 0.8,
		},

		// ── counterfactual ────────────────────────────────────────────────────
		{
			name:        "counterfactual_what_if",
			query:       "What if Sarah hadn't joined Acme?",
			wantClass:   QueryClassCounterfactual,
			wantMinConf: 0.8,
		},
		{
			name:        "counterfactual_suppose",
			query:       "Suppose the deal had closed last quarter — what would have changed?",
			wantClass:   QueryClassCounterfactual,
			wantMinConf: 0.8,
		},
		{
			name:        "counterfactual_never_had",
			query:       "what would have happened if X never had the meeting",
			wantClass:   QueryClassCounterfactual,
			wantMinConf: 0.8,
		},
		{
			name:        "counterfactual_without",
			query:       "Without the partnership, where would Acme be now?",
			wantClass:   QueryClassCounterfactual,
			wantMinConf: 0.8,
		},

		// ── general ───────────────────────────────────────────────────────────
		{
			name:        "general_weather",
			query:       "What is the weather in London today?",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.7,
		},
		{
			name:        "general_pop_culture",
			query:       "Who won the Oscars last year?",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.7,
		},
		{
			name:        "general_will_deal_close",
			query:       "Will the deal close this quarter?",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.7,
		},
		{
			name:        "general_coding_question",
			query:       "How do I write a Go interface?",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.7,
		},
		{
			name:        "general_empty_string",
			query:       "",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.9,
		},

		// ── tricky edge cases ──────────────────────────────────────────────────
		{
			name:        "tricky_who_at_acme_lower",
			query:       "who at acme is responsible for security?",
			wantClass:   QueryClassMultiHop,
			wantMinConf: 0.8,
		},
		{
			name:        "tricky_what_does_sarah_role",
			query:       "what does sarah do in her role?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.7,
		},
		{
			name:        "tricky_first_name_only",
			query:       "What is alice responsible for?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.4,
		},
		{
			name:        "tricky_counterfactual_beats_multi_hop",
			query:       "What if Sarah at Acme hadn't signed the contract?",
			wantClass:   QueryClassCounterfactual,
			wantMinConf: 0.8,
		},
		{
			name:        "tricky_relationship_no_entity_fallback",
			query:       "Who reports to the CEO?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotClass, gotConf := ClassifyQuery(tc.query)
			if gotClass != tc.wantClass {
				t.Errorf("ClassifyQuery(%q) = %q conf=%.2f, want class %q",
					tc.query, gotClass, gotConf, tc.wantClass)
			}
			if gotConf < tc.wantMinConf {
				t.Errorf("ClassifyQuery(%q) class=%q conf=%.2f, want conf >= %.2f",
					tc.query, gotClass, gotConf, tc.wantMinConf)
			}
		})
	}
}

// TestClassifyQuery_StableOnRepeat ensures the classifier is deterministic.
func TestClassifyQuery_StableOnRepeat(t *testing.T) {
	t.Parallel()
	queries := []string{
		"Who at Acme closes enterprise deals?",
		"What does Sarah do?",
		"What if the partnership hadn't happened?",
		"What is the weather?",
	}
	for _, q := range queries {
		c1, f1 := ClassifyQuery(q)
		c2, f2 := ClassifyQuery(q)
		if c1 != c2 || f1 != f2 {
			t.Errorf("ClassifyQuery(%q) is non-deterministic: got (%q,%.2f) then (%q,%.2f)",
				q, c1, f1, c2, f2)
		}
	}
}
