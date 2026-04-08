---
description: List agent skills, view a skill, compile, or sync to local folder
---
Handle skill requests based on $ARGUMENTS:

**No arguments -> list all skills:**
Use the `list_skills` MCP tool. Display results as a table with Name (slug), Trigger, Confidence, and Last Updated.

**Skill slug -> show skill:**
Use `get_skill_by_slug` with the slug. Display the full markdown content including action steps, required integrations, and workspace context.

**"sync" -> sync all skills to local .nex/ folder:**
Use `sync_skills`. This downloads all skills as .md files to .nex/skills/ for fast local access by any AI agent.

**"compile" -> trigger skill compilation:**
Use `compile_skills`. This scans playbook rules and generates executable skills grounded to the workspace's tools, team, and CRM schema.

**"read <slug>" -> read from local cache:**
Use `read_skill` with the slug. Reads from .nex/skills/ first, falls back to API.
