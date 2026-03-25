# WUPHF

WUPHF is a terminal-native multi-agent office.

It launches a team of Claude Code agents in one tmux window, gives them a shared Slack-like office channel, keeps threads readable, supports human interview pauses, and lets the team work together in public instead of hiding all coordination inside one assistant reply.

## What It Is

- One shared office channel: `#general`
- Visible agent panes in the same tmux window
- Threaded team discussion
- Human interview flow when the team is blocked
- Optional Nex integration for context graph memory, proactive notifications, and integrations

## Nex Is Optional

WUPHF works without Nex.

If you start it with `--no-nex`, WUPHF disables Nex-backed tools entirely:

- no context graph reads/writes
- no Nex integrations
- no Nex notification feed
- no setup requirement for a WUPHF/Nex API key

That mode is useful if you want the office and multi-agent collaboration model without coupling the product to Nex.

```bash
./wuphf --no-nex
```

With Nex enabled, the experience gets better:

- durable context across sessions
- proactive notifications from the user’s context graph
- integrations like email, calendar, CRM, and Slack

But it is not required.

## Latest CLI

This repo no longer vendors the old standalone WUPHF/Nex CLI command surface.

If you want the latest published CLI separately, install it with:

```bash
bash scripts/install-latest-wuphf-cli.sh
```

## Build

```bash
go build -o wuphf ./cmd/nex
```

Build the MCP server too if you want the bundled tool runtime:

```bash
cd mcp
bun install
bun run build
```

## Run

Normal mode:

```bash
./wuphf
```

Office-only mode with Nex fully disabled:

```bash
./wuphf --no-nex
```

Single-agent Bubble Tea mode:

```bash
./wuphf --solo
```

Kill a running team:

```bash
./wuphf kill
```

## Manual Testing

### 1. Build

```bash
go build -o wuphf ./cmd/nex
cd mcp && bun install && bun run build && cd ..
```

### 2. Launch the office

```bash
./wuphf
```

Expected:

- tmux opens one window
- left/main office channel is visible
- agent panes are visible in the same window
- header shows `The WUPHF Office`
- channel shows `# general`

### 3. Test office-only mode

```bash
./wuphf --no-nex
```

Expected:

- office launches normally
- no setup auto-start
- notice says Nex tools are disabled
- `/integrate` and other Nex-backed flows refuse cleanly

### 4. Test the REPL

Inside the office channel:

- type `/` and verify slash autocomplete opens
- type `/qui` and press `Enter`; it should submit `/quit`
- type `@` and verify teammate autocomplete opens
- use `Tab` to autocomplete

### 5. Test threads

- send a top-level message
- use `/reply <message-id>` to reply in thread
- verify main-channel thread summary stays collapsed by default
- expand the thread and verify nested replies render in the thread drawer

### 6. Test human interview

Trigger or mock a `human_interview` call and verify:

- the interview card blocks the team
- `Esc` snoozes the card locally
- the team remains paused until answered
- the final option reads `Something else`
- typing a custom answer works

### 7. Test reset

Run:

```text
/reset
```

Expected:

- channel pane stays alive
- office messages clear
- pending interview clears
- agent panes restart
- persisted office state is wiped

### 8. Test termwright smoke

```bash
bash tests/uat/office-channel-e2e.sh
```

That smoke test verifies the office channel renders, slash autocomplete appears, and typed input lands in the composer.

## Notes

- The main binary is still built from `./cmd/nex`, but the shipped command and user-facing product name are `wuphf`.
- Nex-specific strings are kept only where they refer to the optional Nex tool or backend.
