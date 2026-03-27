# Development

## Environments

Both wrapper scripts read `WUPHF_BASE_URL` from the environment, falling back to `https://app.nex.ai` in production.

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

or set it directly to .zshrc or .bashrc

The registration script also supports `WUPHF_REGISTER_URL` for a full override if the registration endpoint differs from the base pattern.
