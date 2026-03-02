# Nex — AI Agent Integrations

Give any AI agent a Context Graph. Three integration paths: **MCP Server** (Claude Desktop, ChatGPT, Cursor, Windsurf), **OpenClaw Plugin** (native memory), and **Claude Code Plugin** (hooks + commands).

## What is Nex?

Nex is a real-time context layer for AI agents. It builds a Context Graph from your conversations and shares organizational context with your agents — query contacts, companies, relationships, tasks, notes, and insights.

## Integration Options

| | MCP Server | OpenClaw Plugin | Claude Code Plugin |
|---|---|---|---|
| **Format** | TypeScript MCP server | Native OpenClaw plugin | Hooks + slash commands |
| **Platforms** | Claude Desktop, ChatGPT, Cursor, Windsurf | OpenClaw | Claude Code CLI |
| **Auto-recall** | No (explicit tool calls) | Yes (`onAssistantTurn` hook) | Yes (`UserPromptSubmit` hook) |
| **Auto-capture** | No | Yes (`onConversationTurn` hook) | Yes (`Stop` hook) |
| **Tools** | 47 typed tools with Zod schemas | `memory_search`, `memory_store`, `memory_forget` | Via MCP server |
| **Auth** | Auto-registration or API key | Config-based | `NEX_API_KEY` env var |
| **Setup** | `npm install` + add to client config | Copy plugin to OpenClaw | Build + configure hooks |

## MCP Server (recommended for explicit tool use)

Works with Claude Desktop, ChatGPT (agent mode), Cursor, Windsurf, and any MCP-compatible client.

### Quick Start

```bash
cd mcp && npm install && npm run build
```

#### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "nex": {
      "command": "npx",
      "args": ["tsx", "/path/to/nex-as-a-skill/mcp/src/index.ts"],
      "env": {
        "NEX_API_KEY": "sk-your_key_here"
      }
    }
  }
}
```

#### Claude Code

```bash
claude mcp add nex -- node /path/to/nex-as-a-skill/mcp/dist/index.js
```

Set `NEX_API_KEY` in your environment.

#### Cursor

Add to `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "nex": {
      "command": "npx",
      "args": ["tsx", "/path/to/nex-as-a-skill/mcp/src/index.ts"],
      "env": {
        "NEX_API_KEY": "sk-your_key_here"
      }
    }
  }
}
```

#### No API Key? Auto-register

If you don't set `NEX_API_KEY`, the server starts in registration mode. Call the `register` tool with your email — it gets an API key and saves it to `~/.nex-mcp.json`.

See [`mcp/README.md`](mcp/README.md) for the full tool reference.

## OpenClaw Plugin (auto-recall + auto-capture)

Native OpenClaw plugin that gives agents persistent long-term memory. Automatically recalls relevant context before each response and captures facts from conversations.

### Setup

1. Copy the plugin:
   ```bash
   cp -r openclaw-plugin /path/to/openclaw/plugins/nex
   cd /path/to/openclaw/plugins/nex && npm install && npm run build
   ```

2. Add to your `openclaw.json`:
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

3. Restart OpenClaw. The plugin registers as the memory provider.

### Features

- **Auto-recall**: Queries Nex before each agent turn, injects relevant context
- **Auto-capture**: Extracts facts from conversations after each turn
- **Tools**: `memory_search`, `memory_store`, `memory_forget`
- **Commands**: `/memory-status`, `/memory-search <query>`

See [`openclaw-plugin/README.md`](openclaw-plugin/README.md) for details.

## Claude Code Plugin (auto-recall + auto-capture)

Full-featured plugin for Claude Code CLI with the same behavior as the OpenClaw plugin: automatic context recall before each turn and fact capture after each turn.

### Setup

1. Build the plugin:
   ```bash
   cd claude-code-plugin && npm install && npm run build
   ```

2. Add hooks to your `.claude/settings.json` (or `.claude/settings.local.json`):
   ```json
   {
     "hooks": {
       "UserPromptSubmit": [{
         "matcher": "",
         "hooks": [{
           "type": "command",
           "command": "node /absolute/path/to/claude-code-plugin/dist/auto-recall.js",
           "timeout": 10000,
           "statusMessage": "Recalling relevant context..."
         }]
       }],
       "Stop": [{
         "matcher": "",
         "hooks": [{
           "type": "command",
           "command": "node /absolute/path/to/claude-code-plugin/dist/auto-capture.js",
           "timeout": 5000,
           "async": true
         }]
       }]
     }
   }
   ```

3. Set your API key:
   ```bash
   export NEX_API_KEY="sk-your_key_here"
   ```

4. (Optional) Add slash commands — copy `claude-code-plugin/commands/` to `.claude/commands/`:
   ```bash
   cp claude-code-plugin/commands/*.md /path/to/project/.claude/commands/
   ```

5. (Optional) Add MCP server for explicit tool access:
   ```bash
   claude mcp add nex -- node /path/to/nex-as-a-skill/mcp/dist/index.js
   ```

### Features

- **Auto-recall** (`UserPromptSubmit` hook): Queries Nex with your prompt, injects `<nex-context>` before Claude processes it
- **Auto-capture** (`Stop` hook): Extracts text from Claude's response, sends to Nex for fact extraction (fire-and-forget)
- **Session persistence**: Tracks session IDs across turns via `~/.nex/claude-sessions.json`
- **Rate limiting**: File-based rate tracking at `~/.nex/rate-limiter.json`
- **Commands**: `/recall <query>`, `/remember <text>` (requires MCP server)

### Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `NEX_API_KEY` | Yes | — |
| `NEX_API_BASE_URL` | No | `https://api.nex-crm.com` |

See [`claude-code-plugin/README.md`](claude-code-plugin/README.md) for details.

## OpenClaw Skill (legacy)

For OpenClaw agents using the SKILL.md approach (simpler but no auto-recall/capture).

### Setup

1. Copy `SKILL.md` to your OpenClaw skills directory:
   ```bash
   mkdir -p ~/.openclaw/workspace/skills/nex
   cp SKILL.md ~/.openclaw/workspace/skills/nex/
   ```

2. Add your API key to `~/.openclaw/openclaw.json`:
   ```json
   {
     "skills": {
       "entries": {
         "nex": {
           "enabled": true,
           "env": {
             "NEX_API_KEY": "sk-your_key_here"
           }
         }
       }
     }
   }
   ```

## API Documentation

See [docs.nex.ai](https://docs.nex.ai) for full API documentation.

## License

MIT License - see [LICENSE](LICENSE)
