---
description: Link two records with a relationship
---
Link records: $ARGUMENTS

If insufficient arguments, respond: "Usage: /link-records <definition_id> <source_record_id> <target_record_id>"

Parse the relationship definition ID, source record ID, and target record ID from arguments.
Use the `mcp__nex__create_relationship` tool with the parsed parameters.
Confirm the relationship was created.
