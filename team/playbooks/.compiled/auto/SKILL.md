---
name: auto
description: 1. Pull the account's ARR.
allowed-tools: team_wiki_read, playbook_list, playbook_execution_record
source_path: team/playbooks/auto.md
compiled_by: archivist
---

# Playbook: Churn prevention

You are executing the **Churn prevention** playbook.

## How to run

1. Call `team_wiki_read` with `article_path="team/playbooks/auto.md"` to load the canonical playbook body.
2. Parse the "What to do" section (or the body if no section header exists) and execute each step using the MCP tools you already have access to. Do NOT invent steps the playbook does not contain.
3. When the execution finishes (success, partial success, or aborted), call `playbook_execution_record` with:
   - `slug`: `auto`
   - `outcome`: `success` | `partial` | `aborted`
   - `summary`: one paragraph describing what actually happened and what you changed.
   - `notes` (optional): anything the next runner should know that is not already captured by the playbook text.

## Guarantees

- The execution log at `team/playbooks/auto.executions.jsonl` is append-only — wrong outcomes are corrected by adding a new entry, never by editing or deleting an existing one.
- This skill recompiles automatically whenever the source playbook changes. Do not edit `SKILL.md` directly; edit `team/playbooks/auto.md` instead.

## Source

The canonical playbook lives at `team/playbooks/auto.md`. This file is a deterministic compilation — see `internal/team/playbook_compiler.go`.
