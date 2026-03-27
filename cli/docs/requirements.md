# WUPHF CLI -- Requirements & Acceptance Criteria

## Phase 1: TUI Shell
- Ink-based TUI renders as default entry point
- View stack navigation (push/pop/home)
- Vim-style normal/insert mode switching
- Command picker with fuzzy match
- Status bar with mode indicator and nav breadcrumbs
- Key bindings for scrolling, history, mode toggle

## Phase 2: Agent Runtime
- Agent state machine: idle -> build_context -> stream_llm -> execute_tool -> done/error
- Tool registry with schema validation
- Session store with DAG-structured entries (parent/child)
- Agent config: slug, name, expertise, budget, tools
- Budget tracking per agent (tokens, cost)

## Phase 3: Office Chat
- Channel-based messaging between agents
- Message routing by channel topic
- System messages for lifecycle events

## Phase 4: Knowledge Propagation
- Ask/Remember API for gossip-style knowledge sharing
- Selective adoption scoring for received knowledge

## Phase 5: Agent Calendar
- Cron-based heartbeat scheduling per agent
- Heartbeat triggers agent wake-up cycle

## Phase 6: Generative TUI
- A2UI JSON schema defines component tree (row, column, card, text, textfield, list, table, progress, spacer)
- JSON Pointer data binding (RFC 6901) resolves dynamic content
- Schema validation with clear error messages
- Streaming data model updates (set, merge, delete)
- Component registry maps type strings to Ink renderers

## Phase 7: Orchestration
- Flat task pool with priority levels and status lifecycle
- Goal definitions group related tasks
- Expertise-based routing (fuzzy skill matching, proficiency weighting)
- Atomic task checkout prevents duplicate assignment
- Concurrent agent execution with configurable limit
- Per-agent and global budget tracking (tokens, cost, warning at 80%, exceeded at 100%)
- Pre-built workflow templates (SEO audit, lead gen, data enrichment)
- Task timeout and auto-retry support
