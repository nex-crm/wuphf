package team

// entity_resolver.go enforces the slug-invention guard described in the wiki
// intelligence design doc (docs/specs/WIKI-SCHEMA.md §7.1, §7.2).
//
// The resolver is the single Go-level gate that decides which canonical slug
// a proposed entity should receive. The LLM extraction prompt is advisory —
// even if the model invents a plausible-looking slug, the resolver always
// checks the signal index first and falls through if the claimed slug is not
// actually present.
//
// Concurrency: ResolveEntity is safe for concurrent use. For the ghost-entity
// single-flight case (Fix H11), call sites should hold an *entityResolverGate
// and pass the gate's broadcasted result to avoid issuing multiple UpsertEntity
// calls for the same subject.

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"unicode"
)

// ── Public types ──────────────────────────────────────────────────────────────

// Signals carries the structured signals the LLM extractor found for a
// proposed entity. All fields are optional; the resolver uses whichever
// are non-empty to identify an existing record.
type Signals struct {
	// Email is the normalised (lowercase) email address, if known.
	Email string
	// PersonName is the display name of the person or company.
	PersonName string
	// Domain is the web domain (companies / customers), e.g. "acme.com".
	Domain string
}

// ProposedEntity is the resolver's input: everything the LLM extraction
// pipeline produced for one entity mention.
type ProposedEntity struct {
	Kind         EntityKind
	ProposedSlug string
	// ExistingSlug is non-empty when the LLM claimed a match against a known
	// slug. The resolver verifies the claim; if the slug does not exist in the
	// index, it logs a warning and falls through to signal-based resolution.
	ExistingSlug string
	Signals      Signals
	Aliases      []string
	Confidence   float64
	// Ghost marks entities the extractor explicitly called out as "no strong
	// identity signal" — create a new record without attempting fuzzy matching.
	Ghost bool
}

// ResolvedEntity is the resolver's output.
type ResolvedEntity struct {
	// Slug is the final canonical slug to use.
	Slug string
	Kind EntityKind
	// Matched is true when the resolver found an existing entity.
	Matched bool
	// MatchReason documents which signal produced the match. One of:
	//   "existing_slug_honored" | "email_match" | "fuzzy_name" | "new_entity" | "ambiguous"
	MatchReason string
	// GhostEntity reflects whether a fresh record was created.
	GhostEntity bool
}

// IndexEntity is the minimal shape the resolver needs from the signal index.
type IndexEntity struct {
	Slug  string
	Kind  EntityKind
	Name  string
	Email string // normalised lowercase
}

// SignalIndex is the narrow read interface the resolver depends on. Implement
// on the in-memory store and the SQLite store; tests use spySignalIndex below.
type SignalIndex interface {
	// EntityBySlug looks up a single entity by its canonical slug within the
	// given kind. Returns (zero, false, nil) when not found.
	EntityBySlug(ctx context.Context, slug string) (IndexEntity, bool, error)
	// EntityByEmail returns the entity with this normalised email, if any.
	EntityByEmail(ctx context.Context, email string) (IndexEntity, bool, error)
	// EntityByDomain returns all entities associated with this web domain.
	EntityByDomain(ctx context.Context, domain string) ([]IndexEntity, error)
	// EntityByName returns all entities whose names are partial or full matches
	// for the query string. The resolver applies its own fuzzy filter on top.
	EntityByName(ctx context.Context, name string) ([]IndexEntity, error)
}

// ── Resolver ──────────────────────────────────────────────────────────────────

// ResolveEntity applies the slug-invention guard and returns the canonical
// ResolvedEntity. It never mutates the index — callers are responsible for
// creating new records when Matched is false and MatchReason is "new_entity".
//
// Algorithm (in priority order):
//  1. If p.ExistingSlug is set, validate it against the index.
//     Honoured → matched; missing → warning + fall through.
//  2. If p.Signals.Email is set, look up by email.
//  3. If p.Signals.PersonName is set, fuzzy name match (JaroWinkler ≥ 0.9).
//     Exactly one high-confidence match → matched.
//     Multiple matches → Matched=false, MatchReason="ambiguous".
//  4. Otherwise create a new entity with collision-safe slug.
func ResolveEntity(ctx context.Context, idx SignalIndex, p ProposedEntity) (ResolvedEntity, error) {
	// Step 1 — honor the LLM's claimed existing slug, but verify it.
	if p.ExistingSlug != "" {
		entity, found, err := idx.EntityBySlug(ctx, p.ExistingSlug)
		if err != nil {
			return ResolvedEntity{}, fmt.Errorf("entity resolver: slug lookup %q: %w", p.ExistingSlug, err)
		}
		if found {
			return ResolvedEntity{
				Slug:        entity.Slug,
				Kind:        p.Kind,
				Matched:     true,
				MatchReason: "existing_slug_honored",
			}, nil
		}
		// LLM hallucinated a slug that does not exist in the index.
		log.Printf("entity resolver: LLM claimed existing_slug=%q but not found in index — falling through to signal match", p.ExistingSlug)
		// Fall through to signal-based resolution below.
	}

	// Step 2 — email signal (strongest identity signal after an explicit slug).
	if email := normalizeEmail(p.Signals.Email); email != "" {
		entity, found, err := idx.EntityByEmail(ctx, email)
		if err != nil {
			return ResolvedEntity{}, fmt.Errorf("entity resolver: email lookup %q: %w", email, err)
		}
		if found {
			return ResolvedEntity{
				Slug:        entity.Slug,
				Kind:        p.Kind,
				Matched:     true,
				MatchReason: "email_match",
			}, nil
		}
	}

	// Step 3 — fuzzy name match (JaroWinkler ≥ 0.9, same Kind only).
	if !p.Ghost && p.Signals.PersonName != "" {
		candidates, err := idx.EntityByName(ctx, p.Signals.PersonName)
		if err != nil {
			return ResolvedEntity{}, fmt.Errorf("entity resolver: name lookup %q: %w", p.Signals.PersonName, err)
		}

		// Filter to the same kind and apply the JaroWinkler threshold.
		const jwThreshold = 0.9
		normQuery := normalizeNameForMatch(p.Signals.PersonName)
		var hits []IndexEntity
		for _, c := range candidates {
			if c.Kind != p.Kind {
				continue
			}
			normCandidate := normalizeNameForMatch(c.Name)
			if jaroWinkler(normQuery, normCandidate) >= jwThreshold {
				hits = append(hits, c)
			}
		}

		switch len(hits) {
		case 1:
			return ResolvedEntity{
				Slug:        hits[0].Slug,
				Kind:        p.Kind,
				Matched:     true,
				MatchReason: "fuzzy_name",
			}, nil
		default:
			if len(hits) > 1 {
				// Ambiguous — do NOT create a new entity; surface for human review.
				return ResolvedEntity{
					Slug:        p.ProposedSlug,
					Kind:        p.Kind,
					Matched:     false,
					MatchReason: "ambiguous",
				}, nil
			}
			// len(hits) == 0: no match, fall through to new-entity creation.
		}
	}

	// Step 4 — create a new entity with a collision-safe slug.
	slug, err := collisionSafeSlug(ctx, idx, p.ProposedSlug)
	if err != nil {
		return ResolvedEntity{}, fmt.Errorf("entity resolver: collision-safe slug for %q: %w", p.ProposedSlug, err)
	}
	return ResolvedEntity{
		Slug:        slug,
		Kind:        p.Kind,
		Matched:     false,
		MatchReason: "new_entity",
		GhostEntity: p.Ghost,
	}, nil
}

// collisionSafeSlug returns proposed when no entity with that slug exists, or
// proposed-2, proposed-3, … on collision. Stops at -100 to prevent runaway
// loops against a corrupted index.
func collisionSafeSlug(ctx context.Context, idx SignalIndex, proposed string) (string, error) {
	candidate := proposed
	for i := 2; i <= 100; i++ {
		_, found, err := idx.EntityBySlug(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("slug collision check %q: %w", candidate, err)
		}
		if !found {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", proposed, i)
	}
	return "", fmt.Errorf("could not find collision-free slug for %q after 100 attempts", proposed)
}

// ── entityResolverGate — single-flight for concurrent ghost entity creation ──

// resolveCall holds the in-flight result and a done channel that is closed
// (not sent to) after the result is written — this enables fan-out: all N
// waiters are unblocked when the channel closes, and all read the same result.
type resolveCall struct {
	result ResolvedEntity
	done   chan struct{} // closed when result is ready
}

// entityResolverGate prevents two goroutines from simultaneously resolving the
// same subject (same name/email/slug) and thereby issuing two UpsertEntity
// calls. The second caller waits on the done channel and reuses the first
// caller's ResolvedEntity without doing its own index round-trip.
//
// Uses close-to-broadcast semantics: the owner goroutine closes the done
// channel after writing the result, so ALL concurrent waiters unblock and
// read the same shared result field.
//
// Gate key precedence (matches ResolveEntity step order):
//  1. "email:<normalised>" when Signals.Email is set
//  2. "name:<normalised>"  when Signals.PersonName is set
//  3. "slug:<proposed>"    otherwise
type entityResolverGate struct {
	mu       sync.Mutex
	inFlight map[string]*resolveCall
}

// newEntityResolverGate returns a ready-to-use gate.
func newEntityResolverGate() *entityResolverGate {
	return &entityResolverGate{inFlight: make(map[string]*resolveCall)}
}

// gateKey derives the coalesce key from a ProposedEntity.
func gateKey(p ProposedEntity) string {
	if email := normalizeEmail(p.Signals.Email); email != "" {
		return "email:" + email
	}
	if p.Signals.PersonName != "" {
		return "name:" + normalizeNameForMatch(p.Signals.PersonName)
	}
	return "slug:" + p.ProposedSlug
}

// Resolve runs ResolveEntity through the gate. If another goroutine is already
// resolving the same subject, this call blocks until the first completes and
// then returns that result — the underlying store sees exactly one resolution
// attempt per distinct subject.
func (g *entityResolverGate) Resolve(ctx context.Context, idx SignalIndex, p ProposedEntity) (ResolvedEntity, error) {
	key := gateKey(p)

	g.mu.Lock()
	if call, ok := g.inFlight[key]; ok {
		// Another goroutine owns this key — wait for it to broadcast.
		g.mu.Unlock()
		select {
		case <-call.done:
			return call.result, nil
		case <-ctx.Done():
			return ResolvedEntity{}, fmt.Errorf("entity resolver gate: context cancelled waiting for in-flight resolution of %q: %w", key, ctx.Err())
		}
	}

	// We are the first; register ownership before dropping the lock.
	call := &resolveCall{done: make(chan struct{})}
	g.inFlight[key] = call
	g.mu.Unlock()

	// Resolve under no lock — the done channel gates all waiters.
	resolved, err := ResolveEntity(ctx, idx, p)

	// Write result and broadcast to all waiters by closing the done channel.
	call.result = resolved
	close(call.done)

	g.mu.Lock()
	delete(g.inFlight, key)
	g.mu.Unlock()

	return resolved, err
}

// ── String helpers ─────────────────────────────────────────────────────────────

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normalizeNameForMatch lowercases, strips punctuation/extra whitespace,
// producing a form suitable for JaroWinkler comparison.
func normalizeNameForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// ── JaroWinkler — inline implementation (~40 LOC) ─────────────────────────────
//
// TODO: replace with a package import once the wiki_index test coverage
// milestone lands (see WIKI-SCHEMA.md §10.1).
//
// Reference: Winkler (1990) "String Comparator Metrics and Enhanced Decision
// Rules in the Fellegi-Sunter Model of Record Linkage."

// jaroWinkler returns the Jaro-Winkler similarity in [0, 1]. Both strings
// must be Unicode normalised before calling (use normalizeNameForMatch).
func jaroWinkler(s, t string) float64 {
	jaro := jaroSimilarity(s, t)
	if jaro == 0 {
		return 0
	}
	// Prefix up to 4 matching chars at the start.
	const maxPrefix = 4
	const p = 0.1 // scaling factor (Winkler constant)
	l := 0
	rs := []rune(s)
	rt := []rune(t)
	maxL := len(rs)
	if len(rt) < maxL {
		maxL = len(rt)
	}
	if maxL > maxPrefix {
		maxL = maxPrefix
	}
	for i := 0; i < maxL; i++ {
		if rs[i] != rt[i] {
			break
		}
		l++
	}
	return jaro + float64(l)*p*(1-jaro)
}

func jaroSimilarity(s, t string) float64 {
	rs := []rune(s)
	rt := []rune(t)
	if len(rs) == 0 && len(rt) == 0 {
		return 1
	}
	if len(rs) == 0 || len(rt) == 0 {
		return 0
	}
	matchDist := int(math.Max(float64(len(rs)), float64(len(rt)))/2) - 1
	if matchDist < 0 {
		matchDist = 0
	}
	sMatched := make([]bool, len(rs))
	tMatched := make([]bool, len(rt))
	matches := 0
	for i, r := range rs {
		lo := i - matchDist
		if lo < 0 {
			lo = 0
		}
		hi := i + matchDist + 1
		if hi > len(rt) {
			hi = len(rt)
		}
		for j := lo; j < hi; j++ {
			if tMatched[j] || rt[j] != r {
				continue
			}
			sMatched[i] = true
			tMatched[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}
	transpositions := 0
	k := 0
	for i, m := range sMatched {
		if !m {
			continue
		}
		for !tMatched[k] {
			k++
		}
		if rs[i] != rt[k] {
			transpositions++
		}
		k++
	}
	m := float64(matches)
	return (m/float64(len(rs)) + m/float64(len(rt)) + (m-float64(transpositions)/2)/m) / 3
}
