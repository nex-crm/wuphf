package main

import (
	"testing"
)

func TestMustInclude(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected expectedBlock
		wantPass bool
		wantFail string // substring of first failure message, empty if pass
	}{
		{
			name: "single substring present",
			raw:  `{"artifact_sha": "abc123", "entities": [{"existing_slug": "sarah-jones"}]}`,
			expected: expectedBlock{
				MustInclude: []string{"sarah-jones", "abc123"},
			},
			wantPass: true,
		},
		{
			name: "substring missing",
			raw:  `{"artifact_sha": "abc123", "entities": []}`,
			expected: expectedBlock{
				MustInclude: []string{"sarah-jones"},
			},
			wantPass: false,
			wantFail: `must_include missing: "sarah-jones"`,
		},
		{
			name: "multiple missing substrings accumulate",
			raw:  `{}`,
			expected: expectedBlock{
				MustInclude: []string{"alice", "bob"},
			},
			wantPass: false,
			wantFail: `must_include missing: "alice"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := assertExpected(tc.raw, nil, tc.expected)
			if res.pass != tc.wantPass {
				t.Fatalf("pass=%v, want %v; failures=%v", res.pass, tc.wantPass, res.failures)
			}
			if !tc.wantPass && tc.wantFail != "" {
				found := false
				for _, f := range res.failures {
					if contains(f, tc.wantFail) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected failure containing %q, got %v", tc.wantFail, res.failures)
				}
			}
		})
	}
}

func TestMustNotInclude(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected expectedBlock
		wantPass bool
		wantFail string
	}{
		{
			name: "forbidden substring absent",
			raw:  `{"answer": "VP of Sales"}`,
			expected: expectedBlock{
				MustNotInclude: []string{"probably", "I think"},
			},
			wantPass: true,
		},
		{
			name: "forbidden substring present",
			raw:  `{"answer": "Sarah probably works at Acme"}`,
			expected: expectedBlock{
				MustNotInclude: []string{"probably"},
			},
			wantPass: false,
			wantFail: `must_not_include present: "probably"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := assertExpected(tc.raw, nil, tc.expected)
			if res.pass != tc.wantPass {
				t.Fatalf("pass=%v, want %v; failures=%v", res.pass, tc.wantPass, res.failures)
			}
			if !tc.wantPass && tc.wantFail != "" {
				found := false
				for _, f := range res.failures {
					if contains(f, tc.wantFail) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected failure containing %q, got %v", tc.wantFail, res.failures)
				}
			}
		})
	}
}

func TestStructuredFlatMatch(t *testing.T) {
	tests := []struct {
		name     string
		parsed   any
		expected expectedBlock
		wantPass bool
		wantFail string
	}{
		{
			name: "flat key matches",
			parsed: map[string]any{
				"query_class": "status",
				"coverage":    "complete",
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"query_class": "status",
					"coverage":    "complete",
				},
			},
			wantPass: true,
		},
		{
			name: "extra keys on actual side are ignored",
			parsed: map[string]any{
				"query_class": "status",
				"coverage":    "complete",
				"confidence":  0.9,
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"query_class": "status",
				},
			},
			wantPass: true,
		},
		{
			name: "value mismatch fails",
			parsed: map[string]any{
				"coverage": "partial",
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"coverage": "complete",
				},
			},
			wantPass: false,
			wantFail: "structured[coverage]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := assertExpected("", tc.parsed, tc.expected)
			if res.pass != tc.wantPass {
				t.Fatalf("pass=%v, want %v; failures=%v", res.pass, tc.wantPass, res.failures)
			}
			if !tc.wantPass && tc.wantFail != "" {
				found := false
				for _, f := range res.failures {
					if contains(f, tc.wantFail) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected failure containing %q, got %v", tc.wantFail, res.failures)
				}
			}
		})
	}
}

func TestStructuredNestedMatch(t *testing.T) {
	// Tests partial-deep-match: nested object inside expected must match the
	// corresponding nested object in actual.
	tests := []struct {
		name     string
		parsed   any
		expected expectedBlock
		wantPass bool
	}{
		{
			name: "nested object partial match passes",
			parsed: map[string]any{
				"artifact_sha": "3f9a21bc4e8d9102",
				"entities": []any{
					map[string]any{
						"existing_slug": "sarah-jones",
						"kind":          "person",
						"confidence":    0.95,
					},
				},
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"artifact_sha": "3f9a21bc4e8d9102",
					"entities": []any{
						map[string]any{"existing_slug": "sarah-jones"},
					},
				},
			},
			wantPass: true,
		},
		{
			name: "nested missing key fails",
			parsed: map[string]any{
				"entities": []any{
					map[string]any{"kind": "person"},
				},
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"entities": []any{
						map[string]any{"ghost": true},
					},
				},
			},
			wantPass: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := assertExpected("", tc.parsed, tc.expected)
			if res.pass != tc.wantPass {
				t.Fatalf("pass=%v, want %v; failures=%v", res.pass, tc.wantPass, res.failures)
			}
		})
	}
}

func TestStructuredArrayMatch(t *testing.T) {
	// Tests that array elementwise matching works, including index-out-of-range.
	tests := []struct {
		name     string
		parsed   any
		expected expectedBlock
		wantPass bool
		wantFail string
	}{
		{
			name: "array element matches by index",
			parsed: map[string]any{
				"sources_cited": []any{float64(1), float64(2), float64(4)},
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"sources_cited": []any{},
				},
			},
			wantPass: true,
		},
		{
			name: "array index out of range fails",
			parsed: map[string]any{
				"items": []any{"a"},
			},
			expected: expectedBlock{
				Structured: map[string]any{
					"items": []any{"a", "b"},
				},
			},
			wantPass: false,
			wantFail: "index out of range",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := assertExpected("", tc.parsed, tc.expected)
			if res.pass != tc.wantPass {
				t.Fatalf("pass=%v, want %v; failures=%v", res.pass, tc.wantPass, res.failures)
			}
			if !tc.wantPass && tc.wantFail != "" {
				found := false
				for _, f := range res.failures {
					if contains(f, tc.wantFail) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected failure containing %q, got %v", tc.wantFail, res.failures)
				}
			}
		})
	}
}

func TestCombinedContractAllThreeTypes(t *testing.T) {
	// Exercises all three matcher types together on a single case.
	raw := `{"query_class":"status","coverage":"complete","answer_markdown":"Sarah is VP of Sales <sup>[1]</sup>."}`
	parsed := map[string]any{
		"query_class":     "status",
		"coverage":        "complete",
		"answer_markdown": "Sarah is VP of Sales <sup>[1]</sup>.",
	}
	expected := expectedBlock{
		MustInclude:    []string{"VP of Sales", "<sup>[1]</sup>"},
		MustNotInclude: []string{"Head of Marketing", "probably"},
		Structured: map[string]any{
			"query_class": "status",
			"coverage":    "complete",
		},
	}
	res := assertExpected(raw, parsed, expected)
	if !res.pass {
		t.Fatalf("expected pass, got failures: %v", res.failures)
	}
}

// contains is a helper because strings.Contains reads clearer in tests.
func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || len(s) > 0 && containsInner(s, sub))
}

func containsInner(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
