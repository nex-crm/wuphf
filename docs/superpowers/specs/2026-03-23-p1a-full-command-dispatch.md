# P1A: Full Command Dispatch — Design Spec

> Port all 55+ commands from the TypeScript CLI to the Go Bubbletea implementation.

**Branch:** `nazz/experiment/multi-agent-cli`
**Date:** 2026-03-23
**Depends on:** Core TUI, Agent Packs, Provider Wiring (all complete)

---

## 1. Personas

| ID | Name | Role | Context |
|----|------|------|---------|
| P1 | **Nazz** | Startup Founder | Runs 10+ concurrent instances. Needs pipeline overview, agent delegation, cross-cutting queries, config management. Power user. |
| P2 | **Sarah** | SDR | Does prospecting. Creates leads, logs call notes, manages follow-up tasks, searches contacts. Speed matters. |
| P3 | **Alex** | Developer | Builds integrations. Manages object schemas, adds attributes, scans project files, checks platform detection. |
| P4 | **Kim** | CS Manager | Manages customer relationships. Views timelines, links records, tracks renewal tasks, reviews notes. |
| P5 | **Jordan** | Content Marketer | Tracks campaigns. Reviews insights, manages content notes, searches context, works with lists. |

---

## 2. Command Inventory

### 2.1 Currently Implemented (21 commands)
ask, search, remember, chat, calendar, orchestration, orch, cal, agents, agent, objects, records, graph, insights, help, clear, quit, q, init, login, provider

### 2.2 Commands to Add (40 new commands)

**Object Management (4 new):**
- `object get <slug>` — GET /v1/objects/{slug}
- `object create --name <n> --slug <s> [--type <t>] [--description <d>]` — POST /v1/objects
- `object update <slug> --name <n> --description <d> --name-plural <p>` — PATCH /v1/objects/{slug}
- `object delete <slug>` — DELETE /v1/objects/{slug}

**Record Management (5 new):**
- `record get <type> <id>` — GET /v1/records/{type}/{id}
- `record create <type> --data <json>` — POST /v1/records/{type}
- `record upsert <type> --match <attr> --data <json>` — POST /v1/records/{type}/upsert
- `record update <type> <id> --data <json>` — PATCH /v1/records/{type}/{id}
- `record delete <type> <id>` — DELETE /v1/records/{type}/{id}
- `record timeline <type> <id> [--limit N] [--cursor C]` — GET /v1/records/{type}/{id}/timeline

**Notes (5 new):**
- `note list [--entity <id>]` — GET /v1/notes
- `note get <id>` — GET /v1/notes/{id}
- `note create --title <t> [--content <c>] [--entity <id>]` — POST /v1/notes
- `note update <id> --title <t> --content <c> --entity <id>` — PATCH /v1/notes/{id}
- `note delete <id>` — DELETE /v1/notes/{id}

**Tasks (5 new):**
- `task list [--entity <id>] [--assignee <a>] [--search <q>] [--completed bool] [--limit N]` — GET /v1/tasks
- `task get <id>` — GET /v1/tasks/{id}
- `task create --title <t> [--description <d>] [--priority <p>] [--due <date>] [--entities <ids>] [--assignees <ids>]` — POST /v1/tasks
- `task update <id> --title <t> --description <d> --priority <p> --due <date> --completed bool` — PATCH /v1/tasks/{id}
- `task delete <id>` — DELETE /v1/tasks/{id}

**Relationships (5 new):**
- `rel list-defs` — GET /v1/relationships/definitions
- `rel create-def --type <t> --entity1 <e1> --entity2 <e2> [--pred12 <p>] [--pred21 <p>]` — POST /v1/relationships/definitions
- `rel delete-def <id>` — DELETE /v1/relationships/definitions/{id}
- `rel create --def <id> --entity1 <id> --entity2 <id>` — POST /v1/relationships
- `rel delete <id>` — DELETE /v1/relationships/{id}

**Attributes (3 new):**
- `attribute create <object> --name <n> --slug <s> --type <t> [--description <d>] [--options <json>]` — POST /v1/objects/{slug}/attributes
- `attribute update <object> <attr> --name <n> --description <d> --options <json>` — PATCH /v1/objects/{slug}/attributes/{attr}
- `attribute delete <object> <attr>` — DELETE /v1/objects/{slug}/attributes/{attr}

**Lists (8 new):**
- `list list <object> [--include-attributes]` — GET /v1/objects/{slug}/lists
- `list get <id>` — GET /v1/lists/{id}
- `list create <object> --name <n> --slug <s> [--description <d>]` — POST /v1/objects/{slug}/lists
- `list delete <id>` — DELETE /v1/lists/{id}
- `list records <id> [--limit N] [--offset N] [--sort <field>]` — GET /v1/lists/{id}/records
- `list add-member <id> --data <json>` — POST /v1/lists/{id}/records
- `list upsert-member <id> --match <attr> --data <json>` — POST /v1/lists/{id}/records/upsert
- `list remove-record <id> <record-id>` — DELETE /v1/lists/{id}/records/{record-id}

**Config & System (4 new):**
- `config show` — show resolved config (masked key, workspace, provider)
- `config set <key> <value>` — set config value
- `config path` — print config file path
- `detect` — detect installed AI platforms
- `scan [dir] [--max-files N] [--force] [--dry-run] [--depth N]` — scan and ingest files
- `session list` — list stored session mappings
- `session clear` — clear all session mappings

### 2.3 Enhanced Existing Commands

- `object list [--include-attributes]` — add flag for full schema
- `record list <type> [--limit N] [--offset N] [--sort <field>]` — add pagination flags
- `help` — update to show all 55+ commands grouped by category

---

## 3. Architecture

### 3.1 File Structure

```
internal/commands/
├── registry.go      # SlashContext, Registry, parsing (existing)
├── dispatch.go      # Non-interactive dispatch (existing)
├── slash.go         # RegisterAllCommands (existing, updated)
├── helpers.go       # NEW: parseFlags, formatTable, formatJSON, parseData
├── cmd_ai.go        # ask, search, remember (moved from slash.go)
├── cmd_objects.go   # NEW: object list/get/create/update/delete
├── cmd_records.go   # NEW: record list/get/create/upsert/update/delete/timeline
├── cmd_notes.go     # NEW: note list/get/create/update/delete
├── cmd_tasks.go     # NEW: task list/get/create/update/delete
├── cmd_rels.go      # NEW: rel list-defs/create-def/delete-def/create/delete
├── cmd_attrs.go     # NEW: attribute create/update/delete
├── cmd_lists.go     # NEW: list list/get/create/delete/records/add-member/upsert-member/remove-record
├── cmd_agents.go    # agents, agent (moved from slash.go)
├── cmd_config.go    # NEW: config show/set/path, detect, scan
├── cmd_system.go    # help, clear, quit, init, provider (moved from slash.go)
└── cmd_graph.go     # graph, insights (stubs → real implementations)
```

### 3.2 Shared Helpers (`helpers.go`)

```go
// parseFlags splits "positional --key value --bool" into parts.
func parseFlags(args string) (positional []string, flags map[string]string)

// parseData extracts --data JSON from flags and returns map[string]any.
func parseData(flags map[string]string) (map[string]any, error)

// formatTable renders a simple ASCII table with headers and rows.
func formatTable(headers []string, rows [][]string) string

// formatJSON pretty-prints any value as indented JSON.
func formatJSON(data any) string

// requireAuth checks API client authentication (existing, moved here).
func requireAuth(ctx *SlashContext) bool
```

### 3.3 API Client Extensions

The existing `api.Client` supports GET, POST. Need to add:
- `api.Patch[T]` — PATCH requests
- `api.Delete` — DELETE requests (no response body)
- Ensure query string params work for GET with filters

### 3.4 Command Dispatch Flow

```
User types: /record create company --data '{"name":"Acme"}'
  ↓
parseSlashInput → name="record", args="create company --data '{...}'"
  ↓
Registry.Get("record") → cmdRecords
  ↓
cmdRecords(ctx, "create company --data '{...}'")
  ↓
Parse subcommand: "create", objectType: "company"
  ↓
parseFlags → flags: {data: '{"name":"Acme"}'}
  ↓
parseData → map[string]any{name: "Acme"}
  ↓
api.Post → /v1/records/company → response
  ↓
ctx.AddMessage("system", formatted output)
```

---

## 4. Acceptance Criteria by Persona

### 4.1 Nazz (Founder) — Pipeline & Overview

| AC# | Scenario | Command | Expected |
|-----|----------|---------|----------|
| N1 | View all object types | `object list` | Shows list of types (company, person, deal, etc.) |
| N2 | View schema with fields | `object list --include-attributes` | Shows types + their attributes |
| N3 | Search across workspace | `search Maria Rodriguez` | Returns matching records with scores |
| N4 | View config | `config show` | Shows masked API key, workspace slug, provider |
| N5 | Get recent insights | `insight list --limit 3` | Shows 3 most recent insights |
| N6 | Quick help | `help` | Shows all commands grouped by category |
| N7 | View record details | `record get company 12345` | Shows full record with attributes |
| N8 | Non-interactive pipeline check | `wuphf --cmd "record list deal --limit 5"` | Top 5 deals, exits cleanly |

### 4.2 Sarah (SDR) — Prospecting & Outreach

| AC# | Scenario | Command | Expected |
|-----|----------|---------|----------|
| S1 | List leads | `record list lead --limit 10` | Shows 10 leads with key fields |
| S2 | Create a lead | `record create lead --data '{"name":"Acme Corp","email":"cto@acme.com"}'` | Creates record, shows ID |
| S3 | Log a call note | `note create --title "Call with Acme CTO" --content "Interested in enterprise"` | Creates note, confirms |
| S4 | Create follow-up task | `task create --title "Follow up Acme" --priority high --due 2026-03-25` | Creates task with due date |
| S5 | View open tasks | `task list --completed false --limit 5` | Shows 5 open tasks |
| S6 | Update lead status | `record update lead 12345 --data '{"stage":"qualified"}'` | Updates record |
| S7 | Search contacts | `search Acme` | Finds all Acme-related records |
| S8 | Upsert lead | `record upsert lead --match email --data '{"email":"cto@acme.com","name":"John"}'` | Creates or updates |

### 4.3 Alex (Developer) — Schema & Integration

| AC# | Scenario | Command | Expected |
|-----|----------|---------|----------|
| A1 | View full schema | `object list --include-attributes` | All types with field definitions |
| A2 | Create custom object | `object create --name "API Key" --slug api-key` | Creates type, shows slug |
| A3 | Add field to object | `attribute create api-key --name "Scope" --slug scope --type text` | Adds attribute |
| A4 | Update field | `attribute update api-key scope --description "OAuth scope string"` | Updates attribute |
| A5 | Delete field | `attribute delete api-key scope` | Removes attribute |
| A6 | List records of custom type | `record list api-key` | Lists records (or empty) |
| A7 | Config path | `config path` | Prints ~/.wuphf/config.json path |
| A8 | Platform detection | `detect` | Shows installed AI platforms |

### 4.4 Kim (CS Manager) — Customers & Relationships

| AC# | Scenario | Command | Expected |
|-----|----------|---------|----------|
| K1 | List customers | `record list customer` | Shows customer records |
| K2 | View timeline | `record timeline customer 12345` | Shows event history |
| K3 | View relationships | `rel list-defs` | Shows relationship definitions |
| K4 | Link records | `rel create --def customer-contact --entity1 12345 --entity2 67890` | Creates link |
| K5 | View notes for customer | `note list --entity 12345` | Notes filtered by entity |
| K6 | Create renewal task | `task create --title "Q4 Renewal" --priority high --entities 12345` | Task linked to entity |
| K7 | Update task status | `task update 99 --completed true` | Marks task complete |
| K8 | Delete relationship | `rel delete 456` | Removes relationship |

### 4.5 Jordan (Content Marketer) — Campaigns & Lists

| AC# | Scenario | Command | Expected |
|-----|----------|---------|----------|
| J1 | View campaign lists | `list list campaign` | Shows lists for campaign type |
| J2 | Get list details | `list get list-123` | Shows list metadata |
| J3 | View list members | `list records list-123 --limit 10` | Shows records in list |
| J4 | Add to list | `list add-member list-123 --data '{"name":"New Lead"}'` | Adds record to list |
| J5 | Create campaign list | `list create campaign --name "Q1 Nurture" --slug q1-nurture` | Creates list |
| J6 | Create content note | `note create --title "Blog Draft: AI in Sales" --content "Outline..."` | Creates note |
| J7 | Search context | `search "content strategy"` | Finds related records |
| J8 | Remove from list | `list remove-record list-123 rec-456` | Removes record from list |

---

## 5. Termwright E2E Tests

### 5.1 Test Strategy

Each persona gets a termwright test script that exercises their primary workflows through the TUI. Tests use `raw` input for Bubbletea compatibility.

Test scripts: `tests/uat/persona-*.sh`

### 5.2 Test Coverage Matrix

| Test File | Persona | Commands Tested |
|-----------|---------|----------------|
| `persona-founder.sh` | Nazz | object list, record list/get, search, config show, insight list, help |
| `persona-sdr.sh` | Sarah | record create/list/update/upsert, note create, task create/list |
| `persona-dev.sh` | Alex | object create, attribute create/update/delete, config path, detect |
| `persona-cs.sh` | Kim | record timeline, rel list-defs/create/delete, note list, task update |
| `persona-marketer.sh` | Jordan | list list/get/records/create/add-member/remove-record, note create |

### 5.3 Non-Interactive Tests

All 55+ commands also tested via `wuphf --cmd "<command>"` for CI coverage in `tests/e2e/commands_test.go`.

---

## 6. Implementation Strategy

### 6.1 Parallel Work Breakdown

Each command group file is independent — perfect for parallel agents:

| Agent | Files | Commands |
|-------|-------|----------|
| Agent 1 | helpers.go, cmd_objects.go | Shared helpers + 5 object commands |
| Agent 2 | cmd_records.go | 7 record commands |
| Agent 3 | cmd_notes.go, cmd_tasks.go | 10 note+task commands |
| Agent 4 | cmd_rels.go, cmd_attrs.go | 8 relationship+attribute commands |
| Agent 5 | cmd_lists.go | 8 list commands |
| Agent 6 | cmd_config.go, cmd_graph.go | Config, detect, scan, graph, insights |
| Integration | slash.go, cmd_system.go, help update | Wire all commands, update help |

### 6.2 API Client Extensions

Before command work begins:
- Add `api.Patch[T]` method
- Add `api.Delete` method
- Verify query parameter support for GET filters

### 6.3 Testing Approach

- Unit tests per command group file
- Integration tests in `tests/e2e/commands_test.go` using mock API
- Termwright E2E scripts per persona

---

## 7. Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| File split | One file per command group | Parallel work, focused files |
| Flag parsing | Custom parseFlags() | Simple, no external deps, matches TS CLI behavior |
| Output format | Text by default, JSON via --format | Matches existing behavior |
| Error handling | ctx.AddMessage("system", error) | Consistent with existing pattern |
| DELETE responses | Show "Deleted." confirmation | No response body from API |
| Autocomplete | Add all new commands to defaultSlashCommands | Immediate discoverability |

---

## 8. Out of Scope

- UI render functions (table, timeline, taskboard) — P1B
- Chat system — P2A
- Calendar/cron — P2B
- Generative TUI — P2C
- Graph visualization — P2D
- Platform plugins — P2E
- Integration OAuth — P2F
