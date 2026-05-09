# PR Quality Checklist

Copy the track that matches the PR into the PR description and keep it current
as review findings come in.

## Protocol-grade PR

Use for new packages, wire shapes, security boundaries, protocol/storage
contracts, and other long-lived surfaces.

- [ ] Has CI wiring in this PR, not a follow-up
- [ ] Has `AGENTS.md` for the package
- [ ] Has demo script (`scripts/demo.ts` or equivalent)
- [ ] Has independent oracle for any wire-contract bytes (cross-language reference)
- [ ] README matches code (no shape drift)
- [ ] Bounded resource budgets defined for any growable input (`MAX_*_BYTES`, `MAX_*_COUNT`)
- [ ] Streaming/incremental APIs for any verifier/codec processing a sequence
- [ ] Strict-unknown rejection at every wire-shape object boundary
- [ ] Validators re-derive (no `instanceof` trust)
- [ ] Cross-field invariants enforced at every site they apply
- [ ] Sustainability/maintainability impact reviewed for ownership, CI cost, and long-term API surface
- [ ] Per-finding disposition table in PR description: every reviewer finding has FIXED/SKIPPED+reason/DEFERRED+issue
- [ ] Pre-push gates green: `bash scripts/test-protocol.sh`, demo, cross-language verifier
  - [ ] File-size budget: `scripts/check-file-size.sh`
- [ ] CI gates green: same as above plus typecheck + biome
  - [ ] File-size budget: `scripts/check-file-size.sh`
- [ ] No `any`, no biome ignores, no `// @ts-ignore`
- [ ] No `--no-verify` on push

Disposition table:

| # | Finding | Status | Notes |
|---|---------|--------|-------|
| 1 |  | FIXED / SKIPPED / DEFERRED |  |

## Routine PR

Use for focused bug fixes, refactors, and documentation-only changes.

- [ ] Tests cover the change (existing or new)
- [ ] Lint + typecheck clean locally
- [ ] No new lint suppressions
- [ ] PR description includes why, not just what
