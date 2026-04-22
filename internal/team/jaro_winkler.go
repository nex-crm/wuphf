package team

// jaro_winkler.go — inline Jaro-Winkler string similarity for Slice 2 predicate dedup.
//
// Slice 2 scope: wire JaroWinkler into UpsertFact for predicate dedup when
// similarity ≥ 0.9. This file ships the primitive and tests so Slice 2 has no
// new dependency to resolve.
//
// Reference: Winkler, W.E. (1990). "String Comparator Metrics and Enhanced
// Decision Rules in the Fellegi-Sunter Model of Record Linkage."

import "math"

// JaroWinkler returns the Jaro-Winkler similarity in [0.0, 1.0] between a and b.
// Higher = more similar; 1.0 = identical, 0.0 = no similarity.
//
// The scaling factor p is fixed at 0.1 (Winkler's standard). The prefix bonus
// is capped at 4 characters per the original definition.
func JaroWinkler(a, b string) float64 {
	if a == b {
		return 1.0
	}
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 || lb == 0 {
		return 0.0
	}

	jaro := jaroSimilarity(ra, rb)

	// Count common prefix (max 4 runes).
	prefix := 0
	maxPrefix := int(math.Min(4, math.Min(float64(la), float64(lb))))
	for i := 0; i < maxPrefix; i++ {
		if ra[i] == rb[i] {
			prefix++
		} else {
			break
		}
	}

	// Winkler adjustment: jaro + prefix * p * (1 - jaro), p = 0.1.
	return jaro + float64(prefix)*0.1*(1.0-jaro)
}

// jaroSimilarity returns the raw Jaro similarity between rune slices a and b.
func jaroSimilarity(a, b []rune) float64 {
	la, lb := len(a), len(b)
	matchDist := int(math.Max(float64(la), float64(lb)))/2 - 1
	if matchDist < 0 {
		matchDist = 0
	}

	matchedA := make([]bool, la)
	matchedB := make([]bool, lb)

	var matches int
	for i := 0; i < la; i++ {
		lo := int(math.Max(0, float64(i-matchDist)))
		hi := int(math.Min(float64(lb-1), float64(i+matchDist)))
		for j := lo; j <= hi; j++ {
			if matchedB[j] || a[i] != b[j] {
				continue
			}
			matchedA[i] = true
			matchedB[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0.0
	}

	// Count transpositions (half the number of matching characters that are
	// not in the same order).
	var transpositions int
	k := 0
	for i := 0; i < la; i++ {
		if !matchedA[i] {
			continue
		}
		for !matchedB[k] {
			k++
		}
		if a[i] != b[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	return (m/float64(la) + m/float64(lb) + (m-float64(transpositions)/2.0)/m) / 3.0
}
