# WUPHF CLI -- Context & Architecture

## Vision
Zero Humans Company in a CLI -- autonomous multi-agent system inside a rich terminal TUI.

## Architecture References
- **Pi-Mono:** Agent execution loop (state machine), DAG sessions, runtime tool registry
- **HyperspaceAI:** Three-layer gossip cascade, selective adoption scoring
- **Paperclip:** Expertise-based routing, atomic task checkout, budget tracking
- **A2UI:** Generative TUI -- agents emit JSON, renderer creates Ink components

## Directory Structure
- `src/tui/` -- Ink TUI shell, components, views, generative renderer
- `src/agent/` -- Agent runtime (loop, tools, sessions, gossip)
- `src/chat/` -- Slack-style chat (channels, messages, routing)
- `src/calendar/` -- Agent scheduling (cron, heartbeats)
- `src/orchestration/` -- Multi-agent orchestration (routing, budget, workflows)
- `src/commands/` -- Commander CLI commands
- `src/lib/` -- Shared utilities (client, config, output, tui primitives)

## Phases
1. TUI Shell -- Ink-based TUI as default entry point
2. Agent Runtime -- Pi-based state machine with tools
3. Office Chat -- Slack-style agent communication
4. Knowledge Propagation -- Gossip layer via Ask/Remember API
5. Agent Calendar -- Cron-based scheduling
6. Generative TUI -- A2UI-style dynamic UI
7. Orchestration -- Multi-agent coordination
