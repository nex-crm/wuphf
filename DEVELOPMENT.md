# Development

## Office Build

```bash
go build -o wuphf ./cmd/wuphf
```

For normal app usage you do not need Bun. The local office/team MCP tools now run from the main Go binary through the hidden `wuphf mcp-team` subcommand.

## First-time setup

Run the bootstrap script once after cloning:

```bash
./scripts/bootstrap.sh
```

It installs Bun deps at the repo root (secretlint, commitlint) and in `web/` (frontend deps), registers the git hooks via `lefthook install`, and prints install hints for `vhs` and `golangci-lint` if either is missing. Re-run it any time you switch branches that changed `package.json` / `web/package.json`.

## Git hooks

Hooks run via [lefthook](https://github.com/evilmartians/lefthook) (`lefthook.yml`). Prerequisites: `./scripts/bootstrap.sh` has been run so `bun`, `lefthook`, `golangci-lint`, and optionally `vhs` are on PATH.

**commit-msg**

| Hook | What it does |
|------|--------------|
| `commitlint` | Enforces Conventional Commits via `@commitlint/config-conventional` |

**pre-commit** (parallel, only runs against staged files)

| Hook | What it does |
|------|--------------|
| `gofmt` | Rejects unformatted `.go` files (run `gofmt -w <file>` to fix) |
| `go-vet` | Runs `go vet ./...` |
| `golangci-lint` | Runs `golangci-lint run ./...` |
| `biome` | Formats + lint-fixes staged `web/**/*.{js,ts,jsx,tsx,json,css}`, re-stages fixes |
| `secretlint` | Scans staged files for leaked secrets (API keys, tokens, PEM blocks) |
| `no-secrets` | Greps the staged diff for assignments like `api_token`/`password`/`api_key`/`secret` set to a string literal (see `lefthook.yml` for the exact regex) |
| `check-merge-conflicts` | Fails if staged `.go/.yml/.yaml/.md/.toml/.json` files contain conflict markers |
| `no-large-files` | Fails if any staged file exceeds 5 MB |

**pre-push** (serial — wiki worker queue saturates under parallel load)

| Hook | What it does |
|------|--------------|
| `smoke` | `go build ./... && go vet ./...` — compile + vet sanity (~10s) |
| `build` | `go build -o /dev/null ./cmd/wuphf` — verify the main binary still links |
| `vhs` | Runs `testdata/vhs/check.sh` if `vhs` is on PATH (skipped with a warning otherwise) |

The full Go test suite runs in CI (`go-test-matrix` job) instead of pre-push — fan-out per package with `-race` on everything except `internal/team` and `internal/teammcp`. Those two packages have known goroutine-leak patterns where a worker spawned by one test outlives that test and races against the next test's setup; the race detector is correct to flag them, but the result is non-deterministic local failures on Mac. The fix lives upstream in those packages' lifecycles (tracked at `internal/team/headless_codex.go` :: `enqueueHeadlessCodexTurnRecord`, where `runHeadlessCodexQueue` is spawned without a per-test cleanup channel). Until that lands, the carve-out keeps CI honest.

### Running tests locally

To match CI's gate locally — per-package fan-out with the same `-race` carve-out — use:

```bash
bash scripts/test-go.sh                  # whole repo (~3-5 min)
bash scripts/test-go.sh ./internal/team  # one package
COUNT=3 bash scripts/test-go.sh ./...    # flake-hunt
```

Plain `go test -race ./...` will reproduce the `internal/team` flakes documented above. If you need to verify a change touches the team package, the script is the sanctioned entry point — it's the same shape CI runs.

**Do NOT push with `--no-verify`.** If a hook fails, fix the underlying failure — skipping it lands the problem in CI for everyone else to hit. If a hook is genuinely wrong for your change, open a PR to the hook config rather than bypassing it.

## Latest Published CLI

The old standalone CLI is no longer vendored in this repo.

If you need the latest published CLI separately:

```bash
bash scripts/install-latest-wuphf-cli.sh
```

The same install step is also wired into setup:

```bash
./wuphf init
```

## Environments

The WUPHF runtime reads `WUPHF_BASE_URL` from the environment, falling back to `https://app.nex.ai` in production.

| Environment | `WUPHF_BASE_URL` |
|-------------|----------------|
| Production  | _(unset — default)_ |
| Staging     | `https://app.staging.wuphf.ai` |
| Local       | `http://localhost:30000` |

### Switching environments

```bash
# Staging
export WUPHF_BASE_URL="https://app.staging.wuphf.ai"

# Local
export WUPHF_BASE_URL="http://localhost:30000"

# Back to production
unset WUPHF_BASE_URL
```

or set it directly in `.zshrc` or `.bashrc`.
