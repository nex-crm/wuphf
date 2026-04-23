# evals/ — Slice 0.5 prompt eval harness

Minimal golden-case harness for the three wiki intelligence prompts. Calibrates
prompt drift, regression-catches before Slice 1 ships, and feeds the Week 0
benchmark corpus.

## Layout

```
evals/
  README.md                        — this file
  extract/                         — extract_entities_lite.tmpl cases
    001_promotion_announcement.json
    002_ghost_entity_in_transcript.json
    003_ambiguous_multi_entity.json
  synthesis/                       — synthesis_v2.tmpl cases
    001_new_brief_first_facts.json
    002_supersede_role_change.json
    003_contradiction_flagged.json
  query/                           — answer_query.tmpl cases
    001_status_lookup.json
    002_multi_hop_relationship.json
    003_out_of_scope_refusal.json
  harness/
    schema.json                    — JSON Schema for an eval case
    README.md                      — how to run (once wiki_index.go lands)
```

## Eval case shape

Each case is a single JSON file:

```jsonc
{
  "id": "extract_001_promotion_announcement",
  "prompt": "extract_entities_lite",        // which template this exercises
  "description": "Explicit promotion announcement with email signals present",
  "template_vars": { /* inputs the template expects */ },
  "expected": {
    "must_include": [ /* substrings or structured matchers */ ],
    "must_not_include": [ /* substrings or structured matchers */ ],
    "structured": { /* schema-shaped expectations */ },
    "notes": "what this case is defending"
  }
}
```

`must_include` / `must_not_include` are string matchers on the serialized LLM
output. `structured` is a partial JSON shape the output must match
(entity count, fact count, specific predicates, etc.).

## How grading works (once `wiki_index.go` + runner land)

1. Load the prompt template from `prompts/{prompt}.tmpl`.
2. Render with `template_vars`.
3. Send to `provider.RunConfiguredOneShot`.
4. Parse the JSON or markdown block the prompt specifies.
5. For each `expected.must_include` substring: assert present.
6. For each `expected.must_not_include`: assert absent.
7. For `expected.structured`: deep-partial match against the parsed output.
8. Pass/fail per case; aggregate report.

## Why Slice 0.5 before Slice 1

The three prompts are the entire moat. If they hallucinate, invent slugs, or
skip citations, the whole wiki intelligence story collapses. A 3-case-per-prompt
harness catches regressions at `~2-3s` per case (single LLM shell-out) and runs
in under a minute locally.

## Adding new cases

Append to the relevant subdir. Keep cases small (one behavior each). When a
production regression hits, add the minimal repro case here so it does not
regress again.

## Relationship to Week 0 benchmark corpus

The Week 0 corpus (`bench/slice-1/corpus.jsonl` + `queries.jsonl`) is a larger
scale recall@20 test — 500 synthetic artifacts across 25 synthetic people + 50
queries. The Slice 0.5 eval harness here is the prompt-quality gate; the Week 0
benchmark is the ship gate. Both are required.
