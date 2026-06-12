package team

import "testing"

// TestInterviewQuestionsSimilar pins the conservative similarity contract
// the cross-agent interview dedupe relies on: paraphrases of the same ask
// merge; entity-swapped or genuinely different questions never do. The
// live-path regression coverage lives in the `interview-dedupe` office
// eval job; this table guards the thresholds themselves.
func TestInterviewQuestionsSimilar(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{
			name: "exact text modulo punctuation and case",
			a:    "Which CRM — HubSpot or Salesforce?",
			b:    "which crm: hubspot or salesforce",
			want: true,
		},
		{
			name: "paraphrase of the same ask merges (the live five-agent failure)",
			a:    "Which CRM should the team standardize on for the pilot — HubSpot or Salesforce?",
			b:    "Which CRM do you want us to standardize on: HubSpot or Salesforce?",
			want: true,
		},
		{
			name: "identical content tokens with different function words merge",
			a:    "Which CRM should we use?",
			b:    "Which CRM do you want the team to use?",
			want: true,
		},
		{
			name: "entity swap stays separate (Acme vs Corti)",
			a:    "Should we send the renewal email to Acme?",
			b:    "Should we send the renewal email to Corti?",
			want: false,
		},
		{
			name: "different topics stay separate",
			a:    "Which CRM should the team standardize on for the pilot — HubSpot or Salesforce?",
			b:    "What is the budget ceiling for paid pilot tooling this quarter?",
			want: false,
		},
		{
			name: "empty question never matches",
			a:    "",
			b:    "Which CRM should we use?",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interviewQuestionsSimilar(tc.a, tc.b); got != tc.want {
				t.Fatalf("interviewQuestionsSimilar(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			// Symmetry: dedupe scans existing-vs-new in both roles.
			if got := interviewQuestionsSimilar(tc.b, tc.a); got != tc.want {
				t.Fatalf("interviewQuestionsSimilar(%q, %q) = %v, want %v (asymmetric)", tc.b, tc.a, got, tc.want)
			}
		})
	}
}
