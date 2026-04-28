# CLAUDE.md

## Build & Test

```bash
go build -o wuphf ./cmd/wuphf        # build the binary
bash scripts/test-go.sh               # full test suite (matches CI)
bash scripts/test-go.sh ./internal/team  # single package
```

Web UI (in `web/`):
```bash
bun install && bun run dev            # dev server
bun run build                         # production build
bunx tsc --noEmit                     # typecheck
bun test                              # vitest
```

Always use `bun` / `bunx` — never npm, npx, node, or pnpm.

## Commit Messages

Conventional Commits enforced by commitlint (`.commitlintrc.json` → `@commitlint/config-conventional`).

Allowed types: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`.

Scopes are free-form, e.g. `fix(team):`, `feat(website):`, `ci(release):`.

Body lines must not exceed 100 characters.

## Lint & Formatting

- Go: `gofmt`, `go vet ./...`, `golangci-lint run ./...`
- Web: `bunx biome check --write` (JS/TS/JSON/CSS in `web/`)
- Secrets: `bunx secretlint` — never commit API keys, tokens, or credentials
- Never suppress lint warnings with ignore comments — fix the code

## Git Hooks (lefthook)

Pre-commit, commit-msg, and pre-push hooks run via lefthook. Run `./scripts/bootstrap.sh` after cloning to install deps and register hooks. Never push with `--no-verify`.

## PR Workflow

- Always branch + PR — never push directly to `main`
- Open PRs in draft (`gh pr create --draft`)
- Run the full test suite before marking ready
