---
description: List notes, optionally for a specific record
---
List notes: $ARGUMENTS

Use the `mcp__nex__list_notes` tool. If $ARGUMENTS is provided, use it as an entity_id filter to show notes for that specific record.

Format each note as:
- **Title** (ID: `note_id`, Created: date)
  - Content preview (first 100 chars)
  - Linked entity if applicable

If no notes found, respond: "No notes found."
