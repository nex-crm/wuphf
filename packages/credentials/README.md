# @wuphf/credentials

Per-agent OS keychain credentials for WUPHF v1.

The protocol package owns the branded `CredentialHandle`, `AgentId`, and
`CredentialScope` types. This package owns the side-effecting store that writes,
reads, and deletes secrets in the host OS keychain.

## API

```ts
import { asAgentId, asCredentialScope } from "@wuphf/protocol";
import { open } from "@wuphf/credentials";

const store = open();
const handle = await store.write({
  agentId: asAgentId("agent_alpha"),
  scope: asCredentialScope("openai"),
  secret: process.env["OPENAI_API_KEY"] ?? "",
});

JSON.stringify(handle); // {"id":"cred_..."}
String(handle); // CredentialHandle(<redacted>)

const secret = await store.read(handle);
await store.delete(handle);
```

`open()` detects `process.platform` and returns the platform adapter. Tests pass
an injectable `spawner` so they never touch the real keychain:

```ts
const store = open({ platform: "linux", spawner: fakeSpawner });
```

The broker integration belongs in broker startup, not a module-global singleton.
Branch 9 should construct one store per broker process and pass handles to agent
runners together with the agent identity that is allowed to use them.

## Adapters

| Platform | Backend | Notes |
|---|---|---|
| macOS | `security` CLI | Uses generic-password items keyed by service + `agentId` + scope. |
| Linux | `secret-tool` / libsecret | Requires an encrypted Secret Service backend. Rejects `basic_text`, plaintext, or unencrypted collection reports with `BasicTextRejected`. |
| Windows | PowerShell + Credential Manager APIs | Uses generic Credential Manager targets. AppContainer ACL hardening is deferred to the branch-8 follow-up issue. |

No adapter falls back to plaintext files. If the keychain command is missing,
locked in a way the CLI cannot use, or unavailable, the store throws
`NoKeyringAvailable`.

## Threat Model

The v0 Electron `safeStorage` bag was process-scoped, so every spawned agent in
the same process could reach the same decrypted material. On Linux it could also
degrade to plaintext storage when no encrypted keyring was available. This
package moves the boundary to per-agent OS keychain entries: the account key
includes `agentId` and `scope`, and `CredentialStore.read(handle)` recomputes
that key every time instead of caching the secret in memory.

`CredentialHandle` is intentionally not a secret container. Its JSON form
contains only the opaque handle id, and string/inspect output is redacted. A
caller that wants secret bytes must call `CredentialStore.read(handle)`, which
goes back to the OS adapter. That keeps accidental logs, serialized task state,
and subprocess arguments from carrying API keys.

The remaining Windows hardening gap is AppContainer ACL scoping for packaged
desktop utility processes. It is out of scope for this substrate branch because
the runner identity and package SID are defined by the branch-9 desktop/broker
integration.
