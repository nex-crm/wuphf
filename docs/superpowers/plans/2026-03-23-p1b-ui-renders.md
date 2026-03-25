# P1B: UI Render Functions — Implementation Plan

> **For agentic workers:** Use TeamCreate for parallel execution.

**Goal:** Implement 4 visual render functions (table, timeline, taskboard, insights) and wire them into existing commands.

**Architecture:** Pure functions in `internal/tui/render/` package. Each takes structured data, returns lipgloss-styled string.

**Tech Stack:** Go 1.24+, lipgloss

---

## Task 1: Shared Styles + Table Render

**Files:**
- Create: `internal/tui/render/styles.go`
- Create: `internal/tui/render/table.go`
- Create: `internal/tui/render/table_test.go`

Implement shared styles (header, muted, alternating row colors) using lipgloss.

Implement `RenderTable(headers []string, rows [][]string, maxWidth int) string`:
- Auto-size columns from data (max of header width and max cell width per column)
- Clamp to maxWidth, truncate cells if needed
- Header: bold + underlined
- Alternating row colors
- Footer: "N rows"
- Handle empty rows gracefully

Tests: verify header presence, row count, column alignment, empty table.

Commit: `"feat: add table render with auto-sized columns"`

---

## Task 2: Timeline Render

**Files:**
- Create: `internal/tui/render/timeline.go`
- Create: `internal/tui/render/timeline_test.go`

Implement `RenderTimeline(events []TimelineEvent) string`:

```go
type TimelineEvent struct {
    Type      string // "created", "updated", "deleted", "note", "task", "relationship"
    Timestamp string
    Actor     string
    Content   string
}
```

- Map type → icon: created=●, updated=◆, deleted=✕, note=✎, task=☐, relationship=⇄
- Vertical connectors `│` between events
- Timestamp (dimmed) + actor + content
- Truncate content to 100 chars

Tests: verify icons, connectors, truncation.

Commit: `"feat: add timeline render with event icons"`

---

## Task 3: Taskboard Render

**Files:**
- Create: `internal/tui/render/taskboard.go`
- Create: `internal/tui/render/taskboard_test.go`

Implement `RenderTaskboard(tasks []TaskCard, width int) string`:

```go
type TaskCard struct {
    Title    string
    Priority string // "urgent", "high", "medium", "low", ""
    Status   string // "todo", "in_progress", "done"
    Due      string
    Ref      string // entity reference
}
```

- 3 columns: To Do (○), In Progress (◐), Done (●)
- Priority badges: !!! red, !! yellow, ! blue, · gray
- Column headers with icons + colors
- Task cards show title, priority badge, due date
- Equal column widths based on terminal width

Tests: verify 3 columns, priority badges, card content.

Commit: `"feat: add taskboard kanban render"`

---

## Task 4: Insights Render

**Files:**
- Create: `internal/tui/render/insights.go`
- Create: `internal/tui/render/insights_test.go`

Implement `RenderInsights(insights []Insight) string`:

```go
type Insight struct {
    Priority string // "critical", "high", "medium", "low"
    Category string
    Title    string
    Body     string
    Target   string
    Time     string
}
```

- Priority badges: [CRIT] red bold, [HIGH] yellow, [MED] blue, [LOW] gray
- Category in brackets
- Title bold + body truncated to 120 chars
- Target hints dimmed
- Timestamp dimmed

Tests: verify badges, truncation, category display.

Commit: `"feat: add insights render with priority badges"`

---

## Task 5: Wire Renders Into Commands

**Files:**
- Modify: `internal/commands/cmd_objects.go` (use RenderTable for list)
- Modify: `internal/commands/cmd_records.go` (use RenderTable for list, RenderTimeline for timeline)
- Modify: `internal/commands/cmd_tasks.go` (use RenderTable for list, RenderTaskboard for --board)
- Modify: `internal/commands/cmd_notes.go` (use RenderTable for list)
- Modify: `internal/commands/cmd_rels.go` (use RenderTable for list-defs)
- Modify: `internal/commands/cmd_system.go` (use RenderInsights for insights)

For each command that returns a list:
1. Parse API response into structured data
2. Extract relevant fields for table headers/rows
3. Call render function
4. Output via ctx.AddMessage

Add `--board` flag to task list for kanban view.

Run go test ./... && go build ./cmd/wuphf.

Commit: `"feat: wire render functions into list commands"`

---

## Task 6: Termwright E2E + Final Verification

- Run `bash tests/uat/run-e2e.sh` — all pass
- Run `go test ./...` — all pass
- Push to remote
