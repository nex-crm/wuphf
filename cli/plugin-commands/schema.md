---
description: View workspace schema (object types and their fields)
---
$ARGUMENTS

If a specific object slug is provided, use the `mcp__nex__get_object` tool with that slug to show its details and attributes.

Otherwise, use `mcp__nex__list_objects` with include_attributes=true to list all object types.

Format each object type as:
- **Object Name** (`slug`) — description
  - Fields: list each attribute with name, type, and whether required
