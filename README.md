# WUPHF

WUPHF is a terminal-native multi-agent office.

It launches a team of Claude Code agents in one tmux window, gives them a shared Slack-like `#general` channel, keeps collaboration visible, supports threads and human interview pauses, and lets you work with a team instead of a single hidden assistant loop.

The name is a nod to WUPHF from *The Office*:

- https://theoffice.fandom.com/wiki/WUPHF.com_(Website)

## What You Get

- One shared office channel: `#general`
- Multiple live agent panes in the same tmux window
- Threaded discussion instead of one giant reply dump
- Human interview flow when the team is blocked
- Optional Nex integration for memory, notifications, and external integrations

## Requirements

You need these installed locally:

- `tmux`
- `claude`
- Go toolchain

If you want Nex-backed features, you also need:

- `nex`
- `nex-mcp`

## Nex Is Optional

WUPHF is not just a frontend for Nex.

If you start it with `--no-nex`, WUPHF disables Nex completely for that run:

- no context graph reads or writes
- no Nex integrations
- no proactive Nex notifications
- no setup requirement for a Nex API key

```bash
./wuphf --no-nex
```

With Nex enabled, the office gets better context and better continuity:

- durable memory across sessions
- proactive signals from the user’s context graph
- integrations like email, calendar, CRM, and Slack

But the office itself still works without it.

## Build

```bash
go build -o wuphf ./cmd/wuphf
```

## Run

Start the office:

```bash
./wuphf
```

Start it with Nex disabled:

```bash
./wuphf --no-nex
```

Stop a running team from outside:

```bash
./wuphf kill
```

## Setup

WUPHF setup installs the latest published CLI automatically.

Outside the UI:

```bash
./wuphf init
```

Inside the office:

```text
/init
```

If you want the published CLI separately, you can still install it directly:

```bash
bash scripts/install-latest-wuphf-cli.sh
```

## Manual Smoke Test

Build:

```bash
go build -o wuphf ./cmd/wuphf
```

Launch:

```bash
./wuphf
```

What you should see:

- one tmux window
- `The WUPHF Office` in the header
- `# general` as the shared channel
- visible agent panes in the same window
- a working composer in the channel pane

Quick interaction checks:

- type `/` and verify slash autocomplete opens
- type `/qui` and press `Enter`; it should submit `/quit`
- type `@` and verify teammate autocomplete opens
- use `/reply <message-id>` to reply in-thread
- use `/reset` and confirm the office state clears without killing the channel pane

Termwright smoke:

```bash
bash tests/uat/office-channel-e2e.sh
```

Full office E2E:

```bash
bash tests/uat/notetaker-e2e.sh
```

## Architecture Notes

- The main binary is built from `./cmd/wuphf`.
- Local office/team MCP tools are Go-native and run from the same binary via an internal subcommand.
- WUPHF no longer needs Bun to run.
- Nex-specific behavior is kept only where it refers to the optional Nex toolchain or backend.
