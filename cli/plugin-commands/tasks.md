---
description: List tasks, optionally filtered
---
List tasks: $ARGUMENTS

Use the `mcp__nex__list_tasks` tool. If $ARGUMENTS is provided, use it as a search filter.

Format each task as:
- [ ] or [x] **Title** (Priority: X, Due: date)
  - Status, assignee if set
  - Brief description if available

If no tasks found, respond: "No tasks found."
