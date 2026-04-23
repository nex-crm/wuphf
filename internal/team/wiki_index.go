package team

// wiki_index.go is the derived cache layer for the wiki intelligence port.
//
// Contract
// ========
//
// Markdown is the source of truth. This index is a rebuildable cache.
// See docs/specs/WIKI-SCHEMA.md §7.4: `rm -rf .wuphf/index/` → restart broker
// → logically-identical rebuild (same canonical hash per table).
//
// Slice 1 scaffolds with small interfaces + in-memory implementations so the
// package compiles and tests run without pulling modernc.org/sqlite or
// blevesearch/bleve/v2 into go.mod yet. The real pure-Go backends slot in
// behind FactStore + TextIndex when Slice 1 benchmarks need them.
//
// Data flow
// =========
//
//	WikiWorker commit → ReconcilePath(ctx, path) → FactStore.Upsert + TextIndex.Index
//	Query handler      → Search / GetFact / ListEntityFacts / ListRelated
//	Boot reconcile     → ReconcileFromMarkdown walks wiki/facts/**/*.jsonl +
//	                     team/entities/*.facts.jsonl + team/**/*.md
//
// Schema alignment
// ================
//
// The TypedFact shape mirrors docs/specs/WIKI-SCHEMA.md §4.2 exactly. The
// existing v1.2 Fact (in entity_facts.go) maps onto TypedFact by leaving
// typed fields at their zero values — legacy parses with defaults, no
// migration step required (§4 opening paragraph).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
)

// TypedFact is the schema-aligned fact row used by the index.
//
// Maps 1:1 to docs/specs/WIKI-SCHEMA.md §4.2. Every field here has a default
// per §4.3 so legacy Fact rows (v1.2) parse cleanly with zero-value typed
// fields — no backfill migration required.
type TypedFact struct {
	ID              string     `json:"id"`
	EntitySlug      string     `json:"entity_slug"`
	Kind            string     `json:"kind,omitempty"` // person | company | project | team | workspace
	Type            string     `json:"type,omitempty"` // status | observation | relationship | background
	Triplet         *Triplet   `json:"triplet,omitempty"`
	Text            string     `json:"text"`
	Confidence      float64    `json:"confidence,omitempty"`
	ValidFrom       time.Time  `json:"valid_from,omitempty"`
	ValidUntil      *time.Time `json:"valid_until,omitempty"`
	Supersedes      []string   `json:"supersedes,omitempty"`
	ContradictsWith []string   `json:"contradicts_with,omitempty"`
	SourceType      string     `json:"source_type,omitempty"` // chat | meeting | email | manual | linkedin
	SourcePath      string     `json:"source_path,omitempty"`
	SentenceOffset  int        `json:"sentence_offset,omitempty"`
	ArtifactExcerpt string     `json:"artifact_excerpt,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	CreatedBy       string     `json:"created_by"`
	ReinforcedAt    *time.Time `json:"reinforced_at,omitempty"`
}

// Triplet is the subject/predicate/object shape from §4.2. `Object` is a slug,
// a literal, or `{kind}:{slug}` when the object references another entity.
type Triplet struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

// IndexEntity is the per-entity header row that the index holds for fast
// signal lookups. Frontmatter fields from §4.1.
type IndexEntity struct {
	Slug               string    `json:"slug"`
	CanonicalSlug      string    `json:"canonical_slug"` // same as Slug unless this is a redirect
	Kind               string    `json:"kind"`
	Aliases            []string  `json:"aliases,omitempty"`
	Signals            Signals   `json:"signals"`
	LastSynthesizedSHA string    `json:"last_synthesized_sha,omitempty"`
	LastSynthesizedAt  time.Time `json:"last_synthesized_at,omitempty"`
	FactCountAtSynth   int       `json:"fact_count_at_synth,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	CreatedBy          string    `json:"created_by"`
}

// Signals are the matching signals the resolver uses to dedupe entities.
type Signals struct {
	Email      string `json:"email,omitempty"`
	Domain     string `json:"domain,omitempty"`
	PersonName string `json:"person_name,omitempty"`
	JobTitle   string `json:"job_title,omitempty"`
}

// IndexEdge is one typed edge in graph.log (§6.2).
type IndexEdge struct {
	Subject   string    `json:"subject"`
	Predicate string    `json:"predicate"`
	Object    string    `json:"object"`
	Timestamp time.Time `json:"timestamp"`
	SourceSHA string    `json:"source_sha"`
}

// Redirect maps a younger slug to its survivor (§7.2).
type Redirect struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	MergedAt  time.Time `json:"merged_at"`
	MergedBy  string    `json:"merged_by"`
	CommitSHA string    `json:"commit_sha"`
}

// SearchHit is one result row from the text index.
type SearchHit struct {
	FactID  string  `json:"fact_id"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
	Entity  string  `json:"entity_slug,omitempty"`
}

// FactStore is the narrow interface the index uses for structured storage.
// modernc.org/sqlite slots in here without wiki_index.go changing.
//
// Implementations MUST be goroutine-safe for concurrent reads. A single
// writer (the broker's ReconcilePath loop) calls Upsert; readers call
// Get / List / Search concurrently.
type FactStore interface {
	UpsertFact(ctx context.Context, f TypedFact) error
	UpsertEntity(ctx context.Context, e IndexEntity) error
	UpsertEdge(ctx context.Context, e IndexEdge) error
	UpsertRedirect(ctx context.Context, r Redirect) error

	GetFact(ctx context.Context, id string) (TypedFact, bool, error)
	ListFactsForEntity(ctx context.Context, slug string) ([]TypedFact, error)
	ListEdgesForEntity(ctx context.Context, slug string) ([]IndexEdge, error)
	ResolveRedirect(ctx context.Context, slug string) (string, bool, error)

	// ListFactsByPredicateObject returns every fact whose triplet matches
	// (predicate, object) exactly. Used by the typed-predicate graph walk
	// for multi_hop queries (Slice 2 Thread A).
	ListFactsByPredicateObject(ctx context.Context, predicate, object string) ([]TypedFact, error)

	// ListFactsByTriplet returns every fact whose triplet matches the given
	// subject + predicate and whose triplet.object starts with objectPrefix
	// (case-sensitive). An empty objectPrefix matches any object. Used by the
	// typed-predicate graph walk and counterfactual rewrite (Slice 2 Thread A).
	ListFactsByTriplet(ctx context.Context, subject, predicate, objectPrefix string) ([]TypedFact, error)

	CanonicalHashFacts(ctx context.Context) (string, error) // §7.4 rebuild check
	CanonicalHashAll(ctx context.Context) (string, error)   // §7.4 composite: facts + entities + edges + redirects
	Close() error
}

// TextIndex is the narrow interface for full-text / BM25 search.
// blevesearch/bleve/v2 slots in here without wiki_index.go changing.
type TextIndex interface {
	Index(ctx context.Context, f TypedFact) error
	Delete(ctx context.Context, factID string) error
	Search(ctx context.Context, query string, topK int) ([]SearchHit, error)
	Close() error
}

// WikiIndex composes a FactStore and a TextIndex behind a single handle the
// rest of the broker uses. Construct via NewWikiIndex.
type WikiIndex struct {
	root  string // wiki repo root (where wiki/ and team/ live)
	store FactStore
	text  TextIndex

	mu        sync.Mutex
	lastBuild time.Time
}

// IndexOption configures a WikiIndex at construction.
type IndexOption func(*WikiIndex)

// WithFactStore injects a custom FactStore. Defaults to inMemoryFactStore.
func WithFactStore(s FactStore) IndexOption {
	return func(w *WikiIndex) { w.store = s }
}

// WithTextIndex injects a custom TextIndex. Defaults to inMemoryTextIndex.
func WithTextIndex(t TextIndex) IndexOption {
	return func(w *WikiIndex) { w.text = t }
}

// NewWikiIndex constructs a WikiIndex rooted at the given wiki repo directory.
// Defaults to in-memory stores so callers can wire up tests without new deps.
// The real pure-Go backends (modernc.org/sqlite + bleve) replace the defaults
// behind the same interfaces when Slice 1 benchmarks demand them.
func NewWikiIndex(root string, opts ...IndexOption) *WikiIndex {
	w := &WikiIndex{root: root}
	for _, opt := range opts {
		opt(w)
	}
	if w.store == nil {
		w.store = newInMemoryFactStore()
	}
	if w.text == nil {
		w.text = newInMemoryTextIndex()
	}
	return w
}

// NewPersistentWikiIndex constructs a WikiIndex that stores facts in a SQLite
// database and uses bleve for full-text (BM25) search — both pure-Go, no cgo.
//
//   - sqlite db: indexDir/wiki.sqlite
//   - bleve dir: indexDir/bleve/
//
// indexDir is created with 0o755 if it does not exist. The caller must call
// Close() when done. NewWikiIndex (in-memory) remains the default for tests and
// the fallback path — this constructor is additive only.
func NewPersistentWikiIndex(root string, indexDir string) (*WikiIndex, error) {
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return nil, fmt.Errorf("wiki_index: mkdir %s: %w", indexDir, err)
	}

	sqlitePath := filepath.Join(indexDir, "wiki.sqlite")
	store, err := NewSQLiteFactStore(sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("wiki_index: sqlite: %w", err)
	}

	bleveDir := filepath.Join(indexDir, "bleve")
	text, err := NewBleveTextIndex(bleveDir)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("wiki_index: bleve: %w", err)
	}

	return NewWikiIndex(root, WithFactStore(store), WithTextIndex(text)), nil
}

// Close releases both backends. Safe to call twice.
func (w *WikiIndex) Close() error {
	var errs []error
	if w.store != nil {
		if err := w.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if w.text != nil {
		if err := w.text.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NormalizeForFactID normalizes a triplet component per §7.3:
// NFC-normalize (so NFD vs NFC forms of the same glyph produce the same hash),
// then lowercase, trim, replace non-alphanumeric runs with a single dash.
func NormalizeForFactID(s string) string {
	s = norm.NFC.String(s) // canonical equivalence across Unicode normalization forms
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ComputeFactID is the deterministic fact ID hash from §7.3:
//
//	sha256(artifact_sha + "/" + sentence_offset + "/" + norm(subject) +
//	       "/" + norm(predicate) + "/" + norm(object))[:16]
//
// Same artifact + same extraction → same ID. Substrate guarantee.
func ComputeFactID(artifactSHA string, sentenceOffset int, subject, predicate, object string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s/%d/%s/%s/%s",
		artifactSHA,
		sentenceOffset,
		NormalizeForFactID(subject),
		NormalizeForFactID(predicate),
		NormalizeForFactID(object),
	)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:16]
}

// Staleness applies the §8.1 formula:
//
//	staleness = (days_old × type_weight) − (confidence × 10) − reinforcement_bonus
//
// type_weight: status=1.0 observation=0.5 relationship=0.2 background=0.1
// reinforcement_bonus = 5.0 × max(0, 1 - days_since_reinforced / 30)
//
// Returns a read-time visibility score. Higher = staler. Query-time filter
// excludes staleness > 20 for status/recency queries (§8.1).
func Staleness(f TypedFact, now time.Time) float64 {
	if now.IsZero() {
		now = time.Now()
	}
	var weight float64
	switch f.Type {
	case "status":
		weight = 1.0
	case "observation":
		weight = 0.5
	case "relationship":
		weight = 0.2
	case "background":
		weight = 0.1
	default:
		weight = 0.5 // default matches §4.3 default type = observation
	}
	var anchor time.Time
	if !f.ValidFrom.IsZero() {
		anchor = f.ValidFrom
	} else {
		anchor = f.CreatedAt
	}
	daysOld := now.Sub(anchor).Hours() / 24.0

	conf := f.Confidence
	if conf == 0 {
		conf = 1.0 // §4.3 default confidence = 1.0
	}

	var reinforcement float64
	if f.ReinforcedAt != nil && !f.ReinforcedAt.IsZero() {
		daysSince := now.Sub(*f.ReinforcedAt).Hours() / 24.0
		decay := 1.0 - (daysSince / 30.0)
		if decay < 0 {
			decay = 0
		}
		reinforcement = 5.0 * decay
	}

	return (daysOld * weight) - (conf * 10) - reinforcement
}

// Search runs a BM25 query against the text index and hydrates hits with
// their full TypedFact row from the store. `topK` is clamped to [1, 100].
func (w *WikiIndex) Search(ctx context.Context, query string, topK int) ([]SearchHit, error) {
	if topK < 1 {
		topK = 10
	}
	if topK > 100 {
		topK = 100
	}
	return w.text.Search(ctx, query, topK)
}

// GetFact returns a single fact by ID.
func (w *WikiIndex) GetFact(ctx context.Context, id string) (TypedFact, bool, error) {
	return w.store.GetFact(ctx, id)
}

// ListFactsForEntity returns every fact indexed against an entity slug,
// respecting redirects: if `slug` is a redirect, the survivor's facts are
// returned.
func (w *WikiIndex) ListFactsForEntity(ctx context.Context, slug string) ([]TypedFact, error) {
	resolved, redirected, err := w.store.ResolveRedirect(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("wiki_index: resolve redirect %q: %w", slug, err)
	}
	if !redirected {
		resolved = slug
	}
	return w.store.ListFactsForEntity(ctx, resolved)
}

// ListEdgesForEntity returns graph.log edges incident on an entity.
func (w *WikiIndex) ListEdgesForEntity(ctx context.Context, slug string) ([]IndexEdge, error) {
	resolved, redirected, err := w.store.ResolveRedirect(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !redirected {
		resolved = slug
	}
	return w.store.ListEdgesForEntity(ctx, resolved)
}

// ReconcilePath indexes a single file by path relative to the wiki root.
// Called from WikiWorker.process after every successful commit so the
// index stays live with the repo (§2 when-to-read).
//
// Paths recognized:
//   - wiki/facts/{kind}/{slug}.jsonl      (new schema)
//   - team/entities/{kind}-{slug}.facts.jsonl  (v1.2 legacy)
//   - team/{kind}/{slug}.md               (entity brief)
//   - graph.log                           (typed edges)
//   - wiki/.lint/report-YYYY-MM-DD.md     (lint report; §3 Layer-2)
func (w *WikiIndex) ReconcilePath(ctx context.Context, relPath string) error {
	abs := filepath.Join(w.root, filepath.FromSlash(relPath))
	switch {
	case isFactLogPath(relPath):
		return w.reconcileFactLog(ctx, abs, relPath)
	case isEntityBriefPath(relPath):
		return w.reconcileEntityBrief(ctx, abs, relPath)
	case isLintReportPath(relPath):
		return w.reconcileLintReport(ctx, abs, relPath)
	case relPath == "graph.log":
		return w.reconcileGraphLog(ctx, abs)
	default:
		return nil // not an indexed path; no-op
	}
}

// ReconcileFromMarkdown walks the wiki repo from scratch and rebuilds the
// index. Implements the §7.4 substrate guarantee: `rm -rf .wuphf/index/`
// → call this → logically-identical index. Safe to run concurrently with
// reads (writes are serialized inside the store).
//
// The mutex guards only lastBuild. All reconcile I/O runs outside the lock so
// that Search and GetFact are never blocked during a long boot reconcile.
// The FactStore's own internal synchronization serializes writes.
func (w *WikiIndex) ReconcileFromMarkdown(ctx context.Context) error {
	factLogs := []string{
		filepath.Join(w.root, "wiki", "facts"),
		filepath.Join(w.root, "team", "entities"),
	}
	for _, dir := range factLogs {
		if err := walkFactLogs(ctx, dir, w); err != nil {
			return fmt.Errorf("wiki_index: reconcile %s: %w", dir, err)
		}
	}

	briefsDir := filepath.Join(w.root, "team")
	if err := walkEntityBriefs(ctx, briefsDir, w); err != nil {
		return fmt.Errorf("wiki_index: reconcile briefs: %w", err)
	}

	graphPath := filepath.Join(w.root, "graph.log")
	if _, err := os.Stat(graphPath); err == nil {
		if err := w.reconcileGraphLog(ctx, graphPath); err != nil {
			return fmt.Errorf("wiki_index: reconcile graph.log: %w", err)
		}
	}

	lintDir := filepath.Join(w.root, "wiki", ".lint")
	if err := walkLintReports(ctx, lintDir, w); err != nil {
		return fmt.Errorf("wiki_index: reconcile lint reports: %w", err)
	}

	w.mu.Lock()
	w.lastBuild = time.Now()
	w.mu.Unlock()
	return nil
}

// LastBuild reports when the most recent full reconcile completed.
func (w *WikiIndex) LastBuild() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastBuild
}

// CanonicalHashFacts returns a stable hash over all indexed facts, used by
// the §7.4 rebuild contract test: hash before rebuild must equal hash after.
func (w *WikiIndex) CanonicalHashFacts(ctx context.Context) (string, error) {
	return w.store.CanonicalHashFacts(ctx)
}

// CanonicalHashAll returns a composite hash over facts, entities, edges, and
// redirects. Used by the §7.4 rebuild contract test to verify no silent drift
// in the entity, edge, or redirect layers after a full reconcile.
func (w *WikiIndex) CanonicalHashAll(ctx context.Context) (string, error) {
	return w.store.CanonicalHashAll(ctx)
}

// --- path routing helpers -------------------------------------------------

var (
	factLogNewSchema = regexp.MustCompile(`^wiki/facts/[^/]+/[^/]+\.jsonl$`)
	factLogLegacyV12 = regexp.MustCompile(`^team/entities/[a-z]+-[a-z0-9][a-z0-9-]*\.facts\.jsonl$`)
	entityBriefPath  = regexp.MustCompile(`^team/[^/]+/[^/]+\.md$`)
	lintReportPath   = regexp.MustCompile(`^wiki/\.lint/report-\d{4}-\d{2}-\d{2}\.md$`)
)

func isFactLogPath(relPath string) bool {
	rel := filepath.ToSlash(relPath)
	return factLogNewSchema.MatchString(rel) || factLogLegacyV12.MatchString(rel)
}

func isEntityBriefPath(relPath string) bool {
	rel := filepath.ToSlash(relPath)
	return entityBriefPath.MatchString(rel)
}

func isLintReportPath(relPath string) bool {
	rel := filepath.ToSlash(relPath)
	return lintReportPath.MatchString(rel)
}

// --- reconcile bodies ----------------------------------------------------

func (w *WikiIndex) reconcileFactLog(ctx context.Context, abs, relPath string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var f TypedFact
		if err := json.Unmarshal([]byte(trimmed), &f); err != nil {
			// Malformed line — skip with a note, same posture as entity_facts.go.
			// Do NOT route to DLQ here; DLQ is an ingest-time concept, not an
			// index-rebuild concept.
			continue
		}
		if f.ID == "" {
			continue // legacy rows without IDs are skipped; a later synth will re-emit them with IDs
		}
		_ = i
		if err := w.store.UpsertFact(ctx, f); err != nil {
			return fmt.Errorf("upsert fact %s: %w", f.ID, err)
		}
		if err := w.text.Index(ctx, f); err != nil {
			return fmt.Errorf("text index fact %s: %w", f.ID, err)
		}
	}
	_ = relPath
	return nil
}

func (w *WikiIndex) reconcileEntityBrief(ctx context.Context, abs, relPath string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	rel := filepath.ToSlash(relPath)
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return nil
	}
	kind := parts[1]
	slug := strings.TrimSuffix(parts[2], ".md")

	entity := IndexEntity{
		Slug:          slug,
		CanonicalSlug: slug,
		Kind:          kind,
	}
	if fm := extractFrontmatter(string(data)); fm != "" {
		if v := frontmatterValue(fm, "canonical_slug"); v != "" {
			entity.CanonicalSlug = v
		}
		if v := frontmatterValue(fm, "kind"); v != "" {
			entity.Kind = v
		}
		if v := frontmatterValue(fm, "last_synthesized_sha"); v != "" {
			entity.LastSynthesizedSHA = v
		}
		if v := frontmatterValue(fm, "last_synthesized_ts"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				entity.LastSynthesizedAt = t
			}
		}
		if v := frontmatterValue(fm, "created_by"); v != "" {
			entity.CreatedBy = v
		}
		if aliases := frontmatterList(fm, "aliases"); len(aliases) > 0 {
			entity.Aliases = aliases
		}
	}
	if entity.CanonicalSlug != entity.Slug {
		if err := w.store.UpsertRedirect(ctx, Redirect{
			From: entity.Slug,
			To:   entity.CanonicalSlug,
		}); err != nil {
			return fmt.Errorf("upsert redirect %s→%s: %w", entity.Slug, entity.CanonicalSlug, err)
		}
	}
	return w.store.UpsertEntity(ctx, entity)
}

func (w *WikiIndex) reconcileGraphLog(ctx context.Context, abs string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			continue
		}
		edge := IndexEdge{Subject: parts[0], Predicate: parts[1], Object: parts[2]}
		if len(parts) >= 4 {
			if ts := parseEdgeTimestamp(parts[3]); !ts.IsZero() {
				edge.Timestamp = ts
			}
		}
		for _, p := range parts[4:] {
			if strings.HasPrefix(p, "src=") {
				edge.SourceSHA = strings.TrimPrefix(p, "src=")
			}
		}
		if err := w.store.UpsertEdge(ctx, edge); err != nil {
			return fmt.Errorf("upsert edge %s->%s->%s: %w", edge.Subject, edge.Predicate, edge.Object, err)
		}
	}
	return nil
}

// parseEdgeTimestamp tries three layouts in order: RFC3339, datetime without
// timezone, date-only. First success wins. Returns zero time if all fail.
func parseEdgeTimestamp(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	if s != "" {
		log.Printf("wiki_index: parseEdgeTimestamp: unrecognised format %q", s)
	}
	return time.Time{}
}

// reconcileLintReport indexes a lint report at wiki/.lint/report-YYYY-MM-DD.md as
// a single TypedFact so BM25 search can find lint observations. The synthetic
// fact ID is lint_YYYY_MM_DD. EntitySlug is "_lint", Type is "observation".
// Structured-store rows stay empty; only the text index is populated.
func (w *WikiIndex) reconcileLintReport(ctx context.Context, abs, relPath string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("wiki_index: read lint report %s: %w", relPath, err)
	}
	// Extract YYYY-MM-DD from filename: wiki/.lint/report-YYYY-MM-DD.md
	base := filepath.Base(abs)
	date := strings.TrimSuffix(strings.TrimPrefix(base, "report-"), ".md")
	syntheticID := "lint_" + strings.ReplaceAll(date, "-", "_")
	f := TypedFact{
		ID:         syntheticID,
		EntitySlug: "_lint",
		Type:       "observation",
		Text:       string(data),
		SourcePath: relPath,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "lint-indexer",
	}
	if err := w.text.Index(ctx, f); err != nil {
		return fmt.Errorf("wiki_index: text index lint report %s: %w", syntheticID, err)
	}
	return nil
}

// --- walkers --------------------------------------------------------------

func walkFactLogs(ctx context.Context, dir string, w *WikiIndex) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, relErr := filepath.Rel(w.root, path)
		if relErr != nil {
			return relErr
		}
		return w.reconcileFactLog(ctx, path, filepath.ToSlash(rel))
	})
}

func walkEntityBriefs(ctx context.Context, dir string, w *WikiIndex) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(w.root, path)
		if relErr != nil {
			return relErr
		}
		return w.reconcileEntityBrief(ctx, path, filepath.ToSlash(rel))
	})
}

func walkLintReports(ctx context.Context, dir string, w *WikiIndex) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(w.root, path)
		if relErr != nil {
			return relErr
		}
		return w.reconcileLintReport(ctx, path, filepath.ToSlash(rel))
	})
}

// --- minimal frontmatter helpers (local to avoid circular deps) ----------

func extractFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return ""
	}
	rest := body[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func frontmatterValue(block, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// frontmatterList parses YAML block-list values for the given key.
//
// Handles two forms:
//
//	# Scalar (single value after colon)
//	aliases: Sarah J.
//
//	# Block list (YAML indent + hyphen)
//	aliases:
//	  - Sarah J.
//	  - sjones
//
// Inline bracket form (aliases: [a, b]) is also accepted.
func frontmatterList(block, key string) []string {
	prefix := key + ":"
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		scalar := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if scalar != "" {
			// Inline bracket form: [a, b, c]
			if strings.HasPrefix(scalar, "[") && strings.HasSuffix(scalar, "]") {
				inner := scalar[1 : len(scalar)-1]
				var out []string
				for _, v := range strings.Split(inner, ",") {
					v = strings.TrimSpace(v)
					if v != "" {
						out = append(out, v)
					}
				}
				return out
			}
			// Bare scalar after colon.
			return []string{scalar}
		}
		// Block list: scan following lines for "  - value".
		var out []string
		for _, item := range lines[i+1:] {
			if !strings.HasPrefix(item, "  - ") {
				break
			}
			out = append(out, strings.TrimSpace(strings.TrimPrefix(item, "  - ")))
		}
		return out
	}
	return nil
}

// --- in-memory backends (default; replaced by sqlite/bleve behind interfaces) -

type inMemoryFactStore struct {
	mu        sync.RWMutex
	facts     map[string]TypedFact
	entities  map[string]IndexEntity
	edgesBy   map[string][]IndexEdge // keyed by subject slug AND object slug
	redirects map[string]Redirect
}

func newInMemoryFactStore() *inMemoryFactStore {
	return &inMemoryFactStore{
		facts:     map[string]TypedFact{},
		entities:  map[string]IndexEntity{},
		edgesBy:   map[string][]IndexEdge{},
		redirects: map[string]Redirect{},
	}
}

func (s *inMemoryFactStore) UpsertFact(_ context.Context, f TypedFact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts[f.ID] = f
	return nil
}

func (s *inMemoryFactStore) UpsertEntity(_ context.Context, e IndexEntity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entities[e.Slug] = e
	return nil
}

func (s *inMemoryFactStore) UpsertEdge(_ context.Context, e IndexEdge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	setKey := e.Subject + "|" + e.Predicate + "|" + e.Object
	s.edgesBy[e.Subject] = upsertEdgeDedup(s.edgesBy[e.Subject], e, setKey)
	if e.Object != e.Subject {
		s.edgesBy[e.Object] = upsertEdgeDedup(s.edgesBy[e.Object], e, setKey)
	}
	return nil
}

// upsertEdgeDedup replaces an existing edge with the same composite key or
// appends it. The key is subject+predicate+object so that repeated
// ReconcileFromMarkdown calls are idempotent.
func upsertEdgeDedup(bucket []IndexEdge, e IndexEdge, key string) []IndexEdge {
	for i, existing := range bucket {
		if existing.Subject+"|"+existing.Predicate+"|"+existing.Object == key {
			bucket[i] = e
			return bucket
		}
	}
	return append(bucket, e)
}

func (s *inMemoryFactStore) UpsertRedirect(_ context.Context, r Redirect) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redirects[r.From] = r
	return nil
}

func (s *inMemoryFactStore) GetFact(_ context.Context, id string) (TypedFact, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.facts[id]
	return f, ok, nil
}

func (s *inMemoryFactStore) ListFactsForEntity(_ context.Context, slug string) ([]TypedFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TypedFact
	for _, f := range s.facts {
		if f.EntitySlug == slug {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// ListFactsByPredicateObject returns all facts whose triplet has the exact
// (predicate, object) pair. Linear scan over the fact map — acceptable for
// the in-memory default backend.
func (s *inMemoryFactStore) ListFactsByPredicateObject(_ context.Context, predicate, object string) ([]TypedFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TypedFact
	for _, f := range s.facts {
		if f.Triplet == nil {
			continue
		}
		if f.Triplet.Predicate == predicate && f.Triplet.Object == object {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListFactsByTriplet returns all facts matching (subject, predicate) whose
// triplet.object begins with objectPrefix. Empty objectPrefix matches any
// object. Linear scan over the fact map.
func (s *inMemoryFactStore) ListFactsByTriplet(_ context.Context, subject, predicate, objectPrefix string) ([]TypedFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TypedFact
	for _, f := range s.facts {
		if f.Triplet == nil {
			continue
		}
		if f.Triplet.Subject != subject || f.Triplet.Predicate != predicate {
			continue
		}
		if objectPrefix != "" && !strings.HasPrefix(f.Triplet.Object, objectPrefix) {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *inMemoryFactStore) ListEdgesForEntity(_ context.Context, slug string) ([]IndexEdge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]IndexEdge(nil), s.edgesBy[slug]...), nil
}

func (s *inMemoryFactStore) ResolveRedirect(_ context.Context, slug string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.redirects[slug]; ok {
		return r.To, true, nil
	}
	return slug, false, nil
}

func (s *inMemoryFactStore) CanonicalHashFacts(_ context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.facts))
	for id := range s.facts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		b, err := json.Marshal(s.facts[id])
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *inMemoryFactStore) CanonicalHashAll(_ context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h := sha256.New()

	// Facts (sorted by ID)
	factIDs := make([]string, 0, len(s.facts))
	for id := range s.facts {
		factIDs = append(factIDs, id)
	}
	sort.Strings(factIDs)
	for _, id := range factIDs {
		b, err := json.Marshal(s.facts[id])
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}

	// Entities (sorted by slug)
	entitySlugs := make([]string, 0, len(s.entities))
	for slug := range s.entities {
		entitySlugs = append(entitySlugs, slug)
	}
	sort.Strings(entitySlugs)
	for _, slug := range entitySlugs {
		b, err := json.Marshal(s.entities[slug])
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}

	// Edges (sorted by subject|predicate|object)
	type edgeKey struct{ s, p, o string }
	seen := map[edgeKey]bool{}
	var allEdges []IndexEdge
	for _, edges := range s.edgesBy {
		for _, e := range edges {
			k := edgeKey{e.Subject, e.Predicate, e.Object}
			if !seen[k] {
				seen[k] = true
				allEdges = append(allEdges, e)
			}
		}
	}
	sort.Slice(allEdges, func(i, j int) bool {
		if allEdges[i].Subject != allEdges[j].Subject {
			return allEdges[i].Subject < allEdges[j].Subject
		}
		if allEdges[i].Predicate != allEdges[j].Predicate {
			return allEdges[i].Predicate < allEdges[j].Predicate
		}
		return allEdges[i].Object < allEdges[j].Object
	})
	for _, e := range allEdges {
		b, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}

	// Redirects (sorted by From)
	redirectFroms := make([]string, 0, len(s.redirects))
	for from := range s.redirects {
		redirectFroms = append(redirectFroms, from)
	}
	sort.Strings(redirectFroms)
	for _, from := range redirectFroms {
		b, err := json.Marshal(s.redirects[from])
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *inMemoryFactStore) Close() error { return nil }

// inMemoryTextIndex is a substring-match fallback that satisfies TextIndex
// without pulling bleve. Scoring is presence-only. Good enough for tests and
// for the first Slice 1 dogfood week; real queries run on bleve.
type inMemoryTextIndex struct {
	mu    sync.RWMutex
	facts map[string]TypedFact
}

func newInMemoryTextIndex() *inMemoryTextIndex {
	return &inMemoryTextIndex{facts: map[string]TypedFact{}}
}

func (t *inMemoryTextIndex) Index(_ context.Context, f TypedFact) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.facts[f.ID] = f
	return nil
}

func (t *inMemoryTextIndex) Delete(_ context.Context, id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.facts, id)
	return nil
}

func (t *inMemoryTextIndex) Search(_ context.Context, query string, topK int) ([]SearchHit, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	var hits []SearchHit
	for id, f := range t.facts {
		text := strings.ToLower(f.Text)
		if strings.Contains(text, q) {
			hits = append(hits, SearchHit{FactID: id, Score: 1.0, Snippet: f.Text, Entity: f.EntitySlug})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].FactID < hits[j].FactID })
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

func (t *inMemoryTextIndex) Close() error { return nil }
