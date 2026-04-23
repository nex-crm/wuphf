package team

import (
	"math"
	"testing"
)

// TestJaroWinkler covers the Jaro-Winkler primitive (H6 fix).
// The ≥0.9 dedup threshold is a Slice 2 gate — this file tests the algorithm
// is correct so Slice 2 can wire it in without a new dependency.
func TestJaroWinkler(t *testing.T) {
	cases := []struct {
		a, b    string
		wantMin float64
		wantMax float64
		desc    string
	}{
		{
			// Near-identical predicates (typo variant) — Slice 2 dedup target.
			a: "works_at", b: "work_at",
			wantMin: 0.95, wantMax: 1.0,
			desc: "one-char deletion variant scores ≥ 0.95",
		},
		{
			// Semantically related but character-different predicates.
			// JW is character-level; these score ~0.75, well below the 0.9 gate.
			a: "works_at", b: "is_working_at",
			wantMin: 0.70, wantMax: 0.80,
			desc: "works_at vs is_working_at scores ~0.75 (below dedup gate)",
		},
		{
			// Unrelated predicates — must score well below the 0.9 gate.
			a: "works_at", b: "reports_to",
			wantMin: 0.0, wantMax: 0.60,
			desc: "unrelated predicates score < 0.6",
		},
		{
			a: "hello", b: "hello",
			wantMin: 1.0, wantMax: 1.0,
			desc: "identical strings score 1.0",
		},
		{
			a: "", b: "foo",
			wantMin: 0.0, wantMax: 0.0,
			desc: "empty string scores 0.0",
		},
		{
			// Classic Jaro-Winkler reference example: MARTHA vs MARHTA.
			a: "MARTHA", b: "MARHTA",
			wantMin: 0.96, wantMax: 1.0,
			desc: "classic MARTHA/MARHTA reference example ≥ 0.96",
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := JaroWinkler(tc.a, tc.b)
			if got < tc.wantMin || got > tc.wantMax+1e-9 {
				t.Errorf("JaroWinkler(%q, %q) = %.4f, want in [%.2f, %.2f]",
					tc.a, tc.b, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestJaroWinkler_Symmetric(t *testing.T) {
	pairs := [][2]string{
		{"works_at", "is_working_at"},
		{"reports_to", "works_at"},
		{"abc", "xyz"},
	}
	for _, p := range pairs {
		ab := JaroWinkler(p[0], p[1])
		ba := JaroWinkler(p[1], p[0])
		if math.Abs(ab-ba) > 1e-9 {
			t.Errorf("JaroWinkler not symmetric: (%q,%q)=%.6f, (%q,%q)=%.6f",
				p[0], p[1], ab, p[1], p[0], ba)
		}
	}
}
