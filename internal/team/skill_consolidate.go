package team

// skill_consolidate.go exposes POST /skills/consolidate for detecting and
// merging overlapping skills. Two modes:
//
//   - dry_run (default): returns detected overlap clusters without mutations.
//   - merge: archives cluster members into a target skill, writes tombstones
//     so the scanner does not re-propose them, and populates RelatedSkills.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// consolidateRequest is the JSON body for POST /skills/consolidate.
type consolidateRequest struct {
	// MergeInto is the slug of the skill that should absorb cluster members.
	// When empty the endpoint only detects clusters (implicit dry_run).
	MergeInto string `json:"merge_into,omitempty"`
	// DryRun when true (or MergeInto is empty) returns clusters without changes.
	DryRun bool `json:"dry_run,omitempty"`
}

// consolidateCluster describes one group of overlapping skills.
type consolidateCluster struct {
	Representative string             `json:"representative"`
	Members        []string           `json:"members"`
	Scores         map[string]float64 `json:"scores,omitempty"`
}

// consolidateResponse is returned by POST /skills/consolidate.
type consolidateResponse struct {
	Clusters []consolidateCluster `json:"clusters"`
	Merged   int                  `json:"merged"`
	Archived int                  `json:"archived"`
}

// handleSkillConsolidate is the HTTP handler for POST /skills/consolidate.
func (b *Broker) handleSkillConsolidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req consolidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	clusters := b.detectSkillClustersLocked()
	b.mu.Unlock()

	resp := consolidateResponse{Clusters: clusters}

	mergeTarget := strings.TrimSpace(req.MergeInto)
	if req.DryRun || mergeTarget == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Merge mode: archive cluster members into the target. We must recompute
	// clusters under the same lock as the merge — concurrent proposals or
	// enhancements could have changed b.skills between detect and merge.
	b.mu.Lock()
	freshClusters := b.detectSkillClustersLocked()
	merged, archived, mergeErr := b.mergeSkillClusterLocked(mergeTarget, freshClusters)
	if mergeErr != nil {
		b.mu.Unlock()
		http.Error(w, mergeErr.Error(), http.StatusBadRequest)
		return
	}

	// Re-render the target skill's wiki markdown after merge.
	var wikiErr error
	if target := b.findSkillByNameLocked(mergeTarget); target != nil {
		slug := skillSlug(mergeTarget)
		fm := teamSkillToFrontmatter(*target)
		mdBytes, renderErr := RenderSkillMarkdown(fm, target.Content)
		if renderErr != nil {
			wikiErr = renderErr
		} else if wikiWorker := b.wikiWorker; wikiWorker != nil {
			wikiPath := "team/skills/" + slug + ".md"
			b.mu.Unlock()
			_, _, wikiErr = wikiWorker.Enqueue(
				context.Background(), slug, wikiPath,
				string(mdBytes), "replace",
				"archivist: consolidate skill "+slug,
			)
			b.mu.Lock()
		}
	}

	saveErr := b.saveLocked()
	b.mu.Unlock()

	if saveErr != nil {
		http.Error(w, "merge succeeded in memory but failed to persist: "+saveErr.Error(), http.StatusInternalServerError)
		return
	}
	if wikiErr != nil {
		// Persisted state is correct but the on-disk markdown is stale.
		// Operators must know consolidation half-succeeded so they can
		// re-render rather than discovering drift on the next agent run.
		slog.Warn("skill_consolidate: wiki write failed after persisted merge", "err", wikiErr)
		http.Error(w,
			"merge persisted but wiki write failed; team/skills/<slug>.md may be stale: "+wikiErr.Error(),
			http.StatusInternalServerError)
		return
	}

	resp.Merged = merged
	resp.Archived = archived
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// detectSkillClustersLocked groups non-archived skills by semantic similarity.
// For each skill, it finds similar skills via findSimilarSkillsLocked and
// builds clusters. Caller MUST hold b.mu.
func (b *Broker) detectSkillClustersLocked() []consolidateCluster {
	// Build adjacency list for all non-archived skills.
	type edge struct {
		target string
		score  float64
	}
	adj := make(map[string][]edge)
	for i := range b.skills {
		sk := &b.skills[i]
		if sk.Status == "archived" {
			continue
		}
		similar := b.findSimilarSkillsLocked(sk.Name, sk.Description)
		for _, sim := range similar {
			adj[sk.Name] = append(adj[sk.Name], edge{sim.Skill.Name, combinedScore(sim)})
			adj[sim.Skill.Name] = append(adj[sim.Skill.Name], edge{sk.Name, combinedScore(sim)})
		}
	}

	// BFS to find connected components (transitive overlaps).
	visited := make(map[string]bool)
	var clusters []consolidateCluster

	for i := range b.skills {
		sk := &b.skills[i]
		if sk.Status == "archived" || visited[sk.Name] || len(adj[sk.Name]) == 0 {
			continue
		}

		// BFS from this skill.
		cluster := consolidateCluster{
			Representative: sk.Name,
			Scores:         make(map[string]float64),
		}
		queue := []string{sk.Name}
		visited[sk.Name] = true

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			for _, e := range adj[current] {
				if visited[e.target] {
					continue
				}
				visited[e.target] = true
				cluster.Members = append(cluster.Members, e.target)
				cluster.Scores[e.target] = e.score
				queue = append(queue, e.target)
			}
		}

		if len(cluster.Members) > 0 {
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

// mergeSkillClusterLocked archives all cluster members that are NOT the target
// skill, writes tombstones, and populates RelatedSkills on the target.
// Returns (merged count, archived count, error). Caller MUST hold b.mu.
func (b *Broker) mergeSkillClusterLocked(targetName string, clusters []consolidateCluster) (int, int, error) {
	target := b.findSkillByNameLocked(targetName)
	if target == nil {
		return 0, 0, errors.New("target skill not found: " + targetName)
	}

	// Find the cluster that contains the target.
	var targetCluster *consolidateCluster
	for i := range clusters {
		if clusters[i].Representative == targetName {
			targetCluster = &clusters[i]
			break
		}
		for _, member := range clusters[i].Members {
			if member == targetName {
				targetCluster = &clusters[i]
				break
			}
		}
		if targetCluster != nil {
			break
		}
	}

	if targetCluster == nil {
		return 0, 0, errors.New("target skill not in any cluster")
	}

	// Collect all skills to archive: everyone in the cluster except the target.
	var toArchive []string
	if targetCluster.Representative != targetName {
		toArchive = append(toArchive, targetCluster.Representative)
	}
	for _, member := range targetCluster.Members {
		if member != targetName {
			toArchive = append(toArchive, member)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	merged := 0
	archived := 0

	for _, name := range toArchive {
		sk := b.findSkillByNameLocked(name)
		if sk == nil {
			continue
		}

		// Populate RelatedSkills on target.
		target.RelatedSkills = appendUnique(target.RelatedSkills, skillSlug(name))

		// Merge tags.
		for _, tag := range sk.Tags {
			target.Tags = appendUnique(target.Tags, tag)
		}

		// Merge content: append unique details from the member skill.
		if memberContent := strings.TrimSpace(sk.Content); memberContent != "" {
			target.Content = mergeSkillContent(target.Content, memberContent, name)
		}

		// Archive the member.
		sk.Status = "archived"
		sk.UpdatedAt = now
		archived++

		// Write tombstone so scanner doesn't re-propose.
		if err := b.appendSkillTombstoneLocked(SkillTombstoneEntry{
			Slug:          skillSlug(name),
			SourceArticle: sk.SourceArticle,
			RejectedAt:    now,
			Reason:        "consolidated into " + targetName,
		}); err != nil {
			slog.Warn("skill_consolidate: tombstone write failed",
				"slug", name, "err", err)
		}

		merged++
	}

	target.UpdatedAt = now

	slog.Info("skill_consolidate: merge complete",
		"target", targetName, "merged", merged, "archived", archived)
	return merged, archived, nil
}

// mergeSkillContent appends member content to the target body under a
// consolidated section header. Skips if the member content is identical to
// the target (pure duplicate).
func mergeSkillContent(targetContent, memberContent, memberSlug string) string {
	target := strings.TrimSpace(targetContent)
	member := strings.TrimSpace(memberContent)
	if member == "" || member == target {
		return target
	}
	header := "\n\n## Consolidated from " + memberSlug + "\n\n"
	return target + header + member
}

// ── One-time auto-consolidation migration ────────────────────────────────

// skillConsolidationMigrationSentinel is the wiki-relative path of the file
// that marks a completed auto-consolidation. Uses .md extension to pass
// validateArticlePath. The skill scanner skips team/skills/.system/.
const skillConsolidationMigrationSentinel = "team/skills/.system/migrated-skill-consolidation.md"

// autoConsolidateSkillsIfNeeded runs once at broker startup. It detects
// overlapping skill clusters and merges them, picking the oldest (first-created)
// skill in each cluster as the representative. A sentinel file prevents
// re-running on subsequent startups.
//
// The caller MUST hold b.mu on entry. The method follows the same
// unlock/relock pattern as other migrations for WikiWorker.Enqueue calls.
func (b *Broker) autoConsolidateSkillsIfNeeded() {
	wikiWorker := b.wikiWorker

	// Check sentinel — skip if already ran.
	if wikiWorker != nil {
		if repo := wikiWorker.Repo(); repo != nil {
			sentinelPath := filepath.Join(repo.Root(), filepath.FromSlash(skillConsolidationMigrationSentinel))
			if _, err := os.Stat(sentinelPath); err == nil {
				return // Already ran.
			}
		}
	}

	clusters := b.detectSkillClustersLocked()
	if len(clusters) == 0 {
		slog.Info("skill_consolidation_migration: no overlapping clusters found")
		b.writeConsolidationSentinelLocked(0, 0)
		return
	}

	totalMerged := 0
	totalArchived := 0
	wikiWriteFailed := false

	for _, cluster := range clusters {
		// Pick the representative as the merge target (it's the first
		// skill encountered in b.skills order, typically the oldest).
		target := cluster.Representative
		merged, archived, err := b.mergeSkillClusterLocked(target, []consolidateCluster{cluster})
		if err != nil {
			slog.Warn("skill_consolidation_migration: merge failed",
				"target", target, "err", err)
			continue
		}
		totalMerged += merged
		totalArchived += archived

		// Re-render the target skill to wiki (only when wiki worker is available).
		if wikiWorker != nil {
			sk := b.findSkillByNameLocked(target)
			if sk != nil {
				slug := skillSlug(target)
				fm := teamSkillToFrontmatter(*sk)
				mdBytes, renderErr := RenderSkillMarkdown(fm, sk.Content)
				if renderErr != nil {
					slog.Warn("skill_consolidation_migration: render failed, aborting",
						"slug", slug, "err", renderErr)
					wikiWriteFailed = true
					continue
				}
				wikiPath := "team/skills/" + slug + ".md"
				b.mu.Unlock()
				_, _, enqErr := wikiWorker.Enqueue(
					context.Background(),
					slug,
					wikiPath,
					string(mdBytes),
					"replace",
					"archivist: auto-consolidate skill "+slug,
				)
				b.mu.Lock()
				if enqErr != nil {
					slog.Warn("skill_consolidation_migration: wiki write failed, aborting",
						"slug", slug, "err", enqErr)
					wikiWriteFailed = true
				}
			}
		}
	}

	if err := b.saveLocked(); err != nil {
		slog.Warn("skill_consolidation_migration: saveLocked failed, skipping sentinel", "err", err)
		// Do NOT write sentinel — next startup should retry.
		return
	}

	if wikiWriteFailed {
		slog.Warn("skill_consolidation_migration: wiki write(s) failed, skipping sentinel for retry")
		return
	}

	b.writeConsolidationSentinelLocked(totalMerged, totalArchived)

	slog.Info("skill_consolidation_migration: completed",
		"clusters", len(clusters), "merged", totalMerged, "archived", totalArchived)
}

// writeConsolidationSentinelLocked writes the sentinel file. Caller MUST hold b.mu.
func (b *Broker) writeConsolidationSentinelLocked(merged, archived int) {
	wikiWorker := b.wikiWorker
	if wikiWorker == nil {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	content := fmt.Sprintf("auto-consolidation completed on %s\n\nmerged: %d archived: %d\n", ts, merged, archived)

	b.mu.Unlock()
	if _, _, err := wikiWorker.Enqueue(
		context.Background(),
		".system",
		skillConsolidationMigrationSentinel,
		content,
		"replace",
		"archivist: skill consolidation migration sentinel",
	); err != nil {
		slog.Warn("skill_consolidation_migration: failed to write sentinel", "err", err)
	}
	b.mu.Lock()
}
