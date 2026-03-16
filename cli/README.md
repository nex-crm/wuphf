# @nex-ai/nex

[![npm version](https://img.shields.io/npm/v/@nex-ai/nex)](https://www.npmjs.com/package/@nex-ai/nex)
[![GitHub](https://img.shields.io/badge/github-nex--crm%2Fnex--as--a--skill-blue)](https://github.com/nex-crm/nex-as-a-skill)

Nex CLI provides organizational context & memory to AI agents across 12 platforms.

**GitHub**: [github.com/nex-crm/nex-as-a-skill](https://github.com/nex-crm/nex-as-a-skill)

## Install

```bash
# Install globally
bun install -g @nex-ai/nex

# Or run directly (no install)
npx @nex-ai/nex ask "who is Maria?"
```

## Quick Start (Recommended)

```bash
# One command to get started — registers, detects platforms, installs hooks, scans files
nex setup
```

`nex setup` handles everything: API key registration, platform detection, hook installation, file scanning, and config creation. Run it once and you're ready to go.

```bash
# Now query your knowledge
nex ask "what's the latest on the Acme deal?"
```

<details>
<summary>Manual setup (if you prefer step-by-step)</summary>

```bash
# 1. Register for an API key
nex register --email you@company.com

# 2. Set up your platforms (auto-detects installed tools)
nex setup

# 3. Query your knowledge
nex ask "what's the latest on the Acme deal?"
```

</details>

## Supported Platforms

`nex setup` auto-detects and configures these platforms with full-depth integration:

| Platform | Hooks | Plugins | Agents | Workflows | Rules | MCP |
|----------|-------|---------|--------|-----------|-------|-----|
| **Claude Code** | SessionStart, UserPromptSubmit, Stop | — | — | 26 slash commands | — | — |
| **Cursor** | sessionStart, userPromptSubmit, stop | — | — | — | `.cursor/rules/nex.md` | `~/.cursor/mcp.json` |
| **Windsurf** | pre_user_prompt, post_cascade_response | — | — | /nex-ask, /nex-remember, /nex-search | `.windsurf/rules/nex.md` | `mcp_config.json` |
| **Cline** | UserPromptSubmit, TaskStart, TaskComplete | — | — | — | `.clinerules/nex.md` | `cline_mcp_settings.json` |
| **OpenClaw** | auto-recall, auto-capture (plugin) | `openclaw plugins install` (49 tools) | — | — | — | — |
| **OpenCode** | — | `.opencode/plugins/nex.ts` | — | — | `AGENTS.md` | `opencode.json` |
| **VS Code** | — | — | `.github/agents/nex.agent.md` | — | `.github/instructions/` | `.vscode/mcp.json` |
| **Kilo Code** | — | — | `.kilocodemodes` | — | `.kilocode/rules/nex.md` | `.kilocode/mcp.json` |
| **Continue.dev** | — | — | — | — | `.continue/rules/nex.md` | `mcp.json` |
| **Zed** | — | — | — | — | `.rules` | `settings.json` |
| **Claude Desktop** | — | — | — | — | — | `claude_desktop_config.json` |
| **Aider** | — | — | — | — | `CONVENTIONS.md` | — |

All MCP-based platforms use the same server entry:

```json
{
  "nex": {
    "command": "npx",
    "args": ["-y", "@nex-crm/mcp-server"],
    "env": { "NEX_API_KEY": "sk-..." }
  }
}
```

## Setup Command

```bash
nex setup                          # Auto-detect platforms, install full stack, scan files, create .nex.toml
nex setup --platform cursor        # Install for a specific platform only
nex setup --no-hooks               # Skip hook installation for all platforms
nex setup --no-plugin              # Skip hooks/commands (alias for --no-hooks)
nex setup --no-rules               # Skip rules/instruction file installation
nex setup --no-scan                # Skip file scanning during setup
nex setup status                   # Show all platforms, install status, and connections
nex graph                          # Open the workspace graph in your browser
```

**Default behavior** (no flags):
- If no API key exists: prompts to register
- If API key exists: offers to regenerate (picks up latest scopes) or change email
- Installs the full 6-layer hierarchy for each detected platform: hooks → plugins → agents → workflows → rules → MCP
- Scans current directory and ingests new/changed files into Nex
- Creates `.nex.toml` project config with commented defaults
- Syncs API key to `~/.nex-mcp.json` (shared config)

**Single install**: `bun install -g @nex-ai/nex` bundles everything — hooks, adapters, platform plugins, slash commands, rules, and MCP server. No separate packages needed.

**Integration hierarchy** (per platform): Hooks > Custom plugins > Custom agents/modes > Workflows > Rules > MCP. Each platform gets every layer it supports.

## Project Config: `.nex.toml`

Per-project settings file created by `nex setup`. All fields are optional.

```toml
[auth]
# api_key = "sk-..."          # Prefer NEX_API_KEY env var or ~/.nex/config.json

[hooks]
# enabled = true              # Master kill switch for all hooks

[hooks.recall]
# enabled = true              # Auto-recall context on each prompt
# debounce_ms = 30000

[hooks.capture]
# enabled = true              # Auto-capture on conversation stop
# min_length = 20
# max_length = 50000

[hooks.session_start]
# enabled = true              # Load context on session start

[scan]
# enabled = true
# extensions = [".md", ".txt", ".csv", ".json", ".yaml", ".yml"]
# ignore_dirs = ["node_modules", ".git", "dist", "build", "__pycache__"]
# max_files = 5
# max_file_size = 100000
# depth = 2

[mcp]
# enabled = false             # Set to true when `nex setup` installs MCP for this project/platform

[output]
# format = "text"             # "text" | "json"
# timeout = 120000
```

**Resolution order:** CLI flags > `.nex.toml` > env vars > `~/.nex/config.json` > defaults

## Commands

### Knowledge Graph

```bash
nex ask <query>              # Query with natural language
nex remember <content>       # Ingest text (meeting notes, emails, docs)
nex recall <query>           # Query → XML-wrapped for agent injection
nex capture [content]        # Rate-limited ingestion for agent hooks
nex artifact <id>            # Check processing status
nex search <query>           # Search CRM records by name
nex insight list [--last 24h]  # Recent insights
nex graph                    # Visualize your workspace graph in the browser
```

### Integrations

```bash
nex integrate list                  # Show all integrations with connection status
nex integrate connect gmail         # Connect Gmail via OAuth
nex integrate connect slack         # Connect Slack
nex integrate disconnect <id>      # Disconnect by connection ID
```

Available integrations: `gmail`, `google-calendar`, `outlook`, `outlook-calendar`, `slack`, `salesforce`, `hubspot`, `attio`.

### CRM Records

```bash
nex object list              # List object types (person, company, deal)
nex record list person --limit 10
nex record create person --data '{"name":"Jane Doe"}'
nex record upsert person --match email --data '{"name":"Jane","email":"jane@co.com"}'
nex record update <id> --data '{"phone":"+1234"}'
nex record delete <id>
nex record timeline <id>
```

### Tasks & Notes

```bash
nex task create --title "Follow up" --priority high --due 2026-04-01
nex task list --assignee me --search "follow up"
nex task update <id> --completed
nex note create --title "Call notes" --content "..." --entity <record-id>
```

### Relationships & Lists

```bash
nex rel list-defs
nex rel create <record-id> --def <def-id> --entity1 <id1> --entity2 <id2>
nex list list person
nex list add-member <list-id> --parent <record-id>
nex list-job create "enterprise contacts in EMEA"
```

### File Scanning

On session start, Nex automatically scans project files and ingests changed content.

**Default text-based extensions:** `.md`, `.txt`, `.csv`, `.json`, `.yaml`, `.yml`

**Document formats** (handled separately): `.docx`, `.doc`, `.odt`, `.xlsx`, `.xls`, `.pptx`, `.ppt`, `.pdf`

Configure via `.nex.toml` `[scan]` section or environment variables (`NEX_SCAN_ENABLED`, `NEX_SCAN_EXTENSIONS`, etc.).

### Config & Sessions

```bash
nex config show              # Resolved config (key masked)
nex config set default_format text
nex config path              # ~/.nex/config.json
nex session list             # Stored session mappings
nex session clear
```

## Global Flags

```
--api-key <key>     Override API key (env: NEX_API_KEY)
--format <fmt>      json | text (default for integrate list) | quiet
--timeout <ms>      Request timeout (default: 120000)
--session <id>      Session ID for multi-turn context
--debug             Debug output on stderr
```

## Stdin Support

`ask`, `remember`, and `capture` read from stdin when no argument is provided:

```bash
cat meeting-notes.txt | nex remember
echo "what happened today?" | nex ask
git diff | nex capture
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (server error, rate limit, invalid input) |
| 2 | Auth error (no API key, invalid key, 401/403) |

## Development

```bash
bun install
bun run build     # TypeScript → dist/
bun run dev       # Run TS directly (no build)
bun test          # Unit + integration tests
NEX_DEV_URL=http://localhost:30000 nex ask "test"  # Local API
```
