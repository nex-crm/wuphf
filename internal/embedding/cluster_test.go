package embedding

import (
	"math"
	"testing"
)

// vec is a tiny helper so test fixtures read like math papers, not
// sliceeral noise.
func vec(values ...float32) []float32 { return values }

// approxEq is the float-comparison helper used across the test suite.
// Cosine returns float32, but our tolerance is in float64 so we cast
// once to keep the comparison code readable.
func approxEq(t *testing.T, got, want float32, tol float64) {
	t.Helper()
	if math.Abs(float64(got)-float64(want)) > tol {
		t.Errorf("got %v, want %v (tol %v)", got, want, tol)
	}
}

func TestCosine_Identical(t *testing.T) {
	v := L2Normalise(vec(1, 2, 3, 4))
	got := Cosine(v, v)
	approxEq(t, got, 1.0, 1e-6)
}

func TestCosine_Orthogonal(t *testing.T) {
	a := L2Normalise(vec(1, 0, 0))
	b := L2Normalise(vec(0, 1, 0))
	got := Cosine(a, b)
	approxEq(t, got, 0.0, 1e-6)
}

func TestCosine_Opposite(t *testing.T) {
	a := L2Normalise(vec(1, 0, 0))
	b := L2Normalise(vec(-1, 0, 0))
	got := Cosine(a, b)
	approxEq(t, got, -1.0, 1e-6)
}

func TestCosine_EmptyOrMismatch(t *testing.T) {
	if got := Cosine(nil, vec(1, 2)); got != 0 {
		t.Errorf("nil/non-nil: got %v want 0", got)
	}
	if got := Cosine(vec(1), vec(1, 2)); got != 0 {
		t.Errorf("length mismatch: got %v want 0", got)
	}
}

func TestL2Normalise_ZeroVector(t *testing.T) {
	got := L2Normalise(vec(0, 0, 0))
	for i, x := range got {
		if x != 0 {
			t.Errorf("zero-in zero-out: got %v at %d", x, i)
		}
	}
}

func TestL2Normalise_UnitOutput(t *testing.T) {
	got := L2Normalise(vec(3, 4)) // length 5
	approxEq(t, got[0], 0.6, 1e-6)
	approxEq(t, got[1], 0.8, 1e-6)
}

func TestClusterByCosine_GroupsSimilar(t *testing.T) {
	// Three nearly-identical vectors + two orthogonal-to-them vectors →
	// 2 clusters. Threshold 0.8 picks "same topic" without merging the
	// outliers.
	similarA := L2Normalise(vec(1, 1, 0, 0))
	similarB := L2Normalise(vec(0.95, 0.95, 0.05, 0.05))
	similarC := L2Normalise(vec(1.05, 0.95, 0.05, 0))
	otherA := L2Normalise(vec(0, 0, 1, 1))
	otherB := L2Normalise(vec(0.05, 0.05, 0.95, 0.95))

	entries := []ClusterEntry{
		{ID: "a1", Vector: similarA},
		{ID: "a2", Vector: similarB},
		{ID: "a3", Vector: similarC},
		{ID: "b1", Vector: otherA},
		{ID: "b2", Vector: otherB},
	}
	clusters := ClusterByCosine(entries, 0.8)
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	sizes := []int{len(clusters[0].Entries), len(clusters[1].Entries)}
	if !(sizes[0] == 3 && sizes[1] == 2) && !(sizes[0] == 2 && sizes[1] == 3) {
		t.Errorf("expected sizes [3,2] or [2,3], got %v", sizes)
	}
}

func TestClusterByCosine_RespectsThreshold(t *testing.T) {
	// Three vectors with progressively decreasing similarity. At 0.5 they
	// should all merge; at 0.95 only the near-identical pair joins,
	// leaving the third in its own cluster.
	a := L2Normalise(vec(1, 0, 0))
	b := L2Normalise(vec(0.99, 0.14, 0))                 // ~cos 0.99 with a
	c := L2Normalise(vec(math.Sqrt2/2, math.Sqrt2/2, 0)) // ~cos 0.7 with a

	entries := []ClusterEntry{
		{ID: "a", Vector: a},
		{ID: "b", Vector: b},
		{ID: "c", Vector: c},
	}

	loose := ClusterByCosine(entries, 0.5)
	if len(loose) != 1 {
		t.Errorf("loose threshold: got %d clusters want 1", len(loose))
	}

	strict := ClusterByCosine(entries, 0.95)
	if len(strict) != 2 {
		t.Errorf("strict threshold: got %d clusters want 2", len(strict))
	}
}

func TestClusterByCosine_EmptyInput(t *testing.T) {
	if got := ClusterByCosine(nil, 0.8); got != nil {
		t.Errorf("nil input: got %v want nil", got)
	}
	if got := ClusterByCosine([]ClusterEntry{}, 0.8); got != nil {
		t.Errorf("empty input: got %v want nil", got)
	}
}

func TestClusterByCosine_SkipsBadDimension(t *testing.T) {
	good := L2Normalise(vec(1, 0, 0))
	bad := vec(1, 0) // wrong dim → skipped silently

	entries := []ClusterEntry{
		{ID: "good", Vector: good},
		{ID: "bad", Vector: bad},
	}
	clusters, skipped := ClusterByCosineWithSkipped(entries, 0.8)
	if len(clusters) != 1 {
		t.Errorf("clusters: got %d want 1", len(clusters))
	}
	if len(skipped) != 1 || skipped[0].ID != "bad" {
		t.Errorf("skipped: got %+v want one entry with ID=bad", skipped)
	}
}

func TestValidate_DimensionMismatch(t *testing.T) {
	entries := []ClusterEntry{
		{ID: "ok", Vector: vec(1, 2, 3)},
		{ID: "wrong", Vector: vec(1, 2)},
	}
	if err := Validate(entries, 3); err == nil {
		t.Error("Validate: expected error on dimension mismatch")
	}
	if err := Validate(entries[:1], 3); err != nil {
		t.Errorf("Validate: unexpected error on matching dims: %v", err)
	}
}
