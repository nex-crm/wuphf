package team

// skill_dedup.go implements semantic skill deduplication. The write funnel
// (writeSkillProposalLocked) calls findSimilarSkillsLocked before creating a
// new skill. Three tiers of comparison are applied in order:
//
//   - Tier 1: Jaro-Winkler on slugs (zero cost, reuses jaro_winkler.go)
//   - Tier 2: Jaro-Winkler on descriptions (zero cost)
//   - Tier 3: Cosine similarity on description embeddings (optional, gracefully
//     skipped when no embedding provider is configured)
//
// buildExistingSkillsSummary produces a compact text block listing all active
// skills, injected into LLM prompts so the model can self-deduplicate.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nex-crm/wuphf/internal/embedding"
)

// Skill dedup thresholds — configurable via env.
var (
	defaultSkillDedupSlugThreshold  = 0.85
	defaultSkillDedupDescThreshold  = 0.80
	defaultSkillDedupEmbedThreshold = float32(0.85)
)

// skillSimilarityResult records why a candidate matched an existing skill.
type skillSimilarityResult struct {
	Skill     *teamSkill
	SlugScore float64 // Jaro-Winkler on slug (tier 1)
	DescScore float64 // Jaro-Winkler on description (tier 2)
	EmbedCos  float32 // Cosine similarity on embeddings (tier 3)
	Tier      int     // which tier triggered (1, 2, or 3)
}

// skillDedupEnabled returns false when WUPHF_SKILL_DEDUP_ENABLED=0.
func skillDedupEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("WUPHF_SKILL_DEDUP_ENABLED")))
	switch raw {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func skillDedupSlugThreshold() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("WUPHF_SKILL_DEDUP_SLUG_THRESHOLD"), 64); err == nil && v > 0 && v <= 1 {
		return v
	}
	return defaultSkillDedupSlugThreshold
}

func skillDedupDescThreshold() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("WUPHF_SKILL_DEDUP_DESC_THRESHOLD"), 64); err == nil && v > 0 && v <= 1 {
		return v
	}
	return defaultSkillDedupDescThreshold
}

func skillDedupEmbedThreshold() float32 {
	if v, err := strconv.ParseFloat(os.Getenv("WUPHF_SKILL_DEDUP_EMBED_THRESHOLD"), 64); err == nil && v > 0 && v <= 1 {
		return float32(v)
	}
	return defaultSkillDedupEmbedThreshold
}

// findSimilarSkillsLocked checks a candidate against all non-archived skills
// using a tiered similarity strategy. Caller MUST hold b.mu.
//
// Returns matching skills sorted by combined score (highest first), or nil
// when no match exceeds the configured thresholds.
func (b *Broker) findSimilarSkillsLocked(candidateName, candidateDesc string) []skillSimilarityResult {
	if !skillDedupEnabled() {
		return nil
	}

	candidateSlug := skillSlug(candidateName)
	candidateDescLower := strings.ToLower(strings.TrimSpace(candidateDesc))
	slugThresh := skillDedupSlugThreshold()
	descThresh := skillDedupDescThreshold()

	var results []skillSimilarityResult

	for i := range b.skills {
		sk := &b.skills[i]
		if sk.Status == "archived" {
			continue
		}

		existingSlug := skillSlug(sk.Name)
		// Skip exact slug match — handled by findSkillByNameLocked already.
		if existingSlug == candidateSlug {
			continue
		}

		existingDescLower := strings.ToLower(strings.TrimSpace(sk.Description))

		// Tier 1: Jaro-Winkler on slugs.
		slugScore := JaroWinkler(candidateSlug, existingSlug)
		if slugScore >= slugThresh {
			results = append(results, skillSimilarityResult{
				Skill:     sk,
				SlugScore: slugScore,
				Tier:      1,
			})
			continue
		}

		// Tier 2: Jaro-Winkler on descriptions.
		if candidateDescLower != "" && existingDescLower != "" {
			descScore := JaroWinkler(candidateDescLower, existingDescLower)
			if descScore >= descThresh {
				results = append(results, skillSimilarityResult{
					Skill:     sk,
					SlugScore: slugScore,
					DescScore: descScore,
					Tier:      2,
				})
				continue
			}
		}
	}

	// Tier 3: Embedding cosine similarity (only when no tier 1/2 matches
	// found and an embedding provider is available).
	if len(results) == 0 {
		results = b.findSimilarByEmbeddingLocked(candidateSlug, candidateDescLower)
	}

	if len(results) == 0 {
		return nil
	}

	// Sort by tier (lower = stronger), then by score within tier.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Tier != results[j].Tier {
			return results[i].Tier < results[j].Tier
		}
		return combinedScore(results[i]) > combinedScore(results[j])
	})
	return results
}

func combinedScore(r skillSimilarityResult) float64 {
	switch r.Tier {
	case 1:
		return r.SlugScore
	case 2:
		return r.DescScore
	case 3:
		return float64(r.EmbedCos)
	}
	return 0
}

// findSimilarByEmbeddingLocked performs tier 3 cosine similarity using the
// embedding provider. Caller MUST hold b.mu. Returns nil when no provider is
// available or no match exceeds the threshold.
func (b *Broker) findSimilarByEmbeddingLocked(candidateSlug, candidateDescLower string) []skillSimilarityResult {
	if candidateDescLower == "" {
		return nil
	}

	provider := b.skillEmbeddingProviderLocked()
	if provider == nil {
		return nil
	}

	// Embed the candidate description (releases lock for I/O).
	b.mu.Unlock()
	candidateVec, err := provider.Embed(context.Background(), candidateDescLower)
	b.mu.Lock()
	if err != nil {
		slog.Debug("skill_dedup: embedding candidate failed", "err", err)
		return nil
	}

	threshold := skillDedupEmbedThreshold()
	var results []skillSimilarityResult

	for i := range b.skills {
		sk := &b.skills[i]
		if sk.Status == "archived" {
			continue
		}
		existingSlug := skillSlug(sk.Name)
		if existingSlug == candidateSlug {
			continue
		}

		existingVec := b.ensureSkillEmbeddingLocked(existingSlug, sk.Description, provider)
		if existingVec == nil {
			continue
		}

		cos := embedding.Cosine(candidateVec, existingVec)
		if cos >= threshold {
			results = append(results, skillSimilarityResult{
				Skill:    sk,
				EmbedCos: cos,
				Tier:     3,
			})
		}
	}

	return results
}

// skillEmbeddingProviderLocked returns the embedding provider or nil. The
// provider is usable when it is not the local-stub (stubs produce non-semantic
// vectors). Caller MUST hold b.mu.
func (b *Broker) skillEmbeddingProviderLocked() embedding.Provider {
	provider := embedding.NewDefault()
	if provider == nil || provider.Name() == "local-stub" {
		return nil
	}
	return provider
}

// ensureSkillEmbeddingLocked returns a cached embedding vector for a skill
// description, computing it on cache miss. Caller MUST hold b.mu.
func (b *Broker) ensureSkillEmbeddingLocked(slug, description string, provider embedding.Provider) []float32 {
	if b.skillDescEmbeddings == nil {
		b.skillDescEmbeddings = make(map[string][]float32)
	}
	if vec, ok := b.skillDescEmbeddings[slug]; ok {
		atomic.AddInt64(&b.skillCompileMetrics.EmbeddingCacheHitsTotal, 1)
		return vec
	}

	desc := strings.ToLower(strings.TrimSpace(description))
	if desc == "" {
		return nil
	}

	// Release lock for the I/O call.
	b.mu.Unlock()
	atomic.AddInt64(&b.skillCompileMetrics.EmbeddingCallsTotal, 1)
	vec, err := provider.Embed(context.Background(), desc)
	b.mu.Lock()
	if err != nil {
		atomic.AddInt64(&b.skillCompileMetrics.EmbeddingCacheMissesTotal, 1)
		slog.Debug("skill_dedup: embedding skill description failed", "slug", slug, "err", err)
		return nil
	}
	atomic.AddInt64(&b.skillCompileMetrics.EmbeddingCacheMissesTotal, 1)
	b.skillDescEmbeddings[slug] = vec
	return vec
}

// invalidateSkillEmbeddingLocked removes a cached description embedding.
// Called when a skill's description changes. Caller MUST hold b.mu.
func (b *Broker) invalidateSkillEmbeddingLocked(slug string) {
	if b.skillDescEmbeddings != nil {
		delete(b.skillDescEmbeddings, slug)
	}
}

// buildExistingSkillsSummary produces a compact text block listing all
// non-archived skills with their slug and description. Capped at maxBytes
// to avoid prompt bloat. Used by Stage A and Stage B LLM prompts so the
// model can self-deduplicate.
func buildExistingSkillsSummary(skills []teamSkill, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 2048
	}

	var b strings.Builder
	b.WriteString("EXISTING SKILLS (if a new skill is an exact duplicate, respond is_skill=false; if it adds new details to an existing skill, respond with is_skill=true and enhance=<existing-slug>):\n")

	for _, sk := range skills {
		if sk.Status == "archived" {
			continue
		}
		line := fmt.Sprintf("- %s: %s\n", sk.Name, strings.TrimSpace(sk.Description))
		if b.Len()+len(line) > maxBytes {
			b.WriteString("- ... (truncated)\n")
			break
		}
		b.WriteString(line)
	}

	return b.String()
}
