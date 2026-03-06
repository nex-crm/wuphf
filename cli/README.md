# @nex-ai/cli

Nex CLI provides organizational context & memory to AI agents. 50+ commands covering knowledge graph queries, CRM CRUD, tasks, notes, insights, and agent hooks.

## Install

```bash
# Run directly (no install)
npx @nex-ai/cli ask "who is Maria?"

# Install globally
npm install -g @nex-ai/cli

# Or run from source
cd cli && npm install && npm run build && node dist/index.js
```

## Setup

```bash
# Register for an API key
nex register --email you@company.com

# Or set an existing key
export NEX_API_KEY=sk-your_key_here

# Verify
nex config show
```

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

# File scanning
nex scan [dir]                # Scan directory, ingest new/changed files
nex scan --dry-run            # Preview what would be scanned
nex scan --force              # Re-scan all (ignore manifest)
nex scan --extensions .md,.py # Override file types
nex scan --max-files 20       # Override max files per run
nex scan --depth 5            # Override max directory depth
```

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
--format <fmt>      json (default) | text | quiet
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

## For Agent Builders

Build auto-recall and auto-capture hooks for any AI agent:

```bash
# Auto-recall: inject context before each prompt
nex recall "$USER_PROMPT"  # Returns <nex-context>...</nex-context> or empty

# Auto-capture: save agent output after each turn
nex capture "$AGENT_RESPONSE"  # Rate-limited, filtered, idempotent
```

The `recall` command includes smart filtering (skips short prompts, tool commands, code-heavy content) and the `capture` command includes rate limiting (10 req/min) and content filtering (strips previously injected context blocks).

## Development

```bash
npm install
npm run build     # TypeScript → dist/
npm run dev       # Run with tsx (no build)
npm test          # 65 unit tests
NEX_DEV_URL=http://localhost:30000 nex ask "test"  # Local API
```
