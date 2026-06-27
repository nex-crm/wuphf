package team

// wiki_source.go defines the "source" identity layer for the Karpathy-style
// LLM wiki. Sources are the raw material the wiki is compiled FROM — captured
// office activity (completed tasks + deliverables, decisions, chat-thread
// digests) and explicit ingests (pasted docs, fetched URLs, freeform notes).
//
// The standalone on-disk source STORE was retired (G6): sources now flow
// through the gbrain capture path rather than being committed to a sources/
// subtree. What remains here is the stable identity surface that path relies
// on — SourceKind classification, DeriveSourceID for write-once dedupe keys,
// and ContentHashHex for stable change detection.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SourceKind enumerates where a source record came from. The first three are
// auto-captured office activity; the last three are explicit ingests.
type SourceKind string

const (
	SourceKindTask     SourceKind = "task"     // a completed task + its deliverables
	SourceKindDecision SourceKind = "decision" // a recorded decision
	SourceKindChat     SourceKind = "chat"     // a digested chat thread
	SourceKindDoc      SourceKind = "doc"      // explicit ingest: pasted document
	SourceKindURL      SourceKind = "url"      // explicit ingest: fetched URL
	SourceKindNote     SourceKind = "note"     // explicit ingest: freeform note
)

var validSourceKinds = map[SourceKind]struct{}{
	SourceKindTask:     {},
	SourceKindDecision: {},
	SourceKindChat:     {},
	SourceKindDoc:      {},
	SourceKindURL:      {},
	SourceKindNote:     {},
}

// IsValid reports whether k is a known source kind.
func (k SourceKind) IsValid() bool {
	_, ok := validSourceKinds[k]
	return ok
}

// SourceRecord is one immutable unit of raw material. The yaml tags define the
// on-disk frontmatter; Content is the body and is intentionally not encoded
// into the frontmatter (yaml:"-").
type SourceRecord struct {
	ID          string     `yaml:"id"`
	Kind        SourceKind `yaml:"kind"`
	Title       string     `yaml:"title"`
	Origin      string     `yaml:"origin,omitempty"`
	CapturedAt  time.Time  `yaml:"captured_at"`
	ContentHash string     `yaml:"content_hash"`
	Content     string     `yaml:"-"`
}

// ContentHashHex returns the lowercase hex SHA-256 of content after trimming
// trailing whitespace, so cosmetically-different-but-equal bodies hash equal.
func ContentHashHex(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(content, "\n ")))
	return hex.EncodeToString(sum[:])
}

var sourceSlugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// slugifySource lowercases s and collapses every run of non-alphanumeric
// characters to a single hyphen, trimming hyphens from the ends. Empty input
// (or input with no usable characters) yields "untitled".
func slugifySource(s string) string {
	out := sourceSlugUnsafe.ReplaceAllString(strings.ToLower(s), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "untitled"
	}
	return out
}

// DeriveSourceID builds a stable, filesystem-safe id for a source. When origin
// is supplied (a task id, channel slug, URL, …) the id is kind-origin so the
// same activity re-captures to the same path (write-once dedupe). Otherwise it
// falls back to kind-title plus a short content-hash suffix to avoid
// collisions between distinct notes that share a title.
func DeriveSourceID(kind SourceKind, origin, title, content string) string {
	if o := slugifySource(origin); o != "untitled" && strings.TrimSpace(origin) != "" {
		return fmt.Sprintf("%s-%s", kind, o)
	}
	base := slugifySource(title)
	return fmt.Sprintf("%s-%s-%s", kind, base, ContentHashHex(content)[:8])
}

// NewSourceRecord validates inputs and returns a fully-populated record with
// its content hash computed. capturedAt is normalized to UTC.
func NewSourceRecord(id string, kind SourceKind, title, origin, content string, capturedAt time.Time) (SourceRecord, error) {
	if strings.TrimSpace(id) == "" {
		return SourceRecord{}, fmt.Errorf("source: id is required")
	}
	if !kind.IsValid() {
		return SourceRecord{}, fmt.Errorf("source: invalid kind %q", kind)
	}
	if strings.TrimSpace(title) == "" {
		return SourceRecord{}, fmt.Errorf("source: title is required")
	}
	if strings.TrimSpace(content) == "" {
		return SourceRecord{}, fmt.Errorf("source: content is required")
	}
	if capturedAt.IsZero() {
		return SourceRecord{}, fmt.Errorf("source: capturedAt is required")
	}
	return SourceRecord{
		ID:          id,
		Kind:        kind,
		Title:       strings.TrimSpace(title),
		Origin:      strings.TrimSpace(origin),
		CapturedAt:  capturedAt.UTC(),
		ContentHash: ContentHashHex(content),
		Content:     content,
	}, nil
}
