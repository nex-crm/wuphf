package team

// skill_migration.go handles the one-time migration of existing in-memory
// broker skills to the Anthropic frontmatter format on disk. The migration
// is idempotent: a sentinel file at team/skills/.system/migrated-anthropic-frontmatter.md
// is written when the migration completes. Subsequent broker startups read
// the sentinel and skip re-migration.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// skillFrontmatterMigrationSentinel is the wiki path of the sentinel file that
// marks a completed Anthropic frontmatter migration. It uses the .md extension
// to pass validateArticlePath in wiki_git.go. The skill scanner explicitly
// skips team/skills/.system/ so this file is never promoted as a skill.
const skillFrontmatterMigrationSentinel = "team/skills/.system/migrated-anthropic-frontmatter.md"

// migrateSkillFrontmatterIfNeededLocked performs a one-time migration of all
// in-memory skills to disk as Anthropic-format markdown files. The caller MUST
// hold b.mu. Releases and re-acquires b.mu around each WikiWorker.Enqueue call
// to avoid the deadlock described in writeSkillProposalLocked.
//
// Steps:
//  1. Check whether the sentinel already exists on disk — if so, return immediately.
//  2. For each skill in b.skills, render Anthropic frontmatter + content and
//     write to team/skills/{slug}.md via WikiWorker.Enqueue. Skip-and-log failures.
//  3. Write the sentinel via WikiWorker.Enqueue.
func (b *Broker) migrateSkillFrontmatterIfNeededLocked() error {
	wikiWorker := b.wikiWorker
	if wikiWorker == nil {
		// Not running with a markdown backend — nothing to migrate.
		return nil
	}

	// --- Step 1: Check sentinel ---
	// Use os.Stat against the wiki root for a fast existence check without
	// going through the write queue.
	// TODO(skill-compile): expose wikiWorker.Repo().Root() publicly or via
	// a dedicated method so this does not reach into the Repo unexpectedly.
	// For now, compute the path the same way WikiRootDir() does.
	repo := wikiWorker.Repo()
	if repo != nil {
		sentinelPath := filepath.Join(repo.Root(), filepath.FromSlash(skillFrontmatterMigrationSentinel))
		if _, err := os.Stat(sentinelPath); err == nil {
			// Sentinel exists; migration already ran.
			return nil
		}
	}

	slog.Info("skill_migration: starting Anthropic frontmatter migration",
		"skill_count", len(b.skills))

	skipped := 0
	written := 0

	// Take a snapshot so we can iterate without holding the lock continuously.
	snapshot := make([]teamSkill, len(b.skills))
	copy(snapshot, b.skills)

	b.mu.Unlock()

	for _, sk := range snapshot {
		slug := skillSlug(sk.Name)
		if !skillSlugRegex.MatchString(slug) {
			slog.Warn("skill_migration: skipping skill with invalid slug",
				"name", sk.Name, "slug", slug)
			skipped++
			continue
		}
		if strings.TrimSpace(sk.Trigger) == "" && strings.TrimSpace(sk.Description) == "" {
			slog.Warn("skill_migration: skipping skill with empty trigger and description",
				"name", sk.Name)
			skipped++
			continue
		}

		fm := teamSkillToFrontmatter(sk)
		mdBytes, err := RenderSkillMarkdown(fm, sk.Content)
		if err != nil {
			slog.Warn("skill_migration: render failed, skipping",
				"name", sk.Name, "err", err)
			skipped++
			continue
		}

		wikiPath := "team/skills/" + slug + ".md"
		_, _, err = wikiWorker.Enqueue(
			context.Background(),
			slug,
			wikiPath,
			string(mdBytes),
			"replace",
			"archivist: skill frontmatter migration — "+slug,
		)
		if err != nil {
			slog.Warn("skill_migration: enqueue failed, skipping",
				"name", sk.Name, "err", err)
			skipped++
			continue
		}
		written++
	}

	// --- Step 3: Write sentinel ---
	ts := time.Now().UTC().Format(time.RFC3339)
	sentinelContent := fmt.Sprintf("migrated by broker on %s\n\nskipped: %d written: %d\n", ts, skipped, written)
	if _, _, err := wikiWorker.Enqueue(
		context.Background(),
		".system",
		skillFrontmatterMigrationSentinel,
		sentinelContent,
		"replace",
		"archivist: skill frontmatter migration sentinel",
	); err != nil {
		slog.Warn("skill_migration: failed to write sentinel", "err", err)
		// Non-fatal: next startup will re-run the migration (idempotent).
	}

	b.mu.Lock()

	slog.Info("skill_migration: completed Anthropic frontmatter migration",
		"written", written, "skipped", skipped)
	return nil
}
