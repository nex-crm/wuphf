# @wuphf/credentials — Agent Guidelines

This package owns per-agent OS keychain access for WUPHF v1. It imports branded
types from `@wuphf/protocol` and is allowed to touch the operating system; the
protocol package is not.

## Hard rules

1. **No plaintext fallback.** If the OS keychain is unavailable, throw
   `NoKeyringAvailable`. Never write credentials to files, environment
   variables, logs, or process-global memory.
2. **Linux rejects plaintext keyrings.** If libsecret reports `basic_text`,
   plaintext, or unencrypted storage, throw `BasicTextRejected` before storing
   anything.
3. **Per-agent scoping is part of the account key.** The keychain lookup key
   must include both `agentId` and `scope`; a handle for one agent must not read
   another agent's credential for the same provider.
4. **Handles are opaque.** A `CredentialHandle` never contains the secret.
   `toJSON`, `toString`, and `util.inspect` output must remain redacted or
   handle-id-only.
5. **Adapters take an injectable spawner.** Tests stub the subprocess boundary;
   production uses `execFile`. Do not let tests touch the real OS keychain.
6. **No native dependencies in v1.** Use OS CLIs (`security`, `secret-tool`,
   PowerShell/Credential Manager APIs). If a platform cannot meet the contract
   that way, ship a throwing adapter instead of a fake success path.

## Validation

```bash
bunx tsc --noEmit
bunx vitest run
bunx biome check src/ tests/
```
