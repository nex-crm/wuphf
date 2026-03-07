---
description: Create a new task
---
Create a task: $ARGUMENTS

If no title provided, respond: "Usage: /create-task <title> [--due <date>] [--priority <low|medium|high>] [--entity <entity_id>]"

Parse the title and optional flags from arguments.
Use the `mcp__nex__create_task` tool with the parsed parameters.
Confirm creation with the task ID and details.
