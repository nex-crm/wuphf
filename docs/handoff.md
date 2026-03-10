# Nex Plugins — Session Handoff

> Last updated: 2026-03-10

## First Steps

1. Read this file fully
2. Check CLI builds: `cd /Users/najmuzzaman/Documents/nex/nex-as-a-skill/cli && npm run build`
3. Run tests: `cd /Users/najmuzzaman/Documents/nex/nex-as-a-skill/cli && npm test`

## What Was Done Last Session (2026-03-10)

### 1. Full Platform Plugins — 6-Layer Setup Hierarchy (v0.1.20)

Upgraded `nex setup` from 3-layer (plugins > rules > MCP) to 6-layer hierarchy per platform:
**Hooks → Plugins → Agents → Workflows → Rules → MCP**

- Extracted shared hook logic into `cli/src/plugin/shared.ts` (doRecall, doCapture, doSessionStart)
- Refactored 3 Claude Code hooks (auto-recall, auto-capture, auto-session-start) to use shared.ts
- Created 8 adapter scripts in `cli/src/plugin/adapters/`:
  - Cursor: session-start, recall, stop
  - Windsurf: recall, capture
  - Cline: recall, task-start, capture
- Created `cli/platform-plugins/` with 7 template files:
  - opencode-plugin.ts, vscode-agent.md, kilocode-modes.yaml, continue-provider.ts
  - windsurf-workflows/: nex-ask.md, nex-remember.md, nex-search.md
- Updated platform-detect.ts: 5 new capability fields, added OpenClaw + Aider (12 platforms total)
- Updated installers.ts: 7 new installer functions
- Updated setup.ts: 6-layer hierarchy, `--no-hooks` flag, fixed name prompt on key regen
- PR #31 merged, published `@nex-ai/nex@0.1.20`

### 2. Core Backend Fixes

- **PR #688 merged & deployed**: Artifact direction fix — `Artifact.__init__()` missing `direction` arg for TEXT-type artifacts in `core/py/context2/pipeline.py`
- **PR #673 confirmed live**: Gmail reconnect (delete + recreate stale account)
- **PR #681 confirmed live**: Historical email processing with engagement-based filtering
- **Full historical email pipeline verified end-to-end** after Gmail reconnect with all 3 PRs deployed

### 3. What Each Platform Gets Now

| Platform | Hooks | Plugins | Agents | Workflows | Rules | MCP |
|----------|-------|---------|--------|-----------|-------|-----|
| Claude Code | 3 hooks | — | — | 26 slash cmds | — | — |
| OpenClaw | (plugin-internal) | 49 tools | — | — | — | — |
| Cursor | 3 hooks | — | — | — | rules | MCP |
| Windsurf | 2 hooks | — | — | 3 workflows | rules | MCP |
| Cline | 3 hooks | — | — | — | rules | MCP |
| OpenCode | — | plugin.ts | — | — | AGENTS.md | MCP |
| VS Code | — | — | @nex agent | — | instructions | MCP |
| Kilo Code | — | — | custom mode | — | rules | MCP |
| Continue.dev | — | — | — | — | rules | MCP |
| Zed | — | — | — | — | .rules | MCP |
| Claude Desktop | — | — | — | — | — | MCP |
| Aider | — | — | — | — | CONVENTIONS | — |

## Next Task: Interactive Integration Connection in `nex setup`

**Goal**: Make `nex setup` a complete one-command onboarding by adding an integration connection step.

**Flow**:
```
nex setup
  → Register (or reuse existing key)
  → Install platform plugins (hooks, rules, MCP — already done)
  → NEW: Show available integrations (Gmail, Slack, Google Calendar, Outlook, etc.)
    → User selects which to connect (arrow-key TUI, can skip all)
    → Already-connected integrations shown as ✓
    → Opens OAuth URL in browser for each selected integration
  → Done — full onboarding complete
```

**Key files to modify**:
- `cli/src/commands/setup.ts` — Add integration step after platform install
- `cli/src/lib/nex-api.ts` — Integration list/connect API calls (already exist)
- `cli/src/lib/prompt.ts` — May need multi-select arrow-key chooser

**Existing integration infrastructure** (already built):
- `nex integrate list` — Lists available + connected integrations
- `nex integrate connect <name>` — Opens OAuth URL in browser
- API: `GET /api/sdk/integrations/available`, `POST /api/sdk/integrations/connect`
- TUI: Interactive list with expand/collapse already in `cli/src/commands/integrate.ts`

## Build & Test

```bash
cd /Users/najmuzzaman/Documents/nex/nex-as-a-skill
cd cli && npm run build && npm test    # 65+ tests pass
cd ../mcp && npm run build             # MCP server
```

## Key Files

| File | Purpose |
|------|---------|
| `cli/src/plugin/shared.ts` | Shared hook logic (doRecall, doCapture, doSessionStart) |
| `cli/src/plugin/adapters/*.ts` | Platform-specific hook adapters |
| `cli/platform-plugins/*` | Plugin/agent/workflow templates |
| `cli/platform-rules/*.md` | Rules/instruction templates |
| `cli/src/lib/platform-detect.ts` | Platform detection + 6 capability fields |
| `cli/src/lib/installers.ts` | All installer functions (hooks, plugins, agents, workflows, rules, MCP) |
| `cli/src/commands/setup.ts` | Setup command — 6-layer hierarchy |
| `cli/src/commands/integrate.ts` | Integration list/connect/disconnect TUI |
| `cli/package.json` | v0.1.20, files: dist, plugin-commands, platform-rules, platform-plugins |

## Core Repo Status

- **PR #688**: Artifact direction fix — merged & deployed to prod
- **PR #681**: Historical email processing — merged & deployed to prod
- **PR #673**: Gmail reconnect — merged & deployed to prod
- **PR #680**: Semantic search date type fix — open
- **PR #687**: Deploy workflow fix — open
