package team

// wiki_classifier_extra_test.go — edge cases and perf guard for ClassifyQuery.
//
// Covers:
//   - Typos and capitalisation noise.
//   - Non-ASCII input (emoji, accented characters).
//   - Mixed-class queries (counterfactual beats multi_hop, wikilink beats
//     general).
//   - Confidence floor invariants.
//   - p95 latency guard — 10k iterations must complete in under a budget that
//     rules out hidden O(n²) regex work.

import (
	"sort"
	"testing"
	"time"
)

func TestClassifyQuery_ExtendedEdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		query       string
		wantClass   QueryClass
		wantMinConf float64
	}{
		{
			// Typo in a role word still classifies as status when entity token is
			// present — heuristic is lenient.
			name:        "typo_in_role_word",
			query:       "What is David's rolle at Acme?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.4,
		},
		{
			name:        "emoji_leading_does_not_panic",
			query:       "🔥 who at acme runs sales?",
			wantClass:   QueryClassMultiHop,
			wantMinConf: 0.7,
		},
		{
			name:        "accented_non_ascii_name_entity",
			query:       "Who reports to Zoë at Acme?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			name:        "trailing_whitespace_tolerated",
			query:       "    Who reports to Sarah?   ",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			// Counterfactual wins against multi_hop clue ("at acme").
			name:        "mixed_counterfactual_beats_multi_hop",
			query:       "What if Sarah at Acme had not signed the Q2 deal?",
			wantClass:   QueryClassCounterfactual,
			wantMinConf: 0.8,
		},
		{
			// Wikilink → never general, even when the rest of the sentence looks
			// like an out-of-scope question.
			name:        "wikilink_beats_general_by_shape",
			query:       "Is the weather nice today for [[people/sarah-jones]]?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.9,
		},
		{
			name:        "question_mark_only",
			query:       "???",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.7,
		},
		{
			// Entity token present but question is pure pleasantry — still classifies
			// entity-adjacent (status), not general.
			name:        "pleasantry_with_entity_token",
			query:       "How is Sarah doing?",
			wantClass:   QueryClassStatus,
			wantMinConf: 0.4,
		},
		{
			name:        "all_stop_words_is_general",
			query:       "what is the a an on in",
			wantClass:   QueryClassGeneral,
			wantMinConf: 0.7,
		},
		{
			name:        "relationship_with_wikilink_entity",
			query:       "who manages [[people/sarah-jones]] on the sales team?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
		{
			// Known behaviour: whoAtEntityRE requires "who at" without
			// intervening punctuation, so "Who, at Acme, ..." falls through
			// to relationship via "who + is-the-like verb" heuristics.
			// Finding: treating commas as whitespace in the whoAtEntityRE
			// would upgrade this to multi_hop. Document for Slice 2.
			name:        "who_comma_at_entity_falls_through_to_relationship",
			query:       "Who, at Acme, owns the pilot?",
			wantClass:   QueryClassRelationship,
			wantMinConf: 0.7,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, conf := ClassifyQuery(tc.query)
			if class != tc.wantClass {
				t.Errorf("ClassifyQuery(%q) = %q (conf=%.2f), want %q",
					tc.query, class, conf, tc.wantClass)
			}
			if conf < tc.wantMinConf {
				t.Errorf("ClassifyQuery(%q) confidence = %.2f, want ≥ %.2f",
					tc.query, conf, tc.wantMinConf)
			}
			if conf < 0 || conf > 1 {
				t.Errorf("confidence out of [0,1] range: %.3f", conf)
			}
		})
	}
}

// TestClassifyQuery_P95LatencyBudget asserts the classifier is cheap enough
// that routing 10k queries stays under a generous ceiling. This guards against
// accidental regex backtracking or O(n²) token iteration in future changes.
//
// The ceiling is 5 ms p95 per classification (the task matrix target). On
// CI hardware the real number should be sub-millisecond; the 5 ms budget
// leaves wide margin so the test is not flaky.
func TestClassifyQuery_P95LatencyBudget(t *testing.T) {
	t.Parallel()
	queries := []string{
		"What does Sarah do?",
		"Who at Acme closes enterprise deals?",
		"What if Sarah hadn't joined Acme?",
		"Who reports to Michael?",
		"What is David's role at Acme Corp?",
		"What is the weather in London today?",
		"Tell me about Acme Corp's Q2 pipeline",
		"Suppose the deal had closed last quarter — what would have changed?",
		"How is Nazz feeling about the launch?",
		"What does [[people/sarah-jones]] do?",
	}
	const iterations = 10000
	samples := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		q := queries[i%len(queries)]
		start := time.Now()
		_, _ = ClassifyQuery(q)
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[int(float64(iterations)*0.95)]
	const budget = 5 * time.Millisecond
	if p95 > budget {
		t.Errorf("ClassifyQuery p95 over %d iterations = %v, want < %v",
			iterations, p95, budget)
	}
}

// TestClassifyQuery_ConfidenceClamped verifies confidence is always within
// [0, 1] across a broad sweep of inputs, protecting against a heuristic that
// accidentally returns > 1.0 (e.g. by accumulating multiple bonuses).
func TestClassifyQuery_ConfidenceClamped(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"",
		"x",
		"?",
		"Who at Acme [[people/sarah-jones]] works?",
		"What if Sarah at Acme hadn't joined?",
		"Will it rain tomorrow?",
	}
	for _, q := range inputs {
		_, conf := ClassifyQuery(q)
		if conf < 0 || conf > 1 {
			t.Errorf("ClassifyQuery(%q) confidence %.3f out of [0,1]", q, conf)
		}
	}
}
