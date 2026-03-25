# Multi-Agent CLI — Design Spec

> Zero Humans Company in a CLI — autonomous multi-agent system inside a rich terminal TUI.

**Branch:** `nazz/experiment/multi-agent-cli`
**Runtime:** Go + Bubbletea (replaces TypeScript/Ink entirely)
**Binary:** `wuphf` (single binary, replaces npm-based CLI)
**Date:** 2026-03-22

---

## 1. Vision

A single terminal window where an autonomous team of AI agents operates like a company. The user gives high-level directives; the Team-Lead (CEO by default) narrates its plan, delegates sub-tasks to specialist agents, and the user watches them work in real-time. No human in the loop unless the user intervenes.

### Architecture References
- **Pi-Mono:** Agent execution loop (state machine), DAG sessions, runtime tool registry
- **HyperspaceAI:** Three-layer gossip cascade, selective adoption scoring
- **Paperclip:** Expertise-based routing, atomic task checkout, budget tracking
- **A2UI:** Generative TUI — agents emit JSON, renderer creates Bubbletea components

---

## 2. Agent Packs

Teams replace single-agent selection. During `/init`, user picks a pack. All agents in the pack are initialized and start listening.

### 2.0 Data Model

```go
// PackDefinition defines a team of agents that work together.
type PackDefinition struct {
    Slug        string        // "founding-team", "coding-team", "lead-gen-agency"
    Name        string        // Display name
    Description string        // One-line description
    LeadSlug    string        // Slug of the Team-Lead agent in this pack
    Agents      []AgentConfig // All agents in the pack (lead + specialists)
}
```

**Registry:** `internal/agent/packs.go` — new file exporting `var Packs = []PackDefinition{...}` with all 3 packs. Replaces individual templates in `templates.go` (old templates kept as aliases for backward compat).

**Config persistence:** Add `Pack string` and `TeamLeadSlug string` fields to `Config` in `config.go`. Saved to `~/.wuphf/config.json` during `/init`.

**MessageRouter update:** Replace hardcoded `"team-lead"` fallback in `message_router.go` lines 105/117 with `config.TeamLeadSlug`. The router reads this from the loaded config at initialization.

### 2.1 Founding Team (Default)

The default pack for "zero human company" mode. CEO is Team-Lead.

| Slug | Name | Role | Expertise |
|------|------|------|-----------|
| `ceo` | CEO | Team-Lead | strategy, decision-making, prioritization, delegation, orchestration |
| `pm` | Product Manager | Specialist | roadmap, user-stories, requirements, prioritization, specs |
| `fe` | FE Engineer | Specialist | frontend, React, CSS, UI/UX implementation, components |
| `be` | BE Engineer | Specialist | backend, APIs, databases, infrastructure, architecture |
| `designer` | Designer | Specialist | UI/UX design, branding, visual-systems, prototyping |
| `cmo` | CMO | Specialist | marketing, content, brand, growth, analytics, campaigns |
| `cro` | CRO | Specialist | sales, pipeline, revenue, partnerships, outreach, closing |

### 2.2 Coding Team Pack

Optimized for high-velocity software development.

| Slug | Name | Role | Expertise |
|------|------|------|-----------|
| `tech-lead` | Tech Lead | Team-Lead | architecture, code-review, technical-decisions, planning |
| `fe` | FE Engineer | Specialist | frontend, React, CSS, components, accessibility |
| `be` | BE Engineer | Specialist | backend, APIs, databases, DevOps, infrastructure |
| `qa` | QA Engineer | Specialist | testing, automation, quality, edge-cases, CI/CD |

### 2.3 Lead Gen Agency Pack

Specialized in quiet outbound systems and automated GTM.

| Slug | Name | Role | Expertise |
|------|------|------|-----------|
| `ae` | Account Executive | Team-Lead | prospecting, outreach, pipeline, closing, negotiation |
| `sdr` | SDR | Specialist | cold-outreach, qualification, booking-meetings, sequences |
| `research` | Research Analyst | Specialist | market-research, competitive-analysis, ICP-profiling, trends |
| `content` | Content Strategist | Specialist | SEO, copywriting, nurture-sequences, thought-leadership |

---

## 3. Demo-Critical Features (P0)

### 3.1 Fix Agent Echo Bug

**Problem:** Default provider echoes user input instead of calling an LLM.
**Fix:** Change the `default` case in `resolver.go` from `CreateNexAskStreamFn` to `CreateClaudeCodeStreamFn`. When `config.LLMProvider` is empty or `"claude-code"`, spawn `claude -p` subprocess via goroutine. Enable `--session-persistence` with per-agent session IDs to support multi-turn context.

**Context window management:** Each agent's session history is passed via the prompt. Cap at 20 most recent session entries (user + assistant + tool). Older entries are summarized as a single system message: "Previous context: [summary]".

**Acceptance criteria:**
- User types message → agent responds with LLM-generated content, not echo
- Agent responses stream to TUI in real-time (chunk by chunk)
- Errors from Claude subprocess surface as system messages
- If `claude` is not in PATH, surface error: "Claude CLI not found. Run `/init` to choose a different provider."

### 3.2 Team-Lead Narrated Delegation

**Flow:**
1. User sends plain text message
2. `MessageRouter.Route()` routes to Team-Lead (CEO/Tech Lead/AE depending on pack)
3. Team-Lead calls Claude Code with system prompt including:
   - Its role and team roster
   - Instruction to narrate delegation: "I'll assign X to @agent-slug"
   - List of available specialist agents and their expertise
4. Team-Lead response appears in chat stream
5. New `delegator.go` in `internal/orchestration/`:
   - **Hook point:** Called from `stream.go`'s `AgentDoneMsg` handler, AFTER the Team-Lead's full response is accumulated, BEFORE it's appended to messages. If the responding agent's slug matches `config.TeamLeadSlug`, run delegation parsing.
   - **Parsing strategy:** Regex `@([a-z][a-z0-9-]*)` extracts all mentioned slugs. For each mention, extract the surrounding sentence (from previous period/newline to next period/newline) as the sub-task description.
   - **Edge cases:**
     - Unknown `@slug` → ignore, log warning
     - No `@` mentions found → no delegation, Team-Lead response shown as-is (Team-Lead answered directly)
     - Specialist already busy → queue the steer message, it will be processed on next idle tick
   - Queues steer messages to mentioned specialists: `[TEAM-LEAD DELEGATION] <extracted sentence>`
   - Calls `agentService.EnsureRunning()` for each specialist
6. Specialist agents process their sub-tasks, output appears inline with distinct styling

**Concurrency limits:** `MaxConcurrentAgents` in config (default: 3). If delegation would exceed this, queue excess agents — they start when a slot opens. This prevents spawning 7 concurrent `claude -p` processes.

**Error recovery:** If a specialist agent errors (provider failure, timeout), the error surfaces as a system message in the chat stream: "[ERROR] @slug failed: <reason>". The Team-Lead is NOT automatically notified (avoids feedback loops). User can manually retry via `@slug try again`.

**System prompt template for Team-Lead:**
```
You are the {role} of a {pack_name}. Your team consists of:
{for each agent: - @{slug} ({name}): {expertise}}

When the user gives you a directive:
1. Analyze what needs to be done
2. Break it into sub-tasks for your team members
3. Narrate your delegation plan, mentioning each agent by @slug
4. Example: "I'll have @research analyze the competitive landscape while @content drafts the positioning document."

Always delegate to the most appropriate specialist. Never do specialist work yourself.
```

**Acceptance criteria:**
- Team-Lead explains what it will do before delegating
- Specialists receive tasks and begin working
- Agent chatter appears inline with agent-specific colors
- User can see all agents' phases in the roster sidebar
- Unknown @mentions are silently ignored
- Concurrent agent limit respected

### 3.3 Full `/init` Onboarding Flow

**States:** `idle` → `api_key` → `provider_choice` → `pack_choice` → `platform_detect` → `done`

**Flow:**
1. **API Key:** Check `~/.wuphf/config.json` for existing key. If missing, show text input for API key (user gets key from https://app.nex.ai/settings). If key is provided, validate via `GET /v1/objects` (any authenticated endpoint). If invalid, show error and re-prompt. Email-based registration is a P2 feature (requires backend endpoint not yet available in Go port).
2. **Provider Choice:** Picker with 3 options (if `claude` not in PATH, show warning next to Claude Code option):
   - Claude Code (default) — requires `claude` in PATH
   - Gemini — requires GEMINI_API_KEY
   - WUPHF Ask — uses WUPHF_API_KEY only
3. **Pack Choice:** Picker with 3 options:
   - Founding Team (default) — CEO + 6 specialists
   - Coding Team — Tech Lead + 3 engineers
   - Lead Gen Agency — AE + 3 specialists
4. **Platform Detect:** Detect installed AI platforms, show summary.
5. **Done:** Save config, create all agents from selected pack, show welcome message.

**Acceptance criteria:**
- `/init` walks through all steps with picker UI
- Config saved to `~/.wuphf/config.json`
- All agents from selected pack created and ready
- Re-running `/init` detects existing config, offers to reconfigure

### 3.4 Context Engineering

**Per-agent system prompts:**
- Each agent gets a system prompt based on its template (role, expertise, personality)
- Team-Lead gets the delegation prompt (section 3.2)
- Specialists get task-focused prompts

**Multi-turn session history:**
- Session entries persist across ticks (BuildContext phase)
- User messages, agent responses, tool calls all tracked in DAG

**Echo prevention:**
- Never pass raw user input as the agent response
- StreamFn must return LLM-generated content only
- If provider fails, surface error message, not echo

### 3.5 Live Agent Activity Feed

**TUI updates:**
- Agent phase changes stream to roster sidebar in real-time
- Phases shown with indicators:
  - `idle` → `○` (gray)
  - `build_context` → spinner + "preparing" (yellow)
  - `stream_llm` → spinner + "thinking" (blue)
  - `execute_tool` → spinner + "running tool" (purple)
  - `done` → `●` (green)
  - `error` → `●` (red)
- Agent messages appear inline in chat stream with:
  - Agent name colored by agent (from palette)
  - `[AGENT-SLUG]` prefix for multi-agent clarity
  - Tool calls shown as system messages

### 3.6 Non-Interactive Dispatch

**`wuphf --cmd "<command>"`** executes a command and exits.
- Wire `main.go`'s `dispatch()` stub to call `commands.Dispatch()` (which already implements all registered commands via `RegisterAllCommands()`)
- Create a `SlashContext` with loaded config, API client, and nil AgentService (non-interactive mode doesn't need agents)
- Support: `ask`, `search`, `remember`, `agents`, `objects`, `records`, `help`, `version`
- Output to stdout in text or JSON format (respect `--format` flag)
- Exit codes: 0 success, 1 general error, 2 auth error (add auth error detection in `commands.Dispatch` by checking for 401 responses)

---

## 4. Deferred Features (P1/P2)

### P1: Full Command Dispatch (55+ commands)

Port remaining commands from TS `dispatch.ts`:
- Object CRUD: `object list|get|create|update|delete`
- Record CRUD: `record list|get|create|upsert|update|delete|timeline`
- Notes: `note list|get|create|update|delete`
- Tasks: `task list|get|create|update|delete`
- Relationships: `rel list-defs|create-def|delete-def|create|delete`
- Attributes: `attribute create|update|delete`
- Lists: `list list|get|create|delete|records|add-member|upsert-member|update-record|remove-record`
- Search: `search` (enhanced)
- Insights: `insight list`
- Graph: `graph`
- Config: `config show|set|path`
- Sessions: `session list|clear`
- Agent: `agent create|start|stop|steer|inspect|templates`

### P1: UI Render Functions

Port from `kill-saas` branch (`wuphf-cli` repo):
- **Table render** (`ui/table.ts` → `internal/tui/table.go`): Auto-sized columns, alternating rows, borders
- **Taskboard render** (`ui/taskboard.ts` → `internal/tui/taskboard.go`): 3-column kanban (To Do, In Progress, Done)
- **Insights render** (`ui/insights.ts` → `internal/tui/insights.go`): Priority badges [CRIT/HIGH/MED/LOW]
- **Timeline render** (`ui/timeline.ts` → `internal/tui/timeline.go`): Event icons, vertical connectors

### P2: Chat System (Phase 3)

- Channel-based messaging between agents
- Message routing by channel topic
- System messages for lifecycle events
- JSONL persistence per channel

### P2: Calendar System (Phase 5)

- Cron-based heartbeat scheduling per agent
- Calendar store (JSON persistence)
- Week grid view

### P2: Generative TUI (Phase 6)

- A2UI JSON schema → Bubbletea component tree
- JSON Pointer data binding (RFC 6901)
- Streaming data model updates

### P2: Graph Visualization

Port from `kill-saas` (`ui/graph.ts`, 595 lines):
- ASCII graph layout with Bresenham line drawing
- Node icons by type (person, company, deal, etc.)
- Force-directed positioning

### P2: Platform Plugin System

Port 6-layer setup hierarchy:
- Hooks → Plugins → Agents → Workflows → Rules → MCP
- 12 platform adapters (Claude Code, Cursor, Windsurf, Cline, etc.)
- Platform detection and auto-installation

### P2: Integration Management

- `integrate list|connect|disconnect`
- OAuth flow for Gmail, Slack, Google Calendar, Outlook, Salesforce, HubSpot, Attio

---

## 5. Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Runtime | Go 1.24+ | Goroutines solve Bun async stalls. Single binary. |
| TUI framework | Bubbletea | Native Go, proven, great component model |
| Styling | Lipgloss | Bubbletea companion, ANSI color support |
| Default LLM | Claude Code | Most capable for multi-turn orchestration |
| Config format | JSON | Simpler than TOML, already in Go port |
| Session store | File-based JSON | Simple, no external deps |
| Binary name | `wuphf` | Replaces TS CLI entirely |
| Agent packs | Team templates | Aligns with "zero human company" vision |

---

## 6. File Structure

```
cli/.worktrees/go-bubbletea-port/
├── cmd/wuphf/main.go                    # Entry point
├── internal/
│   ├── agent/
│   │   ├── loop.go                    # Agent state machine
│   │   ├── service.go                 # Lifecycle + tick management
│   │   ├── tools.go                   # 7 WUPHF API tools
│   │   ├── session.go                 # DAG session store
│   │   ├── gossip.go                  # Knowledge propagation
│   │   ├── adoption.go                # Credibility scoring
│   │   ├── templates.go               # Legacy agent templates (kept for compat)
│   │   ├── packs.go                   # NEW: Pack definitions (founding, coding, lead-gen)
│   │   └── queues.go                  # Steer + follow-up queues
│   ├── orchestration/
│   │   ├── message_router.go          # Skill-based routing
│   │   ├── task_router.go             # Fuzzy matching
│   │   ├── delegator.go              # NEW: Team-Lead → Specialist delegation
│   │   ├── budget.go                  # Token/cost tracking
│   │   └── executor.go               # Concurrent execution
│   ├── provider/
│   │   ├── claude.go                  # Claude Code subprocess (DEFAULT)
│   │   ├── gemini.go                  # Google GenAI SDK
│   │   ├── wuphf.go                     # WUPHF Ask fallback
│   │   └── resolver.go               # Provider selection
│   ├── commands/
│   │   ├── dispatch.go                # Command registry + execution
│   │   ├── slash.go                   # Slash command definitions
│   │   └── registry.go               # Command registry + SlashContext type
│   ├── tui/
│   │   ├── model.go                   # Root Bubbletea model
│   │   ├── stream.go                  # Chat stream view
│   │   ├── init_flow.go               # Onboarding wizard (updated)
│   │   ├── roster.go                  # Agent sidebar
│   │   ├── keybindings.go             # Vim modes
│   │   ├── autocomplete.go            # / commands
│   │   ├── mention.go                 # @ agents
│   │   ├── picker.go                  # Selection UI
│   │   ├── spinner.go                 # Braille animation
│   │   ├── styles.go                  # Lipgloss theme
│   │   └── messages.go                # Bubbletea messages
│   ├── api/
│   │   └── client.go                  # WUPHF HTTP client
│   └── config/
│       └── config.go                  # Env + file config
├── docs/
│   ├── superpowers/specs/             # This spec
│   └── requirements-deferred.md       # P1/P2 detailed requirements
├── go.mod
├── go.sum
└── tests/                             # Termwright E2E tests
```

---

## 7. Success Criteria

### Demo Ready (P0)
- [ ] User runs `wuphf` → TUI launches with banner + input
- [ ] `/init` → walks through provider + pack selection → creates team
- [ ] User types directive → CEO narrates plan → delegates to specialists
- [ ] Specialist agents work in parallel, output streams to chat
- [ ] Roster sidebar shows all agents with live phase indicators
- [ ] `wuphf --cmd "ask who is Maria"` works non-interactively
- [ ] All existing unit tests pass
- [ ] New E2E termwright test: full delegation flow

### Quality Gates
- [ ] No echo bug — agents produce LLM content, not repeated input
- [ ] Clean exit — Ctrl+C twice terminates cleanly
- [ ] Error handling — provider failures surface as messages, not crashes
- [ ] Config persistence — settings survive restart
