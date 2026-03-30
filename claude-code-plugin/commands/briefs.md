---
description: List entity briefs and workspace playbooks, or view a specific brief
---
Handle brief/playbook requests based on $ARGUMENTS:

**No arguments → list all briefs:**
Use the `list_briefs` MCP tool. Display results as a table:
| Title | Type | Updated | Insights |
Show scope_type 1 as "Entity Brief" and scope_type 2 as "Workspace Playbook".

**Entity name → find and show brief:**
First use `search_entities` to find the entity by name. Then use `get_entity_brief` with the context_id. Display the full markdown content.

**"workspace" or "playbooks" → list workspace playbooks only:**
Use `list_briefs` with scope_type=2. Display as table.

**Playbook slug → show workspace playbook:**
Use `get_workspace_playbook` with the slug. Display the full markdown content.

**"compile <entity>" → trigger compilation:**
Search for the entity, then use `compile_brief` with the context_id. Report the result.

**"download <id>" → download as markdown:**
Use `download_brief` with the ID. Save to a file or display the raw markdown.

**"history <id>" → show version history:**
Use `get_brief_history` with the ID. Display events with diff summaries.
