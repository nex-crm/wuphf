# Agent Instructions

## Base Agent Instructions

These instructions apply to AI coding agents working in Nex repositories. Keep
tool-specific root files (`CLAUDE.md`, `AGENTS.md`, etc.) pointed at the same
canonical repo file so Claude, Codex, and other agents receive equivalent
guidance.

No additional setup is required beyond a normal clone of the repository. The
`.github` repository provides rollout tooling and templates only; committed
repo-local instruction files are the runtime source of truth for contributors.

### Working Rules

- Read the relevant code before editing. Do not reason from assumptions when
  the repository can answer the question.
- Prefer narrow, surgical changes that follow existing repo patterns.
- Do not revert changes you did not make unless the user explicitly asks.
- Own bugs surfaced during the work. Do not dismiss them as unrelated when they
  block the requested outcome.
- Ask before destructive or hard-to-reverse actions: deleting state, clearing
  Docker volumes, applying migrations outside local dev, or changing production
  infrastructure.

### Git And PRs

- Never push directly to `main`.
- Use a branch and open a draft PR for code changes.
- Use Conventional Commits for commit messages.
- Run the repo's documented checks before opening or marking a PR ready.
- Do not use `--no-verify` to bypass hooks.

### Quality Bar

- Do not suppress lint or type errors with ignore comments. Fix the code.
- Do not introduce explicit `any` in TypeScript. Use specific types, `unknown`
  with narrowing, or preserve existing untyped boundaries.
- Do not commit secrets, tokens, credentials, or inline API keys.
- Source required secrets from environment files, secret managers, or the
  repo-documented local setup.
- Treat E2E failures as product signals. Do not hand-wave them away.

### Nex Context

Nex is a context graph platform for AI agents, not a CRM. Do not describe the
product as a CRM in code, comments, docs, or external copy.

Use the available Nex memory/context tools when they are installed:

- Query context for people, companies, projects, or prior decisions when that
  context would materially improve the answer.
- Store durable user preferences, project decisions, and lessons learned when
  the user asks to remember them or when future sessions clearly need them.
- Scan repo docs and instruction files after meaningful updates so project
  context stays discoverable.

Tool names differ by platform. Use the equivalent available surface, for
example `query_context` / `nex_ask`, `add_context` / `nex_remember`,
`scan_files`, or `ingest_context_files`.

### Triangulation through orthogonal sub-agents

Use this pattern for high-stakes design decisions, including security
boundaries, wire shapes, schema changes, and new public API surfaces.

1. **Don't trust a single agent's review.** Even with a thorough prompt, one
   agent has one frame.
2. **Spawn 3-5 sub-agents in parallel**, each with a different lens preamble:
   security, perf, API, SRE, architecture, types, or distributed systems. Use
   `bash scripts/dispatch-triangulation.sh`.
3. **Aggregate their outputs.** Findings that 2+ agents flag independently are
   high-confidence. Singletons are lower confidence; verify before fixing.
4. **Direct disagreements** are signals to escalate to human review, not to
   pick a side.
5. **Use this pattern especially when:** introducing a new wire shape; changing
   a security-relevant invariant; designing a new public API; choosing between
   two architectural approaches.

### Verification agents as sounding boards

Use this pattern when Claude, Codex, or a human has a proposed solution and
wants to stress-test it before committing.

1. **Run a verification agent** with
   `bash scripts/dispatch-verification-agent.sh`. Pass the solution, target
   files, and an optional adversarial lens.
2. **The verification agent runs in read-only mode.** It cannot edit; it can
   only find what the solution does not cover.
3. **Treat its findings as a pre-commit review.** Fix what's real, skip with
   reason what's not, and defer what's out of scope.
4. **Use this pattern especially when:** the change is irreversible, such as
   deleting state or dropping schema; the change is in a security boundary,
   such as validators, sanitizers, or freeze boundaries; the change is in code
   with no consumers yet, where downstream would not catch a regression.

### When to use which

| Situation | Pattern |
|---|---|
| Initial design of a new surface | Triangulation (orthogonal lenses) |
| Stress-testing a proposed fix | Verification agent (one adversarial lens) |
| Post-implementation review | Both — triangulation first, then verification on the synthesis |
| Routine bug fix | Neither (overkill) |
| Pre-merge gate | Verification agent + the existing demo + package-specific cross-language oracle (for example, `testdata/verifier-reference.go` in protocol-grade packages) |

## Wuphf Agent Instructions

Use this profile for the Wuphf public repo and its worktrees.

### Commands

```bash
go build -o wuphf ./cmd/wuphf
bash scripts/test-go.sh
bash scripts/test-go.sh ./internal/team
bash scripts/test-web.sh
bash scripts/test-web.sh web/src/path/to/file.test.ts
```

Web UI commands run from `web/`:

```bash
bun install
bun run dev
bun run build
bunx tsc --noEmit
```

Always use `bun` / `bunx` for JavaScript tooling in this repo. Web unit and
component tests run through Vitest. Use `bash scripts/test-web.sh` for the full
Web suite and `bash scripts/test-web.sh web/src/path/to/file.test.ts` for
focused Web tests; do not use `bun test` inside `web/`, because that invokes
Bun's native test runner instead of the repo's Vitest setup.

### PR And Hooks

- Branch and PR for all code changes.
- Open PRs as draft.
- Run the full relevant test suite before marking ready.
- Run `./scripts/bootstrap.sh` after cloning to install dependencies and hooks.
- Never push with `--no-verify`.

### Screenshots

- For any PR that changes files under `web/`, capture screenshots and
  embed them in the PR description. Use the harness at
  `web/e2e/screenshots/`:

  ```bash
  # 1. write web/e2e/screenshots/<feature>.mjs (copy version-chip.mjs)
  # 2. bash web/e2e/screenshots/publish.sh <feature> <pr-number>
  ```

  See `web/e2e/screenshots/README.md` for the spec API. The wrapper
  pushes images to an orphan `screenshots/pr-<n>` branch and appends
  raw URLs to the PR body. Use `--comment` to post as a comment
  instead, or `--dry-run` to preview the markdown locally.

- Skip only when the change is a refactor with no visible UI delta,
  the diff is purely test/doc/build config, or the same feature is
  already covered by a linked sibling PR's screenshots.

### Multi-round review rhythm

Use this heavier rhythm for substantial changes such as new packages,
security-boundary work, protocol or storage changes, and wire-shape additions.
Routine PRs can use a lighter version, but should still keep the disposition
discipline.

1. First pass: implement the change with local tests.
2. Second pass: run multi-agent review with explicit lenses: performance, SRE,
   crypto/security, types, distributed-systems behavior, API contract, and
   architecture. Use the `Agent` tool with general-purpose subagents or
   `codex exec` with parallel agents in worktrees. For long-lived package work,
   include sustainability/maintainability as an explicit lens.
3. Third pass: address CodeRabbit findings. CodeRabbit re-reviews on every
   push window, so check
   `gh api repos/<owner>/<repo>/pulls/<N>/comments --paginate` after each push.
4. Fourth pass: run a staff-engineer review via the `Agent` tool with the
   `staff-code-reviewer` subagent.
5. Per-pass discipline: every PR comment gets exactly one disposition:
   `FIXED` with commit ref, `SKIPPED` with a concrete reason such as already
   addressed in commit X / known-limitation / out of scope, or `DEFERRED` to a
   follow-up issue with a link.

This rhythm is appropriate for protocol-grade work; do not impose it
uncritically on small bug fixes or documentation-only changes.

For PR-shaped reviews specifically (major dependency bumps, IPC surface
changes, anything under `apps/desktop/AGENTS.md`'s hard rules), see
[docs/agents/orthogonal-pr-review.md](/docs/agents/orthogonal-pr-review.md).
It spells out the lens menu, codex-vs-Claude routing, and the round-2
adversarial rhythm.

### Sub-agent dispatch contract

When a human or AI delegates work to a sub-agent through `codex exec`, Claude's
`Agent` tool, or another runner, the dispatch prompt MUST include:

1. A pointer to the package's `AGENTS.md`: "Read packages/X/AGENTS.md first; it
   captures conventions you must follow."
2. The relevant hard rules pasted verbatim, not just referenced. Sub-agents do
   not always read linked docs.
3. Explicit decision options when there is design ambiguity: "Pick (a) unless
   (b) is necessary because Y. Document your choice in the commit body."
4. Verification commands the agent must run before commit, using the exact shell
   invocation.
5. A per-finding disposition format: every finding addressed must end with
   `FIXED`, `SKIPPED` plus reason, or `DEFERRED` plus issue ref.
6. Failure-mode guidance: "If you can't safely fix X, leave a TODO with
   rationale rather than commit a half-fix."
7. A scope boundary listing files the agent SHOULD touch and files it SHOULD NOT
   touch, especially when multiple agents run in parallel.

Copy-paste this dispatch template and fill in the bracketed sections before
sending it to a sub-agent:

```text
You are working in a git worktree on branch [branch-name].

Read [packages/X/AGENTS.md] first; it captures package conventions you must
follow. Also read the root AGENTS.md for repo-wide rules.

Hard rules for this dispatch, pasted verbatim:
[Paste the relevant hard rules from the package AGENTS.md and root AGENTS.md
here. Do not replace this with "see AGENTS.md".]

Task:
[Describe the exact findings or changes assigned to this agent.]

Scope boundary:
- SHOULD touch: [files/directories]
- SHOULD NOT touch: [files/directories owned by other batches]

Design ambiguity:
- Prefer [option A] unless [option B] is necessary because [reason].
- If you choose [option B], document why in the commit body.

Failure mode:
If you cannot safely fix [risk area], leave a TODO with rationale and report the
finding as SKIPPED or DEFERRED rather than committing a half-fix.

Verification before commit:
- [exact command 1]
- [exact command 2]
- [exact command 3]

Commit:
- Use a Conventional Commit message.
- Explain the why in the body when the choice is non-obvious.

End your summary with this disposition table:
| # | Finding | Status | Notes |
|---|---------|--------|-------|
| 1 | <short> | FIXED | commit <sha> |
| 2 | <short> | SKIPPED | <reason> |
| 3 | <short> | DEFERRED | <issue link> |
```

### Worktree-based parallelism

For multi-batch fixes:

1. Identify the file-overlap matrix before dispatching. Record which batches
   touch which files.
2. Create one worktree per batch:
   `git worktree add /path/to/worktree -b batch-name <base-ref>`.
3. Each Codex agent commits to its own branch in its own worktree.
4. Cherry-pick batches in dependency order: least overlap first, most overlap
   last.
5. Resolve conflicts at integration. Do not ask agents to redo work solely
   because integration conflicts surfaced.
6. Clean up integrated worktrees and branches after integration:
   `git worktree remove /path/to/worktree && git branch -D batch-name`.

### Demo + cross-language oracle for protocol-grade packages

Every package that defines a wire shape ships:

- A `scripts/demo.ts` or equivalent that exercises the public API end-to-end
  with adversarial inputs.
- A cross-language reference verifier such as
  `testdata/verifier-reference.go` for any wire-contract bytes.
- CI wiring and lefthook pre-push wiring for both artifacts, scoped with glob
  filters so unrelated changes do not pay the full cost.
- README updates in the same commit as any wire-shape change, so code and
  documented shape cannot drift.

This follows `feedback_atomic_demo_slices.md`: every PR ships a demo plus an
iteration hook; reviewer practice is to run the demo, not eyeball the diff.

### Lint And Security

- Go: `gofmt`, `go vet ./...`, and `golangci-lint run ./...`.
- Web: `bunx biome check --write`.
- Secrets: `bunx secretlint`.
- Do not suppress lint warnings with ignore comments.
