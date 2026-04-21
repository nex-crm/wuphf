package migration

// writer.go owns the wiki-side half of the migration: given a stream of
// MigrationRecord, render each into a standard article, resolve path
// conflicts, and commit via the existing team.WikiWorker so every write
// fires the same SSE event and index-regen pipeline a human edit would.
//
// Commit identity
// ===============
//
// The writer passes MigrateAuthor ("migrate") as the author slug. The
// wiki git layer derives the commit author as `migrate <migrate@wuphf.local>`
// via runGitLocked's identity flags. That keeps migration commits visually
// distinct from human / archivist / agent commits in audit views, and
// matches the CLI spec without any changes to runGitLocked itself.
//
// Dedup
// =====
//
// If team/{kind}/{slug}.md already exists with byte-identical content,
// the record is skipped. If the existing content differs, the record is
// written to a disambiguated path team/{kind}/{slug}-from-{source}-{ts}.md
// and a warning is emitted so a human can reconcile later.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MigrateAuthor is the commit-author slug for every migrated article.
// Yields `migrate <migrate@wuphf.local>` via Repo.runGitLocked's
// identity derivation, in line with the HumanAuthor / ArchivistAuthor
// pattern in internal/team/. Defined here (not in internal/team/) so
// the migration package owns its own identity without editing files
// reserved for sibling agents.
const MigrateAuthor = "migrate"

// WikiWriter is the slice of team.WikiWorker this package needs. Kept
// narrow so tests can drop in an in-memory fake without spinning up the
// full wiki git repo.
type WikiWriter interface {
	// Enqueue mirrors team.WikiWorker.Enqueue(slug, path, content, mode, commitMsg).
	Enqueue(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error)
	// Root returns the wiki root on disk so the writer can check for
	// existing articles (for dedup) without re-implementing the commit
	// serialization protocol.
	Root() string
}

// Plan is a dry-run output row. One per record that would be written.
type Plan struct {
	// Path is the wiki-relative path (team/{kind}/{slug}.md).
	Path string
	// Bytes is the rendered article size in bytes.
	Bytes int
	// Author is the commit author slug — always MigrateAuthor for now,
	// but kept on the row for symmetry with the audit log output.
	Author string
	// Action is "create", "skip-identical", or "collision-rename".
	Action string
	// Source is the adapter that produced the record.
	Source string
	// CollisionWith is populated when Action == "collision-rename" — it
	// carries the base path that already existed with different content.
	CollisionWith string
}

// Migrator orchestrates adapter → writer. Zero value is not usable;
// call NewMigrator.
type Migrator struct {
	writer WikiWriter
	now    func() time.Time
	// Stdout / Stderr streams let the CLI capture output for tests.
	Stdout io.Writer
	Stderr io.Writer
}

// NewMigrator returns a Migrator wired up to the given writer.
func NewMigrator(writer WikiWriter) *Migrator {
	return &Migrator{
		writer: writer,
		now:    NexNow,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// RunOptions controls a single migration run.
type RunOptions struct {
	// DryRun prints the plan and makes no commits.
	DryRun bool
	// Limit caps the number of records processed. Zero means unlimited.
	Limit int
}

// Summary aggregates a completed migration run. Returned by Run so the
// CLI can print a one-liner and exit with a meaningful code.
type Summary struct {
	Written    int
	Skipped    int
	Collisions int
	Plans      []Plan
}

// Run drains the adapter, renders each record, and (unless DryRun is
// set) commits it via the WikiWorker. Collisions are renamed with a
// source + timestamp suffix. Per-record errors are logged and the
// migration continues — a bad row must not block the rest.
func (m *Migrator) Run(ctx context.Context, adapter Adapter, opts RunOptions) (Summary, error) {
	ch, err := adapter.Iter(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("migrate: adapter iter: %w", err)
	}
	var summary Summary
	processed := 0
	for rec := range ch {
		if opts.Limit > 0 && processed >= opts.Limit {
			// Drain the rest of the channel so the adapter goroutine
			// doesn't block on a full buffer. We ignore the dropped
			// records — the user asked for a bounded run.
			go func() {
				for range ch {
				}
			}()
			break
		}
		processed++
		plan, err := m.planRecord(rec)
		if err != nil {
			fmt.Fprintf(m.Stderr, "warning: skip %s/%s: %v\n", rec.Kind, rec.Slug, err)
			continue
		}
		summary.Plans = append(summary.Plans, plan)
		switch plan.Action {
		case "skip-identical":
			summary.Skipped++
			continue
		case "collision-rename":
			summary.Collisions++
		}
		if opts.DryRun {
			continue
		}
		commitMsg := fmt.Sprintf("migrate: import %s/%s from %s", rec.Kind, rec.Slug, rec.Source)
		if _, _, werr := m.writer.Enqueue(ctx, MigrateAuthor, plan.Path, renderArticle(rec, m.now()), "create", commitMsg); werr != nil {
			fmt.Fprintf(m.Stderr, "warning: write %s: %v\n", plan.Path, werr)
			continue
		}
		summary.Written++
	}
	return summary, nil
}

// planRecord determines the on-disk action for one record: skip, create,
// or create-with-rename. Reads the existing article bytes directly from
// the wiki root — the worker has exclusive write access to the repo,
// but reads are safe here because we only take file stat + content, not
// git metadata.
func (m *Migrator) planRecord(rec MigrationRecord) (Plan, error) {
	if strings.TrimSpace(rec.Slug) == "" {
		return Plan{}, fmt.Errorf("empty slug")
	}
	if strings.TrimSpace(rec.Content) == "" {
		return Plan{}, fmt.Errorf("empty content")
	}
	kind := rec.Kind
	if kind == "" {
		kind = KindMisc
	}
	slug := slugify(rec.Slug)
	if slug == "" {
		return Plan{}, fmt.Errorf("slug contains no safe characters")
	}
	basePath := fmt.Sprintf("team/%s/%s.md", kind, slug)
	rendered := renderArticle(rec, m.now())
	existing, exists, err := m.readExisting(basePath)
	if err != nil {
		return Plan{}, err
	}
	switch {
	case !exists:
		return Plan{
			Path:   basePath,
			Bytes:  len(rendered),
			Author: MigrateAuthor,
			Action: "create",
			Source: rec.Source,
		}, nil
	case bytesEqualOrSameHash(existing, []byte(rendered)):
		return Plan{
			Path:   basePath,
			Bytes:  len(rendered),
			Author: MigrateAuthor,
			Action: "skip-identical",
			Source: rec.Source,
		}, nil
	default:
		ts := m.now().UTC().Format("20060102-150405")
		source := slugify(rec.Source)
		if source == "" {
			source = "unknown"
		}
		altPath := fmt.Sprintf("team/%s/%s-from-%s-%s.md", kind, slug, source, ts)
		return Plan{
			Path:          altPath,
			Bytes:         len(rendered),
			Author:        MigrateAuthor,
			Action:        "collision-rename",
			Source:        rec.Source,
			CollisionWith: basePath,
		}, nil
	}
}

// readExisting returns the on-disk bytes for relPath plus whether the
// file exists. A non-existent file is not an error.
func (m *Migrator) readExisting(relPath string) ([]byte, bool, error) {
	root := strings.TrimSpace(m.writer.Root())
	if root == "" {
		return nil, false, nil
	}
	full := filepath.Join(root, filepath.FromSlash(relPath))
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat %s: %w", relPath, err)
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("%s is a directory, not an article", relPath)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", relPath, err)
	}
	return b, true, nil
}

// bytesEqualOrSameHash reports whether two byte slices are equal, via
// SHA-256 for large inputs. The hash path matters less for correctness
// and more for memory: articles can be hundreds of KB and we do not
// want to hold two copies of every record just to byte-compare.
func bytesEqualOrSameHash(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) < 64*1024 {
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	return sha256.Sum256(a) == sha256.Sum256(b)
}

// renderArticle turns a MigrationRecord into the canonical markdown
// body the writer commits. Keeps a simple, scannable header so humans
// reviewing the wiki later can tell at a glance that a page came from
// a legacy import.
func renderArticle(rec MigrationRecord, fallbackTS time.Time) string {
	title := strings.TrimSpace(rec.Title)
	if title == "" {
		title = rec.Slug
	}
	ts := rec.Timestamp
	if ts.IsZero() {
		ts = fallbackTS
	}
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("> Imported from ")
	if rec.Source != "" {
		b.WriteString(rec.Source)
	} else {
		b.WriteString("legacy backend")
	}
	b.WriteString(" on ")
	b.WriteString(fallbackTS.UTC().Format(time.RFC3339))
	b.WriteString(".\n\n")
	if !ts.IsZero() {
		b.WriteString("Upstream last updated: ")
		b.WriteString(ts.UTC().Format(time.RFC3339))
		b.WriteString(".\n\n")
	}
	content := strings.TrimSpace(rec.Content)
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// slugRegex matches any character NOT allowed in a wiki slug. The wiki
// path validator accepts lowercase a-z, 0-9 and hyphen; anything else
// becomes a hyphen and we collapse runs.
var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

// slugify returns a filename-safe slug derived from input. Empty strings
// yield empty strings so the caller can detect-and-reject.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = slugRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// firstNonEmpty returns the first non-empty trimmed string from the
// provided list. Used by both adapters to pick the first usable field
// from heterogeneous upstream shapes.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
