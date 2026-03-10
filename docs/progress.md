# Progress: Nex Surfaces Development

## Completed

### v0.1.20 — Full Platform Plugins + 6-Layer Setup Hierarchy (2026-03-10)
- [x] Extracted shared hook logic into `cli/src/plugin/shared.ts` (doRecall, doCapture, doSessionStart)
- [x] Refactored auto-recall.ts, auto-capture.ts, auto-session-start.ts to use shared.ts
- [x] Created 8 platform adapter scripts in `cli/src/plugin/adapters/`:
  - Cursor: session-start, recall, stop
  - Windsurf: recall, capture
  - Cline: recall, task-start, capture
- [x] Created `cli/platform-plugins/` with 7 template files:
  - opencode-plugin.ts, vscode-agent.md, kilocode-modes.yaml, continue-provider.ts
  - windsurf-workflows: nex-ask.md, nex-remember.md, nex-search.md
- [x] Updated platform-detect.ts: added supportsHooks, supportsCustomTools, supportsCustomAgents, supportsWorkflows, hookConfigPath; added OpenClaw + Aider platforms (12 total)
- [x] Updated installers.ts: 7 new installer functions (hooks, plugins, agents, workflows per platform)
- [x] Updated setup.ts: 6-layer hierarchy (Hooks → Plugins → Agents → Workflows → Rules → MCP)
- [x] Added `--no-hooks` flag to setup
- [x] Fixed name prompt bug: skip name prompt when regenerating API key for same email
- [x] Added `platform-plugins` and `platform-rules` to package.json files array
- [x] Updated CLI README with expanded 12-platform integration table
- [x] Multi-agent team build: 4 parallel agents (cursor-hooks, windsurf-hooks, cline-hooks, platform-templates)
- [x] Build passes, 65 tests pass, all adapter scripts exit 0
- [x] PR #31 merged, published to npm as `@nex-ai/nex@0.1.20`

### v0.1.18 — TUI Polish + Integration Fixes (2026-03-09)
- [x] Interactive integrate list with arrow-key selection, expand/collapse, inline actions
- [x] Connection ID handling: `safenIds()` + `string | number` types
- [x] Optimistic disconnect: removes connection from in-memory list (avoids read-replica lag)
- [x] Meeting bot support
- [x] Cherry-picked all feature branch commits to main
- [x] PR #30 merged, published to npm

### v0.1.16-v0.1.17 — TTY + Actions (2026-03-08)
- [x] TTY detection and formatting
- [x] Integration actions (connect/disconnect/reconnect)
- [x] Meeting bot integration type

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

### v0.1.19 — Platform Rules + Setup Hierarchy (2026-03-09, folded into v0.1.20)
- [x] `nex setup` now installs rules + MCP for all detected platforms (hierarchy: plugins > rules > MCP)
- [x] `--with-mcp` replaced with `--no-rules` (skip rules/instruction files)
- [x] README restructured: `nex setup` is primary Quick Start, manual steps in collapsible sections
- [x] Root README updated: integration table shows `nex setup` instead of `npx @nex-ai/cli`
- [x] Created 9 platform-specific rules/instruction templates in `cli/platform-rules/`
- [x] Added `supportsRules` and `rulesPath` to Platform interface in `platform-detect.ts`
- [x] Added `installRulesFile()` to `installers.ts` (standalone + append modes)
- [x] Updated `setup.ts` install loop with hierarchy logic
- [x] Updated README platforms table to show rules + MCP per platform
- [x] Published as part of v0.1.20

## Core Backend Fixes (2026-03-09 — 2026-03-10)
- [x] Search timeout: 300ms → 2000ms (PR #675, merged & deployed)
- [x] Embedding dimension fix: 768 → 1024 (PR #679, merged & deployed)
- [x] Semantic search date type fix (PR #680, open)
- [x] Historical email processing (PR #681, merged & deployed to prod)
- [x] search_entities domain fallback (PR #676, merged & deployed)
- [x] Gmail reconnect sync fix (PR #673, merged & deployed to prod)
- [x] Artifact direction fix: added `direction=None` to ArtifactModel for TEXT-type artifacts (PR #688, merged & deployed to prod)
- [x] Deploy workflow fix: service=all on workflow_dispatch (PR #687, open)
- [x] Full historical email pipeline verified end-to-end after Gmail reconnect (PRs #673 + #681 + #688)

## Pending / Next

### P0: Interactive Integration Connection in `nex setup`
- [ ] During `nex setup`, after platform install, show available integrations (Gmail, Slack, etc.)
- [ ] Let user select which to connect (or skip all)
- [ ] Integrations that are already connected show as such
- [ ] Then proceed with normal setup flow
- [ ] Goal: one-command onboarding — register + connect integrations + install platform plugins

### P0 (DONE): Platform Plugins/Rules — COMPLETE
- [x] Cursor: `.cursor/rules/nex.md` — rules + MCP
- [x] Windsurf: `.windsurf/rules/nex.md` — rules + MCP
- [x] VS Code / Copilot: `.github/instructions/nex.instructions.md` — instructions + MCP
- [x] Cline: `.clinerules/nex.md` — rules + MCP
- [x] Continue.dev: `.continue/rules/nex.md` — rules + MCP
- [x] Zed: `.rules` (append) — rules + MCP
- [x] Kilo Code: `.kilocode/rules/nex.md` — rules + MCP
- [x] OpenCode: `AGENTS.md` (append) — rules + MCP
- [x] Aider: `CONVENTIONS.md` (append) — rules only (no MCP support)
- [x] `nex setup` installs per-platform (hierarchy: plugins > rules > MCP)

### P1: Integrations Feature Parity — ALREADY COMPLETE
All surfaces already have integration tools:
- [x] MCP server: 4 integration tools (`tools/integrations.ts`)
- [x] OpenClaw plugin: 3 integration tools
- [x] SKILL.md: integration bash scripts
- [x] Claude Code slash commands: `/integrate`

### P2: TUI Polish (plan exists)
- [ ] Create shared `lib/tui.ts` module (spinner, table, box, tree, etc.)
- [ ] Update search/ask command descriptions to differentiate
- [ ] Add TTY formatters for ask, search, scan, setup status, task list
- [ ] Upgrade `prompt.ts` to arrow-key selection

## API Coverage Matrix (Current)

| Surface | Tools/Commands | Coverage |
|---------|---------------|----------|
| CLI | 15+ commands | Full |
| MCP | 38+ tools | Full |
| OpenClaw Plugin | 49 tools | Full |
| Claude Code Plugin | 26 slash commands | Full |
| SKILL.md | All API groups | Full |
