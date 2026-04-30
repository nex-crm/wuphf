# Code Quality Patterns

Concrete patterns for keeping the codebase navigable. The reference points are PRs that already shipped — `git show <commit>` to read the diff.

## Two decomposition patterns

### Horizontal slice — extract a pure layer

**Use when:** logic is pure (no IO, no shared state), can be expressed as `f(input) → output`, and is consumed by a single binary or a tightly-bounded set of callers.

**Reference:** PR #501, commit `546a462c`. Extracted ~3,000 LOC of pure rendering / projection logic out of `cmd/wuphf/channel*.go` into a new `cmd/wuphf/channelui/` package. 56 files, none over 600 LOC. The package depends on no network, no clock, no shared state.

**Naming convention inside the new package:**

- `<domain>.go` for monolithic logical units (`calendar.go`, `recovery.go`)
- `<domain>_<phase>.go` for multi-phase workflows (`artifact_builders.go` + `artifact_renderers.go`)
- `<entity>_<aspect>.go` for narrow helpers (`messages_render.go`)

**Anti-patterns:**

- Adding `// Deprecated:` aliases to bridge callers if the new package is in the same module. The aliases are scaffolding for binary boundaries — see PR #501 only used them because `cmd/wuphf/` wraps the binary and they wanted incremental migration. **Inside `internal/team`, do not add aliases.** Move callers in the same PR.
- Introducing fake purity by accepting 14 callbacks as props. If you can't extract without dragging behavior in, the layer is not pure — use a vertical slice instead.

### Vertical slice — themed sibling files

**Use when:** a god struct has many methods that operate on shared state but represent separable concerns (lifecycle phases, subsystems, transport vs persistence).

**Reference:** PR #503, commit `b7ec049c`. Decomposed `internal/team/launcher.go` (4,998 LOC) into 30+ themed siblings (`launcher_boot.go`, `launcher_session.go`, `launcher_web.go`, `launcher_loops.go`, `launcher_membership.go`, ...). The struct itself stays in `launcher.go`; methods move out by concern. Final `launcher.go` is 263 LOC.

**Naming convention:**

- `<entity>_<phase>.go` for lifecycle-stage groupings (`launcher_boot.go`)
- `<entity>_<subsystem>.go` for cross-cutting subsystems (`headless_codex_queue.go`)
- Free-standing files for cross-cutting concerns (`prompts.go`, `escalation.go`)

**Anti-patterns:**

- Splitting a file just because it's "kinda big". The pattern earns its keep above ~1500 LOC. Below that, splitting fragments grep-ability.
- Creating one sibling per method. Group methods by concern; let cohesive concerns share a file even if the file ends up at 600 LOC.

## Decision tree

```
Is the code pure (no IO, no shared mutable state)?
├── Yes → Horizontal slice into a new package.
│         Reference: PR #501.
│
└── No → Is the file > 1500 LOC?
        ├── Yes → Vertical slice into themed sibling files.
        │         Reference: PR #503.
        │
        └── No → Leave it alone.
                 If the file feels confusing, the answer is renaming
                 functions or adding doc comments, not splitting files.
```

## Epicenter testing

A test is **epicentric** when its failure points at exactly one production file or one concern. If a test failure could indicate any of three different bugs, the test is testing too much.

**Symptoms of non-epicentric tests:**

- The test name is `TestEverythingWorksTogether`.
- The test sets up 200 lines of fixtures.
- Reading the failure tells you "something in module X is broken" but not what.
- The test exercises three subsystems that have their own dedicated tests already.

**Refactor pattern:**

1. Identify what specifically the test is checking. If it's checking five things, write five tests.
2. Replace cross-subsystem coupling with a fake (e.g., `fakeTmuxRunner` instead of real tmux).
3. Co-locate the test with the file it tests, named `<file>_test.go`.

## Test fixtures (DRY)

Shared test scaffolding lives in `internal/team/testfixtures/` (when that package lands in Phase 3). Today the patterns are scattered — when extracting, consolidate:

- `manualClock` — release pending sleepers on demand. See `internal/team/scheduler.go` for the production-side `clock` interface.
- `fakeTmuxRunner` — record-and-assert pattern. See `internal/team/tmux_runner.go`.
- `fakeProcessRunner` — same shape, for `exec.Command`-style work.
- `httptest.Server` — for HTTP callers. Never run a real server in tests.

If you need a third copy of a fixture, you need a shared package.

## Bug-hunt expectations during refactor

PR #501 and PR #503 each found multiple criticals while decomposing. The pattern repeats:

| Smell | Surfaces during |
|---|---|
| Goroutine leak | Vertical slice of a long-lived service |
| Silently swallowed error (`_ = enc.Encode(...)`) | Extracting an HTTP handler |
| Mutable-state aliasing in render functions | Horizontal slice of a render layer |
| Untyped `context.WithValue(ctx, "user", ...)` keys | Auth handler extraction |
| Race condition under shutdown | Anything pulling code out of a `Stop()` path |
| Dead branches that coverage marked as 0% | Anything |

Document findings in the PR description under **Bug-hunt**. If a refactor PR has no bug-hunt section, it's likely under-read.

## What good comments look like

Default to no comments. Add one only when the *why* is non-obvious:

- A hidden invariant (`// must hold lock here — see commit X`)
- A workaround for a specific bug (`// guard against #350 regression`)
- A behavior that would surprise a reader (`// NB: returns nil, not error, on EOF`)
- Cross-platform gotchas (`// Windows: bash doesn't exist; gate on runtime.GOOS`)

What good comments do **not** do:

- Explain what the code already says (`// increment counter`)
- Reference the current task or PR (`// added for issue #123`) — that goes in the commit message
- Reference callers (`// used by FooBar`) — let `git grep` answer that
- Promise future cleanup (`// TODO: clean this up later`) — open a ticket or delete the comment

## File-size gate

`scripts/check-file-size.sh` enforces the cap. Today it warns at 800 LOC and fails at 1500 LOC. Allowlist file `scripts/file-size-allowlist.txt` lists exemptions; entries are forward-only (can shrink, can't grow).

When a file goes over budget:

1. **Don't disable the check.**
2. **Don't add it to the allowlist** (forward-only — adding is forbidden).
3. **Decompose it** using the patterns above.

## Forbidden imports (Phase 9 ratchet)

Will land via `golangci-lint`'s `depguard`:

- `cmd/wuphf` must not import `internal/team` internals — only its public types
- `cmd/wuphf/channelui` must not import network code (no `net/http`, no clients)
- `web/src/components/apps/*` must not import each other — share via `web/src/ui/` or a hook

If a depguard rule fires, fix the import, don't suppress.

## When in doubt

- Read PR #501 and PR #503. They are this repo's canonical worked examples.
- Read the [`CONTRIBUTING.md`](../CONTRIBUTING.md) bar.
- Ask in the PR before merging.
