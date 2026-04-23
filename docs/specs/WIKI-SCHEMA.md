# WIKI-SCHEMA.md — Contract for WUPHF's team wiki

This file is the source of truth for **how the WUPHF wiki is organized and maintained**. Every in-broker LLM prompt that touches wiki state (extract, synthesize, query, lint) references this document as its opening directive. Every human (or agent) editing wiki files by hand reads this document first. Every Go service that writes to or indexes the wiki follows the contract below.

If a contract decision below conflicts with code, the contract wins. Fix the code.

This is Karpathy's "schema layer" for WUPHF — the document that makes the LLM a disciplined wiki maintainer rather than a generic chatbot. See `https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f` for the pattern this implements.

---

## 1. Purpose

The WUPHF wiki is a git-native, human-readable knowledge base for a team of AI agents and humans working together. It is the compounding intelligence layer that makes the moat real: every agent turn, email, meeting, or conversation can become durable, indexed, cross-referenced knowledge in markdown form, readable with `cat` and `git log`.

The wiki is NOT a chat log, NOT a raw artifact dump, NOT a vector database. It is an encyclopedia of the team's operating reality, actively maintained by the agents and humans who contribute to it.

**Guiding principles:**

1. **Markdown is the source of truth.** Every fact, brief, insight, playbook, and lint finding lives in a markdown file, version-controlled by git. Everything else — SQLite indexes, bleve search, vector stores — is a derived cache, rebuildable from markdown on demand.
2. **Substrate guarantee.** `rm -rf .wuphf/index/` → restart broker → the wiki still works. `git clone` of the wiki repo on a fresh machine → functional wiki without any WUPHF process running. Manual markdown edits in vim → picked up by the next index reconcile pass.
3. **Single writer, many readers.** All writes go through the broker's `WikiWorker` queue. Agents, HTTP handlers, and CLI commands all enqueue write requests; the worker serializes commits. This preserves git-log attribution, prevents conflicting writes, and gives us the single-writer invariant that makes fact IDs deterministic.
4. **Per-human git identity.** Every commit is authored by a named identity — either a human (e.g. `nazz`) or the synthetic `archivist` (used for automated extraction, synthesis, and lint). Agent-originated commits are attributed to the human who owns that agent. `git log` on any file shows exactly who did what.
5. **Compounding over curation.** Agents contribute facts by default, not by request. The auto-loop closes without human ritual: agent talks → artifact committed → entities extracted → facts recorded → brief synthesized → next agent queries → reinforces or contradicts. Human intervention is rare and always additive.

---

## 2. When to read this document

Read in full on:
- First call to `wuphf_wiki_lookup` in a session (agent or human)
- First extraction run per artifact kind
- Every `synthesize` pass that creates or updates a brief
- Every `run_lint` invocation
- Onboarding any new agent type or ingest source

Read the relevant section when:
- Adding a new field to any frontmatter (Section 4)
- Filing a new artifact, fact, insight, or playbook (Section 5)
- Resolving a contradiction (Section 9)
- Extending any in-broker prompt (Section 10)

---

## 3. Three-layer architecture

```
Layer 1 — Raw sources (immutable)
  wiki/artifacts/{source}/{sha}.md       # agent messages, meeting transcripts,
                                         # email threads, CLI notes
  These files are the factual record. The LLM reads them but NEVER modifies them.

Layer 2 — The wiki (LLM-owned markdown)
  team/{kind}/{slug}.md                  # entity briefs (person, company, project)
  wiki/facts/{kind}/{slug}.jsonl         # append-only fact log per entity
  wiki/insights/entity/{slug}.jsonl      # append-only typed insight log per entity
  wiki/insights/knowledge/{slug}.md      # workspace-wide facts/decisions/preferences
  wiki/playbooks/{slug}.md               # compiled playbooks (from executions
                                         # and/or insight clusters)
  wiki/playbooks/{slug}.executions.jsonl # per-playbook execution log
  wiki/.lint/report-YYYY-MM-DD.md        # daily lint report
  wiki/.dlq/extractions.jsonl            # extraction failures awaiting replay
  wiki/.dlq/permanent-failures.jsonl     # artifacts that exceeded max_retries
  wiki/redirects.md                      # auto-generated slug redirect index
  graph.log                              # typed cross-entity edges

  The LLM creates, updates, and cross-references these files. Humans may edit
  them directly; the next reconcile pass will pick up the edits.

Layer 3 — The schema (you are reading it)
  docs/specs/WIKI-SCHEMA.md                    # this file
  docs/CLAUDE.md or AGENTS.md            # the agent operating instructions that
                                         # reference this schema
```

---

## 4. Frontmatter vocabulary

YAML frontmatter fields used across wiki files. Every field has a default; legacy entries missing a field parse with the default. No migration step required.

### 4.1 Entity brief — `team/{kind}/{slug}.md`

```yaml
---
canonical_slug: sarah-jones          # authoritative slug. If this is a redirect,
                                      # the value is the real slug this file points to
kind: person                          # person | company | project | team | workspace
aliases:                              # other names this entity is known by
  - Sarah J.
  - sjones
signals:                              # matching signals used by the resolver
  email: sarah@acme.com
  domain: acme.com
  person_name: Sarah Jones
  job_title: VP of Sales
last_synthesized_sha: 3f9a21b         # git HEAD at last synthesis
last_synthesized_ts: 2026-04-22T14:32:00Z
fact_count_at_synthesis: 47           # fact-log length at last synthesis; used
                                      # to decide "new facts since last pass"
created_at: 2026-01-16T09:14:00Z
created_by: nazz                      # the human who caused this entity to exist
---
```

### 4.2 Fact — line in `wiki/facts/{kind}/{slug}.jsonl`

One JSON object per line. `id` is deterministic — see Section 7.

```json
{
  "id": "a3f9b2c14e8d",
  "entity_slug": "sarah-jones",
  "type": "observation",
  "triplet": {
    "subject": "sarah-jones",
    "predicate": "role_at",
    "object": "acme-corp:vp-sales"
  },
  "text": "Role at Acme Corp: VP Sales, promoted 2026-04-10.",
  "confidence": 0.92,
  "valid_from": "2026-04-10T00:00:00Z",
  "valid_until": null,
  "supersedes": ["7c2e88a1b9f3"],
  "contradicts_with": null,
  "source_type": "chat",
  "source_path": "wiki/artifacts/agent/3f9a21bc.md",
  "sentence_offset": 142,
  "artifact_excerpt": "We're delighted to announce Sarah's promotion to VP...",
  "created_at": "2026-04-22T13:01:00Z",
  "created_by": "archivist",
  "reinforced_at": "2026-04-20T09:15:00Z"
}
```

### 4.3 Field reference (authoritative)

| field | type | default | notes |
|---|---|---|---|
| `id` | string(16) | required | sha256 hash — see Section 7 |
| `entity_slug` | string | required | canonical slug of the entity |
| `type` | enum | `"observation"` | `status` / `observation` / `relationship` / `background` |
| `triplet` | object | `null` | s/p/o; null for untyped facts |
| `triplet.subject` | string | — | slug or literal |
| `triplet.predicate` | string | — | normalized lowercase, underscores |
| `triplet.object` | string | — | slug, literal, or `entity:subslug` |
| `text` | string | required | human-readable fact statement |
| `confidence` | float | `1.0` | 0.0-1.0, source-driven |
| `valid_from` | ISO-8601 | `created_at` | when the fact became true |
| `valid_until` | ISO-8601 \| null | `null` | null = "still valid" until decay says otherwise |
| `supersedes` | array[id] | `[]` | fact IDs this replaces |
| `contradicts_with` | array[id] \| null | `null` | fact IDs this contradicts (flagged by lint) |
| `source_type` | enum | `"manual"` | `chat` / `meeting` / `email` / `manual` / `linkedin` / etc. |
| `source_path` | string \| null | `null` | wiki path to originating artifact |
| `sentence_offset` | int | `0` | byte position of sentence in artifact (disambiguates multi-fact artifacts) |
| `artifact_excerpt` | string | derived | optional snippet cached at extraction time |
| `created_at` | ISO-8601 | required | commit timestamp |
| `created_by` | string | required | git identity (agent-mapped-to-human OR `archivist`) |
| `reinforced_at` | ISO-8601 \| null | `null` | last time a merge-by-dedup hit this fact |

### 4.4 Insight — line in `wiki/insights/entity/{slug}.jsonl`

Insights are facts that rise above the noise: status changes, decisions, patterns worth filing as first-class knowledge.

```json
{
  "id": "ins_8c2e88",
  "entity_slug": "sarah-jones",
  "type": "status_change",
  "text": "Sarah was promoted to VP of Sales on 2026-04-10 after leading the marketing-to-sales transition.",
  "derived_from": ["a3f9b2c14e8d", "7c2e88a1b9f3"],
  "confidence": 0.95,
  "created_at": "2026-04-22T14:32:00Z",
  "created_by": "archivist",
  "source": "synthesis" 
}
```

`source` = `"synthesis"` | `"save_as_insight"` | `"lint"` | `"human"`.

### 4.5 Playbook — `wiki/playbooks/{slug}.md`

```yaml
---
kind: playbook
slug: enterprise-pricing-objections
author: nazz
inputs:                                # optional — what the playbook needs
  entity_kinds: [company, person]
  signal_predicates: [pricing_concern, budget_constraint]
execution_count: 7
last_executed: 2026-04-20T10:15:00Z
---
```

Body: the playbook instructions (what to do when pattern matches). After synthesis, a `## What we've learned` section is appended with execution-log insights (v1.3 pattern, extended in Slice 2 to also include insight-cluster patterns as `## Patterns across entities`).

### 4.6 Lint report — `wiki/.lint/report-YYYY-MM-DD.md`

Standard wiki article shape. Sections fixed in order: Contradictions, Orphans, Stale claims, Missing cross-refs, Dedup review, Sources.

---

## 5. Filing conventions

### 5.1 When to create a new brief vs extend an existing one

**Create a new `team/{kind}/{slug}.md`** when:
- Extraction produces an entity with no signal match in the SQLite signal index (exact email, exact normalized name, fuzzy name ≥ 0.9).
- Ghost entity: a speaker name appears in a transcript but no prior entity exists.

**Extend an existing brief** when:
- Extraction produces an entity whose signal matches an existing entity above threshold.
- The new fact is a legitimate update (supersede) or reinforcement (Jaro-Winkler match ≥ 0.9) of an existing fact.

**Never** rename an existing brief. If two briefs turn out to refer to the same entity (discovered later), use redirect (Section 8.2) to merge, preserving both histories.

### 5.2 Fact vs insight vs brief body

| Goes in `wiki/facts/*/{slug}.jsonl` | Goes in `wiki/insights/entity/{slug}.jsonl` | Goes in brief body |
|---|---|---|
| Raw atoms with source citation | Synthesized/promoted/structured claims | Narrative prose synthesized from facts |
| One sentence per entry, high confidence | Promoted from multiple facts or from save-as-insight | Written by `archivist` synthesis or by humans |
| Machine-generated by extraction | May be human-authored | Readable standalone |
| Append-only; never deleted | Append-only; supersedes by `derived_from` | Rewritten on each synthesis |

### 5.3 What counts as a fact

- Statements of fact about an entity with a clear source (`source_path`).
- Explicit triplets: `Sarah works at Acme`, `Acme raised Series B`, `Q2 pilot launched 2026-04-10`.
- Confidence reflects extraction certainty, not content truth.

**NOT facts:**
- Opinions ("Sarah is a great salesperson") — unless expressed as a direct quote with source.
- Aggregations ("Sarah has closed 3 deals this month") — derive at query time from fact list.
- Speculation, hypotheses, or agent inference not grounded in an artifact.

### 5.4 When to promote a fact to an insight

Synthesis (`EntitySynthesizer`) promotes a fact cluster to an insight when:
- `valid_until` on a prior fact was set by a new supersede → the status change is noteworthy.
- Three or more facts share a subject+predicate across artifacts → the pattern is durable.
- An agent calls `save_as_insight` on a `/lookup` answer → user has marked this synthesis as useful.

**Never** promote unreinforced or unsourced facts to insights.

---

## 6. Cross-reference rules

### 6.1 Wikilinks in brief body

Use `[[slug]]` or `[[slug|Display]]` syntax to link to another brief. Example: `[[people/sarah-jones]]`. Broken wikilinks (target doesn't exist) render red; lint will flag them as candidates for ghost-entity creation.

Every mention of an entity in a brief body SHOULD be a wikilink to that entity's brief. Synthesis prompts are instructed to wikilink on first mention per entity.

### 6.2 Typed graph edges in `graph.log`

For relationships stronger than "mentioned in the same document," add a typed edge to `graph.log`:

```
sarah-jones  works_at  acme-corp  2026-04-10  src=a3f9b2
sarah-jones  champions  q2-pilot  2026-04-18  src=1b7d24
```

Typed edges appear in `EntityRelatedPanel` on each brief. Lint flags co-occurring entity pairs that lack a typed edge.

### 6.3 Automatic cross-reference promotion

When two entities co-occur as subject+object in ≥3 facts, lint's "missing cross-refs" pass suggests adding a typed edge. Human or agent confirms via `run_lint` + resolve.

---

## 7. Canonical slug rules — the ID stability contract

This is the load-bearing correctness invariant of the wiki. Getting this wrong means fact IDs drift, supersedes break, and the wiki silently diverges from reality.

### 7.1 Slug assignment (happens exactly once per entity, at creation time)

Algorithm (deterministic, idempotent):

1. Normalize the entity's primary signal (email → lowercase + trim; name → kebab-case lowercased, stripping honorifics and suffixes).
2. If the normalized value is unique in the existing signal index, use it as the slug.
3. If collision: append `-2`, `-3`, etc. until unique. Never strip or truncate.
4. Write the brief to `team/{kind}/{slug}.md` with `canonical_slug: <slug>` in frontmatter.

**Never** rename a slug after creation, even if the entity's name changes. The slug is a stable ID.

### 7.2 Slug merging (happens when two briefs turn out to be the same entity)

Algorithm:

1. Pick the slug with the older `created_at` as the survivor. The younger slug becomes a redirect.
2. Write the younger's `canonical_slug: <survivor-slug>` frontmatter; replace the body with a redirect stub: `This page redirects to [[{survivor-slug}]]. Prior content preserved in git history.`
3. Append the redirect mapping to `wiki/redirects.md` (auto-generated, human-readable index).
4. Re-parent all facts from younger to survivor: rewrite `wiki/facts/{kind}/{younger}.jsonl` entries to set `entity_slug` to survivor, then append to survivor's fact log. (A git mv + ID-preserving rewrite; fact IDs stay the same because they're content-hashed.)
5. Update graph edges.

Merging is a human-confirmed operation, surfaced through lint → `ResolveContradictionModal`.

### 7.3 Fact ID determinism

```go
fact_id = sha256(artifact_sha + "/" + sentence_offset + "/" + norm(subject) + "/" + norm(predicate) + "/" + norm(object))[:16]
```

`norm()` = lowercase, trim, replace non-alphanumeric with `-`.

**Rebuild is deterministic:** same artifacts + same extraction runs produce identical IDs. New extraction runs legitimately produce new IDs. Dedup-by-merge at commit time (Jaro-Winkler ≥ 0.9 on predicate) collapses near-duplicates into the existing fact, bumping `reinforced_at` instead of creating a new row. This is explicit semantics, not a bug.

### 7.4 Rebuild contract

`rm -rf .wuphf/index/` → restart broker → boot reconcile runs → SQLite + bleve re-indexed from markdown. The result must be **logically identical**, not byte-identical. Logical identity means: `SELECT * FROM facts ORDER BY id` produces the same canonical hash pre- and post-rebuild.

---

## 8. Decay & temporal validity

### 8.1 Staleness formula

```
staleness = (days_old × type_weight) − (confidence × 10) − reinforcement_bonus
```

Where:
- `days_old = (now - valid_from) in days`
- `type_weight`: `status=1.0`, `observation=0.5`, `relationship=0.2`, `background=0.1`
- `reinforcement_bonus = 5.0 × max(0, 1 - days_since_reinforced / 30)` (full bonus if reinforced within last day; linear decay to 0 at 30 days)

**Query-time filter:**
- Status/recency queries: exclude facts with `staleness > 20`
- Historical queries: include all facts regardless of staleness
- All facts remain physically in the fact log; staleness is a read-time visibility filter, not deletion

### 8.2 Temporal validity

`valid_from` + `valid_until` bracket the time window a fact was true. A status change writes a new fact with new `valid_from`, AND updates the superseded fact's `valid_until` to the new fact's `valid_from`.

Facts with `valid_until = null` are currently valid (until staleness excludes them).

---

## 9. Lint rules

Daily cron at 09:00 local; manually triggerable via `/lint` slash command or `run_lint` MCP tool. Writes `wiki/.lint/report-YYYY-MM-DD.md` via `WikiWorker` under `archivist` identity.

### 9.1 What lint checks

1. **Contradictions** — critical. For each entity, find facts with same `(subject, predicate)` but conflicting `object`. Run `lint_contradictions.j2` LLM-judge to confirm a real semantic conflict vs benign disagreement. Flag with `contradicts_with` frontmatter on both facts.
2. **Orphans** — warning. Briefs with no inbound graph edges AND no fact activity in 90 days.
3. **Stale claims** — warning. Facts with `staleness > 30` that have never been reinforced.
4. **Missing cross-refs** — info. Entity pairs co-occurring as subject/object in ≥3 facts but lacking a typed graph edge.
5. **Dedup false-positives** — info. Facts merged in the last 7 days with Jaro-Winkler scores in the 0.9-0.93 range (borderline, worth a human glance).

### 9.2 What "Resolve contradiction" does

Clicking **Resolve** on a contradiction finding opens a mini-dialog (reuses `NewArticleModal` pattern):

- **[Fact A]** → appends `supersedes: [B.id]` to Fact A; sets `valid_until` on Fact B to now. Fact B remains in the log for history.
- **[Fact B]** → same with roles reversed.
- **[Both]** → appends `contradicts_with: [A.id]` to Fact B and vice versa; both facts remain currently valid; lint stops flagging (human has acknowledged the ambiguity).

All three options write via `WikiWorker` under the human's git identity. The resolution is a real git commit, visible in `git log`.

### 9.3 Severity and display

- **Critical** (contradictions, dedup false-positives): red `--wikilink-broken` text + `aria-label="Critical finding"`.
- **Warning** (stale claims, orphans): amber `--amber` text + `aria-label="Warning finding"`.
- **Info** (missing cross-refs, advisories): muted `--text-tertiary` text + `aria-label="Info finding"`.

Never rely on color alone. Always pair with text label and `aria-label`.

---

## 10. Prompt guidance

All in-broker LLM prompts touching wiki state begin with: *"Read docs/specs/WIKI-SCHEMA.md first. Follow its contract exactly. Never invent new frontmatter fields, new wiki file locations, or new conventions. If you find yourself wanting to do any of those, stop and surface the question to the user via the broker's /status alert."*

### 10.1 Extraction (`extract_entities_lite.tmpl`)

**Goal:** turn a single artifact into a list of typed entity mentions + candidate facts with triplets + confidence + sentence offset.

**Rules the prompt must enforce:**
- Entity type must match Section 4.1 enum (`person | company | project | team | workspace`).
- Predicates in triplets must be lowercase, underscore-separated.
- Confidence reflects extraction certainty, not content truth.
- Never invent an email address. If the artifact doesn't name the email, leave it blank.
- Never invent a relationship. Only extract what the artifact text supports.
- If a speaker name appears in a transcript but the artifact doesn't give their email/domain, emit an entity anyway with confidence 0.8 and `signals.person_name` only — ghost entity handling.

### 10.2 Synthesis (`synthesis_v2.tmpl`)

**Goal:** given a brief's current body + the new facts since last synthesis, produce an updated brief body.

**Rules the prompt must enforce:**
- Never invent facts. Every claim in the body must trace to a fact in the fact log.
- Preserve existing body structure (sections, order) unless the new facts demand a restructure.
- Mark contradictions inline with italic **Contradiction:** callouts (they'll be upgraded to hatnotes by the renderer).
- Wikilink every entity mention on first occurrence: `[[people/sarah-jones]]`.
- Never write a `## Related` section — that block is managed deterministically from the graph edges by the synthesizer.
- Never write a `## Sources` section — managed by the renderer from git history.

### 10.3 Query (`answer_query.tmpl`)

**Goal:** given a user query + top-K retrieved facts/briefs/playbooks, produce a cited answer with inline `<sup>[n]</sup>` references.

**Rules the prompt must enforce:**
- Every factual claim must cite a source by index.
- Use temporal validity: for status/recency queries, exclude facts with `staleness > 20`.
- For counterfactual queries ("what if X hadn't happened"), identify the causal chain in the evidence and mark outcomes that depend on the removed condition as conditional.
- If the question is outside scope (pure general knowledge with no plausible business context), respond: *"I don't have information about that. I can help with questions about people, companies, and activities in your workspace."*
- Never invent a citation. If you can't find a source, say so.

### 10.4 Lint (`lint_contradictions.tmpl`)

**Goal:** given a cluster of facts sharing `(subject, predicate)`, determine if there is a real semantic conflict.

**Rules the prompt must enforce:**
- Distinguish real contradictions ("Sarah reports to Michael" vs "Sarah reports to David") from benign disagreements ("Sarah works in sales" vs "Sarah works in enterprise sales" — not a contradiction, just different specificities).
- Consider temporal validity: if one fact's `valid_until` has passed, it's not a contradiction with a current fact.
- Output `{contradicts: true|false, reason: "..."}`. No freeform prose.

---

## 11. Anti-patterns — what NOT to do

Every instance of the below is wrong and must be corrected immediately.

1. **Hallucinating facts.** Never write a fact without a grounded `source_path`. If extraction hits something it can't source, it goes to the DLQ for human review, not to the fact log.
2. **Inventing new frontmatter fields.** The vocabulary in Section 4 is authoritative. If a new field is needed, update this schema document and get explicit human approval before writing any code that produces or consumes it.
3. **Inventing new file locations.** The file layout in Section 3 is authoritative. Never write to a path not listed there.
4. **Renaming slugs.** Canonical slugs are immutable once assigned. Merging two entities goes through the redirect mechanism (Section 7.2), never through a rename.
5. **Writing outside the WikiWorker.** All wiki writes must enqueue through the broker's WikiWorker, preserving the single-writer invariant and per-human git identity. Direct filesystem writes break attribution, fact ID determinism, and the rebuild contract.
6. **Guessing at temporal validity.** If the source doesn't state when a fact became true, `valid_from` defaults to the artifact's `occurred_at`. Don't invent a specific date.
7. **Filing chat prose as a fact.** Raw conversational text goes in `wiki/artifacts/`. Only distilled, structured claims go in `wiki/facts/`. Extraction is a distillation step, not a copy step.
8. **Promoting low-confidence facts to insights.** Insights are the compound-interest layer. Only facts with `confidence ≥ 0.8` AND reinforced at least once, OR facts explicitly saved via `save_as_insight`, should become insights.
9. **Overwriting brief frontmatter keys owned by the synthesizer.** Keys like `last_synthesized_sha`, `last_synthesized_ts`, `fact_count_at_synthesis` are managed by `EntitySynthesizer`. Human edits must leave them alone.
10. **Editing git history.** The wiki repo must never be force-pushed, rebased interactively in place, or amended. Every edit is a real, attributable commit. Rewriting history breaks fact ID determinism (the artifact_sha changes).
11. **Filing speculation as fact.** "Sarah probably wants the enterprise plan" is not a fact. "Sarah said on the call: 'we're leaning toward enterprise'" is a fact.
12. **Creating cards where the wiki design uses hatnotes or Sources.** This is a visual anti-pattern but carried through UI code. Cited answers, contradiction markers, and lint findings compose from existing Wikipedia IA primitives. See `DESIGN-WIKI.md` + the `plan-design-review` Component-level Extension Spec.

### §11.13 DLQ semantics

The DLQ (`wiki/.dlq/extractions.jsonl`) holds extraction failures that are eligible for replay. Files are **append-only on disk** — never rewritten. Successful replays and permanent promotions are recorded as tombstone rows.

#### Per-line shape

```json
{
  "artifact_sha": "3f9a21bc",
  "artifact_path": "wiki/artifacts/chat/3f9a21bc.md",
  "kind": "chat",
  "last_error": "json_parse_error: expected { got \"\`\`\`json\\n{\"",
  "error_category": "parse | provider_timeout | validation",
  "retry_count": 2,
  "max_retries": 5,
  "first_failed_at": "2026-04-22T13:00:00Z",
  "last_attempted_at": "2026-04-22T15:10:00Z",
  "next_retry_not_before": "2026-04-22T15:20:00Z"
}
```

#### Tombstone rows

A **resolved** artifact appends:
```json
{"artifact_sha": "3f9a21bc", "resolved_at": "2026-04-22T16:00:00Z"}
```

A **promoted** artifact (exceeded `max_retries`) appends in `extractions.jsonl`:
```json
{"artifact_sha": "3f9a21bc", "promoted_at": "2026-04-22T16:00:00Z"}
```
and a full DLQ entry is written to `permanent-failures.jsonl`.

#### Replay policy

- Backoff: `min(10 min × 2^retry_count, 6 h)`. `next_retry_not_before` encodes the earliest eligible replay time.
- Default `max_retries`: 5.
- `error_category = "validation"` automatically sets `max_retries = 1` — programming errors are never retried past the first attempt.
- After `max_retries` attempts the entry moves to `permanent-failures.jsonl` and is excluded from the active replay queue.
- `ReadyForReplay` scans the full file and skips any `artifact_sha` that has a matching `resolved_at` or `promoted_at` tombstone.

---

## 12. Change log

| Date | Change | Rationale |
|---|---|---|
| 2026-04-22 | Initial draft | Karpathy schema layer for Slice 1 of wiki-intelligence port. Covers three-layer architecture, frontmatter vocabulary, canonical slug rules, decay formula, lint rules, prompt guidance, anti-patterns. |

Update this log on every substantive revision. Small wording tweaks don't require a log entry; new fields, new file locations, and changed algorithms do.

---

## 13. Referenced documents

- `DESIGN-WIKI.md` — the visual design system for the wiki surface (palette, typography, Wikipedia IA primitives, anti-slop policy). Read when rendering any wiki UI.
- `~/.gstack/projects/nex-crm-wuphf/najmuzzaman-feat+slash-registry-design-20260422-174617.md` — the design doc for Slice 1-3 of the wiki intelligence port. Background + rationale; this schema doc is the operational spec derived from it.
- `https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f` — the original LLM Wiki pattern this implements.
