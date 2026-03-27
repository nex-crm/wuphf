# WUPHF: Compounding Intelligence for AI agents

[![npm version](https://img.shields.io/npm/v/@wuphf/wuphf)](https://www.npmjs.com/package/@wuphf/wuphf)
[![GitHub](https://img.shields.io/badge/github-najmuzzaman--mohammad%2Fwuphf-blue)](https://github.com/najmuzzaman-mohammad/wuphf)
[![Discord](https://img.shields.io/badge/Discord-Join%20Community-5865F2?logo=discord&logoColor=white)](https://discord.gg/gjSySC3PzV)

Turn all your AI agent conversations into a unified knowledge graph with proactive context surfacing. Supports Claude Code, Codex, OpenClaw, Cursor, OpenCode, etc. Adds additional context from Email, Meetings, Slack, HubSpot, Salesforce.

<a href="https://discord.gg/gjSySC3PzV"><img src="https://img.shields.io/badge/Join%20our%20Discord-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Join our Discord" /></a>

Talk to the team, share feedback, and connect with other developers building AI agents with WUPHF.

**GitHub**: [github.com/najmuzzaman-mohammad/wuphf](https://github.com/najmuzzaman-mohammad/wuphf)

## Install

```bash
# Install globally
npm install -g @wuphf/wuphf

# Or run directly (no install)
npx @wuphf/wuphf ask "who is Maria?"
```

## Quick Start (Recommended)

```bash
# One command to get started — registers, detects platforms, installs hooks, scans files
wuphf setup
```

`wuphf setup` handles everything: API key registration, platform detection, hook installation, file scanning, and config creation. Run it once and you're ready to go.

```bash
# Now query your knowledge
wuphf ask "what's the latest on the Acme deal?"
```

<details>
<summary>Manual setup (if you prefer step-by-step)</summary>

```bash
# 1. Register for an API key
wuphf register --email you@company.com

# 2. Set up your platforms (auto-detects installed tools)
wuphf setup

# 3. Query your knowledge
wuphf ask "what's the latest on the Acme deal?"
```

</details>

## Supported Platforms

`wuphf setup` auto-detects and configures these platforms with full-depth integration:

| Platform | Hooks | Plugins | Agents | Workflows | Rules | MCP |
|----------|-------|---------|--------|-----------|-------|-----|
| **Claude Code** | SessionStart, UserPromptSubmit, Stop | — | — | 26 slash commands | — | — |
| **Cursor** | sessionStart, userPromptSubmit, stop | — | — | — | `.cursor/rules/wuphf.md` | `~/.cursor/mcp.json` |
| **Windsurf** | pre_user_prompt, post_cascade_response | — | — | /wuphf-ask, /wuphf-remember, /wuphf-search | `.windsurf/rules/wuphf.md` | `mcp_config.json` |
| **Cline** | UserPromptSubmit, TaskStart, TaskComplete | — | — | — | `.clinerules/wuphf.md` | `cline_mcp_settings.json` |
| **OpenClaw** | auto-recall, auto-capture (plugin) | `openclaw plugins install` (49 tools) | — | — | — | — |
| **OpenCode** | — | `.opencode/plugins/wuphf.ts` | — | — | `AGENTS.md` | `opencode.json` |
| **VS Code** | — | — | `.github/agents/wuphf.agent.md` | — | `.github/instructions/` | `.vscode/mcp.json` |
| **Kilo Code** | — | — | `.kilocodemodes` | — | `.kilocode/rules/wuphf.md` | `.kilocode/mcp.json` |
| **Continue.dev** | — | — | — | — | `.continue/rules/wuphf.md` | `mcp.json` |
| **Zed** | — | — | — | — | `.rules` | `settings.json` |
| **Claude Desktop** | — | — | — | — | — | `claude_desktop_config.json` |
| **Aider** | — | — | — | — | `CONVENTIONS.md` | — |

All MCP-based platforms use the same server entry:

```json
{
  "wuphf": {
    "command": "npx",
    "args": ["-y", "@wuphf/mcp-server"],
    "env": { "WUPHF_API_KEY": "sk-..." }
  }
}
```

## Setup Command

```bash
wuphf setup                          # Auto-detect platforms, install full stack, scan files, create .wuphf.toml
wuphf setup --platform cursor        # Install for a specific platform only
wuphf setup --no-hooks               # Skip hook installation for all platforms
wuphf setup --no-plugin              # Skip hooks/commands (alias for --no-hooks)
wuphf setup --no-rules               # Skip rules/instruction file installation
wuphf setup --no-scan                # Skip file scanning during setup
wuphf setup status                   # Show all platforms, install status, and connections
wuphf graph                          # Open the workspace graph in your browser
```

**Default behavior** (no flags):
- If no API key exists: prompts to register
- If API key exists: offers to regenerate (picks up latest scopes) or change email
- Installs the full 6-layer hierarchy for each detected platform: hooks → plugins → agents → workflows → rules → MCP
- Scans current directory and ingests new/changed files into WUPHF
- Creates `.wuphf.toml` project config with commented defaults
- Stores config in `~/.wuphf/config.json`

**Single install**: `npm install -g @wuphf/wuphf` bundles everything — hooks, adapters, platform plugins, slash commands, rules, and MCP server. No separate packages needed.

**Integration hierarchy** (per platform): Hooks > Custom plugins > Custom agents/modes > Workflows > Rules > MCP. Each platform gets every layer it supports.

## Project Config: `.wuphf.toml`

Per-project settings file created by `wuphf setup`. All fields are optional.

```toml
[auth]
# api_key = "sk-..."          # Prefer WUPHF_API_KEY env var or ~/.wuphf/config.json

[hooks]
# enabled = true              # Master kill switch for all hooks

[hooks.recall]
# enabled = true              # Proactive context on every prompt
# debounce_ms = 10000

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
# max_files = 1000
# max_file_size = 100000
# depth = 2

[mcp]
# enabled = false             # Set to true when `wuphf setup` installs MCP for this project/platform

[output]
# format = "text"             # "text" | "json"
# timeout = 120000
```

**Resolution order:** CLI flags > `.wuphf.toml` > env vars > `~/.wuphf/config.json` > defaults

## Commands

### Knowledge Graph

```bash
wuphf ask <query>              # Query with natural language
wuphf remember <content>       # Ingest text (meeting notes, emails, docs)
wuphf recall <query>           # Query → XML-wrapped for agent injection
wuphf capture [content]        # Rate-limited ingestion for agent hooks
wuphf artifact <id>            # Check processing status
wuphf search <query>           # Search CRM records by name
wuphf insight list [--last 24h]  # Recent insights
wuphf graph                    # Visualize your workspace graph in the browser
```

### Integrations

```bash
wuphf integrate list                  # Show all integrations with connection status
wuphf integrate connect gmail         # Connect Gmail via OAuth
wuphf integrate connect slack         # Connect Slack
wuphf integrate disconnect <id>      # Disconnect by connection ID
```

Available integrations: `gmail`, `google-calendar`, `outlook`, `outlook-calendar`, `slack`, `salesforce`, `hubspot`, `attio`.

### CRM Records

```bash
wuphf object list              # List object types (person, company, deal)
wuphf record list person --limit 10
wuphf record create person --data '{"name":"Jane Doe"}'
wuphf record upsert person --match email --data '{"name":"Jane","email":"jane@co.com"}'
wuphf record update <id> --data '{"phone":"+1234"}'
wuphf record delete <id>
wuphf record timeline <id>
```

### Tasks & Notes

```bash
wuphf task create --title "Follow up" --priority high --due 2026-04-01
wuphf task list --assignee me --search "follow up"
wuphf task update <id> --completed
wuphf note create --title "Call notes" --content "..." --entity <record-id>
```

### Relationships & Lists

```bash
wuphf rel list-defs
wuphf rel create <record-id> --def <def-id> --entity1 <id1> --entity2 <id2>
wuphf list list person
wuphf list add-member <list-id> --parent <record-id>
wuphf list-job create "enterprise contacts in EMEA"
```

### File Scanning

On session start, WUPHF automatically scans project files and ingests changed content using concurrent workers (5 parallel requests). After ingestion, compounding intelligence jobs are triggered automatically to generate patterns and playbook rules.

```bash
wuphf scan                    # Scan current directory (up to 1000 files)
wuphf scan --max-files 500    # Limit files per scan
wuphf scan --force            # Re-scan all files (ignore manifest)
wuphf scan --dry-run          # Preview what would be scanned
```

**Default text-based extensions:** `.md`, `.txt`, `.csv`, `.json`, `.yaml`, `.yml`

**Document formats** (handled separately): `.docx`, `.doc`, `.odt`, `.xlsx`, `.xls`, `.pptx`, `.ppt`, `.pdf`

Configure via `.wuphf.toml` `[scan]` section or environment variables (`WUPHF_SCAN_ENABLED`, `WUPHF_SCAN_EXTENSIONS`, etc.).

### Proactive Context

WUPHF surfaces relevant knowledge graph context on every prompt — not just questions. When you say "fix the migration script" or "deploy to staging", the system automatically injects entity insights, knowledge insights, and playbook patterns from your knowledge graph.

Context is injected as `<wuphf-context>` blocks that AI agents use naturally without explicitly referencing the source. Only trivial inputs (yes/ok/lgtm) are skipped.

### Transcript Capture

At session end, the full conversation transcript is automatically ingested into the knowledge graph. This captures complete decision trails, code discussions, and debugging sessions — not just the last message.

### Config & Sessions

```bash
wuphf config show              # Resolved config (key masked)
wuphf config set default_format text
wuphf config path              # ~/.wuphf/config.json
wuphf session list             # Stored session mappings
wuphf session clear
```

## Global Flags

```
--api-key <key>     Override API key (env: WUPHF_API_KEY)
--format <fmt>      json | text (default for integrate list) | quiet
--timeout <ms>      Request timeout (default: 120000)
--session <id>      Session ID for multi-turn context
--debug             Debug output on stderr
```

## Stdin Support

`ask`, `remember`, and `capture` read from stdin when no argument is provided:

```bash
cat meeting-notes.txt | wuphf remember
echo "what happened today?" | wuphf ask
git diff | wuphf capture
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
WUPHF_DEV_URL=http://localhost:30000 wuphf ask "test"  # Local API
```
