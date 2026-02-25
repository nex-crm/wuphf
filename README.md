# Nex — AI Agent Integrations

Give any AI agent a Context Graph. Two integration paths: **MCP Server** (Claude Desktop, ChatGPT, Cursor, Windsurf) and **OpenClaw Skill**.

## What is Nex?

Nex is a real-time context layer for AI agents. It builds a Context Graph from your conversations and shares organizational context with your agents — query contacts, companies, relationships, tasks, notes, and insights.

## Integration Options

| | MCP Server | OpenClaw Skill |
|---|---|---|
| **Format** | TypeScript MCP server | SKILL.md + bash scripts |
| **Platforms** | Claude Desktop, ChatGPT, Cursor, Windsurf, any MCP client | OpenClaw |
| **Transport** | stdio (local) or Streamable HTTP (remote) | Shell exec |
| **Tools** | 47 typed tools with Zod schemas | Bash wrapper around curl |
| **Auth** | Auto-registration or manual API key | Auto-registration or manual |
| **Setup** | `npm install` + add to client config | Copy SKILL.md to skills dir |

## MCP Server (recommended)

Works with Claude Desktop, ChatGPT (agent mode), Cursor, Windsurf, and any MCP-compatible client.

### Quick Start

```bash
cd mcp && npm install
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

If you don't set `NEX_API_KEY`, the server starts in registration mode. Just call the `register` tool with your email — it gets an API key and saves it to `~/.nex-mcp.json` for all future sessions.

See [`mcp/README.md`](mcp/README.md) for the full tool reference and setup guide.

## OpenClaw Skill

For OpenClaw agents specifically.

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

3. Verify: `openclaw agent --message "What skills do you have?"`

Or skip the API key — the skill can auto-register on first use.

## API Documentation

See [docs.nex.ai](https://docs.nex.ai) for full API documentation.

## License

MIT License - see [LICENSE](LICENSE)
