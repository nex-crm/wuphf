# WUPHF

WUPHF is a weird little terminal office for a team of AI coworkers.

You run one command, it opens one tmux window, and suddenly you have a CEO, PM, frontend engineer, backend engineer, AI engineer, designer, CMO, and CRO all sitting in the same place arguing about what to build. That is the product.

See it at work in below video (click image to open YouTube):
<video width="630" height="300" src="https://github.com/user-attachments/assets/f4cdffbf-4388-49bc-891d-6bd050ff8247"></video>

The name is from *The Office*. If you know, you know. If you do not, this is the bit:

- https://theoffice.fandom.com/wiki/WUPHF.com_(Website)

The joke still fits. It is one thing hitting a bunch of people at once.

## What This Actually Does

- gives the team one shared channel: `#general`
- shows live agent panes in the same tmux window
- keeps discussions in threads so the main channel does not become soup
- lets the team pause and ask you a real blocking question
- keeps a real office task list and request queue so not everyone dogpiles the same thing
- separates company defaults from live office state, so the office has a stable roster/channels and a separate “what is happening right now” layer
- can optionally use Nex for memory, notifications, and integrations

This is not “one chatbot with a fancy prompt.” The point is to make the team visible.

## What You Need

Install these first:

- `tmux`
- `claude`
- Go

If you want Nex features too, also install:

- `nex`
- `nex-mcp`

## Nex Is Optional

Nex makes WUPHF better, but it is not mandatory.

If you do not want context graph stuff, integrations, or notifications, just run:

```bash
./wuphf --no-nex
```

That turns Nex off for that run.

What you lose:

- context graph reads and writes
- Nex-powered notifications
- Nex integrations
- any need to configure a Nex API key

What you keep:

- the office
- the channel
- the team
- the arguments

## Company State

WUPHF now has two different kinds of state on purpose:

- company manifest: who is on the team and which channels exist by default
- live office state: messages, tasks, requests, disabled members, costs, cursors, and in-flight work

The company manifest lives at:

```text
~/.wuphf/company.json
```

If that file does not exist, WUPHF falls back to the built-in founding team.

## Requests, Not Just Interviews

The office now has a real request system.

That means the team can open:

- approvals
- confirmations
- freeform questions
- private/secret answers
- classic blocking interviews

The old blocking interview behavior still works, but under the hood it is now just one kind of request.

So no, this repo is not trying to trap people into using Nex. If you want the multi-agent office without it, that is supported on purpose.

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

Kill a running session from outside tmux:

```bash
./wuphf shred
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

If for some reason you want the published CLI separately, there is still a script for that:

```bash
bash scripts/install-latest-wuphf-cli.sh
```

## Telegram

WUPHF can bridge to Telegram. A Telegram group or DM becomes a shared office channel — messages flow both ways, and all assigned agents participate.

### Connect

Run `/connect` in the TUI. Pick Telegram, paste your bot token from [@BotFather](https://t.me/BotFather), and select a group or DM mode. That's it.

If the bot is already in a group and someone has sent a message, the group appears automatically in the picker. If not, add the bot to a group first, send a message, then try `/connect` again.

### What flows to Telegram

- Agent responses show with the agent name: **@CEO**: message
- Human interviews show as decision prompts with options
- Skill invocations and system messages are clearly labeled
- Typing indicators appear when agents are working

### What flows from Telegram

- Group messages route to the mapped office channel
- DM messages to the bot route to the DM channel
- Telegram usernames are mapped to office members when possible

### Setup details

- Bot token is saved to `~/.wuphf/config.json` after first entry
- Channel surface metadata is stored in the company manifest
- The transport starts automatically on launch if a telegram channel exists
- Privacy mode must be disabled on the bot (via @BotFather → `/setprivacy` → Disable) for group messages to work

## Integrations And Actions

If you want agents to do real things across external systems, not just talk about them, you also need a Composio project key and at least one connected account.

That is what powers:

- external actions like sending an email
- reusable workflows
- trigger-based automations

The minimum setup is:

1. create a Composio project
2. generate a project API key
3. connect the accounts you want agents to use, such as Gmail
4. tell WUPHF about the key

Inside WUPHF:

```text
/config set composio_api_key <your-composio-project-key>
/config set action_provider composio
```

You also need the connected account on the Composio side. For example, if you want Gmail actions to work, connect Gmail in Composio first. WUPHF can search and execute actions only after that account exists.

If you run:

```bash
./wuphf --no-nex
```

then Nex-backed integrations are disabled for that run, and the action plane should be treated as off too.

## AI-Assisted Setup Prompt

If you are not technical, paste this into your AI tool of choice and follow it step by step:

```text
Help me set up WUPHF so the agents can actually take actions in external apps.

I am not technical. Walk me through this slowly, one step at a time, and wait for me after each step.

Goal:
- WUPHF runs locally
- Nex is configured
- Composio is configured for the action plane
- Gmail is connected
- WUPHF can send a test email through an agent

Please guide me through this exact flow:

1. Confirm tmux, claude, and Go are installed.
2. Help me build WUPHF from source.
3. Help me run WUPHF and complete /init.
4. Help me create or open a Composio project.
5. Help me generate a Composio project API key.
6. Help me connect Gmail inside Composio.
7. Help me enter the Composio key into WUPHF using:
   /config set composio_api_key <key>
   /config set action_provider composio
8. Help me start a 1:1 CEO session.
9. Help me ask the CEO to send a test email through the Composio action plane.
10. If anything fails, diagnose whether the problem is:
   - missing WUPHF setup
   - missing Nex setup
   - missing Composio key
   - missing connected account
   - wrong provider selected

Be explicit, do not skip steps, and give me the exact command or exact text to paste each time.
```

## What You Should See

When it works, you should get:

- one tmux window
- `The WUPHF Office` at the top
- `# general` as the shared channel
- the team visible in panes
- a working composer in the channel pane

If it feels like a hidden agent loop, something is wrong.

## Quick Manual Test

Build:

```bash
go build -o wuphf ./cmd/wuphf
```

Launch:

```bash
./wuphf
```

Then check a few basics:

- type `/` and make sure slash autocomplete opens
- type `/qui` and hit `Enter`; it should submit `/quit`
- type `@` and make sure teammate autocomplete opens
- use `/reply <message-id>` to reply inside a thread
- use `/requests` and make sure open requests are visible
- use `/reset` and make sure the office clears without killing the channel pane

If Nex is enabled, leave the office running long enough for the CEO insight sweep and make sure:

- Nex summaries land in `#general`
- the CEO assigns tasks instead of just dumping a wall of text
- if something really needs a human decision, it opens a request instead of guessing

## Automated Tests

Channel smoke:

```bash
bash tests/uat/office-channel-e2e.sh
```

Full office flow:

```bash
bash tests/uat/notetaker-e2e.sh
```

## A Few Notes

- The binary lives in `./cmd/wuphf`.
- Local office/team tools are Go-native and run from the same binary through an internal subcommand.
- WUPHF does not need Bun anymore.
- Nex-specific code is kept only where it is actually about Nex.

In other words: this repo is the office, not a pile of leftover CLI baggage wearing a fake mustache.
