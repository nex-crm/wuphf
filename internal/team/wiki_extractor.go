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
	// appendFactLog is the fact-log append seam. Defaults to
	// worker.EnqueueFactLogAppend. Tests override to simulate
	// ErrQueueSaturated without fighting the worker's drain lifecycle.
	appendFactLog func(ctx context.Context, slug, path, content, commitMsg string) (string, int, error)
}

// NewExtractor constructs an Extractor. All arguments except `now` are
// required; pass nil for `now` to get time.Now.UTC by default.
func NewExtractor(provider QueryProvider, worker *WikiWorker, dlq *DLQ, index *WikiIndex) *Extractor {
	tmpl, err := template.New("extract_entities_lite").Parse(extractEntitiesLiteTmpl)
	if err != nil {
		// The template is embedded and known-valid; a parse error is a build-time bug.
		panic(fmt.Sprintf("wiki_extractor: parse embedded template: %v", err))
	}
	ex := &Extractor{
		provider: provider,
		worker:   worker,
		gate:     newEntityResolverGate(),
		dlq:      dlq,
		index:    index,
		tmpl:     tmpl,
		now:      func() time.Time { return time.Now().UTC() },
	}
	ex.appendFactLog = func(ctx context.Context, slug, path, content, commitMsg string) (string, int, error) {
		return worker.EnqueueFactLogAppend(ctx, slug, path, content, commitMsg)
	}
	return ex
}

// SetNow overrides the clock used for created_at / valid_from defaults.
// Test-only hook.
func (e *Extractor) SetNow(now func() time.Time) { e.now = now }

// setAppendFactLog overrides the fact-log append seam. Test-only — used to
// simulate ErrQueueSaturated deterministically without racing the worker's
// drain goroutine.
func (e *Extractor) setAppendFactLog(fn func(ctx context.Context, slug, path, content, commitMsg string) (string, int, error)) {
	e.appendFactLog = fn
}

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
	// Substrate guarantee (§7.4): after in-memory submission succeeds, persist
	// every NEW (non-reinforced) fact to the append-only JSONL log so a wipe +
	// reconcile rebuilds to a logically-identical index. Reinforced facts only
	// update reinforced_at in-memory; they are already in the JSONL file from
	// the first extraction run, so re-appending would duplicate the line.
	//
	// Failures here never fail the caller — the artifact commit already
	// succeeded and SubmitFacts was atomic from the caller's perspective. A
	// persistence failure is logged + queued to the DLQ for replay.
	e.persistFactLogs(ctx, out.ArtifactSHA, factsToWrite)
	return nil
}

// persistFactLogs appends newly-introduced facts to wiki/facts/{kind}/{slug}.jsonl
// under the archivist identity. Batches per-entity so one artifact with N
// facts about the same entity results in 1 commit (not N).
//
// This is the §7.4 substrate-rebuild closure for the extraction path: without
// it, the fact lives only in the derived index cache and evaporates on
// `rm -rf .wuphf/index/`.
//
// Errors are logged and routed through the DLQ under the dedicated
// DLQCategoryFactLogPersist category so ReplayDLQ can retry the append
// without re-running extraction. Re-running extraction would skip the fact
// as reinforcement (§7.3) and the JSONL line would never be written,
// permanently breaking §7.4 for that fact.
func (e *Extractor) persistFactLogs(ctx context.Context, artifactSHA string, facts []TypedFact) {
	if len(facts) == 0 || e.worker == nil {
		return
	}
	// Group by (kind, entity_slug). Reinforced facts (ReinforcedAt != nil) are
	// already on disk from a prior run; skip to avoid duplicate JSONL lines.
	type groupKey struct{ kind, slug string }
	groups := make(map[groupKey][]TypedFact)
	for _, f := range facts {
		if f.ReinforcedAt != nil {
			continue
		}
		kind := strings.TrimSpace(f.Kind)
		slug := strings.TrimSpace(f.EntitySlug)
		if kind == "" || slug == "" {
			// Missing kind/slug means we cannot deterministically locate the
			// fact log. Log and skip — the in-memory submission already ran,
			// so this fact is findable at query time but will be dropped on
			// the next rebuild. Slice 3 hardens this via a resolver fallback.
			log.Printf("wiki_extractor: persist fact log: missing kind/slug for fact %s (kind=%q slug=%q)", f.ID, kind, slug)
			continue
		}
		groups[groupKey{kind: kind, slug: slug}] = append(groups[groupKey{kind: kind, slug: slug}], f)
	}
	if len(groups) == 0 {
		return
	}
	msg := fmt.Sprintf("archivist: extract facts from %s", artifactSHA)
	for key, batch := range groups {
		body, serErr := serializeFactsAsJSONL(batch)
		if serErr != nil {
			// Should be unreachable now that serializeFactsAsJSONL collects
			// partials — but keep the guard so a future invariant change
			// cannot silently swallow a whole batch.
			log.Printf("wiki_extractor: serialize fact log for %s/%s: %v", key.kind, key.slug, serErr)
			continue
		}
		if body == "" {
			continue
		}
		path := factLogPath(key.kind, key.slug)
		if _, _, err := e.appendFactLog(ctx, ArchivistAuthor, path, body, msg); err != nil {
			log.Printf("wiki_extractor: append fact log %s: %v", path, err)
			// SubmitFacts already ran. Route the append failure to the DLQ
			// under the dedicated fact-log-persist category so ReplayDLQ
			// retries the APPEND (not the full extraction). Retrying the
			// full extraction would treat every fact as reinforcement and
			// never write the missing JSONL lines. See §7.4.
			e.queueFactLogPersistDLQ(ctx, artifactSHA, key.kind, key.slug, path, body, err)
		}
	}
}

// factLogPath returns the JSONL path for an entity's fact log, matching the
// §3 Layer-2 layout and the walker in wiki_index.go.
func factLogPath(kind, slug string) string {
	return "wiki/facts/" + kind + "/" + slug + ".jsonl"
}

// serializeFactsAsJSONL marshals each TypedFact as one JSON object per line,
// terminated by '\n'. Returns "" when the input is empty.
//
// A marshal failure on any single fact is logged and that fact is skipped —
// the remaining facts in the batch are still serialized and returned. An
// earlier version aborted the whole batch on the first failure, silently
// dropping every sibling fact in the same (kind, slug) group and breaking
// the §7.4 substrate guarantee for every fact that came after the bad
// record. That trade-off was strictly worse than losing one pathological
// fact — TypedFact has no pointers to types that json/encoding would fail
// on under normal operation, so the skip path is defensive only.
func serializeFactsAsJSONL(facts []TypedFact) (string, error) {
	if len(facts) == 0 {
		return "", nil
	}
	var b strings.Builder
	for i := range facts {
		line, err := json.Marshal(facts[i])
		if err != nil {
			log.Printf("wiki_extractor: skipping unmarshalable fact %s in batch: %v", facts[i].ID, err)
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// queueFactLogPersistDLQ routes a fact-log append failure to the DLQ under
// the dedicated DLQCategoryFactLogPersist category. The entry carries the
// exact JSONL lines the original append tried to write so ReplayDLQ can
// reconstruct the call without re-running extraction.
func (e *Extractor) queueFactLogPersistDLQ(ctx context.Context, artifactSHA, kind, slug, path, body string, appendErr error) {
	if e.dlq == nil {
		return
	}
	entry := DLQEntry{
		ArtifactSHA:   FactLogAppendSHA(kind, slug, artifactSHA),
		ArtifactPath:  path,
		Kind:          kind,
		LastError:     appendErr.Error(),
		ErrorCategory: DLQCategoryFactLogPersist,
		FactLogAppend: &FactLogAppendPayload{
			Kind:        kind,
			Slug:        slug,
			ArtifactSHA: artifactSHA,
			JSONLLines:  body,
		},
	}
	if enqErr := e.dlq.Enqueue(ctx, entry); enqErr != nil {
		log.Printf("wiki_extractor: enqueue fact-log-persist DLQ for %s/%s: %v", kind, slug, enqErr)
	}
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

// ReplayDLQ walks the DLQ replay queue and retries each ready entry. Routing
// depends on the ErrorCategory:
//
//   - DLQCategoryFactLogPersist: re-attempt the JSONL append only (no
//     extraction rerun). Re-running extraction would skip the fact as
//     reinforcement and the append would never recover — see §7.4.
//   - everything else: re-run ExtractFromArtifact on the artifact.
//
// Success → MarkResolved tombstone; failure → RecordAttempt so the entry
// either backs off or promotes to permanent-failures.jsonl.
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
		retiredThis, handledErr := e.replayDLQEntry(ctx, entry)
		if handledErr != nil {
			cat := string(entry.ErrorCategory)
			if cat == "" {
				cat = string(DLQCategoryProviderTimeout)
			}
			if recErr := e.dlq.RecordAttempt(ctx, entry.ArtifactSHA, handledErr, cat); recErr != nil {
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
		_ = retiredThis
	}
	return processed, retired, nil
}

// replayDLQEntry dispatches one DLQ entry to the right retry path based on
// its ErrorCategory. Returns (retiredImmediately, err); err != nil means the
// retry failed and the caller should RecordAttempt.
func (e *Extractor) replayDLQEntry(ctx context.Context, entry DLQEntry) (bool, error) {
	if entry.ErrorCategory == DLQCategoryFactLogPersist {
		if err := e.replayFactLogAppend(ctx, entry); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := e.ExtractFromArtifact(ctx, entry.ArtifactPath); err != nil {
		return false, err
	}
	return true, nil
}

// replayFactLogAppend retries a failed fact-log JSONL append. It is
// idempotent: the on-disk file is consulted first and any fact_id that is
// already present is dropped from the append payload before re-enqueueing.
// If every payload line is already on disk the retry is treated as resolved
// with a no-op, which is the intended behaviour when the prior attempt
// actually landed on disk but this process missed the success signal.
func (e *Extractor) replayFactLogAppend(ctx context.Context, entry DLQEntry) error {
	if entry.FactLogAppend == nil {
		return fmt.Errorf("replay fact-log append: missing payload on entry %q", entry.ArtifactSHA)
	}
	payload := *entry.FactLogAppend
	kind := strings.TrimSpace(payload.Kind)
	slug := strings.TrimSpace(payload.Slug)
	if kind == "" || slug == "" {
		return fmt.Errorf("replay fact-log append: empty kind/slug in payload")
	}
	path := factLogPath(kind, slug)

	// Dedupe by fact_id against the current on-disk fact log — the prior
	// attempt may have partially landed, or a concurrent extraction may have
	// reinforced these facts via a different code path and persisted them
	// already. Either way, append only the fact_ids that are still missing.
	onDisk, err := e.readFactIDsFromFactLog(path)
	if err != nil {
		return fmt.Errorf("replay fact-log append: read existing: %w", err)
	}

	missing, err := filterMissingJSONLLines(payload.JSONLLines, onDisk)
	if err != nil {
		return fmt.Errorf("replay fact-log append: filter missing: %w", err)
	}
	if missing == "" {
		// Every line already present — nothing to do. Treat as success so
		// ReplayDLQ writes the resolved tombstone and this entry leaves the
		// active queue.
		return nil
	}

	artifactSHA := payload.ArtifactSHA
	if artifactSHA == "" {
		artifactSHA = entry.ArtifactSHA
	}
	msg := fmt.Sprintf("archivist: replay fact-log append for %s", artifactSHA)
	if _, _, err := e.appendFactLog(ctx, ArchivistAuthor, path, missing, msg); err != nil {
		return fmt.Errorf("replay fact-log append %s: %w", path, err)
	}
	return nil
}

// readFactIDsFromFactLog reads wiki/facts/{kind}/{slug}.jsonl and returns
// the set of fact_ids currently on disk. A missing file returns an empty
// set (the typical first-retry case). Malformed lines are skipped with a
// log — reconcile applies the same tolerance.
func (e *Extractor) readFactIDsFromFactLog(relPath string) (map[string]struct{}, error) {
	if e.worker == nil || e.worker.Repo() == nil {
		return map[string]struct{}{}, nil
	}
	full := filepath.Join(e.worker.Repo().Root(), filepath.FromSlash(relPath))
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	ids := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
			// Tolerate malformed rows — the same posture reconcile takes.
			continue
		}
		if probe.ID != "" {
			ids[probe.ID] = struct{}{}
		}
	}
	return ids, nil
}

// filterMissingJSONLLines returns the subset of `lines` whose fact_id is
// NOT already present in `onDisk`. Lines that cannot be parsed as a JSON
// object with an `id` field are passed through unchanged — they were valid
// in the original append attempt and the retry should carry them forward.
func filterMissingJSONLLines(lines string, onDisk map[string]struct{}) (string, error) {
	if lines == "" {
		return "", nil
	}
	var b strings.Builder
	for _, line := range strings.Split(lines, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
			// Preserve verbatim — caller validated the payload at queue time.
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if _, already := onDisk[probe.ID]; already {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
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
