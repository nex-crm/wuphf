# Add a Transport to WUPHF

This guide walks you through wiring a new external integration (Slack, Discord,
WhatsApp, Hermes, …) into WUPHF as a typed transport adapter. The whole process
takes about 30 minutes once you have read this page.

## What is a transport?

A transport bridges an external messaging service with the WUPHF office broker.
Three scopes exist:

| Scope | Example | One external chat/session maps to… |
|---|---|---|
| **Channel-bound** | Telegram | One office channel (`#standup`) |
| **Member-bound** | OpenClaw | One office member per bridged session |
| **Office-bound** | Human-share | The whole office (admitted human) |

Pick the scope that matches how your integration behaves:

- One external chat → one team channel? → **Channel-bound** (`Transport`)
- Each bridged session becomes an agent/human member? → **Member-bound** (`MemberBoundTransport`)
- External human joins the whole office? → **Office-bound** (`OfficeBoundTransport`)

## Package layout

```text
internal/team/transport/
  types.go          — Scope, Binding, Participant, Message, Outbound, Health
  transport.go      — Transport, MemberBoundTransport, OfficeBoundTransport, Host interfaces
  errors.go         — typed sentinel errors + wrapped error types
  webhook_fake_test.go — canonical fake adapters (start here when reading)
  host_misuse_test.go  — 9 tests: 6 misuse paths + compile-time assertions + constant uniqueness checks

internal/team/
  broker_transport.go    — transport.Host implementation (backed by Broker)
  launcher_transports.go — RegisterTransports() — the single adapter registration site
```

**Package boundary:** `internal/team/transport` imports only the standard
library and external SDKs. It does NOT import `internal/team`. Go's import rules
make this a compile-time invariant: if your adapter needs something from the
broker, add a method to `transport.Host` instead of reaching into `internal/team`.

## Step 1: Implement the interface

Create `internal/team/transport/slack.go` (or wherever makes sense):

```go
package transport

import (
    "context"
    "time"
)

type SlackTransport struct {
    Token       string
    ChannelID   string
    OfficeChan  string // office channel slug, e.g. "general"
    // ... Slack API client, health fields ...
}

func (s *SlackTransport) Name() string { return "slack" }

func (s *SlackTransport) Binding() Binding {
    return Binding{Scope: ScopeChannel, ChannelSlug: s.OfficeChan}
}

func (s *SlackTransport) Run(ctx context.Context, host Host) error {
    // Connect to Slack RTM or Events API.
    // For each inbound message:
    //   1. UpsertParticipant (ALWAYS before ReceiveMessage for new users)
    //   2. ReceiveMessage
    // Block until ctx.Done().
    return nil
}

func (s *SlackTransport) Send(ctx context.Context, msg Outbound) error {
    // Post msg.Text to the Slack channel.
    return nil
}

func (s *SlackTransport) Health() Health {
    // Return a cached Health snapshot. Do NOT make a network call here.
    return Health{State: HealthConnected, LastSuccessAt: time.Now()}
}
```

### Interface contract: method by method

**`Name() string`**
- Must be a stable, lowercase identifier (e.g. `"slack"`). Never change this between releases — it is the namespace key for all participant identities. Changing it orphans existing participants.

**`Binding() Binding`**
- Called once at registration. For channel-bound: set `ChannelSlug` to the office channel the external chat maps to. Verify that channel exists before registering.

**`Run(ctx context.Context, host Host) error`**
- Your main polling/event loop. Block until `ctx.Done()`. The Host supervises Run in a goroutine: non-nil return triggers reconnect with backoff; nil return means intentional shutdown (no reconnect).
- Call `host.UpsertParticipant` before the first `host.ReceiveMessage` for any new external identity. Skipping this returns `ErrParticipantUnknown`.

**`Send(ctx context.Context, msg Outbound) error`**
- Deliver one outbound message. Respect `ctx` for timeouts. The Host calls this from a per-transport worker goroutine so a slow Send does not block other adapters.

**`Health() Health`**
- Return a cached struct, not a live network probe. Called on every channel-header render.

## Step 2: Register the adapter

Open `internal/team/launcher_transports.go` and add your adapter to `RegisterTransports`:

```go
func RegisterTransports(host transport.Host) error {
    token := os.Getenv("SLACK_BOT_TOKEN")
    channelID := os.Getenv("SLACK_CHANNEL_ID")
    if token != "" && channelID != "" {
        slack := &transport.SlackTransport{
            Token:      token,
            ChannelID:  channelID,
            OfficeChan: "general",
        }
        // Phase 2b: host.Register(slack) — available after Phase 2b lands.
        _ = slack
    }
    return nil
}
```

This is the **only** registration site. Both `Launch()` (tmux-mode) and
`LaunchWeb()` (web-mode) call `RegisterTransports` after `broker.Start()`,
so your adapter automatically runs in both surfaces.

## Step 3: Wire `UpsertParticipant` before `ReceiveMessage`

This is the most common mistake. In your `Run` loop:

```go
// Wrong — ReceiveMessage before UpsertParticipant:
host.ReceiveMessage(ctx, transport.Message{ ... }) // returns ErrParticipantUnknown

// Correct:
host.UpsertParticipant(ctx, transport.Participant{
    AdapterName: s.Name(),
    Key:         strconv.FormatInt(slackUser.ID, 10), // stable, content-addressed
    DisplayName: slackUser.Name,
    Human:       true,
}, s.Binding())
host.ReceiveMessage(ctx, transport.Message{
    Participant: transport.Participant{AdapterName: s.Name(), Key: ...},
    Binding:     s.Binding(),
    Text:        event.Text,
})
```

Call `UpsertParticipant` on **every** inbound event, not just the first. It is
idempotent — the second call is a no-op if nothing changed.

## Host contract

`transport.Host` is the only surface an adapter uses to touch the broker:

| Method | When to call | Error | Remediation |
|---|---|---|---|
| `UpsertParticipant` | Before first `ReceiveMessage` for any new identity | `ErrRegistrationConflict` | Use stable content-addressed keys |
| `ReceiveMessage` | For each inbound message | `ErrParticipantUnknown` | Call UpsertParticipant first |
| `ReceiveMessage` | For each inbound message | `ErrBindingChannelMissing` | Verify channel slug at startup |
| `DetachParticipant` | When a session/invite expires | — | Idempotent; no-op if not known |
| `RevokeParticipant` | Office-bound only: invite revoked | — | Permanent; member record deleted |

## Error contract

```go
import "errors"
import "github.com/nex-crm/wuphf/internal/team/transport"

if errors.Is(err, transport.ErrParticipantUnknown) {
    // Call UpsertParticipant first, then retry.
}
if errors.Is(err, transport.ErrBindingChannelMissing) {
    // Channel was deleted. Shut down cleanly; log for the user.
}
if errors.Is(err, transport.ErrSendTimeout) {
    // Message dropped. Check upstream API latency.
}
if errors.Is(err, transport.ErrHealthDegraded) {
    // Broker is pausing inbound from this adapter. Run reconnect logic.
}
if errors.Is(err, transport.ErrRegistrationConflict) {
    // Unstable participant key. Use a content-addressed identifier.
}
```

## Common pitfalls

1. **`ReceiveMessage` before `UpsertParticipant`** → `ErrParticipantUnknown`. Always upsert first for new identities.
2. **Declaring a non-existent channel slug in `Binding`** → `ErrBindingChannelMissing` at first message. Verify the slug at startup.
3. **Unstable participant keys** (e.g. row auto-increment IDs that reset after restart) → `ErrRegistrationConflict`. Use the upstream service's stable identifier (Slack user ID, OpenClaw session key).
4. **Making a network call inside `Health()`** → channel-header renders hang. Cache the health state; update it in your polling loop.
5. **Importing `internal/team` from your adapter** → compile error (one-way boundary). Extend `transport.Host` instead.
6. **Registering in only one launcher** — not possible if you use `RegisterTransports`. Only one site; both surfaces covered automatically.

## Running the contract tests

```bash
bash scripts/test-go.sh ./internal/team/transport/...
```

All 9 tests in `host_misuse_test.go` should be green before opening a PR. The
compile-time assertions in `TestFakeAdaptersSatisfyInterfaces` will catch
missing interface methods immediately.

## Reference implementations

- **`webhook_fake_test.go`** — canonical minimal adapters for all three scopes.
- **`internal/team/telegram.go`** — the existing channel-bound implementation (pre-contract).
- **`internal/team/openclaw.go`** — the existing member-bound implementation (pre-contract).

The Telegram and OpenClaw implementations are being migrated onto the contract in
Phases 2b and 3b. Use the fake adapters as your template, not the pre-contract
implementations.
