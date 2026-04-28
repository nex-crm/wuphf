package team

// skill_tombstone.go manages the append-only rejected-skill log at
// team/skills/.rejected.md. The tombstone prevents the scanner from
// re-proposing skills that the team has explicitly declined.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// skillTombstonePath is the wiki-relative path of the YAML-body markdown file
// that records rejected skill proposals. The .md extension is required to pass
// validateArticlePath in wiki_git.go. The skill scanner skips team/skills/.rejected.md
// by name so it is never promoted as a skill proposal.
const skillTombstonePath = "team/skills/.rejected.md"

// SkillTombstoneEntry records a single rejected skill proposal.
type SkillTombstoneEntry struct {
	// Slug is the skill's normalised name slug.
	Slug string `yaml:"slug"`
	// SourceArticle is the wiki path that triggered the proposal, if any.
	SourceArticle string `yaml:"source_article,omitempty"`
	// RejectedAt is an RFC3339 timestamp of when the rejection occurred.
	RejectedAt string `yaml:"rejected_at"`
	// Reason is a human-readable explanation (e.g. "rejected by guard: dangerous").
	Reason string `yaml:"reason,omitempty"`
}

// tombstoneFile is the on-disk YAML wrapper.
type tombstoneFile struct {
	Rejected []SkillTombstoneEntry `yaml:"rejected"`
}

// loadSkillTombstoneLocked reads the tombstone file from disk and returns the
// list of rejected entries. Caller MUST hold b.mu. Missing file returns empty
// list without error. Malformed file logs a warning and returns empty.
func (b *Broker) loadSkillTombstoneLocked() ([]SkillTombstoneEntry, error) {
	wikiWorker := b.wikiWorker
	if wikiWorker == nil {
		return nil, nil
	}

	repo := wikiWorker.Repo()
	if repo == nil {
		return nil, nil
	}

	// TODO(skill-compile): use a wikiWorker.ReadArticle helper once it is
	// extended to handle non-catalog paths, or add a dedicated Repo.ReadRaw.
	// For now, read directly from the filesystem using the known root path.
	fsPath := filepath.Join(repo.Root(), filepath.FromSlash(skillTombstonePath))
	raw, err := os.ReadFile(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		slog.Warn("skill_tombstone: failed to read tombstone file",
			"path", fsPath, "err", err)
		return nil, nil
	}

	var tf tombstoneFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		slog.Warn("skill_tombstone: malformed tombstone YAML, treating as empty",
			"path", fsPath, "err", err)
		return nil, nil
	}
	return tf.Rejected, nil
}

// appendSkillTombstoneLocked appends entry to the tombstone file, loading the
// current list first and writing the updated list back via WikiWorker.Enqueue.
// Caller MUST hold b.mu. Releases and re-acquires b.mu around Enqueue.
func (b *Broker) appendSkillTombstoneLocked(entry SkillTombstoneEntry) error {
	if entry.RejectedAt == "" {
		entry.RejectedAt = time.Now().UTC().Format(time.RFC3339)
	}

	existing, _ := b.loadSkillTombstoneLocked()
	updated := append(existing, entry)

	tf := tombstoneFile{Rejected: updated}
	raw, err := yaml.Marshal(tf)
	if err != nil {
		return fmt.Errorf("skill_tombstone: yaml marshal: %w", err)
	}

	// Wrap YAML in minimal markdown so it passes validateArticlePath.
	content := string(raw)

	wikiWorker := b.wikiWorker
	if wikiWorker == nil {
		slog.Warn("skill_tombstone: no wiki worker, tombstone not persisted",
			"slug", entry.Slug)
		return nil
	}

	// Release lock before Enqueue to avoid deadlock with PublishWikiEvent.
	b.mu.Unlock()
	_, _, enqErr := wikiWorker.Enqueue(
		context.Background(),
		".rejected",
		skillTombstonePath,
		content,
		"replace",
		"archivist: tombstone skill "+entry.Slug,
	)
	b.mu.Lock()

	if enqErr != nil {
		return fmt.Errorf("skill_tombstone: enqueue for %q: %w", entry.Slug, enqErr)
	}
	return nil
}
