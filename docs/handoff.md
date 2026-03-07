# Handoff: API Parity + CLI v0.1.11

## What Was Done (Session 2026-03-07)

### Full API Parity Across All Nex Surfaces

All Developer API operations are now available on every surface:

1. **OpenClaw Plugin** (`openclaw-plugin/src/index.ts`): 42 new tools added (49 total)
   - Schema: list/get/create/update/delete objects + create/update/delete attributes
   - Records: create/upsert/list/get/update/delete + timeline
   - Search: search_records
   - Relationships: list/create/delete defs + create/delete instances
   - Lists: create/get/delete/list + add/upsert/list/update/remove members
   - Tasks: create/list/get/update/delete
   - Notes: create/list/get/update/delete
   - Context: artifact_status, insights
   - Added `patch()` and `put()` methods to NexClient
   - Fixed `baseUrl` config resolution bug (was ignoring plugin config)

2. **Claude Code Plugin** (`cli/plugin-commands/`): 20 new slash commands (26 total)
   - schema, create-object, add-field, update-field
   - search, record, create-record, update-record, upsert-record, timeline
   - tasks, create-task, notes, create-note
   - relationships, link-records, lists, list-members
   - insights, artifact

3. **SKILL.md**: Added Integrations section (Search already existed)

### Setup Key Regeneration Fix

`nex setup` now:
- Shows masked key + workspace when key exists
- Asks "Generate a new API key? (picks up latest scopes)"
- Prompts for email + name if regenerating (allows changing email)
- Uses extracted `registerAndPersist()` helper

### Published `@nex-ai/nex@0.1.11` to npm

## PRs (All Merged)

| PR | Branch | Content |
|----|--------|---------|
| #25 | `feat/developer-api-oauth` | OAuth integration + OpenClaw tools + SKILL.md |
| #26 | `feat/api-parity` | 20 Claude Code slash commands |
| #27 | `fix/setup-key-regeneration` | Setup key regeneration fix |

Core PR #669 (`nazz/feat/developer-api-oauth`) also merged - adds integration endpoints + scopes.

## Current State

- **Branch**: `main` (up to date with all merged PRs)
- **Uncommitted**: `cli/package.json` version bump `0.1.10` -> `0.1.11` (published to npm, needs commit)
- **npm**: `@nex-ai/nex@0.1.11` is live

## Pending Manual Tests

1. `nex setup` -> regenerate key -> `nex integrate list` works (no 403)
2. Slash commands appear after `nex setup` in Claude Code
3. All new OpenClaw tools work with a live API

## Key Files

| File | Purpose |
|------|---------|
| `openclaw-plugin/src/index.ts` | 49 tools (main plugin file) |
| `openclaw-plugin/src/nex-client.ts` | HTTP client with get/post/patch/put/delete |
| `openclaw-plugin/src/config.ts` | Config resolution (fixed baseUrl bug) |
| `cli/src/commands/setup.ts` | Key regeneration logic |
| `cli/plugin-commands/*.md` | 26 slash commands |
| `SKILL.md` | Full API docs for OpenClaw surface |
| `mcp/src/tools/*.ts` | 38+ MCP tools (reference, no changes) |
