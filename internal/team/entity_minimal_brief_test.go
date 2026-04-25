package team

// entity_minimal_brief_test.go — byte-exact and determinism tests for
// the ghost-entity brief writer. The §7.4 substrate-rebuild guarantee
// requires that two runs over the same IndexEntity produce
// byte-identical bytes (no map iteration, no clock reads, no random).

import (
	"strings"
	"testing"
	"time"
)

func TestMinimalBrief_PeopleWithFullSignals(t *testing.T) {
	createdAt := time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC)
	ent := IndexEntity{
		Slug:          "jane-doe",
		CanonicalSlug: "jane-doe",
		Kind:          "people",
		Aliases:       []string{"JD", "Jane"},
		Signals: Signals{
			Email:      "jane@example.com",
			Domain:     "example.com",
			PersonName: "Jane Doe",
			JobTitle:   "VP Engineering",
		},
		CreatedAt: createdAt,
		CreatedBy: ArchivistAuthor,
	}

	want := `---
slug: jane-doe
canonical_slug: jane-doe
kind: people
aliases:
  - Jane
  - JD
created_at: 2026-04-25T10:30:00Z
created_by: archivist
---

# Jane Doe

## Signals

- email: jane@example.com
- domain: example.com
- person_name: Jane Doe
- job_title: VP Engineering

_This page was auto-created when the team encountered a new entity. Facts will be synthesized here as they accumulate._
`

	got := MinimalBrief(ent)
	if got != want {
		t.Errorf("MinimalBrief mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestMinimalBrief_CompanyMinimal(t *testing.T) {
	createdAt := time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC)
	ent := IndexEntity{
		Slug:          "acme-corp",
		CanonicalSlug: "acme-corp",
		Kind:          "companies",
		Signals: Signals{
			Domain:     "acme.com",
			PersonName: "Acme Corp",
		},
		CreatedAt: createdAt,
		CreatedBy: ArchivistAuthor,
	}

	got := MinimalBrief(ent)

	// H1 should derive from the company "name" (PersonName is the name field
	// for non-people kinds in the IndexEntity Signals shape) — confirmed
	// against the resolver, which reuses Signals.PersonName for company-kind
	// names.
	if !strings.Contains(got, "\n# Acme Corp\n") {
		t.Errorf("expected H1 'Acme Corp' for company-kind brief; got:\n%s", got)
	}
	// No aliases section when none provided.
	if strings.Contains(got, "aliases:") {
		t.Errorf("aliases line should be absent when no aliases provided; got:\n%s", got)
	}
	// Non-empty signal renders as bullet.
	if !strings.Contains(got, "- domain: acme.com") {
		t.Errorf("expected domain bullet in Signals; got:\n%s", got)
	}
	// Empty signal (Email, JobTitle) must NOT produce orphan lines.
	if strings.Contains(got, "- email:") {
		t.Errorf("empty email signal must be skipped; got:\n%s", got)
	}
	if strings.Contains(got, "- job_title:") {
		t.Errorf("empty job_title signal must be skipped; got:\n%s", got)
	}
}

func TestMinimalBrief_CompanyHumanizesSlug(t *testing.T) {
	// PersonName empty: H1 must fall back to humanised canonical slug.
	createdAt := time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC)
	ent := IndexEntity{
		Slug:          "north-star",
		CanonicalSlug: "north-star",
		Kind:          "companies",
		CreatedAt:     createdAt,
		CreatedBy:     ArchivistAuthor,
	}
	got := MinimalBrief(ent)
	if !strings.Contains(got, "\n# North Star\n") {
		t.Errorf("expected humanised H1 'North Star'; got:\n%s", got)
	}
	// Empty signals → "(none)" placeholder.
	if !strings.Contains(got, "- (none)") {
		t.Errorf("expected (none) placeholder when all signals empty; got:\n%s", got)
	}
}

func TestMinimalBrief_DeterministicAcrossRuns(t *testing.T) {
	ent := IndexEntity{
		Slug:          "deterministic",
		CanonicalSlug: "deterministic",
		Kind:          "people",
		Aliases:       []string{"alpha", "beta", "Charlie", "delta", "Echo"},
		Signals: Signals{
			Email:      "d@example.com",
			Domain:     "example.com",
			PersonName: "D Person",
			JobTitle:   "Eng",
		},
		CreatedAt: time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC),
		CreatedBy: ArchivistAuthor,
	}

	// Run 100 times — any map-iteration nondeterminism would surface
	// stochastically across this many calls.
	first := MinimalBrief(ent)
	for i := 0; i < 100; i++ {
		got := MinimalBrief(ent)
		if got != first {
			t.Fatalf("MinimalBrief is non-deterministic on run %d:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
}

func TestMinimalBrief_AliasesSortedCaseInsensitive(t *testing.T) {
	ent := IndexEntity{
		Slug:          "case-insensitive-alias",
		CanonicalSlug: "case-insensitive-alias",
		Kind:          "people",
		Aliases:       []string{"zoe", "Alice", "BOB"},
		CreatedAt:     time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC),
		CreatedBy:     ArchivistAuthor,
	}

	got := MinimalBrief(ent)

	// Expected order: Alice, BOB, zoe (case-insensitive lex order).
	wantBlock := "aliases:\n  - Alice\n  - BOB\n  - zoe\n"
	if !strings.Contains(got, wantBlock) {
		t.Errorf("aliases not sorted case-insensitively\nwant block:\n%s\n--- full ---\n%s", wantBlock, got)
	}

	// Permuted input must produce identical output — sort is total, not
	// dependent on input order.
	ent2 := ent
	ent2.Aliases = []string{"BOB", "zoe", "Alice"}
	got2 := MinimalBrief(ent2)
	if got != got2 {
		t.Errorf("alias permutation broke determinism:\n--- got1 ---\n%s\n--- got2 ---\n%s", got, got2)
	}
}

func TestMinimalBrief_EmptyAliasesAreDropped(t *testing.T) {
	// Whitespace-only / empty alias entries must be dropped, not rendered
	// as `  - ` lines that would break frontmatter parsers downstream.
	ent := IndexEntity{
		Slug:          "drops-empties",
		CanonicalSlug: "drops-empties",
		Kind:          "people",
		Aliases:       []string{"", "  ", "Real"},
		CreatedAt:     time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC),
		CreatedBy:     ArchivistAuthor,
	}
	got := MinimalBrief(ent)
	if !strings.Contains(got, "  - Real\n") {
		t.Errorf("expected real alias rendered; got:\n%s", got)
	}
	if strings.Contains(got, "  - \n") || strings.Contains(got, "  -   ") {
		t.Errorf("blank alias must be dropped; got:\n%s", got)
	}
}

func TestMinimalBrief_FallsBackToSlugWhenCanonicalEmpty(t *testing.T) {
	// CanonicalSlug zero value must default to Slug so the frontmatter
	// stays valid even when the call site forgot to set it.
	ent := IndexEntity{
		Slug:      "fallback",
		Kind:      "people",
		CreatedAt: time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC),
		CreatedBy: ArchivistAuthor,
	}
	got := MinimalBrief(ent)
	if !strings.Contains(got, "canonical_slug: fallback\n") {
		t.Errorf("canonical_slug must default to Slug; got:\n%s", got)
	}
}
