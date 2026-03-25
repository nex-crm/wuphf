---
description: View records in a list
---
Show list members: $ARGUMENTS

If no list ID provided, respond: "Usage: /list-members <list_id>"

Use the `mcp__nex__list_list_records` tool with the provided list_id.

Format results as a table showing:
- Record name/title
- Key attributes
- Date added to list

If no members found, respond: "This list has no members."
