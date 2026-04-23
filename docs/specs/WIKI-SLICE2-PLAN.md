# Wiki Intelligence — Slice 2 Plan

Slice 1 (PR #245) shipped the index, typed facts, classifier, `/lookup`, `/lint`,
and extraction loop. The Week 0 benchmark came back at 66% pass rate against the
85% ship gate (micro-averaged recall 90.49%, so retrieval-as-a-whole works — the
shortfall is structural in multi_hop + counterfactual queries). Slice 2 closes
the gap, fixes the substrate-guarantee leak in extraction, extends synthesis to
cluster-level insights, and lands the security hardening from the devil's
advocate review.

## Ship gate

- `go run ./cmd/bench-slice-1` exits 0 with `pass_rate ≥ 85%`.
- All Thread D security items shipped: prompt-injection escaping, citation
  bounds validator, triplet sanitizer, persistent-backend signal iteration,
  DLQ inspection surface.
- Slice 1 ship_blockers remain closed. Should_fixes from
  `docs/WIKI-SLICE1-REVIEW.md` either land in Slice 2 or get a one-line
  "Slice 3 because X" rationale in the Out of scope section.

## Threads

### Thread A — retrieval quality (ship gate) — L

Files to touch:
- `internal/team/wiki_query.go:136-166` — Answer: after classify, branch on
  class to a new `retrieveForClass` router.
- `internal/team/wiki_index.go:142-156` — FactStore: add
  `ListFactsByPredicateObject(ctx, predicate, object)` and
  `ListFactsByTriplet(ctx, subject, predicate, objectPrefix)`.
- `internal/team/wiki_index_sqlite.go:77` — new index already exists on
  `(triplet_subject, triplet_predicate)`; add one on
  `(triplet_predicate, triplet_object)`.
- `internal/team/wiki_classifier.go:66-119` — already emits multi_hop +
  counterfactual correctly; no change needed beyond returning matched spans.
- New file `internal/team/wiki_query_rewrite.go` —
  `parseMultiHopSpans(query) → {companyDisplay, projectDisplay}`;
  `parseCounterfactualSubject(query) → {personDisplay}`.
- `bench/slice-1/generate.go:1164-1186,1211-1227` — cap `|expected| ≤ topK`
  in `expectedFactsForProjectAnyPredicate` + `expectedMultiHop`;
  deterministically drop the oldest fact IDs (sorted) so truncation is stable.

Specific changes:
1. **Typed-predicate graph walk for multi_hop.** Resolve company/project
   display to slugs (entityByName fuzzy match, threshold 0.9). Then:
   `A = ListFactsByPredicateObject("champions", "project:"+projSlug)`.
   `B = ListFactsByPredicateObject("role_at", "company:"+companySlug)`.
   Take the union of A plus `role_at` facts for each subject in A whose
   subject also appears in B. Union with BM25 top-20. Merge, keep `≤ topK=20`.
2. **Counterfactual rewrite.** When `class=counterfactual` and we can identify
   a person span, retrieve `ListFactsByTriplet(personSlug, "role_at", "")`
   first, then fall back to BM25 on the stripped noun phrase.
3. **Expected-set cap in generator.** For queries where `len(expected) > topK`,
   sort ascending and truncate. Document in `RESULTS.md`.

Tests:
- `TestMultiHopTypedRetrieval` — table over q_036..q_045, recall@20 ≥ 0.85.
- `TestCounterfactualRewrite` — q_047 passes (ivan-petrov role_at surfaces).
- `TestQueryRewriteParser` — table over query shapes, wikilinks, misspellings.
- `TestListFactsByPredicateObject_SQLite` + `_InMemory`.
- `TestBenchSlice1Gate` — integration: generator + benchmark, assert ≥ 0.85.

Risks:
- Entity resolution from query text is fuzzy; "Acme Corp" → wrong slug → zero
  hits from graph walk. Mitigation: BM25 union fallback, never below current 50%.

### Thread B — extraction → markdown closure — M

Files to touch:
- `internal/team/wiki_extractor.go:358-364` — after `SubmitFacts` succeeds,
  serialize each TypedFact to JSONL and call
  `e.worker.EnqueueFactLog(ctx, ArchivistAuthor, factLogPath(f.Kind, f.EntitySlug), body, msg)`.
- `internal/team/wiki_index.go:310-312` — amend §7.4 doc comment: ReinforcedAt
  excluded from CanonicalHashFacts; CanonicalHashAll includes it for drift
  detection. Mirror sentence in `docs/specs/WIKI-SCHEMA.md` §7.4.
- New helper `factLogPath(kind, slug string) → "wiki/facts/"+kind+"/"+slug+".jsonl"`.

Specific changes:
1. **JSONL append on successful extraction.** Load existing file (or empty),
   marshal each new TypedFact, append under `archivist: extract facts from <sha>`.
   Reinforcement path carries forward CreatedAt/CreatedBy; dedupe at reconcile
   via ID equality.
2. **Document ReinforcedAt exclusion** in WIKI-SCHEMA §7.4.

Tests:
- `TestExtractionSurvivesReboot` — extract → assert BM25 hit → `rm -rf` index →
  reconcile → CanonicalHashAll matches, BM25 still hits.
- `TestEnqueueFactLogAppendsJSONL` — file grows on subsequent runs.
- `TestReinforcementHashInvariance` — two runs: identical CanonicalHashFacts,
  different CanonicalHashAll.

Risks:
- Double-commit load (SubmitFacts + EnqueueFactLog) per artifact. Worker queue
  is cap=64. Mitigation: batch per-entity so one 5-fact artifact ≤ 5 commits;
  measure p95 queue depth during `TestBenchSlice1Gate`.

### Thread C — PlaybookSynthesizer extension — M

Files to touch:
- `internal/team/playbook_synthesizer.go:52-60` (system prompt) — extend Rules
  4 with "also surface Patterns across entities when provided."
- `internal/team/playbook_synthesizer.go` — add
  `clusterReinforcedFacts(w *WikiIndex, predicate string, minReinforcement int) []InsightCluster`.
- New prompt `internal/team/prompts/synthesis_playbook_v2.tmpl` — input section
  for `{{range .Clusters}}`, instruction to render `## Patterns across entities`.

Specific changes:
1. **InsightCluster input stream.** Cluster =
   `{Predicate; Entities []string; Count; SharedObject}`. Query index once:
   group facts by `(predicate, object)` where `reinforcement_count ≥ 3`
   (counted across distinct entities).
2. **Prompt template.** Rule 4a: "If Clusters is non-empty, append a
   `## Patterns across entities` section below What we've learned. Cite entity
   count, not individual facts."
3. No new worker, no new provider. Reuses `PlaybookSynthesizer.submit`.

Tests:
- `TestClusterReinforcedFacts` — 3 entities sharing `(champions, q2-pilot)` →
  one cluster emitted.
- `TestPlaybookSynthesisWithClusters` — fake QueryProvider asserts rendered
  prompt includes Clusters; output preserves body verbatim + appends Patterns.
- `evals/playbook_clusters.golden.json` — new golden case.

Risks:
- Cluster detection is full-table scan (no cross-entity index). Bounded by
  fact count (~500 in bench). Acceptable at Slice 2; index if > 10k facts.

### Thread D — security + observability — M

Files to touch:
- `internal/team/prompts/extract_entities_lite.tmpl` §39-41, `synthesis_v2.tmpl`
  §53-55, `answer_query.tmpl` §45-51 (Source.Excerpt) — render via
  `escapeForPromptBody`.
- New helper `internal/team/prompt_escape.go` — `EscapeForPromptBody(s)`
  strips/escapes triple-backticks, `---`, YAML frontmatter delimiters,
  injection-flavored tokens. Conservative: replace with visibly-broken
  variants, never silently drop.
- `internal/team/wiki_query.go:263-275` — after parse, validate
  `sources_cited[i] in [1..len(sources)]`; drop invalid + append to `answer.Notes`.
- `internal/team/wiki_extractor.go:196-202` — after parse, run
  `sanitizeTriplets(out)`; reject control chars, newlines, `';` sequences;
  route rejected to DLQ with category=validation.
- `internal/team/wiki_index_signal_adapter.go:39-40,55-57,77-79,102-105` —
  when in-memory cast fails, fall through to `FactStore.IterateEntities`.
- `internal/team/wiki_index.go:142-156` — add
  `IterateEntities(ctx, fn func(IndexEntity) error) error` to FactStore.
- `internal/team/wiki_index_sqlite.go` — implement via streaming
  `SELECT ... FROM entities`.
- New broker handler: `GET /wiki/dlq` returns pending + promoted JSON.
- `web/src/pages/WikiDLQ.tsx` — minimal list view with retry/resolve columns.

Tests:
- `TestEscapeForPromptBody` — triple-backticks, `---\nfrontmatter:`, "Ignore
  previous instructions" → safe non-executing text.
- `TestSourcesCitedBoundsValidator` — hallucinated `[99]` dropped, Notes updated.
- `TestTripletCharValidator` — control chars/newlines/`';` rejected → DLQ.
- `TestSQLiteIterateEntities` — 100 entities, cursor visits each exactly once.
- `TestSignalAdapterOnSQLite` — EntityByEmail/Name return hits.
- `TestHandleWikiDLQ` — 2 pending + 1 promoted → correct JSON; auth required.

Risks:
- Overly aggressive escape mutates legitimate markdown. Mitigation: narrow
  list (backticks, line-start `---`, frontmatter keys); golden eval asserts
  benign code-fenced email extracts identical facts before/after.

### Thread E — web polish — S

Files to touch:
- `web/src/components/ResolveContradictionModal.tsx` — `isResolving` state;
  disable + spinner while POST in flight.
- `web/src/components/LookupComposer.tsx` — track submission, inline spinner
  until SSE answer lands; clear stale toast on resubmit.
- `web/src/components/SlashAutocomplete.tsx` — add `/lint` to completions.
- `web/src/pages/WikiResolve.tsx` — success toast linking to `<commit-sha>`.

Tests:
- `ResolveContradictionModal.test.tsx` — disabled state + toast + commit link.
- `SlashAutocomplete.test.tsx` — `/li` → `/lint`.
- Playwright E2E: `/lookup` shows spinner for queries > 200ms.

Risks: low; UX-only.

## Sequencing

1. **Thread A first** (longest pole + ship gate). FactStore interface + SQLite
   index + in-memory impl → query rewriter → generator cap. One PR turns the
   benchmark green.
2. **Thread D security items** in parallel with A. Prompt escape + triplet
   sanitizer have no retrieval dependencies; they unblock the security gate.
3. **Thread B** after A/D — fact-log writes touch the extractor that D also
   sanitizes.
4. **Thread C** after B — cluster synthesis reads the now-authoritative fact log.
5. **Thread E** lands alongside B/C as UX polish, not a separate PR.

## Estimates

- Thread A: **L**, 3–4 days. Typed retrieval + rewriter is the real work.
- Thread B: **M**, 1.5 days. Mostly wiring; reboot test surfaces edge cases.
- Thread C: **M**, 1.5 days. Cluster detection simple; prompt + eval careful.
- Thread D: **M**, 2 days. Iterator small; escape + DLQ UI adds up.
- Thread E: **S**, 0.5 day.
- **Total: L, 8–9 working days. Budget 10 for review cycles.**

## Open questions

1. Generator cap semantics: truncate `expected` to `topK`, or scale per-query
   `expected_min_recall_at_20` instead? Both fix the math; truncate is cleaner.
2. Escape policy: reversible at render time, or one-way? One-way is simpler.
   Proposing one-way unless excerpt becomes user-facing.
3. InsightCluster threshold: N=3 reinforcements feels right for 500 facts.
   For 50k facts we may want N=5+ or a configurable env var. OK to ship
   hardcoded 3 with a TODO?

## Out of scope (Slice 3)

- InsightCluster as first-class rows (computed on demand in Slice 2).
- Embedding-based retrieval (BM25 + typed graph walk is sufficient at this corpus size).
- MCP server discovery + cross-workspace search.
- Lint-driven auto-merge of redirects.
- Playbook-execution rate limiting (not ship-gate-relevant).
- Sections-cache invalidation race (debounce workaround holding).
- Human-identity forgery on `/wiki/write` (bearer auth is the current gate).
- Wiki-backup GC policy (disk is cheap; no real user has hit the limit).
