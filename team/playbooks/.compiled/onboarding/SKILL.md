---
name: onboarding
description: Compiled playbook skill.
allowed-tools: team_wiki_read, playbook_list, playbook_execution_record
source_path: team/playbooks/onboarding.md
compiled_by: archivist
---

# Playbook: Onboarding

You are executing the **Onboarding** playbook.

## How to run

1. Call `team_wiki_read` with `article_path="team/playbooks/onboarding.md"` to load the canonical playbook body.
2. Parse the "What to do" section (or the body if no section header exists) and execute each step using the MCP tools you already have access to. Do NOT invent steps the playbook does not contain.
3. When the execution finishes (success, partial success, or aborted), call `playbook_execution_record` with:
   - `slug`: `onboarding`
   - `outcome`: `success` | `partial` | `aborted`
   - `summary`: one paragraph describing what actually happened and what you changed.
   - `notes` (optional): anything the next runner should know that is not already captured by the playbook text.

## Guarantees

- The execution log at `team/playbooks/onboarding.executions.jsonl` is append-only — wrong outcomes are corrected by adding a new entry, never by editing or deleting an existing one.
- This skill recompiles automatically whenever the source playbook changes. Do not edit `SKILL.md` directly; edit `team/playbooks/onboarding.md` instead.

## Source

The canonical playbook lives at `team/playbooks/onboarding.md`. This file is a deterministic compilation — see `internal/team/playbook_compiler.go`.
