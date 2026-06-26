package runner

import (
	"math"
	"testing"
)

const eps = 1e-9

func almostEqual(a, b float64) bool { return math.Abs(a-b) < eps }

func TestScoreRecallAtK(t *testing.T) {
	cases := []struct {
		name     string
		expected []string
		got      []string
		k        int
		want     float64
	}{
		{"out of scope returns zero", nil, []string{"a", "b"}, 3, 0},
		{"perfect single fact at top", []string{"a"}, []string{"a", "b", "c"}, 1, 1},
		{"relevant fact below k misses", []string{"c"}, []string{"a", "b", "c"}, 2, 0},
		{"relevant fact within k hits", []string{"c"}, []string{"a", "b", "c"}, 3, 1},
		{"two of three relevant in topk", []string{"a", "b", "z"}, []string{"a", "b", "c"}, 3, 2.0 / 3.0},
		{"recall@1 capped by expected size", []string{"a", "b"}, []string{"a", "b"}, 1, 0.5},
		{"k larger than got is clamped", []string{"b"}, []string{"a", "b"}, 50, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scoreRecallAtK(c.expected, c.got, c.k); !almostEqual(got, c.want) {
				t.Fatalf("scoreRecallAtK(%v,%v,%d) = %v, want %v", c.expected, c.got, c.k, got, c.want)
			}
		})
	}
}

func TestScoreNDCG(t *testing.T) {
	// IDCG@k constants for binary relevance, ideal ordering.
	idcg1 := 1.0                    // 1/log2(2)
	idcg2 := 1.0 + 1.0/math.Log2(3) // ranks 1,2
	cases := []struct {
		name     string
		expected []string
		got      []string
		k        int
		want     float64
	}{
		{"out of scope returns zero", nil, []string{"a"}, 10, 0},
		{"single relevant at top is perfect", []string{"a"}, []string{"a", "b", "c"}, 10, 1},
		{"single relevant at rank 2", []string{"b"}, []string{"a", "b", "c"}, 10, (1.0 / math.Log2(3)) / idcg1},
		{"two relevant ideally ordered", []string{"a", "b"}, []string{"a", "b", "c"}, 10, idcg2 / idcg2},
		{"two relevant reordered", []string{"a", "c"}, []string{"a", "b", "c"}, 10, (1.0 + 1.0/math.Log2(4)) / idcg2},
		{"none retrieved", []string{"z"}, []string{"a", "b", "c"}, 10, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scoreNDCG(c.expected, c.got, c.k); !almostEqual(got, c.want) {
				t.Fatalf("scoreNDCG(%v,%v,%d) = %v, want %v", c.expected, c.got, c.k, got, c.want)
			}
		})
	}
}

func TestScoreMRR(t *testing.T) {
	cases := []struct {
		name     string
		expected []string
		got      []string
		want     float64
	}{
		{"out of scope returns zero", nil, []string{"a"}, 0},
		{"first relevant at rank 1", []string{"a"}, []string{"a", "b", "c"}, 1},
		{"first relevant at rank 3", []string{"c"}, []string{"a", "b", "c"}, 1.0 / 3.0},
		{"earliest of several wins", []string{"b", "c"}, []string{"a", "b", "c"}, 0.5},
		{"none retrieved", []string{"z"}, []string{"a", "b", "c"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scoreMRR(c.expected, c.got); !almostEqual(got, c.want) {
				t.Fatalf("scoreMRR(%v,%v) = %v, want %v", c.expected, c.got, got, c.want)
			}
		})
	}
}
