---
description: Search for entities (people, companies, topics) in Nex knowledge base
---
Search for entities matching: $ARGUMENTS
If no query provided, respond: "Usage: /entities <search query>"

Use the `mcp__nex__query_context` tool with the query.
From the response, extract the `entity_references` array.
If no entities found, respond: "No matching entities found."

Format each entity as a bullet list:
```
Found {count} entities:
- {name} ({type_label}) — {mention_count} mentions
```

Type labels: type "14" → Person, type "15" → Company, all others → Entity.
Only show mention count if available (count > 0).
