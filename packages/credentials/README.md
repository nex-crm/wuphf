# @wuphf/credentials

Broker-gated OS keychain credential storage for WUPHF v1.

The protocol package owns `CredentialHandle`, `AgentId`, `CredentialScope`, and
`CredentialHandleId`. This package owns the side-effecting store that writes,
reads, and deletes secrets in the host OS keychain. Per-agent isolation is
enforced by the broker, not by the operating-system keychain.

## API

```ts
import {
  asAgentId,
  asCredentialScope,
  credentialHandleToJson,
} from "@wuphf/protocol";
import { open, type BrokerIdentity } from "@wuphf/credentials";

const broker: BrokerIdentity = brokerStartupIdentityForAgent; // constructed by @wuphf/broker
const store = open();
const agentId = asAgentId("agent_alpha");
const scope = asCredentialScope("openai");

const handle = await store.write({
  broker,
  agentId,
  scope,
  secret: process.env["OPENAI_API_KEY"] ?? "",
});

const { id: handleId } = credentialHandleToJson(handle);
JSON.stringify(handle); // {"version":1,"id":"cred_..."}
String(handle); // CredentialHandle(<redacted>)

const secret = await store.read({ broker, handleId, agentId });
await store.delete({ broker, handleId, agentId });
```

`open()` detects `process.platform` and returns the platform adapter. Tests pass
an injectable `spawner` so they never touch the real keychain:

```ts
const store = open({ platform: "linux", spawner: fakeSpawner });
```

Test code constructs broker identities through the testing subpath:

```ts
import { forBrokerTests } from "@wuphf/credentials/testing";

const broker = forBrokerTests({ agentId });
```

The broker integration belongs in broker startup, not a module-global singleton.
Branch 9 should construct one store per broker process, mint `BrokerIdentity`
tokens at runner spawn, and pass only the caller's `agentId` plus handle id into
store reads and deletes.

## Adapters

| Platform | Backend | Notes |
|---|---|---|
| macOS | `security` CLI | Uses generic-password items keyed by service + handle id. Stores `agentId`/scope in item comments and labels for audit/debug context. |
| Linux | `secret-tool` / libsecret | Requires an encrypted Secret Service backend. Rejects `basic_text`, plaintext, or unencrypted collection reports with `BasicTextRejected`. Stores handle id, `agentId`, and scope as libsecret attributes. |
| Windows | PowerShell + Credential Manager APIs | Uses generic Credential Manager targets keyed by service + handle id. Stores `agentId`/scope in the credential comment. AppContainer ACL hardening is deferred. |

No adapter falls back to plaintext files. If the keychain command is missing,
locked in a way the CLI cannot use, or unavailable, the store throws
`NoKeyringAvailable`.

## Threat Model

The OS keychain does not enforce per-process ACLs on same-user processes. This
package provides credential storage and redaction; the broker enforces per-agent
isolation. Any read/write goes through the broker, which gates by caller
identity. A compromised agent runner can request reads of credentials assigned
to *its own* agentId; it cannot request reads belonging to other agents because
the broker's authorization layer denies them.

Limitations on Linux: when libsecret reports a `basic_text` backend, `write()`
throws `BasicTextRejected` and the broker surfaces an actionable error. On
Windows v1, the AppContainer ACL hardening (#847) is deferred; v1 matches the v0
`safeStorage` baseline of per-user (not per-process) access.

`CredentialHandle` is intentionally not a secret container. Its JSON form is
`{ "version": 1, "id": "cred_..." }`; the handle id is the capability. The
keychain account key is the handle id, so wrong-id reads return `NotFound` and a
deleted handle cannot be reused. `agentId` and scope stay in private runtime
slots and broker-side metadata so logs, serialized task state, and subprocess
arguments do not carry API keys.

## Integration Shape

Agent runners must not import `@wuphf/credentials` or invoke adapters directly.
They call the broker over loopback IPC. The broker maps the bearer token to the
caller `agentId`, verifies the credential operation is allowed for that caller,
and then calls:

```ts
await store.read({ broker, handleId, agentId });
await store.write({ broker, agentId, scope, secret });
await store.delete({ broker, handleId, agentId });
```

`BrokerIdentity` is a sealed runtime token with a private broker-minted symbol.
This package only accepts instances of that token; the production factory is
owned by `@wuphf/broker`. Tests use `@wuphf/credentials/testing`.

`packages/protocol/src/ipc.ts` still contains `KeychainHandleId` for the legacy
desktop contextBridge keychain path. New broker-mediated credential IPC uses
`CredentialHandle` / `CredentialHandleId` and the `{ version: 1, id }` JSON
shape. Branch 9 should migrate broker calls to the credential envelopes and then
remove the legacy desktop keychain bridge in the desktop follow-up.
