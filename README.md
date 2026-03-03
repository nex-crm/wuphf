# Nex — Cross-Agent Context Layer

Give any AI agent persistent memory. One knowledge graph, every agent.

Tell something to OpenClaw. Ask about it in Claude Code. Reference it from Cursor. Context follows you across tools — no copy-pasting, no re-explaining, no lost context.

## How It Works

```
You → OpenClaw: "Maria Rodriguez, CTO of TechFlow, wants to expand to Europe in Q3. Budget is $2M."

You → Claude Code: "What do you know about Maria Rodriguez?"
Claude Code: "Maria Rodriguez is the CTO of TechFlow. They're planning European expansion
              in Q3 with a $2M budget."

You → Cursor: "Which companies are planning European expansion?"
Cursor: "TechFlow — Maria Rodriguez (CTO) confirmed Q3 timeline, $2M budget."
```

One fact entered once. Available everywhere, instantly.

## Integration Options

| | MCP Server | OpenClaw Plugin | Claude Code Plugin |
|---|---|---|---|
| **Platforms** | Claude Desktop, ChatGPT, Cursor, Windsurf | OpenClaw | Claude Code CLI |
| **Auto-recall** | No (explicit tool calls) | Yes | Yes |
| **Auto-capture** | No | Yes | Yes |
| **Tools** | 47 typed tools | 3 memory tools | Via MCP server |
| **Setup** | `npm install` + config | Copy plugin | Build + hooks |

## Quick Start

### MCP Server (Claude Desktop, Cursor, Windsurf)

```bash
cd mcp && npm install && npm run build
```

Add to your client config:

```json
{
  "mcpServers": {
    "nex": {
      "command": "npx",
      "args": ["tsx", "/path/to/nex-as-a-skill/mcp/src/index.ts"],
      "env": { "NEX_API_KEY": "sk-your_key_here" }
    }
  }
}
```

No API key? The server starts in registration mode — call the `register` tool with your email.

See [`mcp/README.md`](mcp/README.md) for all 47 tools.

### OpenClaw Plugin (auto-recall + auto-capture)

```bash
cp -r openclaw-plugin /path/to/openclaw/plugins/nex
cd /path/to/openclaw/plugins/nex && npm install && npm run build
```

Add to `openclaw.json`:

```json
{
  "plugins": {
    "load": { "paths": ["/path/to/plugins/nex"] },
    "slots": { "memory": "nex" },
    "entries": {
      "nex": {
        "enabled": true,
        "config": {
          "apiKey": "sk-your_key_here",
          "baseUrl": "https://api.nex-crm.com"
        }
      }
    }
  }
}
```

See [`openclaw-plugin/README.md`](openclaw-plugin/README.md) for details.

### Claude Code Plugin (auto-recall + auto-capture)

```bash
cd claude-code-plugin && npm install && npm run build
```

Add hooks to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [{
      "matcher": "",
      "hooks": [{
        "type": "command",
        "command": "NEX_API_KEY=sk-your_key node /path/to/claude-code-plugin/dist/auto-recall.js",
        "timeout": 10000
      }]
    }],
    "Stop": [{
      "matcher": "",
      "hooks": [{
        "type": "command",
        "command": "NEX_API_KEY=sk-your_key node /path/to/claude-code-plugin/dist/auto-capture.js",
        "timeout": 5000,
        "async": true
      }]
    }]
  }
}
```

Optional slash commands and MCP server:

```bash
cp claude-code-plugin/commands/*.md ~/.claude/commands/    # /recall, /remember
claude mcp add nex -- node /path/to/mcp/dist/index.js      # 47 tools
```

See [`claude-code-plugin/README.md`](claude-code-plugin/README.md) for details.

## Architecture

```
                    ┌─────────────────────┐
                    │   Nex Context Graph  │
                    │  (people, companies, │
                    │  insights, tasks...) │
                    └──────────┬──────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
     ┌────────▼───────┐ ┌─────▼──────┐ ┌───────▼──────┐
     │  MCP Server    │ │  OpenClaw  │ │  Claude Code │
     │  (47 tools)    │ │  Plugin    │ │  Plugin      │
     └────────┬───────┘ └─────┬──────┘ └───────┬──────┘
              │               │                │
     Claude Desktop    OpenClaw agents    Claude Code CLI
     ChatGPT           Clawgent          Any project
     Cursor
     Windsurf
```

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `NEX_API_KEY` | Yes | — |
| `NEX_API_BASE_URL` | No | `https://api.nex-crm.com` |

## Testing

- **OpenClaw plugin**: 38/38 unit tests pass (`cd openclaw-plugin && npx vitest run`)
- **Claude Code plugin**: 14/14 manual E2E tests pass (see [`docs/nex-plugin-test-results.md`](../docs/nex-plugin-test-results.md))
- **MCP server**: Builds clean, all tools typed with Zod schemas

## License

MIT
