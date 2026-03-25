---
description: Fuzzy keyword search to find specific named records (use /recall for AI-powered queries)
---
Search for: $ARGUMENTS

If no query provided, respond: "Usage: /search <query>"

Use the `mcp__nex__search_records` tool with the query.
Format results grouped by object type. For each result show:
- **Name** (ID: `record_id`) — object type
- Key attributes if available
If no results found, respond: "No records found matching your query."
