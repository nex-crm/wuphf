# Progress: Nex Surfaces Development

## Completed

### v0.1.12 — Setup UX + Integrate Simplification (2026-03-07)
- [x] `nex setup`: 3-option key regeneration (reuse email / change email / keep)
- [x] Email persisted in `~/.nex/config.json`
- [x] Status integrations fetch timeout: 120s -> 5s
- [x] `nex integrate connect <name>` — single command (gmail, slack, etc.)
- [x] Removed `connect <type> <provider>` syntax
- [x] README, slash commands, handoff docs updated
- [x] Published to npm

### v0.1.11 — API Parity + Setup Fix (2026-03-07)
- [x] OpenClaw plugin: 42 new tools (49 total, full API coverage)
- [x] Claude Code plugin: 20 new slash commands (26 total)
- [x] SKILL.md: Integrations section added
- [x] `nex setup`: Key regeneration added
- [x] Config fix: `baseUrl` now properly resolves from plugin config
- [x] Published to npm
- [x] PRs #25, #26, #27 merged

### v0.1.10 — Developer API OAuth (2026-03-06)
- [x] CLI: `nex integrate list|connect|disconnect` commands
- [x] MCP: 4 integration tools
- [x] OpenClaw: 3 integration tools (list/connect/disconnect)
- [x] Claude Code: `/integrate` slash command
- [x] Registration endpoint fix (no duplicate workspaces)
- [x] Published to npm

### v0.1.x — Foundation (earlier sessions)
- [x] OpenClaw plugin: base 7 tools (ask, remember, search, etc.)
- [x] Claude Code plugin: hooks (UserPromptSubmit + Stop) + MCP server
- [x] CLI: core commands (recall, remember, search, entities, register, setup, scan)
- [x] SKILL.md: comprehensive API documentation
- [x] File scanner with .nex.toml config
- [x] Platform detection (10+ platforms)

## API Coverage Matrix (Current)

| Surface | Tools/Commands | Coverage |
|---------|---------------|----------|
| CLI | 15+ commands | Full |
| MCP | 38+ tools | Full |
| OpenClaw Plugin | 49 tools | Full |
| Claude Code Plugin | 26 slash commands | Full |
| SKILL.md | All API groups | Full |
