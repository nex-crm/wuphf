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
3. **Broker mediation is the isolation boundary.** Adapters MUST NOT be called
   directly by code outside `@wuphf/credentials` and `@wuphf/broker`. The public
   API requires a `BrokerIdentity` token that only `@wuphf/broker` can
   construct. Test code uses the `testing/forBrokerTests` factory.
4. **Handle id is the account key.** The keychain lookup key is the
   `CredentialHandle` id. Store `agentId` and `scope` as adapter metadata for
   broker audit/debug context; do not derive the account key from them.
5. **Handles are opaque.** A `CredentialHandle` never contains the secret.
   `toJSON`, `toString`, and `util.inspect` output must remain redacted or
   handle-id-only.
6. **Adapters take an injectable spawner.** Tests stub the subprocess boundary;
   production uses `execFile`. Do not let tests touch the real OS keychain.
7. **No native dependencies in v1.** Use OS CLIs (`security`, `secret-tool`,
   PowerShell/Credential Manager APIs). If a platform cannot meet the contract
   that way, ship a throwing adapter instead of a fake success path.

## Validation

```bash
bunx tsc --noEmit
bunx vitest run
bunx biome check src/ tests/
```
