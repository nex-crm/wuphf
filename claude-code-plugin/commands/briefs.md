---
description: List entity briefs and workspace playbooks, or view a specific brief
---
Handle brief/playbook requests based on $ARGUMENTS:

**No arguments → list all briefs:**
Use the `list_briefs` MCP tool. Display results as a table with Title, Type (Entity Brief / Workspace Playbook), and Last Updated.

**Entity name → find and show brief:**
First use `search_entities` to find the entity by name. Then use `get_entity_brief` with the context_id. Display the full markdown content.

**"workspace" or "playbooks" → list workspace playbooks only:**
Use `list_briefs` with scope_type=2.

**"sync" → sync all briefs to local .nex/ folder:**
Use `sync_briefs`. This downloads all briefs as .md files for fast local access.

**"compile <entity>" → trigger compilation:**
Search for the entity, then use `compile_brief` with the context_id.

**"history <id>" → show version history:**
Use `get_brief_history` with the ID.
