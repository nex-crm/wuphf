# Slice 1 benchmark corpus — Week 0 recall gate

This directory is the **hard ship gate** for Slice 1 of the wiki intelligence
port. Before email sync pipes thousands of real entities into the wiki, the
retriever must prove it can find the right facts on a synthetic corpus.

The gate: **recall@20 >= 85%** across the 50 queries in `queries.jsonl`.

## Files

| File | What it is |
|------|------------|
| `generate.go` | Deterministic generator (seed 42). Produces both JSONL files. |
| `corpus.jsonl` | 500 synthetic artifacts (chat / meeting / email). Each line is one artifact. |
| `queries.jsonl` | 50 retrieval queries, each mapped to the fact IDs the retriever must surface. |

## What the corpus covers

- **25 synthetic people** across **5 synthetic companies** (5 people each):
  Acme Corp, Blueshift, Dunder Mifflin, Northwind, Vandelay Industries.
- **8 synthetic projects**: Q2 Pilot Program, Pricing Rework, APAC Launch,
  Security Audit, Onboarding V3, Data Platform Migration, Mobile Revamp,
  Partner Program.
- **500 artifacts** over a ~4-month simulated timeline (one every 6 hours
  starting 2026-01-15), distributed as:
  - **300 single-entity status / observation facts** (60%)
  - **100 multi-entity relationship facts** (20% — leads / champions /
    works-with)
  - **50 superseding facts** (10% — role changes that replace an earlier
    statement)
  - **25 contradictions** (5% — two facts that disagree on the same
    subject+predicate)
  - **25 noise artifacts** (5% — no extractable fact; test null handling)
- **475 extractable facts** total across those artifacts.
- **Linguistic variety**: every fact type rotates through 10 different
  sentence patterns so the extractor sees real prose, not templates.

## What the queries cover

- **20 status queries** — "What does X do?", "Who is X?", "Where does X
  work?", "What is X's current role?"
- **15 relationship queries** — "Who leads Y?", "Who champions Y?", "Who is
  involved in Y?"
- **10 multi-hop queries** — "Who at Acme championed Q2 Pilot?" (intersects
  champions(project) with role_at(company))
- **3 counterfactual queries** — "What would have happened if X hadn't
  taken her current role?" The retriever should still surface the
  current-role facts; the reasoning layer decides whether to refuse.
- **2 out-of-scope queries** — expected fact set is empty; retriever should
  return nothing.

## What `expected_min_recall_at_20 >= 0.85` means

For each query:

1. Run the retriever, take the top 20 fact IDs.
2. Intersect with `expected_fact_ids`.
3. `recall = |intersection| / |expected_fact_ids|`
4. Fail the query if `recall < expected_min_recall_at_20`.

Out-of-scope queries have `expected_fact_ids: []` and a recall target of
`1.0`, which is vacuously satisfied when the retriever returns nothing. If
the retriever surfaces facts for an out-of-scope query, score that as a
separate precision failure.

Pass the gate when at least **85% of queries** clear their individual
recall threshold.

## Regenerating

The generator is deterministic (seed = 42). Running it twice produces
byte-identical output:

```bash
go run ./bench/slice-1/
```

Output lines:

```
wrote 500 artifacts, 475 facts, corpus size ~258000 bytes
wrote 50 queries
```

Never hand-edit `corpus.jsonl` or `queries.jsonl` — rerun the generator
and commit the regenerated files.

## Fact ID contract

Every `fact_id` in the corpus is computed by
`team.ComputeFactID(artifact_sha, sentence_offset, subject, predicate, object)`
from `internal/team/wiki_index.go` — the same helper the live index uses.
This makes the benchmark byte-compatible with production extraction: if the
generator emits `fact_id = 57b5960baf097ab5` for a triplet, the production
extractor seeing the same artifact SHA and triplet produces the same ID.

The hash follows §7.3 of `docs/specs/WIKI-SCHEMA.md`:

```
sha256(artifact_sha + "/" + sentence_offset + "/" +
       norm(subject)  + "/" + norm(predicate) + "/" +
       norm(object))[:16]
```

where `norm(s)` lowercases, trims, and replaces non-alphanumeric runs with
a single dash. Same artifact + same extraction → same ID.

## Validation baked into the generator

Every run asserts:

- All `artifact_id` values are unique.
- Every `fact_id` referenced from a query exists in the corpus.
- Total `corpus.jsonl` size <= 2 MB.

A validation failure fails the generator with a non-zero exit code.

## Why Slice 1 needs this

Per the Slice 1 design note
(`~/.gstack/projects/nex-crm-wuphf/najmuzzaman-feat+slash-registry-design-20260422-174617.md`),
Week 0 ships the benchmark and the null retriever so we can measure
improvement delta as we wire in hybrid search (BM25 + semantic) and the
typed-fact index during Weeks 1-4. Without a ground-truth corpus, every
retrieval improvement is speculation.
