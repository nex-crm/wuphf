package team

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// ── spySignalIndex ─────────────────────────────────────────────────────────────

// spySignalIndex is an in-memory SignalIndex implementation for tests.
type spySignalIndex struct {
	mu      sync.Mutex
	bySlug  map[string]IndexEntity // key = slug
	byEmail map[string]IndexEntity // key = normalised email
}

func newSpyIndex() *spySignalIndex {
	return &spySignalIndex{
		bySlug:  make(map[string]IndexEntity),
		byEmail: make(map[string]IndexEntity),
	}
}

func (s *spySignalIndex) add(e IndexEntity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bySlug[e.Slug] = e
	if e.Email != "" {
		s.byEmail[e.Email] = e
	}
}

func (s *spySignalIndex) EntityBySlug(_ context.Context, slug string) (IndexEntity, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.bySlug[slug]
	return e, ok, nil
}

func (s *spySignalIndex) EntityByEmail(_ context.Context, email string) (IndexEntity, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byEmail[email]
	return e, ok, nil
}

func (s *spySignalIndex) EntityByDomain(_ context.Context, _ string) ([]IndexEntity, error) {
	return nil, nil
}

func (s *spySignalIndex) EntityByName(_ context.Context, name string) ([]IndexEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normQuery := normalizeNameForMatch(name)
	var out []IndexEntity
	for _, e := range s.bySlug {
		if strings.Contains(normalizeNameForMatch(e.Name), normQuery) || normalizeNameForMatch(e.Name) == normQuery {
			out = append(out, e)
		}
	}
	return out, nil
}

// ── Tests for ResolveEntity ───────────────────────────────────────────────────

func TestResolveEntity_ExistingSlugHonored(t *testing.T) {
	idx := newSpyIndex()
	idx.add(IndexEntity{Slug: "sarah-jones", Kind: EntityKindPeople, Name: "Sarah Jones"})

	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "sarah-j",
		ExistingSlug: "sarah-jones",
		Signals:      Signals{PersonName: "Sarah Jones"},
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Slug != "sarah-jones" {
		t.Errorf("slug: want %q got %q", "sarah-jones", got.Slug)
	}
	if !got.Matched {
		t.Error("want Matched=true")
	}
	if got.MatchReason != "existing_slug_honored" {
		t.Errorf("reason: want %q got %q", "existing_slug_honored", got.MatchReason)
	}
}

func TestResolveEntity_HallucinatedExistingSlug_FallsThrough(t *testing.T) {
	idx := newSpyIndex()
	// Index has "sarah-jones" but NOT "sarah-j".
	idx.add(IndexEntity{Slug: "sarah-jones", Kind: EntityKindPeople, Name: "Sarah Jones", Email: "sarah@example.com"})

	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "sarah-j",
		ExistingSlug: "sarah-j", // hallucinated — not in index
		Signals:      Signals{Email: "sarah@example.com"},
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall through to email match and find sarah-jones.
	if got.Slug != "sarah-jones" {
		t.Errorf("slug: want %q got %q", "sarah-jones", got.Slug)
	}
	if got.MatchReason != "email_match" {
		t.Errorf("reason: want %q got %q", "email_match", got.MatchReason)
	}
}

func TestResolveEntity_EmailMatchOverridesProposedSlug(t *testing.T) {
	idx := newSpyIndex()
	idx.add(IndexEntity{Slug: "michael-chen", Kind: EntityKindPeople, Name: "Michael Chen", Email: "michael@corp.com"})

	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "michael",
		Signals:      Signals{Email: "MICHAEL@corp.com"}, // uppercase — should normalise
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Slug != "michael-chen" {
		t.Errorf("slug: want %q got %q", "michael-chen", got.Slug)
	}
	if got.MatchReason != "email_match" {
		t.Errorf("reason: want %q got %q", "email_match", got.MatchReason)
	}
}

func TestResolveEntity_FuzzyNameSingleMatch(t *testing.T) {
	idx := newSpyIndex()
	idx.add(IndexEntity{Slug: "sarah-jones", Kind: EntityKindPeople, Name: "Sarah Jones"})

	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "sarah-j-2",
		Signals:      Signals{PersonName: "Sarah Jones"}, // identical name → JW = 1.0
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Slug != "sarah-jones" {
		t.Errorf("slug: want %q got %q", "sarah-jones", got.Slug)
	}
	if got.MatchReason != "fuzzy_name" {
		t.Errorf("reason: want %q got %q", "fuzzy_name", got.MatchReason)
	}
}

func TestResolveEntity_FuzzyNameAmbiguous_NoEntityCreated(t *testing.T) {
	idx := newSpyIndex()
	// Two entries with identical names — ambiguous.
	idx.add(IndexEntity{Slug: "sarah-jones-a", Kind: EntityKindPeople, Name: "Sarah Jones"})
	idx.add(IndexEntity{Slug: "sarah-jones-b", Kind: EntityKindPeople, Name: "Sarah Jones"})

	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "sarah-jones",
		Signals:      Signals{PersonName: "Sarah Jones"},
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Matched {
		t.Error("want Matched=false for ambiguous case")
	}
	if got.MatchReason != "ambiguous" {
		t.Errorf("reason: want %q got %q", "ambiguous", got.MatchReason)
	}
	// Proposed slug must be returned unchanged — caller sends to human-review.
	if got.Slug != "sarah-jones" {
		t.Errorf("slug: want proposed %q got %q", "sarah-jones", got.Slug)
	}
}

func TestResolveEntity_GhostEntityCreated(t *testing.T) {
	idx := newSpyIndex()
	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "new-person",
		Signals:      Signals{PersonName: "New Person"},
		Confidence:   0.8,
		Ghost:        true,
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Slug != "new-person" {
		t.Errorf("slug: want %q got %q", "new-person", got.Slug)
	}
	if got.MatchReason != "new_entity" {
		t.Errorf("reason: want %q got %q", "new_entity", got.MatchReason)
	}
	if !got.GhostEntity {
		t.Error("want GhostEntity=true")
	}
}

func TestResolveEntity_SlugCollisionSuffix(t *testing.T) {
	idx := newSpyIndex()
	// "sarah-jones" already taken.
	idx.add(IndexEntity{Slug: "sarah-jones", Kind: EntityKindPeople, Name: "Someone Else"})

	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "sarah-jones",
		// No signals → new-entity path with collision avoidance.
	}
	got, err := ResolveEntity(context.Background(), idx, p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Slug != "sarah-jones-2" {
		t.Errorf("slug: want %q got %q", "sarah-jones-2", got.Slug)
	}
}

// ── Tests for entityResolverGate (Fix H11) ────────────────────────────────────

func TestEntityResolverGate_TwoGoroutinesSameGhost_SameSlug(t *testing.T) {
	idx := newSpyIndex()
	gate := newEntityResolverGate()
	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "michael-chen",
		Signals:      Signals{PersonName: "Michael Chen"},
		Ghost:        true,
	}

	const workers = 2
	slugs := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			r, err := gate.Resolve(context.Background(), idx, p)
			if err != nil {
				t.Errorf("goroutine %d: %v", workerIdx, err)
				return
			}
			slugs[workerIdx] = r.Slug
		}(i)
	}
	wg.Wait()

	if slugs[0] != slugs[1] {
		t.Errorf("both goroutines must get same slug; got %q and %q", slugs[0], slugs[1])
	}
}

func TestEntityResolverGate_TenGoroutinesSameTarget_AllSameSlug(t *testing.T) {
	idx := newSpyIndex()
	gate := newEntityResolverGate()
	p := ProposedEntity{
		Kind:         EntityKindPeople,
		ProposedSlug: "michael-chen",
		Signals:      Signals{PersonName: "Michael Chen"},
		Ghost:        true,
	}

	const n = 10
	slugs := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			r, err := gate.Resolve(context.Background(), idx, p)
			if err != nil {
				t.Errorf("goroutine %d: %v", workerIdx, err)
				return
			}
			slugs[workerIdx] = r.Slug
		}(i)
	}
	wg.Wait()

	// All goroutines must receive the same slug.
	for i, slug := range slugs {
		if slug != slugs[0] {
			t.Errorf("goroutine %d: want slug %q got %q", i, slugs[0], slug)
		}
	}
}

// ── Tests for JaroWinkler ─────────────────────────────────────────────────────

func TestJaroWinkler_KnownValues(t *testing.T) {
	cases := []struct {
		a, b    string
		wantMin float64
	}{
		{"sarah jones", "sarah jones", 1.0},
		{"sarah jones", "sarah jonez", 0.96},
		{"michael chen", "michael chang", 0.90},
		{"abc", "xyz", 0.0},
	}
	for _, tc := range cases {
		score := jaroWinkler(tc.a, tc.b)
		if score < tc.wantMin {
			t.Errorf("jaroWinkler(%q, %q) = %.4f; want >= %.4f", tc.a, tc.b, score, tc.wantMin)
		}
	}
}
