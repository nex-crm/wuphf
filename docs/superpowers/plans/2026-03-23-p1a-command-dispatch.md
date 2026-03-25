# P1A: Full Command Dispatch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement all 55+ CLI commands in the Go Bubbletea TUI, matching full TS CLI parity.

**Architecture:** Split commands into 8 group files under `internal/commands/`. Each file implements one category of commands using a shared helpers module. All commands follow the same pattern: parse args → call API → format output → ctx.AddMessage.

**Tech Stack:** Go 1.24+, existing api.Client (GET/POST/PATCH/DELETE already implemented)

**Spec:** `docs/superpowers/specs/2026-03-23-p1a-full-command-dispatch.md`

---

## Task 1: Shared Helpers + Refactor slash.go

**Files:**
- Create: `internal/commands/helpers.go`
- Create: `internal/commands/helpers_test.go`
- Modify: `internal/commands/slash.go` (move existing command implementations to group files)

- [ ] **Step 1: Create helpers.go with parseFlags, parseData, formatTable**

```go
// internal/commands/helpers.go
package commands

import (
    "encoding/json"
    "fmt"
    "strings"
)

// parseFlags splits "pos1 pos2 --key value --bool" into positional args and named flags.
// Handles quoted values: --data '{"key":"value"}'
func parseFlags(args string) (positional []string, flags map[string]string) {
    flags = make(map[string]string)
    tokens := tokenize(args)
    for i := 0; i < len(tokens); i++ {
        if strings.HasPrefix(tokens[i], "--") {
            key := strings.TrimPrefix(tokens[i], "--")
            if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") {
                flags[key] = tokens[i+1]
                i++
            } else {
                flags[key] = "true"
            }
        } else {
            positional = append(positional, tokens[i])
        }
    }
    return
}

// tokenize splits on whitespace, respecting single-quoted strings.
func tokenize(s string) []string {
    var tokens []string
    var current strings.Builder
    inQuote := false
    for _, r := range s {
        switch {
        case r == '\'' && !inQuote:
            inQuote = true
        case r == '\'' && inQuote:
            inQuote = false
        case r == ' ' && !inQuote:
            if current.Len() > 0 {
                tokens = append(tokens, current.String())
                current.Reset()
            }
        default:
            current.WriteRune(r)
        }
    }
    if current.Len() > 0 {
        tokens = append(tokens, current.String())
    }
    return tokens
}

// parseData extracts --data flag and parses as JSON map.
func parseData(flags map[string]string) (map[string]any, error) {
    raw, ok := flags["data"]
    if !ok {
        return nil, fmt.Errorf("--data flag required")
    }
    var data map[string]any
    if err := json.Unmarshal([]byte(raw), &data); err != nil {
        return nil, fmt.Errorf("invalid JSON in --data: %w", err)
    }
    return data, nil
}

// formatTable renders headers + rows as aligned text.
func formatTable(headers []string, rows [][]string) string {
    if len(rows) == 0 {
        return "(no results)"
    }
    // Calculate column widths
    widths := make([]int, len(headers))
    for i, h := range headers {
        widths[i] = len(h)
    }
    for _, row := range rows {
        for i, cell := range row {
            if i < len(widths) && len(cell) > widths[i] {
                widths[i] = cell
            }
        }
    }
    // Render
    var sb strings.Builder
    for i, h := range headers {
        sb.WriteString(fmt.Sprintf("%-*s  ", widths[i], h))
    }
    sb.WriteString("\n")
    for i := range headers {
        sb.WriteString(strings.Repeat("─", widths[i]))
        sb.WriteString("  ")
    }
    sb.WriteString("\n")
    for _, row := range rows {
        for i, cell := range row {
            if i < len(widths) {
                sb.WriteString(fmt.Sprintf("%-*s  ", widths[i], cell))
            }
        }
        sb.WriteString("\n")
    }
    return sb.String()
}

// formatJSON pretty-prints any value.
func formatJSON(data any) string {
    b, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return fmt.Sprintf("%v", data)
    }
    return string(b)
}

// getFlag returns a flag value or empty string.
func getFlag(flags map[string]string, key string) string {
    return flags[key]
}

// getFlagOr returns a flag value or the default.
func getFlagOr(flags map[string]string, key, def string) string {
    if v, ok := flags[key]; ok {
        return v
    }
    return def
}
```

- [ ] **Step 2: Create helpers_test.go**

Test parseFlags with various inputs (simple, quoted JSON, boolean flags, no flags).
Test tokenize with quoted strings.
Test parseData with valid/invalid JSON.

- [ ] **Step 3: Move existing commands from slash.go into group files**

Move cmdAsk/cmdSearch/cmdRemember → cmd_ai.go
Move cmdAgents/cmdAgent → cmd_agents.go
Move cmdHelp/cmdClear/cmdQuit/cmdInit/cmdProvider → cmd_system.go
Move cmdObjects/cmdRecords/cmdGraph/cmdInsights → their respective files

Keep slash.go as just RegisterAllCommands().

- [ ] **Step 4: Run tests, commit**

Run: `go test ./internal/commands/ -v && go test ./...`
Commit: `"refactor: split commands into group files, add shared helpers"`

---

## Task 2: Object Commands (5 commands)

**Files:**
- Create: `internal/commands/cmd_objects.go`
- Test: `internal/commands/cmd_objects_test.go`

Implement:
- `object list [--include-attributes]` — GET /v1/objects
- `object get <slug>` — GET /v1/objects/{slug}
- `object create --name <n> --slug <s> [--type <t>] [--description <d>]` — POST /v1/objects
- `object update <slug> [--name] [--description] [--name-plural]` — PATCH /v1/objects/{slug}
- `object delete <slug>` — DELETE /v1/objects/{slug}

Pattern: single `cmdObject(ctx, args)` function with subcommand dispatch (first positional arg).

- [ ] **Step 1: Implement cmdObject with all 5 subcommands**
- [ ] **Step 2: Add tests for subcommand parsing and edge cases**
- [ ] **Step 3: Register in slash.go, run tests, commit**

Commit: `"feat: implement object CRUD commands (list/get/create/update/delete)"`

---

## Task 3: Record Commands (7 commands)

**Files:**
- Create: `internal/commands/cmd_records.go`
- Test: `internal/commands/cmd_records_test.go`

Implement:
- `record list <type> [--limit N] [--offset N] [--sort <field>]` — GET /v1/records?object_type=...
- `record get <type> <id>` — GET /v1/records/{type}/{id}
- `record create <type> --data <json>` — POST /v1/records/{type}
- `record upsert <type> --match <attr> --data <json>` — POST /v1/records/{type}/upsert
- `record update <type> <id> --data <json>` — PATCH /v1/records/{type}/{id}
- `record delete <type> <id>` — DELETE /v1/records/{type}/{id}
- `record timeline <type> <id> [--limit N] [--cursor C]` — GET /v1/records/{type}/{id}/timeline

Pattern: single `cmdRecord(ctx, args)` with subcommand dispatch.

- [ ] **Step 1: Implement cmdRecord with all 7 subcommands**
- [ ] **Step 2: Add tests**
- [ ] **Step 3: Register in slash.go, run tests, commit**

Commit: `"feat: implement record CRUD + timeline commands"`

---

## Task 4: Note + Task Commands (10 commands)

**Files:**
- Create: `internal/commands/cmd_notes.go`
- Create: `internal/commands/cmd_tasks.go`

Implement notes:
- `note list [--entity <id>]` — GET /v1/notes
- `note get <id>` — GET /v1/notes/{id}
- `note create --title <t> [--content <c>] [--entity <id>]` — POST /v1/notes
- `note update <id> [--title] [--content] [--entity]` — PATCH /v1/notes/{id}
- `note delete <id>` — DELETE /v1/notes/{id}

Implement tasks:
- `task list [--entity] [--assignee] [--search] [--completed bool] [--limit N]` — GET /v1/tasks
- `task get <id>` — GET /v1/tasks/{id}
- `task create --title <t> [--description] [--priority] [--due] [--entities] [--assignees]` — POST /v1/tasks
- `task update <id> [--title] [--description] [--priority] [--due] [--completed bool]` — PATCH /v1/tasks/{id}
- `task delete <id>` — DELETE /v1/tasks/{id}

- [ ] **Step 1: Implement cmdNote and cmdTask**
- [ ] **Step 2: Register in slash.go, run tests, commit**

Commit: `"feat: implement note and task CRUD commands"`

---

## Task 5: Relationship + Attribute Commands (8 commands)

**Files:**
- Create: `internal/commands/cmd_rels.go`
- Create: `internal/commands/cmd_attrs.go`

Implement relationships:
- `rel list-defs` — GET /v1/relationships/definitions
- `rel create-def --type <t> --entity1 <e1> --entity2 <e2> [--pred12] [--pred21]` — POST
- `rel delete-def <id>` — DELETE /v1/relationships/definitions/{id}
- `rel create --def <id> --entity1 <id> --entity2 <id>` — POST /v1/relationships
- `rel delete <id>` — DELETE /v1/relationships/{id}

Implement attributes:
- `attribute create <object> --name <n> --slug <s> --type <t> [--description] [--options <json>]`
- `attribute update <object> <attr> [--name] [--description] [--options]`
- `attribute delete <object> <attr>`

- [ ] **Step 1: Implement cmdRel and cmdAttribute**
- [ ] **Step 2: Register in slash.go, run tests, commit**

Commit: `"feat: implement relationship and attribute commands"`

---

## Task 6: List Commands (8 commands)

**Files:**
- Create: `internal/commands/cmd_lists.go`

Implement:
- `list list <object> [--include-attributes]` — GET /v1/objects/{slug}/lists
- `list get <id>` — GET /v1/lists/{id}
- `list create <object> --name <n> --slug <s> [--description]` — POST /v1/objects/{slug}/lists
- `list delete <id>` — DELETE /v1/lists/{id}
- `list records <id> [--limit] [--offset] [--sort]` — GET /v1/lists/{id}/records
- `list add-member <id> --data <json>` — POST /v1/lists/{id}/records
- `list upsert-member <id> --match <attr> --data <json>` — POST /v1/lists/{id}/records/upsert
- `list remove-record <id> <record-id>` — DELETE /v1/lists/{id}/records/{record-id}

- [ ] **Step 1: Implement cmdList with all 8 subcommands**
- [ ] **Step 2: Register in slash.go, run tests, commit**

Commit: `"feat: implement list management commands"`

---

## Task 7: Config, System & Graph Commands

**Files:**
- Create: `internal/commands/cmd_config.go`
- Modify: `internal/commands/cmd_system.go` (update help to show all commands)

Implement config:
- `config show` — show resolved config (masked key, workspace, provider, pack)
- `config set <key> <value>` — set config value
- `config path` — print config file path

Implement detect:
- `detect` — list installed AI coding platforms (check for claude, cursor, windsurf, etc.)

Implement sessions:
- `session list` — list stored sessions
- `session clear` — clear all sessions

Update help:
- Group all 55+ commands by category
- Show command + description for each

Update graph/insights:
- `graph` — call GET /v1/graph, format output
- `insight list [--last <duration>] [--limit N]` — call GET /v1/insights

- [ ] **Step 1: Implement cmd_config.go with config/detect/session commands**
- [ ] **Step 2: Update cmdHelp in cmd_system.go with full grouped help**
- [ ] **Step 3: Implement real graph and insights commands**
- [ ] **Step 4: Register all, run tests, commit**

Commit: `"feat: implement config, detect, session, graph, insights commands"`

---

## Task 8: Wire All + Update Autocomplete + Final Integration

**Files:**
- Modify: `internal/commands/slash.go` (ensure all commands registered)
- Modify: `internal/tui/stream.go` (update defaultSlashCommands for autocomplete)

- [ ] **Step 1: Verify all commands registered in RegisterAllCommands**

Ensure slash.go registers all 55+ commands including the new subcommand dispatchers.

- [ ] **Step 2: Update defaultSlashCommands in stream.go**

Add all new top-level commands to the autocomplete list: object, record, note, task, rel, attribute, list, config, detect, session.

- [ ] **Step 3: Run full test suite**

Run: `go test ./... && go build -o wuphf ./cmd/wuphf`

- [ ] **Step 4: Run termwright E2E**

Run: `bash tests/uat/run-e2e.sh`

- [ ] **Step 5: Commit and push**

Commit: `"feat: wire all 55+ commands, update autocomplete and help"`
Push: `git push origin nazz/experiment/multi-agent-cli`

---

## Task 9: Persona-Based Termwright E2E Tests

**Files:**
- Create: `tests/uat/persona-founder.sh`
- Create: `tests/uat/persona-sdr.sh`
- Create: `tests/uat/persona-dev.sh`
- Create: `tests/uat/persona-cs.sh`
- Create: `tests/uat/persona-marketer.sh`

Each persona script tests their acceptance criteria from the spec using the TUI via termwright raw input. Each test:
1. Types the command
2. Presses Enter
3. Asserts expected output text
4. Takes a screenshot

Note: Commands that require real API data (record create, etc.) will return auth errors without a valid API key. Tests should verify the command is dispatched correctly (shows "Not authenticated" or actual data if key is present).

- [ ] **Step 1: Write 5 persona test scripts**
- [ ] **Step 2: Run each, fix failures**
- [ ] **Step 3: Commit**

Commit: `"test: add persona-based termwright E2E tests for all command groups"`

---

## Task 10: Final Verification

- [ ] **Step 1: `go test ./...` — all pass**
- [ ] **Step 2: `go build -o wuphf ./cmd/wuphf` — clean build**
- [ ] **Step 3: `./wuphf --cmd "help"` — shows all 55+ commands grouped**
- [ ] **Step 4: `bash tests/uat/run-e2e.sh` — all pass**
- [ ] **Step 5: Push to remote**
