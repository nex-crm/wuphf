# Development

## Environments

Both wrapper scripts read `NEX_BASE_URL` from the environment, falling back to `https://app.nex.ai` in production.

| Environment | `NEX_BASE_URL` |
|-------------|----------------|
| Production  | _(unset — default)_ |
| Staging     | `https://app.staging.nex.ai` |
| Local       | `http://localhost:30000` |

### Switching environments

```bash
# Staging
export NEX_BASE_URL="https://app.staging.nex.ai"

# Local
export NEX_BASE_URL="http://localhost:30000"

# Back to production
unset NEX_BASE_URL
```

or set it directly to .zshrc or .bashrc

The registration script also supports `NEX_REGISTER_URL` for a full override if the registration endpoint differs from the base pattern.
