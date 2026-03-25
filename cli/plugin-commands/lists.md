---
description: View lists for an object type
---
Show lists for: $ARGUMENTS

If no object slug provided, respond: "Usage: /lists <object_slug>"

Use the `mcp__nex__list_object_lists` tool with the provided object_slug.

Format each list as:
- **List Name** (ID: `list_id`)
  - Description and member count if available

If no lists found, respond: "No lists found for this object type."
