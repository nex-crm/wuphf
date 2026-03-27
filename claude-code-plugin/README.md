# WUPHF Memory Plugin for Claude Code

Persistent context intelligence for Claude Code, powered by WUPHF. Automatically recalls relevant knowledge before each prompt and captures conversation facts after each response.

## Features

- **Auto-recall** — `UserPromptSubmit` hook queries WUPHF and injects relevant context
- **Auto-capture** — `Stop` hook captures assistant responses to build your knowledge base
- **Slash commands** — `/recall <query>` and `/remember <text>` for manual control
- **MCP tools** — Full WUPHF API access via the MCP server

## Prerequisites

- Node.js 18+
- A WUPHF API key (from your developer dashboard) — [API docs](https://docs.nex.ai)
- Claude Code CLI

## Installation

```bash
cd claude-code-plugin
bun install
bun run build
```

## Setup

### 1. Environment Variables

```bash
export WUPHF_API_KEY="your-api-key-here"
export WUPHF_API_BASE_URL="https://app.nex.ai"  # optional, defaults to app.nex.ai
```

### 2. MCP Server Registration

Register the WUPHF MCP server so Claude Code can use `/recall` and `/remember`:

```bash
claude mcp add wuphf -- node /path/to/mcp/dist/index.js
```

### 3. Hook Configuration

Copy the hook entries from `settings.json` into your Claude Code settings at `~/.claude/settings.json`. Update the `<path-to>` placeholder with the actual path:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "node /absolute/path/to/claude-code-plugin/dist/auto-session-start.js",
            "timeout": 10000,
            "statusMessage": "Loading knowledge context..."
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "node /absolute/path/to/claude-code-plugin/dist/auto-recall.js",
            "timeout": 10000,
            "statusMessage": "Recalling relevant memories..."
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "node /absolute/path/to/claude-code-plugin/dist/auto-capture.js",
            "timeout": 5000,
            "async": true
          }
        ]
      }
    ]
  }
}
```

### 4. Slash Commands

Copy the `commands/` directory to your project's `.claude/commands/` or global `~/.claude/commands/`:

```bash
cp -r commands/ ~/.claude/commands/
```

Then use:
- `/recall <query>` — Search your WUPHF knowledge base
- `/remember <text>` — Store information in WUPHF

## How It Works

### Session Start (SessionStart Hook)

1. Fires once when a new Claude Code session begins
2. Queries WUPHF for a baseline context summary ("key active context, recent interactions, important updates")
3. Injects as system context so the agent "already knows" relevant business context from the first message
4. On any error: returns `{}`, logs to stderr (graceful degradation)

### Auto-Recall (UserPromptSubmit Hook)

1. Reads the user's prompt from stdin (`{ "prompt": "...", "session_id": "..." }`)
2. Runs prompt through `recall-filter.ts` — skips short directives, tool commands, code-heavy prompts; always recalls on questions and first prompt
3. If recall needed, queries WUPHF `/ask` endpoint for relevant context
4. Returns `{ "additionalContext": "<wuphf-context>...</wuphf-context>" }` to inject into the conversation
5. On any error: returns `{}`, logs to stderr (graceful degradation)

### Auto-Capture (Stop Hook)

1. Reads `{ "last_assistant_message": "...", "session_id": "..." }` from stdin
2. Strips any `<wuphf-context>` blocks (prevents feedback loops)
3. Filters out short, duplicate, or command messages
4. Sends to WUPHF `/text` endpoint (fire-and-forget, `async: true`)
5. On any error: returns `{}` (graceful degradation)

## Architecture

```
claude-code-plugin/
├── src/
│   ├── auto-session-start.ts  # SessionStart hook — baseline context load
│   ├── auto-recall.ts         # UserPromptSubmit hook — selective recall
│   ├── auto-capture.ts        # Stop hook — conversation capture
│   ├── recall-filter.ts       # Smart prompt classifier + debounce
│   ├── wuphf-client.ts          # HTTP client for WUPHF API
│   ├── config.ts              # Environment variable config
│   ├── context-format.ts      # XML context formatting
│   ├── capture-filter.ts      # Smart capture filtering
│   ├── rate-limiter.ts        # Sliding window rate limiter
│   └── session-store.ts       # LRU session ID mapping
├── commands/
│   ├── recall.md              # /recall slash command
│   └── remember.md            # /remember slash command
├── settings.json              # Hook configuration template
└── README.md
```
