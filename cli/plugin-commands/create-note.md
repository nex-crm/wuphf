---
description: Create a new note
---
Create a note: $ARGUMENTS

If no title provided, respond: "Usage: /create-note <title> [--content <body>] [--entity <entity_id>]"

Parse the title and optional content/entity from arguments.
Use the `mcp__nex__create_note` tool with the parsed parameters.
Confirm creation with the note ID and details.
