package embedding

// cluster.go groups embedded entries by cosine similarity. It is the
// semantic counterpart to internal/team/notebook_signal_scanner.go's
// Jaccard clustering — same shape (slice of clusters), better recall on
// paraphrases.
//
// The algorithm is deliberately greedy:
//
//  1. For each entry, compute cosine similarity against every existing
//     cluster's centroid.
//  2. If the best score >= threshold, join that cluster; otherwise start
//     a new one.
//  3. After joining, recompute the centroid as the L2-normalised mean of
//     all member vectors.
//
// Greedy is good enough for v1 — the synthesizer is the LLM gate that
// catches false-positive clusters. Single-link agglomerative would have
// better quality at the cost of complexity (and more API calls if we
// ever materialise pairwise distance matrices). Revisit if the
// SkillCandidate quality bar tightens.

import "fmt"

// ClusterEntry is the input row to ClusterByCosine. ID is opaque to the
// clustering layer — callers use it to map cluster members back to their
// source (e.g. notebook entry path).
type ClusterEntry struct {
	// Text is the original text the vector was computed from. Carried
	// through so callers can build excerpts without re-reading the
	// source. Optional — empty string is fine.
	Text string

	// Vector is the L2-normalised embedding. Length must match across
	// every entry passed in a single call.
	Vector []float32

	// ID is caller-supplied. Examples: notebook entry path,
	// SkillCandidateExcerpt.Path. Carried through so callers can map
	// cluster output back to their domain types.
	ID string
}

// Cluster is a group of entries whose pairwise centroid-cosine all
// exceed the threshold supplied to ClusterByCosine.
type Cluster struct {
	// Entries are the cluster members in insertion order. Stable — we
	// never reorder once an entry has joined.
	Entries []ClusterEntry

	// Centroid is the L2-normalised mean of every member's vector.
	// Used by ClusterByCosine to score new candidates. Callers may use
	// it for downstream "most-representative" picks.
	Centroid []float32
}

// ClusterByCosine groups entries where pairwise cosine similarity to a
// cluster centroid is at least threshold. The centroid is recomputed as
// the L2-normalised mean of all member vectors after every join.
//
// Threshold tuning: 0.7 = "loosely related", 0.8 = "same topic", 0.9 =
// "near-paraphrase". Default for the notebook scanner is 0.8 (see
// WUPHF_STAGE_B_NOTEBOOK_COSINE_THRESHOLD).
//
// Returns clusters in insertion order. An empty input slice returns nil.
// Vectors of mismatched length cause the offending entry to be dropped
// silently — this matches the notebook scanner's "best-effort over
// unreadable entries" semantics, but we surface the count via the
// returned skipped slice so callers can log it.
func ClusterByCosine(entries []ClusterEntry, threshold float32) []Cluster {
	clusters, _ := ClusterByCosineWithSkipped(entries, threshold)
	return clusters
}

// ClusterByCosineWithSkipped is the variant that surfaces dropped entries
// for telemetry. Most callers should use ClusterByCosine; this exists
// for the notebook scanner where we want to log "n vectors had wrong
// dimension".
func ClusterByCosineWithSkipped(entries []ClusterEntry, threshold float32) (clusters []Cluster, skipped []ClusterEntry) {
	if len(entries) == 0 {
		return nil, nil
	}
	dim := -1
	for _, e := range entries {
		if len(e.Vector) > 0 {
			dim = len(e.Vector)
			break
		}
	}
	if dim <= 0 {
		// Every input was empty — nothing to cluster.
		return nil, entries
	}

	for _, e := range entries {
		if len(e.Vector) != dim {
			skipped = append(skipped, e)
			continue
		}
		bestIdx := -1
		var bestScore float32
		for i := range clusters {
			score := Cosine(clusters[i].Centroid, e.Vector)
			if score >= threshold && score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			centroid := make([]float32, dim)
			copy(centroid, e.Vector)
			clusters = append(clusters, Cluster{
				Entries:  []ClusterEntry{e},
				Centroid: L2Normalise(centroid),
			})
			continue
		}
		clusters[bestIdx].Entries = append(clusters[bestIdx].Entries, e)
		clusters[bestIdx].Centroid = recomputeCentroid(clusters[bestIdx].Entries, dim)
	}
	return clusters, skipped
}

// recomputeCentroid returns the L2-normalised mean of every member's
// vector. We always recompute from scratch so a member with a slightly
// off-axis vector does not pull the centroid permanently. Linear cost,
// but cluster sizes are tiny in practice (≤ 10 entries).
func recomputeCentroid(entries []ClusterEntry, dim int) []float32 {
	if len(entries) == 0 {
		return nil
	}
	mean := make([]float32, dim)
	for _, e := range entries {
		if len(e.Vector) != dim {
			continue
		}
		for i, v := range e.Vector {
			mean[i] += v
		}
	}
	inv := float32(1) / float32(len(entries))
	for i := range mean {
		mean[i] *= inv
	}
	return L2Normalise(mean)
}

// Validate checks a slice of entries for the basic preconditions
// callers expect (non-empty vectors, consistent dimension). Surfaces a
// helpful error so the cluster layer's silent skip doesn't hide
// upstream bugs. Optional — ClusterByCosine tolerates input it can't
// process.
func Validate(entries []ClusterEntry, expectedDim int) error {
	if expectedDim <= 0 {
		return fmt.Errorf("embedding: cluster: expectedDim must be positive, got %d", expectedDim)
	}
	for i, e := range entries {
		if len(e.Vector) != expectedDim {
			return fmt.Errorf("embedding: cluster: entry %d (%q): vector dim %d != expected %d",
				i, e.ID, len(e.Vector), expectedDim)
		}
	}
	return nil
}
