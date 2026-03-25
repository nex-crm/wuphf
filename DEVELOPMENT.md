# Development

## Office Build

```bash
go build -o wuphf ./cmd/wuphf
```

For normal app usage you do not need Bun. The local office/team MCP tools now run from the main Go binary through the hidden `wuphf mcp-team` subcommand.

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
