# evals/harness/

Runner contract and schema for the Slice 0.5 prompt eval harness.

## Schema

`schema.json` defines the shape of a single eval case. Every file under
`evals/{extract,synthesis,query,lint}/*.json` validates against it.

## Runner (pending wiki_index.go landing)

The Go-side runner lives at `cmd/eval-prompts/main.go` (not yet written).
Responsibilities:

1. Walk `evals/*/` for `.json` files.
2. For each case:
   - Load template from `prompts/{case.prompt}.tmpl`.
   - Render with `case.template_vars` using `text/template`.
   - Shell out via `provider.RunConfiguredOneShot` (same provider the broker
     uses, no mock).
   - Parse output per prompt contract (JSON for extract/query/lint; markdown
     body for synthesis).
   - Assert `expected.must_include`, `expected.must_not_include`,
     `expected.structured` partial-match.
3. Emit a table of pass/fail per case and a total.

Pass gate for Slice 1: 100% of cases pass. Any regression is a ship-blocker.

## Adding cases

Write a new `{suite}_{NNN}_{slug}.json` file, validate against `schema.json`,
commit. No test-file bookkeeping needed; the runner walks the directory.
