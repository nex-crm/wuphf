package team

// entity_graph.go is the cross-entity graph built on top of the v1.2 fact log.
//
// Every time an agent records a fact on one entity that references another
// entity (e.g. "Sarah works at [[companies/acme]]" logged on people/sarah),
// we emit an edge to an append-only adjacency log at
// team/entities/.graph.jsonl. Reads coalesce the log into a deduplicated
// adjacency list keyed by (from_kind, from_slug, to_kind, to_slug).
//
// v1 parser scope:
//   - Wikilinks: [[kind/slug]] where kind is one of people|companies|customers.
//   - Plain [[slug]] wikilinks resolved against known entity briefs on disk,
//     exact-match only. A slug that matches more than one kind is skipped
//     (ambiguous — v2 will add fuzzy name resolution).
//
// Writes ride the shared WikiWorker queue with a new IsEntityGraph
// discriminator so the single-writer invariant is preserved. One
// fact_recorded hook = one batched append of every new edge (file rewrite,
// same pattern as the fact log).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// EntityGraphPath is the wiki-root-relative path to the graph log.
const EntityGraphPath = "team/entities/.graph.jsonl"

// ErrEntityGraphNotRunning mirrors ErrFactLogNotRunning — returned when a
// graph operation runs without a wiki worker attached.
var ErrEntityGraphNotRunning = errors.New("entity graph: worker is not attached")

// EntityRef is one parsed reference to another entity inside a fact.
type EntityRef struct {
	Kind EntityKind `json:"kind"`
	Slug string     `json:"slug"`
}

// EntityEdge is one row of the append-only adjacency log.
type EntityEdge struct {
	FromKind        EntityKind `json:"from_kind"`
	FromSlug        string     `json:"from_slug"`
	ToKind          EntityKind `json:"to_kind"`
	ToSlug          string     `json:"to_slug"`
	FirstSeenFactID string     `json:"first_seen_fact_id"`
	LastSeenTS      time.Time  `json:"last_seen_ts"`
}

// CoalescedEdge is the reader's post-dedup view of an edge. Preserves the
// first-seen fact id and the most recent timestamp so UI panels can sort by
// recency without re-reading the full log.
type CoalescedEdge struct {
	FromKind        EntityKind `json:"from_kind"`
	FromSlug        string     `json:"from_slug"`
	ToKind          EntityKind `json:"to_kind"`
	ToSlug          string     `json:"to_slug"`
	FirstSeenFactID string     `json:"first_seen_fact_id"`
	LastSeenTS      time.Time  `json:"last_seen_ts"`
	OccurrenceCount int        `json:"occurrence_count"`
}

// EntityGraph owns all read/write access to the graph log.
type EntityGraph struct {
	worker *WikiWorker
	mu     sync.Mutex
}

// NewEntityGraph constructs a graph backed by the supplied worker.
func NewEntityGraph(worker *WikiWorker) *EntityGraph {
	return &EntityGraph{worker: worker}
}

// Kindprefixed wikilinks: `[[people/nazz]]`, `[[companies/acme]]`, `[[customers/foo]]`.
// Kind token is captured separately so the entity kind is verified.
var kindedWikilinkPattern = regexp.MustCompile(`\[\[(people|companies|customers)/([a-z0-9][a-z0-9-]*)(?:\|[^\]]*)?\]\]`)

// Bare `[[slug]]` wikilinks (no kind prefix, no path). Captured for
// exact-match lookup against known entity briefs on disk.
var bareWikilinkPattern = regexp.MustCompile(`\[\[([a-z0-9][a-z0-9-]*)(?:\|[^\]]*)?\]\]`)

// ExtractRefs parses `statement` for references to entities *other than* the
// source entity (self-references are elided). `known` supplies a closure the
// caller can plug in to resolve bare `[[slug]]` wikilinks to a single kind;
// return ("", false) to skip (ambiguous or unknown). Passing nil disables
// bare-slug lookup entirely — useful in tests and when the graph is being
// rebuilt without disk access.
func ExtractRefs(sourceKind EntityKind, sourceSlug, statement string, known func(slug string) (EntityKind, bool)) []EntityRef {
	seen := make(map[string]bool)
	out := make([]EntityRef, 0, 4)

	add := func(kind EntityKind, slug string) {
		if kind == sourceKind && slug == sourceSlug {
			return
		}
		key := string(kind) + "/" + slug
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, EntityRef{Kind: kind, Slug: slug})
	}

	// First pass: explicit [[kind/slug]] — always wins.
	for _, m := range kindedWikilinkPattern.FindAllStringSubmatch(statement, -1) {
		kind := EntityKind(m[1])
		slug := m[2]
		if !slugPattern.MatchString(slug) {
			continue
		}
		add(kind, slug)
	}

	// Second pass: bare [[slug]] — only if `known` is provided.
	if known != nil {
		for _, m := range bareWikilinkPattern.FindAllStringSubmatch(statement, -1) {
			slug := m[1]
			// Skip forms already consumed by the kinded pass: re-matching the
			// inner slug of `[[people/nazz]]` would double-count because the
			// bare pattern sees `[[people/nazz]]` as not matching (there is a
			// slash), BUT the same bare pattern does match `[[nazz]]` on its
			// own. So the filter here only needs to guard against the slug
			// that is literally present as a kinded target too.
			if !slugPattern.MatchString(slug) {
				continue
			}
			if containsKinded(statement, slug) {
				continue
			}
			kind, ok := known(slug)
			if !ok {
				continue
			}
			add(kind, slug)
		}
	}

	return out
}

// containsKinded reports whether the statement already contains a kinded
// reference whose slug equals the bare slug — so we don't double-count.
func containsKinded(statement, slug string) bool {
	for _, m := range kindedWikilinkPattern.FindAllStringSubmatch(statement, -1) {
		if m[2] == slug {
			return true
		}
	}
	return false
}

// knownEntityResolver returns a closure that maps a bare slug to a unique
// entity kind by walking the on-disk team/{kind}/ directories. Ambiguous
// slugs (present under multiple kinds) return false — v1 refuses to guess.
func (g *EntityGraph) knownEntityResolver() func(slug string) (EntityKind, bool) {
	if g == nil || g.worker == nil {
		return nil
	}
	root := g.worker.Repo().Root()
	// Build the index once per call — cheap for a hundred briefs, and the
	// graph append is not on the hot path for agent turns.
	index := make(map[string][]EntityKind)
	for _, kind := range ValidEntityKinds() {
		dir := filepath.Join(root, "team", string(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".md") {
				continue
			}
			slug := strings.TrimSuffix(name, ".md")
			if !slugPattern.MatchString(slug) {
				continue
			}
			index[slug] = append(index[slug], kind)
		}
	}
	return func(slug string) (EntityKind, bool) {
		kinds, ok := index[slug]
		if !ok || len(kinds) != 1 {
			return "", false
		}
		return kinds[0], true
	}
}

// RecordFactRefs extracts entity references from the given fact and, when
// any exist, appends new edges to the graph log via the wiki worker. Edges
// that already exist in the log are still appended (the reader coalesces
// — keeps the writer path cheap and idempotent on retries).
func (g *EntityGraph) RecordFactRefs(ctx context.Context, fact Fact) ([]EntityRef, error) {
	if g == nil || g.worker == nil {
		return nil, ErrEntityGraphNotRunning
	}
	refs := ExtractRefs(fact.Kind, fact.Slug, fact.Text, g.knownEntityResolver())
	if len(refs) == 0 {
		return nil, nil
	}

	// Build the new file contents under the lock — a concurrent RecordFactRefs
	// must see this write as atomic for the read-then-append contract to hold.
	// Release the lock BEFORE enqueuing, so a read path that takes g.mu in the
	// future (or a full write queue) can never deadlock waiting on a worker
	// that is waiting on us.
	g.mu.Lock()
	existing := g.readExistingLocked()
	var buf strings.Builder
	buf.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		buf.WriteString("\n")
	}
	for _, ref := range refs {
		edge := EntityEdge{
			FromKind:        fact.Kind,
			FromSlug:        fact.Slug,
			ToKind:          ref.Kind,
			ToSlug:          ref.Slug,
			FirstSeenFactID: fact.ID,
			LastSeenTS:      fact.CreatedAt.UTC(),
		}
		line, err := json.Marshal(edge)
		if err != nil {
			g.mu.Unlock()
			return nil, fmt.Errorf("entity graph: marshal: %w", err)
		}
		buf.Write(line)
		buf.WriteString("\n")
	}
	content := buf.String()
	g.mu.Unlock()

	msg := fmt.Sprintf("graph: %s/%s → %d ref(s)", fact.Kind, fact.Slug, len(refs))
	if _, _, err := g.worker.EnqueueEntityGraph(ctx, ArchivistAuthor, content, msg); err != nil {
		return refs, fmt.Errorf("entity graph: enqueue: %w", err)
	}
	return refs, nil
}

// readExistingLocked returns the current graph log bytes, or nil if missing.
// Caller holds g.mu.
func (g *EntityGraph) readExistingLocked() []byte {
	full := filepath.Join(g.worker.Repo().Root(), filepath.FromSlash(EntityGraphPath))
	bytes, err := os.ReadFile(full)
	if err != nil {
		return nil
	}
	return bytes
}

// readAll streams the log and returns every parseable edge in file order.
func (g *EntityGraph) readAll() ([]EntityEdge, error) {
	if g == nil || g.worker == nil {
		return nil, ErrEntityGraphNotRunning
	}
	full := filepath.Join(g.worker.Repo().Root(), filepath.FromSlash(EntityGraphPath))
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entity graph: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	edges := make([]EntityEdge, 0, 64)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var edge EntityEdge
		if err := json.Unmarshal([]byte(line), &edge); err != nil {
			log.Printf("entity graph: skip malformed line %d: %v", lineNo, err)
			continue
		}
		if edge.FromKind == "" || edge.FromSlug == "" || edge.ToKind == "" || edge.ToSlug == "" {
			log.Printf("entity graph: skip underspecified line %d", lineNo)
			continue
		}
		edges = append(edges, edge)
	}
	if err := scanner.Err(); err != nil {
		// Returning the partial slice here would let Coalesce/Query silently
		// hand back stale results after a truncated/disk-error read. Surface
		// the error so callers can distinguish "no edges match" from
		// "scan aborted halfway."
		return nil, fmt.Errorf("entity graph: scan aborted after line %d: %w", lineNo, err)
	}
	return edges, nil
}

// coalesceKey is the dedup key used by the coalescer.
type coalesceKey struct {
	fromKind EntityKind
	fromSlug string
	toKind   EntityKind
	toSlug   string
}

// Coalesce reads the full log, deduplicates by (from, to), and returns one
// row per distinct edge. Newest-first by LastSeenTS.
func (g *EntityGraph) Coalesce() ([]CoalescedEdge, error) {
	edges, err := g.readAll()
	if err != nil {
		return nil, err
	}
	if len(edges) == 0 {
		return nil, nil
	}
	idx := make(map[coalesceKey]*CoalescedEdge, len(edges))
	order := make([]coalesceKey, 0, len(edges))
	for _, e := range edges {
		k := coalesceKey{e.FromKind, e.FromSlug, e.ToKind, e.ToSlug}
		row, ok := idx[k]
		if !ok {
			row = &CoalescedEdge{
				FromKind:        e.FromKind,
				FromSlug:        e.FromSlug,
				ToKind:          e.ToKind,
				ToSlug:          e.ToSlug,
				FirstSeenFactID: e.FirstSeenFactID,
				LastSeenTS:      e.LastSeenTS,
				OccurrenceCount: 1,
			}
			idx[k] = row
			order = append(order, k)
			continue
		}
		row.OccurrenceCount++
		if e.LastSeenTS.After(row.LastSeenTS) {
			row.LastSeenTS = e.LastSeenTS
		}
	}
	out := make([]CoalescedEdge, 0, len(order))
	for _, k := range order {
		out = append(out, *idx[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastSeenTS.Equal(out[j].LastSeenTS) {
			return out[i].LastSeenTS.After(out[j].LastSeenTS)
		}
		if out[i].ToKind != out[j].ToKind {
			return out[i].ToKind < out[j].ToKind
		}
		return out[i].ToSlug < out[j].ToSlug
	})
	return out, nil
}

// Direction describes which edge endpoints to return from Query.
type Direction string

const (
	DirectionOut  Direction = "out"
	DirectionIn   Direction = "in"
	DirectionBoth Direction = "both"
)

// Query filters the coalesced graph to edges touching (kind, slug) in the
// requested direction. Passing direction="" treats it as DirectionOut.
func (g *EntityGraph) Query(kind EntityKind, slug string, direction Direction) ([]CoalescedEdge, error) {
	if direction == "" {
		direction = DirectionOut
	}
	all, err := g.Coalesce()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	out := make([]CoalescedEdge, 0, 16)
	for _, e := range all {
		matchOut := e.FromKind == kind && e.FromSlug == slug
		matchIn := e.ToKind == kind && e.ToSlug == slug
		switch direction {
		case DirectionOut:
			if matchOut {
				out = append(out, e)
			}
		case DirectionIn:
			if matchIn {
				out = append(out, e)
			}
		case DirectionBoth:
			if matchOut || matchIn {
				out = append(out, e)
			}
		}
	}
	return out, nil
}
