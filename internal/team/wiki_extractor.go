package team

// wiki_extractor.go — the extraction loop for WUPHF Wiki Intelligence Slice 1.
//
// Data flow:
//
//   agent publishes artifact
//     │
//     ▼
//   WikiWorker.EnqueueArtifact
//     │ git commit to wiki/artifacts/{kind}/{sha}.md (atomic)
//     ▼
//   WikiWorker.process fires maybeRunExtractor in a tracked side goroutine
//     │
//     ▼
//   Extractor.ExtractFromArtifact
//     │ 1. read artifact bytes
//     │ 2. gather known entities + predicate vocabulary from index
//     │ 3. render prompts/extract_entities_lite.tmpl
//     │ 4. provider.RunPrompt (RunConfiguredOneShot behind QueryProvider)
//     │ 5. strip JSON fence, parse into extractionOutput
//     │ 6. for each entity → entityResolverGate.Resolve
//     │ 7. for each fact → ComputeFactID + reinforcement-merge
//     │ 8. WikiWorker.SubmitFacts (single-writer invariant)
//     ▼
//   failure path: DLQ.Enqueue with category (parse / provider_timeout /
//   validation) so the replay loop can pick it up later. Commit never fails
//   due to extraction errors — markdown remains the source of truth.
//
// Schema alignment: docs/specs/WIKI-SCHEMA.md §4.2 (fact), §7.3 (fact ID),
// §10.1 (prompt rules), §11.13 (DLQ).

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed prompts/extract_entities_lite.tmpl
var extractEntitiesLiteTmpl string

// ── Public types ──────────────────────────────────────────────────────────────

// Extractor is the extraction-loop orchestrator. It is safe for concurrent
// use; the underlying components (provider, worker, resolver, DLQ, index)
// have their own concurrency controls.
type Extractor struct {
	provider QueryProvider
	worker   *WikiWorker
	gate     *entityResolverGate
	dlq      *DLQ
	index    *WikiIndex
	tmpl     *template.Template
	// now returns the current time; overridable in tests for deterministic
	// created_at / valid_from values.
	now func() time.Time
}

// NewExtractor constructs an Extractor. All arguments except `now` are
// required; pass nil for `now` to get time.Now.UTC by default.
func NewExtractor(provider QueryProvider, worker *WikiWorker, dlq *DLQ, index *WikiIndex) *Extractor {
	tmpl, err := template.New("extract_entities_lite").Parse(extractEntitiesLiteTmpl)
	if err != nil {
		// The template is embedded and known-valid; a parse error is a build-time bug.
		panic(fmt.Sprintf("wiki_extractor: parse embedded template: %v", err))
	}
	return &Extractor{
		provider: provider,
		worker:   worker,
		gate:     newEntityResolverGate(),
		dlq:      dlq,
		index:    index,
		tmpl:     tmpl,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetNow overrides the clock used for created_at / valid_from defaults.
// Test-only hook.
func (e *Extractor) SetNow(now func() time.Time) { e.now = now }

// ── Prompt template context ───────────────────────────────────────────────────

// tmplEntity shadows IndexEntity with the two pre-flattened fields the
// template expects (SignalsOneLine, AliasesJoined) so template.Parse does
// not need a method on IndexEntity.
type tmplEntity struct {
	Slug           string
	Kind           string
	SignalsOneLine string
	AliasesJoined  string
	Aliases        []string
}

type extractTmplVars struct {
	ArtifactKind        string
	ArtifactSHA         string
	ArtifactPath        string
	OccurredAt          string
	Body                string
	KnownEntities       []tmplEntity
	PredicateVocabulary []string
}

// ── LLM response shape ────────────────────────────────────────────────────────

// extractionOutput mirrors the JSON shape emitted by extract_entities_lite.tmpl.
type extractionOutput struct {
	ArtifactSHA string            `json:"artifact_sha"`
	Entities    []extractedEntity `json:"entities"`
	Facts       []extractedFact   `json:"facts"`
	Notes       string            `json:"notes,omitempty"`
}

type extractedEntity struct {
	Kind         string          `json:"kind"`
	ProposedSlug string          `json:"proposed_slug"`
	ExistingSlug string          `json:"existing_slug,omitempty"`
	Signals      extractedSignal `json:"signals"`
	Aliases      []string        `json:"aliases,omitempty"`
	Confidence   float64         `json:"confidence"`
	Ghost        bool            `json:"ghost"`
}

type extractedSignal struct {
	Email      string `json:"email"`
	Domain     string `json:"domain"`
	PersonName string `json:"person_name"`
	JobTitle   string `json:"job_title"`
}

type extractedFact struct {
	EntitySlug      string   `json:"entity_slug"`
	Type            string   `json:"type"`
	Triplet         *Triplet `json:"triplet"`
	Text            string   `json:"text"`
	Confidence      float64  `json:"confidence"`
	ValidFrom       string   `json:"valid_from"`
	ValidUntil      *string  `json:"valid_until,omitempty"`
	SourceType      string   `json:"source_type"`
	SourcePath      string   `json:"source_path"`
	SentenceOffset  int      `json:"sentence_offset"`
	ArtifactExcerpt string   `json:"artifact_excerpt"`
}

// ── ExtractFromArtifact ───────────────────────────────────────────────────────

// ExtractFromArtifact reads a committed artifact from disk, runs the
// extraction prompt, resolves each entity through the gate, and submits the
// resulting facts + ghost entities back to the WikiWorker.
//
// The artifactPath must match wiki/artifacts/{kind}/{sha}.md. Failures at
// any step route to the DLQ with an appropriate category so the replay loop
// can pick them up later. Returns a non-nil error only for callers that
// want to surface it (e.g. the ReplayDLQ loop); the commit pipeline logs
// and discards.
func (e *Extractor) ExtractFromArtifact(ctx context.Context, artifactPath string) error {
	kind, ok := ArtifactKind(artifactPath)
	if !ok {
		return fmt.Errorf("extractor: path %q is not an artifact", artifactPath)
	}
	sha, _ := ArtifactSHAFromPath(artifactPath)

	body, readErr := e.readArtifact(artifactPath)
	if readErr != nil {
		// Validation-class failure — missing file is not retryable by the LLM.
		e.queueDLQ(ctx, sha, artifactPath, kind, readErr, DLQCategoryValidation)
		return readErr
	}

	promptStr, tmplErr := e.renderPrompt(ctx, kind, sha, artifactPath, body)
	if tmplErr != nil {
		e.queueDLQ(ctx, sha, artifactPath, kind, tmplErr, DLQCategoryValidation)
		return tmplErr
	}

	raw, provErr := e.provider.RunPrompt(ctx, "", promptStr)
	if provErr != nil {
		cat := DLQCategoryProviderTimeout
		if errors.Is(provErr, context.Canceled) || errors.Is(provErr, context.DeadlineExceeded) {
			cat = DLQCategoryProviderTimeout
		}
		e.queueDLQ(ctx, sha, artifactPath, kind, provErr, cat)
		return provErr
	}

	parsed, parseErr := parseExtractionResponse(raw)
	if parseErr != nil {
		// Malformed JSON is a programming/LLM-contract error — never retried
		// past the first attempt (§11.13 replay policy).
		e.queueDLQ(ctx, sha, artifactPath, kind, parseErr, DLQCategoryValidation)
		return parseErr
	}

	// Overwrite artifact_sha from the path so a misreporting LLM cannot
	// poison the fact ID hash. §7.3 determinism starts here.
	parsed.ArtifactSHA = sha

	return e.apply(ctx, parsed, artifactPath, kind)
}

// apply resolves every entity, computes fact IDs, reinforces matches, and
// submits the batch to the WikiWorker.
func (e *Extractor) apply(ctx context.Context, out extractionOutput, artifactPath, kind string) error {
	var entitiesToWrite []IndexEntity
	var factsToWrite []TypedFact

	// Map proposed_slug → resolved_slug so fact entries that reference a
	// freshly-minted ghost slug use the canonical (collision-safe) slug.
	resolved := make(map[string]string, len(out.Entities))
	resolvedKind := make(map[string]string, len(out.Entities))

	adapter := NewWikiIndexSignalAdapter(e.index)

	for _, ent := range out.Entities {
		existing := strings.TrimSpace(ent.ExistingSlug)
		proposed := ProposedEntity{
			Kind:         EntityKind(ent.Kind),
			ProposedSlug: strings.TrimSpace(ent.ProposedSlug),
			ExistingSlug: existing,
			Signals: Signals{
				Email:      ent.Signals.Email,
				Domain:     ent.Signals.Domain,
				PersonName: ent.Signals.PersonName,
				JobTitle:   ent.Signals.JobTitle,
			},
			Aliases:    ent.Aliases,
			Confidence: ent.Confidence,
			Ghost:      ent.Ghost,
		}
		// Ghost dedup: if the prompt flagged this entity as a ghost AND no
		// existing_slug was supplied, check the index for a prior ghost
		// under the same proposed slug + compatible person_name. Without
		// this, the resolver's collision-safe slug path would mint a
		// fresh slug on every re-extraction — breaking fact-ID
		// determinism for the §7.3 contract. The person_name guard
		// prevents collision with an unrelated entity that happens to
		// share the bare proposed slug.
		if ent.Ghost && existing == "" && proposed.ProposedSlug != "" {
			if prior, found, _ := adapter.EntityBySlug(ctx, proposed.ProposedSlug); found {
				if strings.EqualFold(strings.TrimSpace(prior.Name), strings.TrimSpace(proposed.Signals.PersonName)) {
					proposed.ExistingSlug = proposed.ProposedSlug
				}
			}
		}
		r, err := e.gate.Resolve(ctx, adapter, proposed)
		if err != nil {
			log.Printf("wiki_extractor: resolve %q: %v", ent.ProposedSlug, err)
			continue
		}
		resolved[proposed.ProposedSlug] = r.Slug
		if proposed.ExistingSlug != "" {
			resolved[proposed.ExistingSlug] = r.Slug
		}
		resolvedKind[r.Slug] = string(r.Kind)

		if !r.Matched {
			// New entity or ghost — write the IndexEntity row so fact rows
			// always resolve against an indexed entity.
			entitiesToWrite = append(entitiesToWrite, IndexEntity{
				Slug:          r.Slug,
				CanonicalSlug: r.Slug,
				Kind:          string(r.Kind),
				Aliases:       ent.Aliases,
				Signals: Signals{
					Email:      ent.Signals.Email,
					Domain:     ent.Signals.Domain,
					PersonName: ent.Signals.PersonName,
					JobTitle:   ent.Signals.JobTitle,
				},
				CreatedAt: e.now(),
				CreatedBy: ArchivistAuthor,
			})
		}
	}

	for _, f := range out.Facts {
		subject := strings.TrimSpace(f.EntitySlug)
		if mapped, ok := resolved[subject]; ok {
			subject = mapped
		}
		if subject == "" {
			continue
		}
		triplet := f.Triplet
		if triplet == nil {
			// Fact with no triplet cannot compute a deterministic fact_id per
			// §7.3; skip rather than fabricate.
			log.Printf("wiki_extractor: skipping fact with nil triplet for %s", subject)
			continue
		}
		// Remap triplet subject/object if they reference a freshly-resolved
		// proposed_slug.
		tSubject := remapSlug(triplet.Subject, resolved)
		tObject := remapSlug(triplet.Object, resolved)
		tPredicate := triplet.Predicate

		factID := ComputeFactID(out.ArtifactSHA, f.SentenceOffset, tSubject, tPredicate, tObject)

		validFrom := parseTimestamp(f.ValidFrom)
		if validFrom.IsZero() {
			validFrom = e.now()
		}
		var validUntil *time.Time
		if f.ValidUntil != nil && strings.TrimSpace(*f.ValidUntil) != "" {
			if t := parseTimestamp(*f.ValidUntil); !t.IsZero() {
				validUntil = &t
			}
		}

		tf := TypedFact{
			ID:              factID,
			EntitySlug:      subject,
			Kind:            resolvedKind[subject],
			Type:            coerceFactType(f.Type),
			Triplet:         &Triplet{Subject: tSubject, Predicate: tPredicate, Object: tObject},
			Text:            f.Text,
			Confidence:      f.Confidence,
			ValidFrom:       validFrom,
			ValidUntil:      validUntil,
			SourceType:      ifBlank(f.SourceType, kind),
			SourcePath:      ifBlank(f.SourcePath, artifactPath),
			SentenceOffset:  f.SentenceOffset,
			ArtifactExcerpt: f.ArtifactExcerpt,
			CreatedAt:       e.now(),
			CreatedBy:       ArchivistAuthor,
		}

		// Reinforcement: if the fact already exists (same ID), bump
		// reinforced_at + carry the prior CreatedAt so we do not overwrite
		// history. §7.3 calls this the "dedup-by-merge at commit time"
		// path.
		if existing, ok, _ := e.index.GetFact(ctx, factID); ok {
			now := e.now()
			tf.CreatedAt = existing.CreatedAt
			tf.CreatedBy = existing.CreatedBy
			tf.ReinforcedAt = &now
			// Carry any supersede/contradict history forward.
			tf.Supersedes = existing.Supersedes
			tf.ContradictsWith = existing.ContradictsWith
		}

		factsToWrite = append(factsToWrite, tf)
	}

	if len(factsToWrite) == 0 && len(entitiesToWrite) == 0 {
		return nil
	}
	if err := e.worker.SubmitFacts(ctx, factsToWrite, entitiesToWrite); err != nil {
		// Submission failure is a transient provider-class error — retry
		// later. The artifact is already committed, so we can replay.
		e.queueDLQ(ctx, out.ArtifactSHA, artifactPath, kind, err, DLQCategoryProviderTimeout)
		return err
	}
	return nil
}

// ── DLQ plumbing ──────────────────────────────────────────────────────────────

// queueDLQ is the common DLQ enqueue path used from every failure branch.
// If the entry already exists, the caller is expected to call
// DLQ.RecordAttempt instead; this is the first-failure path.
func (e *Extractor) queueDLQ(ctx context.Context, sha, path, kind string, err error, cat DLQErrorCategory) {
	if e.dlq == nil {
		return
	}
	entry := DLQEntry{
		ArtifactSHA:   sha,
		ArtifactPath:  path,
		Kind:          kind,
		LastError:     err.Error(),
		ErrorCategory: cat,
	}
	if enqErr := e.dlq.Enqueue(ctx, entry); enqErr != nil {
		log.Printf("wiki_extractor: enqueue DLQ for %s: %v", sha, enqErr)
	}
}

// ReplayDLQ walks the DLQ replay queue and re-runs ExtractFromArtifact on
// each ready entry. Success → MarkResolved tombstone; failure → RecordAttempt
// so the entry either backs off or promotes to permanent-failures.jsonl.
//
// Returns (processed, retired, err) where processed is the number of entries
// attempted, retired is the count that moved out of the active queue
// (resolved + permanent promotions since this call started).
func (e *Extractor) ReplayDLQ(ctx context.Context) (int, int, error) {
	if e.dlq == nil {
		return 0, 0, fmt.Errorf("extractor: DLQ is not wired")
	}
	ready, err := e.dlq.ReadyForReplay(ctx, e.now())
	if err != nil {
		return 0, 0, fmt.Errorf("extractor: ready for replay: %w", err)
	}
	var processed, retired int
	for _, entry := range ready {
		processed++
		if err := e.ExtractFromArtifact(ctx, entry.ArtifactPath); err != nil {
			cat := string(entry.ErrorCategory)
			if cat == "" {
				cat = string(DLQCategoryProviderTimeout)
			}
			if recErr := e.dlq.RecordAttempt(ctx, entry.ArtifactSHA, err, cat); recErr != nil {
				log.Printf("wiki_extractor: record attempt for %s: %v", entry.ArtifactSHA, recErr)
				continue
			}
			// RecordAttempt promotes to permanent-failures when retries are
			// exhausted — treat that as retired.
			if entry.RetryCount+1 >= entry.MaxRetries {
				retired++
			}
			continue
		}
		if err := e.dlq.MarkResolved(ctx, entry.ArtifactSHA); err != nil {
			log.Printf("wiki_extractor: mark resolved for %s: %v", entry.ArtifactSHA, err)
			continue
		}
		retired++
	}
	return processed, retired, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// readArtifact loads the raw artifact body from disk. Artifacts live under
// wiki/artifacts/ which is outside the team/ subtree validateArticlePath
// gates, so we do a direct filesystem read gated by IsArtifactPath.
func (e *Extractor) readArtifact(relPath string) (string, error) {
	if !IsArtifactPath(relPath) {
		return "", fmt.Errorf("extractor: path %q is not an artifact path", relPath)
	}
	full := filepath.Join(e.worker.Repo().Root(), filepath.FromSlash(relPath))
	body, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read artifact %s: %w", relPath, err)
	}
	return string(body), nil
}

// renderPrompt executes the embedded template with the artifact context +
// a best-effort signal-index snapshot.
func (e *Extractor) renderPrompt(ctx context.Context, kind, sha, path, body string) (string, error) {
	known, predicates := e.gatherIndexContext(ctx)
	vars := extractTmplVars{
		ArtifactKind:        kind,
		ArtifactSHA:         sha,
		ArtifactPath:        path,
		OccurredAt:          e.now().Format(time.RFC3339),
		Body:                body,
		KnownEntities:       known,
		PredicateVocabulary: predicates,
	}
	var buf bytes.Buffer
	if err := e.tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("render extraction prompt: %w", err)
	}
	return buf.String(), nil
}

// gatherIndexContext returns a snapshot of known entities and predicates
// for the prompt. Bounded so a large index does not blow the context
// window; we take the first 50 entities and 30 predicates sorted by seen
// order. This is a Slice 1 heuristic — Slice 2 may add signal-scoped
// retrieval.
func (e *Extractor) gatherIndexContext(ctx context.Context) ([]tmplEntity, []string) {
	if e.index == nil {
		return nil, nil
	}
	mem, ok := e.index.store.(*inMemoryFactStore)
	if !ok {
		// Persistent backends do not currently expose an Iterate method.
		// Slice 2 will add one; for now the prompt runs without known
		// entities, which simply means the LLM will propose fresh slugs
		// the resolver can still dedupe against the SQLite rows.
		_ = ctx
		return nil, nil
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()

	var ents []tmplEntity
	const maxEntities = 50
	for slug, ent := range mem.entities {
		if len(ents) >= maxEntities {
			break
		}
		ents = append(ents, tmplEntity{
			Slug:           slug,
			Kind:           ent.Kind,
			SignalsOneLine: signalsOneLine(ent.Signals),
			AliasesJoined:  strings.Join(ent.Aliases, ", "),
			Aliases:        ent.Aliases,
		})
	}

	predSet := map[string]struct{}{}
	for _, f := range mem.facts {
		if f.Triplet == nil {
			continue
		}
		if f.Triplet.Predicate == "" {
			continue
		}
		predSet[f.Triplet.Predicate] = struct{}{}
	}
	predicates := make([]string, 0, len(predSet))
	for p := range predSet {
		predicates = append(predicates, p)
	}
	const maxPredicates = 30
	if len(predicates) > maxPredicates {
		predicates = predicates[:maxPredicates]
	}
	return ents, predicates
}

func signalsOneLine(s Signals) string {
	var parts []string
	if s.Email != "" {
		parts = append(parts, "email="+s.Email)
	}
	if s.Domain != "" {
		parts = append(parts, "domain="+s.Domain)
	}
	if s.PersonName != "" {
		parts = append(parts, "name="+s.PersonName)
	}
	if s.JobTitle != "" {
		parts = append(parts, "title="+s.JobTitle)
	}
	return strings.Join(parts, " ")
}

// parseExtractionResponse strips a markdown code fence (if present) and
// extracts the outermost JSON object. Mirrors parseProviderResponse in
// wiki_query.go so the two paths share failure semantics.
func parseExtractionResponse(raw string) (extractionOutput, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		end := len(lines) - 1
		for end > 0 && strings.TrimSpace(lines[end]) == "```" {
			end--
		}
		if len(lines) > 2 {
			raw = strings.Join(lines[1:end+1], "\n")
		}
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return extractionOutput{}, fmt.Errorf("no JSON object in extraction response (len=%d)", len(raw))
	}
	jsonStr := raw[start : end+1]
	var out extractionOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return extractionOutput{}, fmt.Errorf("unmarshal extraction response: %w (raw: %.120s)", err, jsonStr)
	}
	return out, nil
}

// remapSlug returns the resolved slug for `s` if the mapping contains a
// match, otherwise returns s unchanged. Used to rewrite triplet subject /
// object fields to the canonical resolver slugs.
func remapSlug(s string, resolved map[string]string) string {
	if r, ok := resolved[s]; ok {
		return r
	}
	// triplet.object may carry a `{kind}:{slug}` qualifier per §4.2.
	if idx := strings.Index(s, ":"); idx > 0 {
		head := s[:idx]
		tail := s[idx+1:]
		if r, ok := resolved[tail]; ok {
			return head + ":" + r
		}
	}
	return s
}

func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func coerceFactType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "status", "observation", "relationship", "background":
		return strings.ToLower(t)
	default:
		return "observation" // §4.3 default
	}
}

func ifBlank(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
