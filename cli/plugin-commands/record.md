---
description: Get a record by ID
---
Get record: $ARGUMENTS

If no record ID provided, respond: "Usage: /record <record_id>"

Use the `mcp__nex__get_record` tool with the provided record_id.
Format the record as:
- **Name** (Type: `object_type`)
- All attributes in a readable key-value list
- Show created_at and updated_at timestamps
