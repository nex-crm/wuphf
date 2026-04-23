package team

// wiki_query_rewrite_test.go — table-driven tests for the Slice 2 Thread A
// query rewriter. Covers the bench query shapes (q_036..q_045, q_046..q_048)
// plus wikilink variants and defensive negatives so the typed graph walk
// never fires on a mis-parsed query.

import "testing"

func TestParseMultiHopSpans(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		query       string
		wantCompany string
		wantProject string
		wantOK      bool
	}{
		{
			name:        "bench q_036 plain — Blueshift + Q2 Pilot Program",
			query:       "Who at Blueshift championed the Q2 Pilot Program project?",
			wantCompany: "Blueshift",
			wantProject: "Q2 Pilot Program",
			wantOK:      true,
		},
		{
			name:        "bench q_038 — Acme Corp + Data Platform Migration",
			query:       "Who at Acme Corp championed the Data Platform Migration project?",
			wantCompany: "Acme Corp",
			wantProject: "Data Platform Migration",
			wantOK:      true,
		},
		{
			name:        "bench q_037 — Vandelay Industries + Data Platform Migration",
			query:       "Who at Vandelay Industries championed the Data Platform Migration project?",
			wantCompany: "Vandelay Industries",
			wantProject: "Data Platform Migration",
			wantOK:      true,
		},
		{
			name:        "bench q_039 — Dunder Mifflin + Mobile Revamp",
			query:       "Who at Dunder Mifflin championed the Mobile Revamp project?",
			wantCompany: "Dunder Mifflin",
			wantProject: "Mobile Revamp",
			wantOK:      true,
		},
		{
			name:        "no trailing 'project' — still matches",
			query:       "Who at Blueshift championed the Q2 Pilot Program?",
			wantCompany: "Blueshift",
			wantProject: "Q2 Pilot Program",
			wantOK:      true,
		},
		{
			name:        "champions present tense",
			query:       "Who at Acme Corp champions the Pricing Rework project?",
			wantCompany: "Acme Corp",
			wantProject: "Pricing Rework",
			wantOK:      true,
		},
		{
			name:        "no leading article on project",
			query:       "Who at Acme Corp championed Pricing Rework?",
			wantCompany: "Acme Corp",
			wantProject: "Pricing Rework",
			wantOK:      true,
		},
		{
			name:        "wikilink on company — [[Display]]",
			query:       "Who at [[Acme Corp]] championed the Q2 Pilot Program project?",
			wantCompany: "Acme Corp",
			wantProject: "Q2 Pilot Program",
			wantOK:      true,
		},
		{
			name:        "wikilink on company — [[slug|Display]]",
			query:       "Who at [[acme-corp|Acme Corp]] championed the Q2 Pilot Program project?",
			wantCompany: "Acme Corp",
			wantProject: "Q2 Pilot Program",
			wantOK:      true,
		},
		{
			name:   "negative — 'who leads X' is relationship, not multi_hop",
			query:  "Who leads Q2 Pilot Program?",
			wantOK: false,
		},
		{
			name:   "negative — counterfactual shape",
			query:  "What would have happened if Ivan Petrov had not taken her current role?",
			wantOK: false,
		},
		{
			name:   "negative — status shape",
			query:  "What does Marcus Lee do?",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			company, project, ok := parseMultiHopSpans(tc.query)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (company=%q project=%q)", ok, tc.wantOK, company, project)
			}
			if !ok {
				return
			}
			if company != tc.wantCompany {
				t.Errorf("company = %q, want %q", company, tc.wantCompany)
			}
			if project != tc.wantProject {
				t.Errorf("project = %q, want %q", project, tc.wantProject)
			}
		})
	}
}

func TestParseCounterfactualSubject(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		query      string
		wantSubj   string
		wantOK     bool
	}{
		{
			name:     "bench q_047 — 'what would have happened if X had not'",
			query:    "What would have happened if Ivan Petrov had not taken her current role?",
			wantSubj: "Ivan Petrov",
			wantOK:   true,
		},
		{
			name:     "bench q_046 shape — Oscar Delmar",
			query:    "What would have happened if Oscar Delmar had not taken her current role?",
			wantSubj: "Oscar Delmar",
			wantOK:   true,
		},
		{
			name:     "if X hadn't pattern",
			query:    "If Sarah Jones hadn't joined Acme, would the deal have closed?",
			wantSubj: "Sarah Jones",
			wantOK:   true,
		},
		{
			name:     "what if short form",
			query:    "What if Ivan Petrov had left in Q2?",
			wantSubj: "Ivan Petrov",
			wantOK:   true,
		},
		{
			name:     "without X pattern",
			query:    "Without Harper Quinn, would the program have shipped?",
			wantSubj: "Harper Quinn",
			wantOK:   true,
		},
		{
			name:     "suppose X had pattern",
			query:    "Suppose Ivan Petrov had accepted the earlier offer.",
			wantSubj: "Ivan Petrov",
			wantOK:   true,
		},
		{
			name:   "negative — status query",
			query:  "What does Marcus Lee do?",
			wantOK: false,
		},
		{
			name:   "negative — multi_hop query",
			query:  "Who at Blueshift championed the Q2 Pilot Program project?",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subj, ok := parseCounterfactualSubject(tc.query)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (subj=%q)", ok, tc.wantOK, subj)
			}
			if !ok {
				return
			}
			if subj != tc.wantSubj {
				t.Errorf("subject = %q, want %q", subj, tc.wantSubj)
			}
		})
	}
}

// TestQueryRewriteParser is the alias name the spec calls out. It runs a
// combined table of both rewriters so regressions in either direction fail
// this single entry point.
func TestQueryRewriteParser(t *testing.T) {
	t.Parallel()
	t.Run("multi_hop", TestParseMultiHopSpans)
	t.Run("counterfactual", TestParseCounterfactualSubject)
}

func TestDisplayToSlugCandidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		display string
		want    []string
	}{
		{
			name:    "single word — acme-corp",
			display: "Acme Corp",
			want:    []string{"acme-corp", "acme"},
		},
		{
			name:    "vandelay industries → vandelay",
			display: "Vandelay Industries",
			want:    []string{"vandelay-industries", "vandelay"},
		},
		{
			name:    "q2 pilot program → q2-pilot",
			display: "Q2 Pilot Program",
			want:    []string{"q2-pilot-program", "q2-pilot", "q2"},
		},
		{
			name:    "blueshift — single token",
			display: "Blueshift",
			want:    []string{"blueshift"},
		},
		{
			name:    "empty",
			display: "",
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := displayToSlugCandidates(tc.display)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d — got %v want %v", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] = %q, want %q — full got %v", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
