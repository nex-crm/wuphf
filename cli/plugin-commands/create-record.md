---
description: Create a new record
---
Create a new record: $ARGUMENTS

If insufficient arguments, respond: "Usage: /create-record <object_slug> <field=value> [field=value ...]"
Example: /create-record contacts name="Jane Doe" email="jane@example.com"

Parse the object slug and field key=value pairs from arguments.
Use the `mcp__nex__create_record` tool with object_slug and the attributes object.
Confirm creation with the returned record ID and details.
