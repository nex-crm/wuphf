# P1B: UI Render Functions — Design Spec

> Port 4 visual render functions from kill-saas TS branch to Go + lipgloss.

**Branch:** `nazz/experiment/multi-agent-cli`
**Date:** 2026-03-23
**Source:** `wuphf-cli` repo, branch `nazz/kill-saas`, `ts/src/ui/`

---

## 1. Components

### 1.1 Table Render
**Source:** `ts/src/ui/table.ts` (100 lines)
**Target:** `internal/tui/render/table.go`

Auto-sized column table with:
- Column interface: title + optional fixed width
- `AutoSize()` — calculates widths from data
- Alternating row colors (bright/dim)
- Header: bold, underlined
- Footer: "N rows" count
- Max width constraint (terminal width)
- Multi-byte text support via `utf8.RuneCountInString`

**Used by:** object list, record list, task list, note list, rel list-defs, list records

### 1.2 Timeline Render
**Source:** `ts/src/ui/timeline.ts` (80 lines)
**Target:** `internal/tui/render/timeline.go`

Vertical event history:
- Event icons: ● created, ◆ updated, ✕ deleted, ✎ note, ☐ task, ⇄ relationship
- Vertical connector lines `│` between events
- Timestamp formatting + actor attribution
- Truncated body text

**Used by:** record timeline

### 1.3 Taskboard Render
**Source:** `ts/src/ui/taskboard.ts` (218 lines)
**Target:** `internal/tui/render/taskboard.go`

3-column kanban layout:
- Columns: To Do (○), In Progress (◐), Done (●)
- Column header with icon + color
- Task cards with priority badges: !!! (urgent/red), !! (high/yellow), ! (medium/blue), · (default/gray)
- Shows: title, record reference, due date
- Fixed column widths with wrapping

**Used by:** task list --board

### 1.4 Insights Render
**Source:** `ts/src/ui/insights.ts` (70 lines)
**Target:** `internal/tui/render/insights.go`

Priority-badged insight list:
- Badges: [CRIT] red, [HIGH] yellow, [MED] blue, [LOW] gray
- Category label
- Title (bold) + body (truncated to 120 chars)
- Target hints (entity references)
- Timestamp

**Used by:** insight list

---

## 2. Architecture

```
internal/tui/render/
├── table.go          # RenderTable(headers, rows, maxWidth) string
├── table_test.go
├── timeline.go       # RenderTimeline(events) string
├── timeline_test.go
├── taskboard.go      # RenderTaskboard(tasks, width) string
├── taskboard_test.go
├── insights.go       # RenderInsights(insights) string
├── insights_test.go
└── styles.go         # Shared lipgloss styles for renders
```

Each render function is a pure function: data in, styled string out. No side effects, no state. Uses lipgloss for ANSI styling.

### 2.1 Integration Points

Commands call render functions instead of `formatJSON`:

| Command | Current Output | New Output |
|---------|---------------|------------|
| `object list` | JSON array | Table (Name, Slug, Type) |
| `record list` | JSON array | Table (Name, ID, key fields) |
| `task list` | JSON array | Table (Title, Priority, Due, Status) |
| `task list --board` | JSON array | Taskboard kanban |
| `note list` | JSON array | Table (Title, Entity, Created) |
| `record timeline` | JSON array | Timeline with icons |
| `insight list` | JSON array | Insights with badges |
| `rel list-defs` | JSON array | Table (Type, Entity1, Entity2) |

---

## 3. Testing

- Unit tests per render file with sample data
- Verify output contains expected text, icons, formatting
- Termwright E2E: `/task list` shows table, `/insight list` shows badges
