// Package migration ports team knowledge out of legacy memory backends
// (Nex, GBrain) and into the WUPHF markdown wiki at ~/.wuphf/wiki/team/.
//
// Why this exists
// ===============
//
// Existing Nex or GBrain installs store their team knowledge in those legacy
// backends. Moving to WUPHF's markdown wiki without a migration path means
// walking away from that prior investment. This package closes the gap:
// `wuphf memory migrate --from {nex,gbrain}` walks the source, converts each
// record to a standard wiki article, and commits it via the existing wiki
// worker so the index regenerates and SSE events fire the same way they
// would for any other write.
//
// Design notes
// ============
//
//   - Adapters expose a single Iter() method that returns a channel of
//     MigrationRecord. Streaming (not a slice) so large backends don't
//     balloon memory and errors can terminate the walk mid-stream.
//   - The writer owns identity ("migrate" slug → `migrate <migrate@wuphf.local>`
//     via Repo.runGitLocked), path construction, and dedup. Adapters stay
//     ignorant of wiki conventions.
//   - Serial by design (v1). The wiki worker is single-reader; parallel
//     imports would just queue up behind it. Keep the code simple.
package migration

import (
	"context"
	"time"
)

// Kind categorises a record into a wiki subtree. New kinds can be added
// without breaking callers; unknown kinds collapse to "misc".
type Kind string

const (
	// KindPeople maps to team/people/{slug}.md — profile-style notes on
	// humans and agents.
	KindPeople Kind = "people"
	// KindCompanies maps to team/companies/{slug}.md — external org briefs.
	KindCompanies Kind = "companies"
	// KindCustomers maps to team/customers/{slug}.md — customer accounts.
	KindCustomers Kind = "customers"
	// KindTopics maps to team/topics/{slug}.md — subject-area knowledge.
	KindTopics Kind = "topics"
	// KindNotes maps to team/notes/{slug}.md — general retained notes.
	KindNotes Kind = "notes"
	// KindMisc is the fallback when a record kind can't be classified.
	KindMisc Kind = "misc"
)

// NormalizeKind maps a free-form type string from a source backend onto a
// known Kind. Empty / unknown inputs collapse to KindMisc so the migration
// can still proceed without dropping data.
func NormalizeKind(s string) Kind {
	switch s {
	case "person", "people", "contact", "contacts":
		return KindPeople
	case "company", "companies", "organisation", "organization", "org":
		return KindCompanies
	case "customer", "customers", "account", "accounts":
		return KindCustomers
	case "topic", "topics", "subject":
		return KindTopics
	case "note", "notes", "memory", "memories":
		return KindNotes
	case "":
		return KindNotes
	default:
		return KindMisc
	}
}

// MigrationRecord is one unit of source content ready to become a wiki
// article. All fields except Content can be derived from a fallback when
// missing; Content is the only hard requirement.
type MigrationRecord struct {
	// Kind determines the wiki subtree (team/{kind}/).
	Kind Kind
	// Slug is the base filename (no extension). Must be a-z0-9 plus hyphen.
	Slug string
	// Title is the first-line heading for the article. Falls back to Slug.
	Title string
	// Content is the body bytes written to disk. Front-matter optional —
	// the writer renders its own standard header regardless.
	Content string
	// Source identifies the originating backend ("nex" or "gbrain"). Used
	// to disambiguate filenames on dedup collisions.
	Source string
	// Timestamp is when the record was last updated upstream. Zero values
	// are tolerated (the writer stamps time.Now() into the rendered article).
	Timestamp time.Time
}

// Adapter is the shared contract every source backend implements. Kept
// deliberately tiny (one method) so test fakes are trivial and production
// adapters can back onto whatever CLI / HTTP surface their upstream exposes.
type Adapter interface {
	// Iter streams records from the source until the channel closes. The
	// caller is expected to drain the channel; adapters should close it
	// when the source is exhausted or ctx is cancelled. Errors encountered
	// mid-walk are surfaced via the error return value (for fatal startup
	// failures) or logged and skipped (for per-record issues) — the choice
	// is the adapter's to make based on what the upstream can tell us.
	Iter(ctx context.Context) (<-chan MigrationRecord, error)
}
