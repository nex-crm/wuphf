---
description: Add a field (attribute) to an object type
---
Add a field to an object type: $ARGUMENTS

If insufficient arguments, respond: "Usage: /add-field <object_slug> <field_name> <field_type> [--required] [--label <label>]"

Supported field types: text, number, email, phone, url, date, datetime, boolean, select, multi_select, currency, rich_text.

Parse the object slug, field name, type, and optional flags from arguments.
Use the `mcp__nex__create_attribute` tool with the parsed parameters.
Confirm the field was added successfully.
