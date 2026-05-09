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

### Lint And Security

- Go: `gofmt`, `go vet ./...`, and `golangci-lint run ./...`.
- Web: `bunx biome check --write`.
- Secrets: `bunx secretlint`.
- Do not suppress lint warnings with ignore comments.
