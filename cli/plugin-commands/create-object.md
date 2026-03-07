---
description: Create a new object type definition
---
Create a new object type: $ARGUMENTS

If no name provided, respond: "Usage: /create-object <object name> [--plural <plural name>] [--description <description>]"

Parse the arguments for the object name, optional plural name, and description.
Use the `mcp__nex__create_object` tool with the parsed parameters.
Confirm creation with the returned slug and details.
