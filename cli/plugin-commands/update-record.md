---
description: Update an existing record
---
Update record: $ARGUMENTS

If insufficient arguments, respond: "Usage: /update-record <record_id> <field=value> [field=value ...]"
Example: /update-record rec_123 email="new@example.com" phone="+1234567890"

Parse the record ID and field key=value pairs from arguments.
Use the `mcp__nex__update_record` tool with the record_id and attributes object.
Confirm the update with the returned details.
