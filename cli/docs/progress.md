# WUPHF CLI -- Progress Tracker

## Phase 1: TUI Shell -- DONE
- [x] Ink app root with ThemeProvider + TuiContext
- [x] Store (reducer, dispatch, subscribe)
- [x] View stack with Router (10 views: home, help, record-list, record-detail, ask-chat, agent-list, chat, calendar, orchestration, generative)
- [x] View registration with live service subscriptions
- [x] StatusBar, Picker, Viewport, HelpScreen components
- [x] ChatInput, MessageList, AgentCard components
- [x] Vim-style keybindings (normal/insert mode)
- [x] Navigation keybindings: a=agents, c=chat, C=calendar, o=orchestration, ?=help
- [x] TUI-only entry point (Commander removed)
- [x] Non-interactive CLI: `wuphf <command>` dispatches and exits
- [x] Command aliases: agents, objects, orch
- [x] Command-to-view routing (ask→ask-chat, agents→agent-list, etc.)
- [x] Loading indicators via SET_LOADING
- [x] Mode state flows to all views via TuiContext

## Phase 2: Agent Runtime -- DONE (fully integrated)
- [x] Agent types (phase, config, state, tools, sessions)
- [x] Tool registry with schema validation (9 tools: 7 builtin + 2 gossip)
- [x] Session store with DAG entries
- [x] Agent queues (steer, follow-up)
- [x] Agent loop state machine (idle → build_context → stream_llm → execute_tool → done)
- [x] Mock streamFn for testing
- [x] AgentService singleton with TUI integration
- [x] Agent commands in dispatch (list, create, start, stop, steer, inspect, templates)
- [x] Live agent-list view with service subscriptions

## Phase 3: Office Chat -- DONE (fully integrated)
- [x] Channel model with persistence (~/.wuphf/chat/channels.json)
- [x] Message store (JSONL per channel)
- [x] Message routing with @mention parsing
- [x] Suggested responses
- [x] ChatService singleton
- [x] Chat view with live service subscription
- [x] Send/receive real messages

## Phase 4: Knowledge Propagation -- DONE
- [x] Gossip layer (publish/query via NexClient)
- [x] Adoption scoring (credibility 0.4 × relevance 0.4 × freshness 0.2)
- [x] CredibilityTracker with persistent running average
- [x] Gossip wired into agent loop (buildContext queries, handleDone publishes)
- [x] Gossip tools (nex_gossip_publish, nex_gossip_query)
- [x] Credibility updates on task completion

## Phase 5: Agent Calendar -- DONE (fully integrated)
- [x] Cron scheduling (daily, hourly, Nh, standard 5-field)
- [x] Calendar store (JSON persistence)
- [x] CalendarService singleton
- [x] Calendar view with live service subscription
- [x] Week grid with real scheduled heartbeats

## Phase 6: Generative TUI -- DONE (standalone)
- [x] A2UI component type definitions (9 types)
- [x] JSON Pointer bindings (RFC 6901)
- [x] Component registry
- [x] GenerativeRenderer with schema validation
- [x] GenerativeView with error boundary
- [x] View registered in router
- [ ] **Remaining**: No agent or command triggers generative view yet

## Phase 7: Orchestration -- DONE (fully integrated)
- [x] BudgetTracker with warning/exceeded thresholds
- [x] TaskRouter with Dice-coefficient fuzzy matching
- [x] OrchestratorExecutor with atomic checkout + concurrency
- [x] Workflow templates (seo-audit, lead-gen-pipeline, enrichment-batch)
- [x] OrchestrationService singleton
- [x] Orchestration view with goals, task pool, budget bars
- [x] Live service subscription

## Cross-Cutting

### Command Dispatch Bridge -- DONE
- [x] 55+ commands registered in dispatch.ts
- [x] Command aliases (agents, objects, orch)
- [x] TUI view hint commands (chat, calendar, orchestration)
- [x] Input tokenizer (parse-input.ts)
- [x] Structured CommandResult (output, data, exitCode, error, nav)
- [x] Home view routes commands to structured views

### Test Coverage -- 445 tests, 0 failures
- Store: 21 tests
- Keybindings: 32+ tests
- Components: 25 tests
- Dispatch: 36+ tests
- Agent tools: 9 tests
- Agent loop: 10 tests
- Agent sessions: 8 tests
- Gossip integration: 15 tests
- Chat router: 7 tests
- Chat service: 9 tests
- Calendar scheduler: 18 tests
- Calendar service: 7 tests
- Orchestration budget: 17 tests
- Orchestration router: 12 tests
- Orchestration service: 20 tests
- Generative bindings: 20 tests
- Generative renderer: 20 tests
- View registration: 23+ tests
- Agent service: 24 tests
- Orchestration view: 7 tests

### Deep Review -- DONE
- [x] Mode state flows to views (TuiContext)
- [x] Navigation keybindings for all views
- [x] Double StatusBar removed from all views
- [x] Help screen matches actual dispatch registry
- [x] Curated home picker (10 commands)
- [x] Service subscriptions for live re-renders
- [x] Command-to-view routing
- [x] Loading indicators
- [x] Gossip layer connected to agent loop

### Remaining Items
- [ ] Real LLM streamFn (currently mock)
- [ ] Generative UI trigger from agent output
- [ ] Agent-to-agent chat (agents posting in channels)
- [ ] Streaming agent output in TUI
- [ ] Welcome/onboarding experience
- [ ] TextInput controlled mode (store-driven input)
