# Deferred Requirements — Multi-Agent CLI

> Features not included in demo build. Each section contains full specs for future implementation.

**Last Updated:** 2026-03-22
**Source Branches:** `kill-saas` (wuphf-cli), `feat/multi-threaded-agents` (wuphf), TS CLI main

---

## P1: Full Command Dispatch

### Object Management
- `object list` — List all object types `GET /v1/objects` `[--include-attributes]`
- `object get <slug>` — Get object by slug `GET /v1/objects/{slug}`
- `object create` — Create object `POST /v1/objects` `--name --slug [--type] [--description]`
- `object update <slug>` — Update object `PATCH /v1/objects/{slug}` `--name --description --name-plural`
- `object delete <slug>` — Delete object `DELETE /v1/objects/{slug}`

### Record Management
- `record list <type>` — List records `GET /v1/records?object_type={type}` `[--limit --offset --sort]`
- `record get <type> <id>` — Get record `GET /v1/records/{type}/{id}`
- `record create <type>` — Create record `POST /v1/records/{type}` `--data <json>`
- `record upsert <type>` — Upsert record `POST /v1/records/{type}/upsert` `--match <attr> --data <json>`
- `record update <type> <id>` — Update record `PATCH /v1/records/{type}/{id}` `--data <json>`
- `record delete <type> <id>` — Delete record `DELETE /v1/records/{type}/{id}`
- `record timeline <type> <id>` — Get timeline `GET /v1/records/{type}/{id}/timeline` `[--limit --cursor]`

### Notes
- `note list` — `GET /v1/notes` `[--entity]`
- `note get <id>` — `GET /v1/notes/{id}`
- `note create` — `POST /v1/notes` `--title [--content] [--entity]`
- `note update <id>` — `PATCH /v1/notes/{id}` `--title --content --entity`
- `note delete <id>` — `DELETE /v1/notes/{id}`

### Tasks
- `task list` — `GET /v1/tasks` `[--entity --assignee --search --completed --limit]`
- `task get <id>` — `GET /v1/tasks/{id}`
- `task create` — `POST /v1/tasks` `--title [--description --priority --due --entities --assignees]`
- `task update <id>` — `PATCH /v1/tasks/{id}` `--title --description --priority --due --completed`
- `task delete <id>` — `DELETE /v1/tasks/{id}`

### Relationships
- `rel list-defs` — `GET /v1/relationships/definitions`
- `rel create-def` — `POST /v1/relationships/definitions` `--type --entity1 --entity2 [--pred12 --pred21]`
- `rel delete-def <id>` — `DELETE /v1/relationships/definitions/{id}`
- `rel create` — `POST /v1/relationships` `--def --entity1 --entity2`
- `rel delete <id>` — `DELETE /v1/relationships/{id}`

### Attributes
- `attribute create <object>` — `POST /v1/objects/{slug}/attributes` `--name --slug --type [--description --options]`
- `attribute update <object> <attr>` — `PATCH /v1/objects/{slug}/attributes/{attr}`
- `attribute delete <object> <attr>` — `DELETE /v1/objects/{slug}/attributes/{attr}`

### Lists
- `list list <object>` — `GET /v1/objects/{slug}/lists` `[--include-attributes]`
- `list get <id>` — `GET /v1/lists/{id}`
- `list create <object>` — `POST /v1/objects/{slug}/lists` `--name --slug [--description]`
- `list delete <id>` — `DELETE /v1/lists/{id}`
- `list records <id>` — `GET /v1/lists/{id}/records` `[--limit --offset --sort]`
- `list add-member <id>` — `POST /v1/lists/{id}/records` `--data <json>`
- `list upsert-member <id>` — `POST /v1/lists/{id}/records/upsert` `--match --data`
- `list update-record <id> <record>` — `PATCH /v1/lists/{id}/records/{record}` `--data`
- `list remove-record <id> <record>` — `DELETE /v1/lists/{id}/records/{record}`

### Other
- `insight list` — `GET /v1/insights` `[--last --from --to --limit]`
- `graph` — `GET /v1/graph` `[--limit --out --no-open]`
- `config show|set|path` — Configuration management
- `session list|clear` — Session management
- `detect` — Platform detection

---

## P1: UI Render Functions

### Table Render
Port from `wuphf-cli/ts/src/ui/table.ts` (100 lines).
- Column interface: `{title, width}`
- `autoSize()` — intelligent column width from data
- `renderTable()` — bordered table with alternating row styles
- Multi-byte text support, max width constraints
- **Go target:** `internal/tui/table.go` using lipgloss

### Taskboard Render
Port from `wuphf-cli/ts/src/ui/taskboard.ts` (218 lines).
- 3-column kanban: To Do (○), In Progress (◐), Done (●)
- Task cards with priority badges: !!! (urgent), !! (high), ! (medium)
- Shows: title, record reference, due date
- **Go target:** `internal/tui/taskboard.go`

### Insights Render
Port from `wuphf-cli/ts/src/ui/insights.ts` (70 lines).
- Priority badges: [CRIT] red, [HIGH] yellow, [MED] blue, [LOW] gray
- Dual format: new API (type/content/confidence) + legacy (title/body/priority)
- Truncated body, target hints, timestamps
- **Go target:** `internal/tui/insights.go`

### Timeline Render
Port from `wuphf-cli/ts/src/ui/timeline.ts` (80 lines).
- Event icons: ● created, ◆ updated, ✕ deleted, ✎ note, ☐ task, ⇄ relationship
- Vertical connectors (│) between events
- Timestamp + actor attribution
- **Go target:** `internal/tui/timeline.go`

---

## P2: Chat System (Phase 3 from context.md)

### Channel Model
- Channel types: direct (1:1), group, public
- Persistence: `~/.wuphf/chat/channels.json`
- Message store: JSONL per channel
- 5-minute message grouping window

### Message Routing
- @mention parsing for agent targeting
- Channel topic-based routing
- System messages for lifecycle events (agent join/leave/phase change)

### Suggested Responses
- AI-generated reply suggestions based on conversation context

---

## P2: Calendar System (Phase 5 from context.md)

### Cron Scheduling
- Per-agent heartbeat schedules (daily, hourly, Nh, 5-field cron)
- Heartbeat triggers agent wake-up cycle
- Calendar store: JSON persistence

### Week Grid View
- 7-day grid showing scheduled heartbeats
- Left/right navigation for week offset
- Agent-colored event markers

---

## P2: Generative TUI (Phase 6 from context.md)

### A2UI Component Types
- row, column, card, text, textfield, list, table, progress, spacer
- JSON Pointer data binding (RFC 6901)
- Schema validation with error messages
- Streaming data model updates (set, merge, delete)

### Agent → UI Pipeline
- Agents emit JSON schemas as tool results
- Renderer creates Bubbletea components from schema
- Live data binding updates components in real-time

---

## P2: Graph Visualization

Port from `wuphf-cli/ts/src/ui/graph.ts` (595 lines).

### ASCII Graph Layout
- Bresenham-style line drawing with Unicode box-drawing characters
- Node icons by entity type: 👤 person, 🏢 company, 💰 deal, ☑ task, 📝 note, ✉ email, 📅 event, 📦 product, 📋 project, 🎫 ticket, 📍 location
- Force-directed positioning with collision detection
- Legend rendering with auto-detected node types
- 500+ lines of layout + collision detection logic
- **Go target:** `internal/tui/graph.go` — consider shared Canvas type with banner

---

## P2: Platform Plugin System

### 6-Layer Setup Hierarchy
1. **Hooks** — Event-driven scripts (UserPromptSubmit, Stop, SessionStart)
2. **Plugins** — Full tool implementations (OpenCode, VS Code Agent)
3. **Agents** — Custom agent modes (Kilo Code)
4. **Workflows** — Multi-step automations (Windsurf)
5. **Rules** — Instruction files (.cursor/rules, .clinerules, etc.)
6. **MCP** — Model Context Protocol server integration

### Platform Adapters (12 platforms)
- Claude Code: 3 hooks
- Cursor: 3 hooks + rules + MCP
- Windsurf: 2 hooks + 3 workflows + rules + MCP
- Cline: 3 hooks + rules + MCP
- OpenCode: plugin.ts + AGENTS.md + MCP
- VS Code: @wuphf agent + instructions + MCP
- Kilo Code: custom mode + rules + MCP
- Continue.dev: rules + MCP
- Zed: .rules + MCP
- Claude Desktop: MCP only
- Aider: CONVENTIONS.md only
- OpenClaw: 49-tool plugin

---

## P2: Integration Management

### Commands
- `integrate list` — List available + connected integrations
- `integrate connect <name>` — Open OAuth URL in browser
- `integrate disconnect <connection-id>` — Remove connection

### Supported Integrations
- Gmail, Google Calendar, Outlook, Outlook Calendar
- Slack
- Salesforce, HubSpot, Attio

### OAuth Flow
- API returns OAuth URL → open in browser → user authorizes → API receives callback
- Connection status polling after redirect
