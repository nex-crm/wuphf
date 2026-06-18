package team

import "testing"

func TestTitlesAreSimilar(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Ship the MVP", "ship mvp!", true},                          // punctuation + stopwords
		{"Wire Stripe webhooks", "wire the stripe webhook", true},    // stopword + singular/plural-ish token overlap
		{"MVP onboarding flow", "onboarding flow for the MVP", true}, // order-insensitive
		{"Build the billing system", "Wire Stripe webhooks", false},  // genuinely distinct
		{"Send the weekly digest", "Send the monthly report", false}, // share only low-signal tokens
		{"", "anything", false},                                      // empty
		{"the a an", "please can you", false},                        // all stopwords → no content
	}
	for _, tc := range cases {
		if got := titlesAreSimilar(tc.a, tc.b); got != tc.want {
			t.Errorf("titlesAreSimilar(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
