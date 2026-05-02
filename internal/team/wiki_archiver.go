package team

// wiki_archiver.go — WikiArchiver.Sweep moves zero-read articles older than
// the cutoff to .archive/ and replaces them with tombstones.
//
// Design: docs/specs/wiki-archival-icp-examples.md

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultArchiveCutoffDays is the number of days an article must have been
	// unread before it is eligible for archival.
	DefaultArchiveCutoffDays = 90

	// archiveMinWordCount is the minimum word count for archival eligibility.
	// Stubs below this threshold are left in place — they may still accumulate
	// facts and get synthesized later.
	archiveMinWordCount = 50
)

// SweepResult summarises a single WikiArchiver.Sweep run.
type SweepResult struct {
	Archived int `json:"archived"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
}

// WikiArchiver sweeps team/ for stale articles and moves them to .archive/.
// nil readLog means every article is treated as never-read.
type WikiArchiver struct {
	repo    *Repo
	readLog *ReadLog
	cutoff  time.Duration
}

type archiveCandidate struct {
	relPath string
	content []byte
}

// NewWikiArchiver returns an archiver. cutoff=0 uses DefaultArchiveCutoffDays.
func NewWikiArchiver(repo *Repo, readLog *ReadLog, cutoff time.Duration) *WikiArchiver {
	if cutoff <= 0 {
		cutoff = DefaultArchiveCutoffDays * 24 * time.Hour
	}
	return &WikiArchiver{repo: repo, readLog: readLog, cutoff: cutoff}
}

// Sweep walks team/ and archives every eligible article.
//
// Eligibility (all must hold):
//  1. File age ≥ cutoff (oldest commit via commitBoundsByPath)
//  2. Last read ≥ cutoff days ago (or never read at all)
//  3. Word count ≥ archiveMinWordCount (skip stubs)
//  4. Not already a tombstone (frontmatter archived: true)
func (a *WikiArchiver) Sweep(ctx context.Context) (SweepResult, error) {
	bounds, err := a.repo.commitBoundsByPath(ctx)
	if err != nil {
		return SweepResult{}, fmt.Errorf("wiki archive: commitBoundsByPath: %w", err)
	}

	var readStats map[string]ReadStats
	if a.readLog != nil {
		readStats = a.readLog.AllStats()
	}

	cutoffAgo := time.Now().UTC().Add(-a.cutoff)
	cutoffDays := int(a.cutoff / (24 * time.Hour))

	teamDir := filepath.Join(a.repo.Root(), "team")
	var toArchive []archiveCandidate
	var skipped int

	walkErr := filepath.WalkDir(teamDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		// Skip hidden entries and non-catalog subtrees (inbox = raw ingests,
		// skills = generated output). These are not lifecycle-managed articles.
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "inbox" || name == "skills" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, relErr := filepath.Rel(a.repo.Root(), path)
		if relErr != nil {
			return nil //nolint:nilerr // non-fatal: skip unresolvable path
		}
		rel = filepath.ToSlash(rel)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr // non-fatal: skip unreadable file (race with delete)
		}

		// Already archived — not counted as skipped (not active content).
		if parseFrontmatterBool(string(data), "archived") {
			return nil
		}

		// Word count too low — article is a stub, leave it in place.
		if countWords(data) < archiveMinWordCount {
			skipped++
			return nil
		}

		// Article has been edited too recently — use the most recent commit
		// (Latest), not the first (Oldest), so a page updated yesterday is
		// never archived even if it was first created years ago.
		b, ok := bounds[rel]
		if !ok || b.Latest.Timestamp.IsZero() || b.Latest.Timestamp.After(cutoffAgo) {
			skipped++
			return nil
		}

		// Article was read within the cutoff window (inclusive: read == cutoff disqualifies).
		if readStats != nil {
			if s, ok := readStats[rel]; ok && s.LastRead != nil {
				if !s.LastRead.Before(cutoffAgo) {
					skipped++
					return nil
				}
			}
		}
		// Never read (nil LastRead) counts as unread since first commit —
		// falls through to archive.

		// Capture content here to avoid a second read inside archiveOne,
		// which prevents TOCTOU if the WikiWorker modifies the file between
		// the eligibility check and the commit.
		toArchive = append(toArchive, archiveCandidate{relPath: rel, content: data})
		return nil
	})
	if walkErr != nil {
		return SweepResult{}, fmt.Errorf("wiki archive: walk: %w", walkErr)
	}

	var result SweepResult
	result.Skipped = skipped
	for _, c := range toArchive {
		if err := a.archiveOne(ctx, c.relPath, c.content, cutoffDays); err != nil {
			log.Printf("wiki archive: archive %s: %v", c.relPath, err)
			result.Errors++
			continue
		}
		result.Archived++
	}
	return result, nil
}

// archiveOne moves a single article to .archive/. content is the file bytes
// captured during the eligibility walk — passing it avoids a second read and
// eliminates the TOCTOU window where the WikiWorker could modify the file
// between the eligibility check and this commit.
func (a *WikiArchiver) archiveOne(ctx context.Context, relPath string, content []byte, cutoffDays int) error {
	now := time.Now().UTC()
	archivePath := ".archive/" + relPath

	tombstone := fmt.Sprintf("---\narchived: true\narchived_at: %s\narchive_path: %s\n---\n\n*This article was archived on %s. It had not been accessed in %d+ days.*\n\nThe full content is preserved at `%s`.\n",
		now.Format(time.RFC3339),
		archivePath,
		now.Format("2006-01-02"),
		cutoffDays,
		archivePath,
	)

	msg := fmt.Sprintf("archivist: archive %s (unread %d+ days)", relPath, cutoffDays)
	if _, err := a.repo.CommitArchive(ctx, relPath, tombstone, archivePath, string(content), msg); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
