# WUPHF: Compounding Intelligence for AI agents

[![npm version](https://img.shields.io/npm/v/@wuphf/wuphf)](https://www.npmjs.com/package/@wuphf/wuphf)
[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)

Turn all your AI agent conversations into a unified knowledge graph. Supports Claude Code, Codex, OpenClaw, Cursor, OpenCode, etc. Adds additional context from Email, Meetings, Slack, HubSpot, Salesforce.

Tell something to OpenClaw. Ask about it in Claude Code. Reference it from Cursor. Context follows you across tools — no copy-pasting, no re-explaining, no lost context.

<a href="https://discord.gg/gjSySC3PzV"><img src="https://img.shields.io/badge/Join%20our%20Discord-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Join our Discord" /></a>

Talk to the team, share feedback, and connect with other developers building AI agents with WUPHF.

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

| | CLI | MCP Server | OpenClaw Plugin | Claude Code Plugin | SKILL.md |
|---|---|---|---|---|---|
| **Platforms** | Any terminal / AI agent | Claude Desktop, ChatGPT, Cursor, Windsurf | OpenClaw | Claude Code CLI | OpenClaw (script-based) |
| **Auto-recall** | Via `wuphf recall` | No (tool calls) | Yes (smart filter) | Yes (smart filter) | No (manual) |
| **Auto-capture** | Via `wuphf capture` | No | Yes | Yes | No (manual) |
| **Commands** | 50+ CLI commands | 50+ typed tools | 4 tools + 4 commands | 5 slash commands + MCP | bash scripts |
| **Rate limiting** | File-based | File-based | Queue + file-based | File-based | N/A |
| **Session tracking** | File-based | File-based | In-memory LRU | File-based | N/A |
| **Setup** | `wuphf setup` | `wuphf setup --with-mcp` | Copy plugin | `wuphf setup` | Set `WUPHF_API_KEY` |

## Quick Start (Recommended)

```bash
# Install and run setup — handles everything in one step
bun install -g @wuphf/wuphf
wuphf setup
```

`wuphf setup` registers your API key, auto-detects your AI platforms (Claude Code, Cursor, Windsurf, etc.), installs hooks, scans project files, and creates config. One command, fully configured.

```bash
# Now use it from any agent
wuphf ask "who is Maria Rodriguez?"
wuphf remember "Met with Maria, CTO of TechFlow. European expansion Q3, $2M budget."
```

See [`cli/README.md`](cli/README.md) for all 50+ commands.

---

<details>
<summary>Manual setup per platform (if you prefer step-by-step)</summary>

### CLI (any terminal, any AI agent)

```bash
npx @wuphf/wuphf register --email you@company.com
```

That's it. Now use it:

```bash
# Ask your knowledge graph
wuphf ask "who is Maria Rodriguez?"

# Ingest information
wuphf remember "Met with Maria Rodriguez, CTO of TechFlow. European expansion Q3, $2M budget."

# Or pipe from stdin
cat meeting-notes.txt | wuphf remember

# Search CRM records
wuphf search "TechFlow"

# CRUD operations
wuphf record list person --limit 10
wuphf task create --title "Follow up with Maria" --priority high
wuphf insight list --last 24h

# Build auto-recall hooks for any agent
wuphf recall "what do I know about TechFlow?"  # Returns <wuphf-context> XML block

# Build auto-capture hooks
wuphf capture "Agent conversation text..."  # Rate-limited, filtered
```

Install globally: `bun install -g @wuphf/wuphf`

### MCP Server (Claude Desktop, Cursor, Windsurf)

```bash
cd mcp && bun install && bun run build
```

Add to your client config:

```json
{
  "mcpServers": {
    "wuphf": {
      "command": "bun",
      "args": ["/path/to/wuphf/mcp/src/index.ts"],
      "env": { "WUPHF_API_KEY": "sk-your_key_here" }
    }
  }
}
```

No API key? The server starts in registration mode — call the `register` tool with your email.

See [`mcp/README.md`](mcp/README.md) for all tools.

### OpenClaw Plugin (auto-recall + auto-capture)

```bash
cp -r openclaw-plugin /path/to/openclaw/plugins/wuphf
cd /path/to/openclaw/plugins/wuphf && bun install && bun run build
```

Add to `openclaw.json`:

```json
{
  "plugins": {
    "load": { "paths": ["/path/to/plugins/wuphf"] },
    "slots": { "memory": "wuphf" },
    "entries": {
      "wuphf": {
        "enabled": true,
        "config": {
          "apiKey": "sk-your_key_here"
        }
      }
    }
  }
}
```

See [`openclaw-plugin/README.md`](openclaw-plugin/README.md) for details.

### Claude Code Plugin (auto-recall + auto-capture)

```bash
cd claude-code-plugin && bun install && bun run build
```

Add hooks to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [{
      "matcher": "",
      "hooks": [{
        "type": "command",
        "command": "WUPHF_API_KEY=sk-your_key node /path/to/claude-code-plugin/dist/auto-recall.js",
        "timeout": 10000
      }]
    }],
    "Stop": [{
      "matcher": "",
      "hooks": [{
        "type": "command",
        "command": "WUPHF_API_KEY=sk-your_key node /path/to/claude-code-plugin/dist/auto-capture.js",
        "timeout": 5000,
        "async": true
      }]
    }]
  }
}
```

Slash commands and MCP server:

```bash
cp claude-code-plugin/commands/*.md ~/.claude/commands/    # /recall, /remember, /scan, /entities
claude mcp add wuphf -- node /path/to/mcp/dist/index.js      # Full toolset
```

See [`claude-code-plugin/README.md`](claude-code-plugin/README.md) for details.

### SKILL.md (OpenClaw script-based)

For OpenClaw agents without the plugin, SKILL.md provides bash-script-based access:

```bash
# Register and get API key
bash scripts/wuphf-openclaw-register.sh your@email.com "Your Name"

# Query context
printf '{"query":"who is Maria?"}' | bash scripts/wuphf-api.sh POST /v1/context/ask

# Ingest text
printf '{"content":"Meeting notes..."}' | bash scripts/wuphf-api.sh POST /v1/context/text

# Scan project files
bash scripts/wuphf-scan-files.sh --dir . --max-files 10
```

See [`SKILL.md`](SKILL.md) for the full API reference.

</details>

## Shared Config

All surfaces share configuration for cross-tool compatibility:

| File | Purpose | Shared by |
|------|---------|-----------|
| `~/.wuphf-mcp.json` | API key + workspace info | All surfaces |
| `~/.wuphf/file-scan-manifest.json` | File change tracking | All surfaces |
| `~/.wuphf/rate-limiter.json` | Rate limit timestamps | OC, MCP, CC |
| `~/.wuphf/recall-state.json` | Recall debounce state | CC |

Register once via any surface → all other surfaces pick up the key automatically.

## Architecture

```
                    ┌─────────────────────┐
                    │   WUPHF Context Graph  │
                    │  (people, companies, │
                    │  insights, tasks...) │
                    └──────────┬──────────┘
                               │
      ┌────────────────────────┼────────────────────────┐
      │              │                   │              │
  ┌───▼────┐  ┌─────▼───────┐  ┌───────▼──────┐  ┌───▼──────────┐
  │  CLI   │  │  MCP Server │  │  OpenClaw    │  │  Claude Code │
  │  50+   │  │  50+ tools  │  │  Plugin     │  │  Plugin      │
  │  cmds  │  │  + scan     │  │  + recall   │  │  + recall    │
  └───┬────┘  └─────┬───────┘  └──────┬──────┘  └──────┬───────┘
      │             │                 │                 │
  Any agent    Claude Desktop    OpenClaw agents   Claude Code
  Aider        ChatGPT          Clawgent          Any project
  Codex        Cursor            WhatsApp
  Custom       Windsurf
```

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `WUPHF_API_KEY` | Yes (or register) | — |
| `WUPHF_DEV_URL` | No (dev only) | `https://app.nex.ai` |
| `WUPHF_SCAN_ENABLED` | No | `true` |
| `WUPHF_SCAN_EXTENSIONS` | No | `.md,.txt,.rtf,.html,.htm,.csv,.tsv,.json,.yaml,.yml,.toml,.xml,.js,.ts,.jsx,.tsx,.py,.rb,.go,.rs,.java,.sh,.bash,.zsh,.fish,.org,.rst,.adoc,.tex,.log,.env,.ini,.cfg,.conf,.properties` |
| `WUPHF_SCAN_MAX_FILES` | No | `5` |
| `WUPHF_SCAN_DEPTH` | No | `20` |
| `WUPHF_SCAN_MAX_FILE_SIZE` | No | `100000` (bytes) |
| `WUPHF_SCAN_IGNORE_DIRS` | No | `node_modules,.git,dist,build,.next,__pycache__,vendor,.venv,.claude,coverage,.turbo,.cache` |

## Testing

- **CLI**: 119 tests (`cd cli && bun test`)
- **OpenClaw plugin**: 38/38 unit tests (`cd openclaw-plugin && npx vitest run`)
- **Claude Code plugin**: 21/21 E2E tests (see `docs/wuphf-plugin-test-results.md`)
- **MCP server**: Builds clean, all tools typed with Zod schemas
- **SKILL scripts**: Syntax validated, injection-resistant, cross-platform (macOS + Linux)

## License

MIT
