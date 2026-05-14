package team

// entity_facts.go is the append-only fact log for v1.2 entity briefs.
//
// Facts live at team/entities/{kind}-{slug}.facts.jsonl inside the wiki
// repo. Each line is one atomic observation recorded by an agent. The file
// is append-only — wrong facts get counter-facts, not deletions (same
// principle as git itself, see project_entity_briefs_v1_2.md).
//
// Writes go through the WikiWorker queue so we reuse the single-writer
// invariant the rest of the wiki relies on. One fact = one git commit
// authored by the recording agent. The commit message includes a short
// preview of the fact so the audit log stays human-readable.
//
// Read path (List, CountSinceSHA) does NOT touch the queue — it streams
// the jsonl directly from disk, skipping malformed lines with a warning
// (mirrors promotion_log.go recovery posture).

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// EntityKind is the narrow set of wiki subtrees we treat as "entities" for
// brief synthesis. Matches the existing IA — no new directories.
type EntityKind string

const (
	EntityKindPeople    EntityKind = "people"
	EntityKindCompanies EntityKind = "companies"
	EntityKindCustomers EntityKind = "customers"
)

// ValidEntityKinds lists every kind the fact log accepts. Any other value is
// rejected at the API boundary — there is no fallback to a generic "entity"
// bucket.
func ValidEntityKinds() []EntityKind {
	return []EntityKind{EntityKindPeople, EntityKindCompanies, EntityKindCustomers}
}

// MaxFactTextLen is the hard cap on a single fact's text. Picked to keep
// lines comfortable for manual review in any editor and to bound how much
// prompt tokens a single append can cost on the next synthesis.
const MaxFactTextLen = 4000

// ErrFactLogNotRunning is returned when Append is called without a wiki
// worker. The broker wires these together in ensureWikiWorker; tests using
// FactLog directly must pass a worker explicitly.
var ErrFactLogNotRunning = errors.New("entity facts: worker is not attached")

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Fact is one atomic observation recorded by an agent.
type Fact struct {
	ID         string     `json:"id"`
	Kind       EntityKind `json:"kind"`
	Slug       string     `json:"slug"`
	Text       string     `json:"text"`
	SourcePath string     `json:"source_path,omitempty"`
	RecordedBy string     `json:"recorded_by"`
	CreatedAt  time.Time  `json:"created_at"`
}

// FactLog is the append-only log rooted in a wiki repo. It is safe to share
// across goroutines — every operation takes its own lock and either streams
// directly from disk (reads) or enqueues through the WikiWorker (writes).
type FactLog struct {
	worker *WikiWorker
	mu     sync.Mutex
}

// NewFactLog constructs a FactLog backed by the supplied worker. The worker
// must be running before Append is called.
func NewFactLog(worker *WikiWorker) *FactLog {
	return &FactLog{worker: worker}
}

// FactLogPath returns the path, relative to the wiki root, where the
// jsonl for a single entity is stored. Exported for tests + handlers.
func FactLogPath(kind EntityKind, slug string) string {
	return filepath.ToSlash(filepath.Join("team", "entities", string(kind)+"-"+slug+".facts.jsonl"))
}

// ValidateFactInput checks every field of a prospective fact. Returns nil
// when the fact is acceptable to persist. Exported so HTTP handlers can
// validate before they format a response.
func ValidateFactInput(kind EntityKind, slug, text, sourcePath, recordedBy string) error {
	found := false
	for _, k := range ValidEntityKinds() {
		if k == kind {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("entity_kind must be one of people|companies|customers; got %q", kind)
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("entity_slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("fact text is required")
	}
	if len(text) > MaxFactTextLen {
		return fmt.Errorf("fact text must be <= %d chars; got %d", MaxFactTextLen, len(text))
	}
	if strings.TrimSpace(recordedBy) == "" {
		return fmt.Errorf("recorded_by is required")
	}
	if s := strings.TrimSpace(sourcePath); s != "" {
		if !(strings.HasPrefix(s, "agents/") || strings.HasPrefix(s, "team/")) {
			return fmt.Errorf("source_path must start with agents/ or team/; got %q", sourcePath)
		}
	}
	return nil
}

// Append validates the inputs, serializes one Fact, and enqueues the append
// through the wiki worker. Returns the persisted Fact on success.
func (l *FactLog) Append(ctx context.Context, kind EntityKind, slug, text, sourcePath, recordedBy string) (Fact, error) {
	if l == nil || l.worker == nil {
		return Fact{}, ErrFactLogNotRunning
	}
	text = strings.TrimSpace(text)
	recordedBy = strings.TrimSpace(recordedBy)
	sourcePath = strings.TrimSpace(sourcePath)
	if err := ValidateFactInput(kind, slug, text, sourcePath, recordedBy); err != nil {
		return Fact{}, err
	}

	// Deterministic ID: hash of immutable content fields so the same fact
	// recorded twice produces the same ID. This enables dedup at both the
	// JSONL append layer (below) and the SQLite UpsertFact layer.
	factID := deterministicFactID(kind, slug, text, recordedBy)

	fact := Fact{
		ID:         factID,
		Kind:       kind,
		Slug:       slug,
		Text:       text,
		SourcePath: sourcePath,
		RecordedBy: recordedBy,
		CreatedAt:  time.Now().UTC(),
	}

	line, err := json.Marshal(fact)
	if err != nil {
		return Fact{}, fmt.Errorf("entity facts: marshal: %w", err)
	}

	relPath := FactLogPath(kind, slug)
	// The WikiWorker needs the full merged contents for its append mode —
	// we serialize through the queue so we read the existing file under
	// the same lock that commits the result.
	l.mu.Lock()
	defer l.mu.Unlock()

	existing := l.readExistingLocked(relPath)

	// Dedup: skip append if a fact with the same ID already exists in the
	// file. This prevents duplicate entries when the same observation is
	// recorded multiple times (e.g. re-extraction, retry after timeout).
	// Return the persisted fact so callers see the original CreatedAt.
	if existingFact, found := findFactInJSONL(existing, factID); found {
		return existingFact, nil
	}
	buf := make([]byte, 0, len(existing)+len(line)+1)
	if len(existing) > 0 {
		buf = append(buf, existing...)
		if !strings.HasSuffix(string(existing), "\n") {
			buf = append(buf, '\n')
		}
	}
	buf = append(buf, line...)
	buf = append(buf, '\n')

	msg := fmt.Sprintf("fact: %s/%s — %s", kind, slug, firstLine(text))
	if _, _, err := l.worker.EnqueueEntityFact(ctx, recordedBy, relPath, string(buf), msg); err != nil {
		return Fact{}, fmt.Errorf("entity facts: enqueue: %w", err)
	}
	return fact, nil
}

// readExistingLocked reads the current bytes at relPath, or an empty slice
// if the file does not exist. Caller holds l.mu.
func (l *FactLog) readExistingLocked(relPath string) []byte {
	full := filepath.Join(l.worker.Repo().Root(), filepath.FromSlash(relPath))
	bytes, err := os.ReadFile(full)
	if err != nil {
		return nil
	}
	return bytes
}

// List returns every fact for the given entity, newest first. Malformed
// lines are skipped with a warning. Missing file returns (nil, nil).
func (l *FactLog) List(kind EntityKind, slug string) ([]Fact, error) {
	if l == nil || l.worker == nil {
		return nil, ErrFactLogNotRunning
	}
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("entity_slug must match ^[a-z0-9][a-z0-9-]*$; got %q", slug)
	}
	found := false
	for _, k := range ValidEntityKinds() {
		if k == kind {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("entity_kind must be one of people|companies|customers; got %q", kind)
	}

	relPath := FactLogPath(kind, slug)
	full := filepath.Join(l.worker.Repo().Root(), filepath.FromSlash(relPath))
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("entity facts: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	lineNo := 0
	facts := make([]Fact, 0, 16)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var fact Fact
		if err := json.Unmarshal([]byte(line), &fact); err != nil {
			log.Printf("entity facts: skip malformed line %d in %s: %v", lineNo, relPath, err)
			continue
		}
		if fact.ID == "" || fact.Kind == "" || fact.Slug == "" {
			log.Printf("entity facts: skip underspecified line %d in %s", lineNo, relPath)
			continue
		}
		facts = append(facts, fact)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("entity facts: scanner error in %s after line %d: %v", relPath, lineNo, err)
	}

	// Newest first.
	sort.SliceStable(facts, func(i, j int) bool {
		return facts[i].CreatedAt.After(facts[j].CreatedAt)
	})
	return facts, nil
}

// CountSinceSHA returns the number of facts recorded after the given commit
// SHA (exclusive). When sha is empty, every fact counts. When sha does not
// appear in the repo history (or the file predates the sha), every fact
// counts — same semantics as "no prior synthesis."
func (l *FactLog) CountSinceSHA(ctx context.Context, kind EntityKind, slug, sha string) (int, error) {
	facts, err := l.List(kind, slug)
	if err != nil {
		return 0, err
	}
	sha = strings.TrimSpace(sha)
	if sha == "" || l == nil || l.worker == nil {
		return len(facts), nil
	}

	// Resolve the commit's timestamp. Facts with CreatedAt strictly after
	// that timestamp are "new". If the sha doesn't resolve, return the
	// whole count — the brief has never been synthesized cleanly.
	ts, err := l.commitTimestamp(ctx, sha)
	if err != nil {
		// Documented contract above: an unresolvable SHA means "treat
		// every fact as new" — the brief hasn't been synthesized at
		// this revision yet, so the synthesizer needs to run on the
		// full set. Don't propagate the error.
		return len(facts), nil //nolint:nilerr // intentional: unresolved SHA = all-facts-new, per doc contract
	}
	// Commit timestamps are second-precision; fact CreatedAt carries
	// sub-second precision. Compare at second resolution so a fact
	// created in the same second as the referenced commit is NOT
	// counted as "new."
	refSec := ts.UTC().Unix()
	n := 0
	for _, f := range facts {
		if f.CreatedAt.UTC().Unix() > refSec {
			n++
		}
	}
	return n, nil
}

// commitTimestamp looks up the commit timestamp for a given short SHA.
func (l *FactLog) commitTimestamp(ctx context.Context, sha string) (time.Time, error) {
	repo := l.worker.Repo()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out, err := repo.runGitLocked(ctx, "system", "show", "-s", "--format=%cI", sha)
	if err != nil {
		return time.Time{}, fmt.Errorf("entity facts: resolve sha %s: %w: %s", sha, err, out)
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return time.Time{}, fmt.Errorf("entity facts: empty timestamp for sha %s", sha)
	}
	ts, err := time.Parse(time.RFC3339, line)
	if err != nil {
		return time.Time{}, fmt.Errorf("entity facts: parse timestamp %q: %w", line, err)
	}
	return ts.UTC(), nil
}

// deterministicFactID computes a stable ID from the immutable content fields.
// The same observation recorded twice produces the same ID, enabling dedup at
// both the JSONL append layer and the SQLite UpsertFact layer. The ID is a
// 16-character hex prefix of SHA-256 — collision probability is negligible for
// the expected fact counts per entity (hundreds, not millions).
func deterministicFactID(kind EntityKind, slug, text, recordedBy string) string {
	h := sha256.New()
	h.Write([]byte(kind))
	h.Write([]byte{0}) // separator
	h.Write([]byte(slug))
	h.Write([]byte{0})
	h.Write([]byte(text))
	h.Write([]byte{0})
	h.Write([]byte(recordedBy))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// findFactInJSONL scans existing JSONL bytes for a fact with the given ID.
// Returns the full Fact and true if found, so callers get the original
// CreatedAt rather than a freshly minted timestamp.
func findFactInJSONL(existing []byte, factID string) (Fact, bool) {
	if len(existing) == 0 || factID == "" {
		return Fact{}, false
	}
	// Fast path: search for the ID string in the raw bytes before parsing.
	// This avoids JSON decoding when the ID is clearly absent.
	if !bytes.Contains(existing, []byte(factID)) {
		return Fact{}, false
	}
	// Slow path: decode each line to confirm it's an "id" field match.
	scanner := bufio.NewScanner(bytes.NewReader(existing))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var f Fact
		if json.Unmarshal(line, &f) == nil && f.ID == factID {
			return f, true
		}
	}
	return Fact{}, false
}
