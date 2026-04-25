package team

// wiki_index_sqlite.go — SQLiteFactStore: pure-Go persistent FactStore backend.
//
// Uses modernc.org/sqlite (no cgo). Schema per docs/specs/WIKI-SCHEMA.md §7.4.
// Open via NewSQLiteFactStore(path). All methods are goroutine-safe; the
// underlying *sql.DB handles the connection pool internally.
//
// §7.4 rebuild contract: CanonicalHashFacts serialises every fact row sorted by
// ID then sha256-hashes them — identical to the in-memory implementation so the
// contract test works against both backends.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLiteFactStore implements FactStore via modernc.org/sqlite.
type SQLiteFactStore struct {
	db *sql.DB
}

// NewSQLiteFactStore opens (or creates) the SQLite database at path and applies
// the schema. The caller must call Close() when done.
func NewSQLiteFactStore(path string) (*SQLiteFactStore, error) {
	// ?_journal=WAL reduces write latency; _busy_timeout prevents SQLITE_BUSY
	// errors under the broker's concurrent readers.
	dsn := path + "?_journal=WAL&_busy_timeout=5000&_foreign_keys=off"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open %s: %w", path, err)
	}
	// Single writer keeps WAL simple; readers share the pool.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &SQLiteFactStore{db: db}
	if err := s.applySchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite schema: %w", err)
	}
	return s, nil
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS facts (
  id                  TEXT PRIMARY KEY,
  entity_slug         TEXT NOT NULL,
  kind                TEXT,
  type                TEXT,
  triplet_subject     TEXT,
  triplet_predicate   TEXT,
  triplet_object      TEXT,
  text                TEXT NOT NULL,
  confidence          REAL,
  valid_from          TEXT,
  valid_until         TEXT,
  supersedes          TEXT,       -- JSON array
  contradicts_with    TEXT,       -- JSON array
  source_type         TEXT,
  source_path         TEXT,
  sentence_offset     INTEGER,
  artifact_excerpt    TEXT,
  created_at          TEXT NOT NULL,
  created_by          TEXT NOT NULL,
  reinforced_at       TEXT
);
CREATE INDEX IF NOT EXISTS idx_facts_entity   ON facts(entity_slug);
CREATE INDEX IF NOT EXISTS idx_facts_triplet  ON facts(triplet_subject, triplet_predicate);
CREATE INDEX IF NOT EXISTS idx_facts_triplet_pred_obj ON facts(triplet_predicate, triplet_object);

CREATE TABLE IF NOT EXISTS entities (
  slug                    TEXT PRIMARY KEY,
  canonical_slug          TEXT NOT NULL,
  kind                    TEXT NOT NULL,
  aliases                 TEXT,       -- JSON array
  signals_email           TEXT,
  signals_domain          TEXT,
  signals_person_name     TEXT,
  signals_job_title       TEXT,
  last_synthesized_sha    TEXT,
  last_synthesized_at     TEXT,
  fact_count_at_synth     INTEGER,
  created_at              TEXT,
  created_by              TEXT
);

CREATE TABLE IF NOT EXISTS edges (
  subject     TEXT NOT NULL,
  predicate   TEXT NOT NULL,
  object      TEXT NOT NULL,
  timestamp   TEXT,
  source_sha  TEXT,
  PRIMARY KEY (subject, predicate, object)
);
CREATE INDEX IF NOT EXISTS idx_edges_subject ON edges(subject);
CREATE INDEX IF NOT EXISTS idx_edges_object  ON edges(object);

CREATE TABLE IF NOT EXISTS redirects (
  slug_from   TEXT PRIMARY KEY,
  slug_to     TEXT NOT NULL,
  merged_at   TEXT,
  merged_by   TEXT,
  commit_sha  TEXT
);
`

func (s *SQLiteFactStore) applySchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, sqliteSchema)
	return err
}

// likeEscaper escapes the three SQL LIKE metacharacters so a raw user-supplied
// prefix string matches only itself when used with `... LIKE ? ESCAPE '\'`.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// --- FactStore interface --------------------------------------------------

func (s *SQLiteFactStore) UpsertFact(ctx context.Context, f TypedFact) error {
	supersedes, err := json.Marshal(f.Supersedes)
	if err != nil {
		return fmt.Errorf("marshal supersedes: %w", err)
	}
	contradicts, err := json.Marshal(f.ContradictsWith)
	if err != nil {
		return fmt.Errorf("marshal contradicts_with: %w", err)
	}

	var tripletSubject, tripletPredicate, tripletObject sql.NullString
	if f.Triplet != nil {
		tripletSubject = sql.NullString{String: f.Triplet.Subject, Valid: true}
		tripletPredicate = sql.NullString{String: f.Triplet.Predicate, Valid: true}
		tripletObject = sql.NullString{String: f.Triplet.Object, Valid: true}
	}

	var validFrom, validUntil, reinforcedAt sql.NullString
	if !f.ValidFrom.IsZero() {
		validFrom = sql.NullString{String: f.ValidFrom.UTC().Format(time.RFC3339), Valid: true}
	}
	if f.ValidUntil != nil {
		validUntil = sql.NullString{String: f.ValidUntil.UTC().Format(time.RFC3339), Valid: true}
	}
	if f.ReinforcedAt != nil {
		reinforcedAt = sql.NullString{String: f.ReinforcedAt.UTC().Format(time.RFC3339), Valid: true}
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO facts (
			id, entity_slug, kind, type,
			triplet_subject, triplet_predicate, triplet_object,
			text, confidence, valid_from, valid_until,
			supersedes, contradicts_with,
			source_type, source_path, sentence_offset, artifact_excerpt,
			created_at, created_by, reinforced_at
		) VALUES (?,?,?,?, ?,?,?, ?,?,?,?, ?,?, ?,?,?,?, ?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			entity_slug=excluded.entity_slug,
			kind=excluded.kind,
			type=excluded.type,
			triplet_subject=excluded.triplet_subject,
			triplet_predicate=excluded.triplet_predicate,
			triplet_object=excluded.triplet_object,
			text=excluded.text,
			confidence=excluded.confidence,
			valid_from=excluded.valid_from,
			valid_until=excluded.valid_until,
			supersedes=excluded.supersedes,
			contradicts_with=excluded.contradicts_with,
			source_type=excluded.source_type,
			source_path=excluded.source_path,
			sentence_offset=excluded.sentence_offset,
			artifact_excerpt=excluded.artifact_excerpt,
			created_at=excluded.created_at,
			created_by=excluded.created_by,
			reinforced_at=excluded.reinforced_at`,
		f.ID, f.EntitySlug, f.Kind, f.Type,
		tripletSubject, tripletPredicate, tripletObject,
		f.Text, f.Confidence, validFrom, validUntil,
		string(supersedes), string(contradicts),
		f.SourceType, f.SourcePath, f.SentenceOffset, f.ArtifactExcerpt,
		f.CreatedAt.UTC().Format(time.RFC3339), f.CreatedBy, reinforcedAt,
	)
	return err
}

func (s *SQLiteFactStore) UpsertEntity(ctx context.Context, e IndexEntity) error {
	aliases, err := json.Marshal(e.Aliases)
	if err != nil {
		return fmt.Errorf("marshal aliases: %w", err)
	}
	var lastSynthAt sql.NullString
	if !e.LastSynthesizedAt.IsZero() {
		lastSynthAt = sql.NullString{String: e.LastSynthesizedAt.UTC().Format(time.RFC3339), Valid: true}
	}
	var createdAt sql.NullString
	if !e.CreatedAt.IsZero() {
		createdAt = sql.NullString{String: e.CreatedAt.UTC().Format(time.RFC3339), Valid: true}
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO entities (
			slug, canonical_slug, kind, aliases,
			signals_email, signals_domain, signals_person_name, signals_job_title,
			last_synthesized_sha, last_synthesized_at, fact_count_at_synth,
			created_at, created_by
		) VALUES (?,?,?,?, ?,?,?,?, ?,?,?, ?,?)
		ON CONFLICT(slug) DO UPDATE SET
			canonical_slug=excluded.canonical_slug,
			kind=excluded.kind,
			aliases=excluded.aliases,
			signals_email=excluded.signals_email,
			signals_domain=excluded.signals_domain,
			signals_person_name=excluded.signals_person_name,
			signals_job_title=excluded.signals_job_title,
			last_synthesized_sha=excluded.last_synthesized_sha,
			last_synthesized_at=excluded.last_synthesized_at,
			fact_count_at_synth=excluded.fact_count_at_synth,
			created_at=excluded.created_at,
			created_by=excluded.created_by`,
		e.Slug, e.CanonicalSlug, e.Kind, string(aliases),
		e.Signals.Email, e.Signals.Domain, e.Signals.PersonName, e.Signals.JobTitle,
		e.LastSynthesizedSHA, lastSynthAt, e.FactCountAtSynth,
		createdAt, e.CreatedBy,
	)
	return err
}

func (s *SQLiteFactStore) UpsertEdge(ctx context.Context, e IndexEdge) error {
	var ts sql.NullString
	if !e.Timestamp.IsZero() {
		ts = sql.NullString{String: e.Timestamp.UTC().Format(time.RFC3339), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO edges (subject, predicate, object, timestamp, source_sha)
		VALUES (?,?,?,?,?)
		ON CONFLICT(subject, predicate, object) DO UPDATE SET
			timestamp=excluded.timestamp,
			source_sha=excluded.source_sha`,
		e.Subject, e.Predicate, e.Object, ts, e.SourceSHA,
	)
	return err
}

func (s *SQLiteFactStore) UpsertRedirect(ctx context.Context, r Redirect) error {
	var mergedAt sql.NullString
	if !r.MergedAt.IsZero() {
		mergedAt = sql.NullString{String: r.MergedAt.UTC().Format(time.RFC3339), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO redirects (slug_from, slug_to, merged_at, merged_by, commit_sha)
		VALUES (?,?,?,?,?)
		ON CONFLICT(slug_from) DO UPDATE SET
			slug_to=excluded.slug_to,
			merged_at=excluded.merged_at,
			merged_by=excluded.merged_by,
			commit_sha=excluded.commit_sha`,
		r.From, r.To, mergedAt, r.MergedBy, r.CommitSHA,
	)
	return err
}

func (s *SQLiteFactStore) GetFact(ctx context.Context, id string) (TypedFact, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, entity_slug, kind, type,
		        triplet_subject, triplet_predicate, triplet_object,
		        text, confidence, valid_from, valid_until,
		        supersedes, contradicts_with,
		        source_type, source_path, sentence_offset, artifact_excerpt,
		        created_at, created_by, reinforced_at
		 FROM facts WHERE id = ?`, id)
	f, err := scanFact(row)
	if err == sql.ErrNoRows {
		return TypedFact{}, false, nil
	}
	if err != nil {
		return TypedFact{}, false, err
	}
	return f, true, nil
}

func (s *SQLiteFactStore) ListFactsForEntity(ctx context.Context, slug string) ([]TypedFact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, entity_slug, kind, type,
		        triplet_subject, triplet_predicate, triplet_object,
		        text, confidence, valid_from, valid_until,
		        supersedes, contradicts_with,
		        source_type, source_path, sentence_offset, artifact_excerpt,
		        created_at, created_by, reinforced_at
		 FROM facts WHERE entity_slug = ?
		 ORDER BY created_at ASC`, slug)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanFacts(rows)
}

// ListFactsByPredicateObject returns every fact whose triplet predicate+object
// match exactly. Backed by idx_facts_triplet_pred_obj.
func (s *SQLiteFactStore) ListFactsByPredicateObject(ctx context.Context, predicate, object string) ([]TypedFact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, entity_slug, kind, type,
		        triplet_subject, triplet_predicate, triplet_object,
		        text, confidence, valid_from, valid_until,
		        supersedes, contradicts_with,
		        source_type, source_path, sentence_offset, artifact_excerpt,
		        created_at, created_by, reinforced_at
		 FROM facts
		 WHERE triplet_predicate = ? AND triplet_object = ?
		 ORDER BY id ASC`, predicate, object)
	if err != nil {
		return nil, fmt.Errorf("ListFactsByPredicateObject: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanFacts(rows)
}

// ListFactsByTriplet returns every fact whose triplet matches (subject, predicate)
// and whose triplet.object starts with objectPrefix. Empty objectPrefix matches
// any object. Backed by idx_facts_triplet (subject, predicate).
//
// objectPrefix may contain any bytes; SQL LIKE metacharacters are escaped so a
// literal "%" or "_" in the prefix matches only itself, not any-char wildcards.
func (s *SQLiteFactStore) ListFactsByTriplet(ctx context.Context, subject, predicate, objectPrefix string) ([]TypedFact, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if objectPrefix == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, entity_slug, kind, type,
			        triplet_subject, triplet_predicate, triplet_object,
			        text, confidence, valid_from, valid_until,
			        supersedes, contradicts_with,
			        source_type, source_path, sentence_offset, artifact_excerpt,
			        created_at, created_by, reinforced_at
			 FROM facts
			 WHERE triplet_subject = ? AND triplet_predicate = ?
			 ORDER BY id ASC`, subject, predicate)
	} else {
		// Escape SQL LIKE metacharacters so raw bytes in objectPrefix don't
		// turn into wildcards. Callers today funnel through NormalizeForFactID
		// ([a-z0-9-] only) so this is defensive, not corrective — but cheap.
		escaped := likeEscaper.Replace(objectPrefix)
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, entity_slug, kind, type,
			        triplet_subject, triplet_predicate, triplet_object,
			        text, confidence, valid_from, valid_until,
			        supersedes, contradicts_with,
			        source_type, source_path, sentence_offset, artifact_excerpt,
			        created_at, created_by, reinforced_at
			 FROM facts
			 WHERE triplet_subject = ? AND triplet_predicate = ?
			   AND triplet_object LIKE ? ESCAPE '\'
			 ORDER BY id ASC`, subject, predicate, escaped+"%")
	}
	if err != nil {
		return nil, fmt.Errorf("ListFactsByTriplet: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanFacts(rows)
}

// CountFacts returns the total number of rows in the facts table. Cheap —
// COUNT(*) hits the primary index on id. Used by cross-entity consumers
// to pre-check corpus size before triggering a full scan via ListAllFacts.
func (s *SQLiteFactStore) CountFacts(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facts`).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: count facts: %w", err)
	}
	return n, nil
}

// ListAllFacts returns every fact in the store sorted by ID. Matches
// inMemoryFactStore ordering so cross-entity consumers (playbook clustering)
// see deterministic results regardless of backend.
func (s *SQLiteFactStore) ListAllFacts(ctx context.Context) ([]TypedFact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, entity_slug, kind, type,
		        triplet_subject, triplet_predicate, triplet_object,
		        text, confidence, valid_from, valid_until,
		        supersedes, contradicts_with,
		        source_type, source_path, sentence_offset, artifact_excerpt,
		        created_at, created_by, reinforced_at
		 FROM facts
		 ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list all facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanFacts(rows)
}

func (s *SQLiteFactStore) ListEdgesForEntity(ctx context.Context, slug string) ([]IndexEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT subject, predicate, object, timestamp, source_sha
		 FROM edges WHERE subject = ? OR object = ?`, slug, slug)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []IndexEdge
	for rows.Next() {
		var e IndexEdge
		var ts sql.NullString
		if err := rows.Scan(&e.Subject, &e.Predicate, &e.Object, &ts, &e.SourceSHA); err != nil {
			return nil, err
		}
		if ts.Valid {
			if t, err := time.Parse(time.RFC3339, ts.String); err == nil {
				e.Timestamp = t
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteFactStore) ResolveRedirect(ctx context.Context, slug string) (string, bool, error) {
	var to string
	err := s.db.QueryRowContext(ctx,
		`SELECT slug_to FROM redirects WHERE slug_from = ?`, slug).Scan(&to)
	if err == sql.ErrNoRows {
		return slug, false, nil
	}
	if err != nil {
		return slug, false, err
	}
	return to, true, nil
}

// CanonicalHashFacts implements §7.4: sha256 over all fact rows sorted by ID.
// Serialisation is identical to inMemoryFactStore so the contract test passes
// against both backends from the same markdown corpus. ReinforcedAt is
// EXCLUDED from the hash input so two extraction runs on the same artifact
// (the second one purely bumps reinforced_at) produce identical hashes.
// End-to-end drift including reinforcement lives in CanonicalHashAll.
func (s *SQLiteFactStore) CanonicalHashFacts(ctx context.Context) (string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM facts ORDER BY id ASC`)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	// Fetch facts individually in sorted order to match json.Marshal(TypedFact)
	// as used by the in-memory implementation.
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		row := s.db.QueryRowContext(ctx,
			`SELECT id, entity_slug, kind, type,
			        triplet_subject, triplet_predicate, triplet_object,
			        text, confidence, valid_from, valid_until,
			        supersedes, contradicts_with,
			        source_type, source_path, sentence_offset, artifact_excerpt,
			        created_at, created_by, reinforced_at
			 FROM facts WHERE id = ?`, id)
		f, err := scanFact(row)
		if err != nil {
			return "", fmt.Errorf("hash scan %s: %w", id, err)
		}
		f.ReinforcedAt = nil
		b, err := json.Marshal(f)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CanonicalHashAll extends §7.4 to cover facts + entities + edges + redirects.
// The per-table serialisation matches the in-memory implementation so contract
// tests pass against both backends from the same markdown corpus.
func (s *SQLiteFactStore) CanonicalHashAll(ctx context.Context) (string, error) {
	// Start from the facts hash, then append entities, edges, redirects.
	h := sha256.New()

	// --- facts (sorted by id) ---
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM facts ORDER BY id ASC`)
	if err != nil {
		return "", fmt.Errorf("CanonicalHashAll facts ids: %w", err)
	}
	var factIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return "", err
		}
		factIDs = append(factIDs, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, id := range factIDs {
		row := s.db.QueryRowContext(ctx,
			`SELECT id, entity_slug, kind, type,
			        triplet_subject, triplet_predicate, triplet_object,
			        text, confidence, valid_from, valid_until,
			        supersedes, contradicts_with,
			        source_type, source_path, sentence_offset, artifact_excerpt,
			        created_at, created_by, reinforced_at
			 FROM facts WHERE id = ?`, id)
		f, err := scanFact(row)
		if err != nil {
			return "", fmt.Errorf("CanonicalHashAll fact %s: %w", id, err)
		}
		b, err := json.Marshal(f)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}

	// --- entities (sorted by slug) ---
	entRows, err := s.db.QueryContext(ctx,
		`SELECT slug, canonical_slug, kind, aliases,
		        signals_email, signals_domain, signals_person_name, signals_job_title,
		        last_synthesized_sha, last_synthesized_at, fact_count_at_synth,
		        created_at, created_by
		 FROM entities ORDER BY slug ASC`)
	if err != nil {
		return "", fmt.Errorf("CanonicalHashAll entities: %w", err)
	}
	defer func() { _ = entRows.Close() }()
	for entRows.Next() {
		var e IndexEntity
		var aliases sql.NullString
		var lastSynthAt, createdAt sql.NullString
		if err := entRows.Scan(
			&e.Slug, &e.CanonicalSlug, &e.Kind, &aliases,
			&e.Signals.Email, &e.Signals.Domain, &e.Signals.PersonName, &e.Signals.JobTitle,
			&e.LastSynthesizedSHA, &lastSynthAt, &e.FactCountAtSynth,
			&createdAt, &e.CreatedBy,
		); err != nil {
			return "", err
		}
		if aliases.Valid && aliases.String != "" && aliases.String != "null" {
			_ = json.Unmarshal([]byte(aliases.String), &e.Aliases)
		}
		if lastSynthAt.Valid {
			if t, err2 := time.Parse(time.RFC3339, lastSynthAt.String); err2 == nil {
				e.LastSynthesizedAt = t
			}
		}
		if createdAt.Valid {
			if t, err2 := time.Parse(time.RFC3339, createdAt.String); err2 == nil {
				e.CreatedAt = t
			}
		}
		b, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	if err := entRows.Err(); err != nil {
		return "", err
	}

	// --- edges (sorted by subject, predicate, object) ---
	edgeRows, err := s.db.QueryContext(ctx,
		`SELECT subject, predicate, object, timestamp, source_sha
		 FROM edges ORDER BY subject ASC, predicate ASC, object ASC`)
	if err != nil {
		return "", fmt.Errorf("CanonicalHashAll edges: %w", err)
	}
	defer func() { _ = edgeRows.Close() }()
	for edgeRows.Next() {
		var e IndexEdge
		var ts sql.NullString
		if err := edgeRows.Scan(&e.Subject, &e.Predicate, &e.Object, &ts, &e.SourceSHA); err != nil {
			return "", err
		}
		if ts.Valid {
			if t, err2 := time.Parse(time.RFC3339, ts.String); err2 == nil {
				e.Timestamp = t
			}
		}
		b, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	if err := edgeRows.Err(); err != nil {
		return "", err
	}

	// --- redirects (sorted by slug_from) ---
	redRows, err := s.db.QueryContext(ctx,
		`SELECT slug_from, slug_to, merged_at, merged_by, commit_sha
		 FROM redirects ORDER BY slug_from ASC`)
	if err != nil {
		return "", fmt.Errorf("CanonicalHashAll redirects: %w", err)
	}
	defer func() { _ = redRows.Close() }()
	for redRows.Next() {
		var r Redirect
		var mergedAt sql.NullString
		if err := redRows.Scan(&r.From, &r.To, &mergedAt, &r.MergedBy, &r.CommitSHA); err != nil {
			return "", err
		}
		if mergedAt.Valid {
			if t, err2 := time.Parse(time.RFC3339, mergedAt.String); err2 == nil {
				r.MergedAt = t
			}
		}
		b, err := json.Marshal(r)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	if err := redRows.Err(); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *SQLiteFactStore) Close() error {
	return s.db.Close()
}

// --- scan helpers ---------------------------------------------------------

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanFact(row rowScanner) (TypedFact, error) {
	var f TypedFact
	var tripletSubject, tripletPredicate, tripletObject sql.NullString
	var validFrom, validUntil, reinforcedAt sql.NullString
	var createdAt string
	var supersedes, contradicts sql.NullString

	if err := row.Scan(
		&f.ID, &f.EntitySlug, &f.Kind, &f.Type,
		&tripletSubject, &tripletPredicate, &tripletObject,
		&f.Text, &f.Confidence, &validFrom, &validUntil,
		&supersedes, &contradicts,
		&f.SourceType, &f.SourcePath, &f.SentenceOffset, &f.ArtifactExcerpt,
		&createdAt, &f.CreatedBy, &reinforcedAt,
	); err != nil {
		return TypedFact{}, err
	}

	if tripletSubject.Valid || tripletPredicate.Valid || tripletObject.Valid {
		f.Triplet = &Triplet{
			Subject:   tripletSubject.String,
			Predicate: tripletPredicate.String,
			Object:    tripletObject.String,
		}
	}
	if validFrom.Valid {
		if t, err := time.Parse(time.RFC3339, validFrom.String); err == nil {
			f.ValidFrom = t
		}
	}
	if validUntil.Valid {
		if t, err := time.Parse(time.RFC3339, validUntil.String); err == nil {
			f.ValidUntil = &t
		}
	}
	if reinforcedAt.Valid {
		if t, err := time.Parse(time.RFC3339, reinforcedAt.String); err == nil {
			f.ReinforcedAt = &t
		}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		f.CreatedAt = t
	}
	if supersedes.Valid && supersedes.String != "" && supersedes.String != "null" {
		_ = json.Unmarshal([]byte(supersedes.String), &f.Supersedes)
	}
	if contradicts.Valid && contradicts.String != "" && contradicts.String != "null" {
		_ = json.Unmarshal([]byte(contradicts.String), &f.ContradictsWith)
	}
	return f, nil
}

func scanFacts(rows *sql.Rows) ([]TypedFact, error) {
	var out []TypedFact
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
