# ADR-0001: God-file decomposition strategy

- **Status:** Accepted
- **Date:** 2026-04-30
- **Deciders:** Engineering
- **Reference PRs:** #431 (broker initial split), #501 (channelui extraction), #503 (launcher decomposition)

## Context

By early 2026 the repo had several god files: `internal/team/broker.go` (~12K LOC at peak), `cmd/wuphf/channel.go` (~5K LOC), `internal/team/launcher.go` (~5K LOC). Each mixed multiple unrelated responsibilities, blocked unit testing of subcomponents, and made code review painful. PRs #431, #501, and #503 broke up two of the three; this ADR codifies the patterns so the rest of the repo can follow and so the structure doesn't regress.

## Decision

Adopt two decomposition patterns and one set of guardrails.

### Pattern A — Horizontal slice (extract a pure layer)

Pull a pure-function layer (rendering, projection, validation) out of a stateful god file into its own package. The new package does no IO and holds no state. **When to use:** the layer is genuinely pure. **Reference:** PR #501, `cmd/wuphf/channelui/`.

### Pattern B — Vertical slice (themed sibling files)

Split a god struct's methods into themed sibling files in the same package; the struct stays in the original file. **When to use:** the god file mixes concerns but the methods share state. **Reference:** PR #503, `internal/team/launcher_*.go`.

### Guardrails (CI gates)

- **File size.** Warn at 800 LOC, fail at 1500 LOC. Forward-only allowlist for current offenders. Calibrated against the median size of #503's siblings + 50%.
- **No `time.Sleep` in tests.** Use a manual clock. Raised to a CI gate in Phase 9.
- **Per-package coverage floor**, ratcheted upward. New floors are forward-only.
- **Cognitive complexity** (`gocognit`) warning at 30; promoted to error in Phase 9.
- **Forbidden imports** via `depguard`: enforced package layering. Lands in Phase 9.

## Alternatives considered

- **Lifting subcomponents into separate Go modules.** Rejected: premature; adds versioning burden across a single-team monorepo. We can do this later if a component genuinely earns library status.

- **Banning files over 800 LOC outright.** Rejected: the calibration would force fragmentation of files that are coherent at 1000 LOC. Warn-and-allowlist gives signal without false alarms.

- **Auto-formatter that splits files.** Rejected: structure is a design decision, not a syntax concern. Tooling can flag, humans must split.

- **Strict 1:1 method-per-file.** Rejected: cohesive concerns should share a file. Over-fragmentation harms grep-ability and increases context-switching cost during review.

## Consequences

**Positive:**

- New code has a documented pattern to follow; reviewers have a documented bar to enforce.
- God files have a forced ceiling — they can't grow indefinitely.
- Test coverage is harder to skip on new code (per-package coverage floor ratchets upward).

**Negative:**

- Reviewers may over-apply the patterns to files that are coherent at 800-1500 LOC. Mitigation: the decision tree in `docs/CODE-QUALITY.md` says "leave it alone" below 1500 LOC unless there's a concrete confusion to resolve.
- The forward-only allowlist requires discipline to maintain. Mitigation: every refactor PR must include the allowlist diff in its description; PRs without one get bounced.

**Neutral:**

- Some legacy code remains over budget for a while. The allowlist tracks this debt explicitly.

## Notes

- This ADR is paired with [`CONTRIBUTING.md`](../../CONTRIBUTING.md) and [`docs/CODE-QUALITY.md`](../CODE-QUALITY.md). Those documents are the canonical *how*; this ADR is the *why*.
- Future ADRs can supersede this one. To do so, set this ADR's status to `Superseded by ADR-NNNN` and explain in the new ADR.
