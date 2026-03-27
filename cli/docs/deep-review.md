# Deep Review: WUPHF CLI TUI vs Original Vision

## Executive Summary

The TUI has a solid architectural foundation -- clean module boundaries, a working dispatch layer, Ink-based view stack, vim keybindings, and all major backend systems (agent, chat, calendar, orchestration, generative) wired through service layers. However, **the views are islands with no bridges**: every view beyond "home" and "help" is registered but unreachable from any user action. The mode state (insert/normal) does not flow to the home view, so the ChatInput never activates. These two issues -- broken navigation and broken input focus -- mean the TUI launches and looks correct, but the user cannot actually *do* anything beyond viewing the banner and picker.

## Module Load & Compile Status

- `npx tsc --noEmit`: **PASSES** (zero errors)
- `npx tsx -e "import('./src/tui/index.js')..."`: **LOADS OK**
- `npm test`: **418 passing, 0 failures**

The TUI module tree imports cleanly, TypeScript compiles, and all tests pass. No circular import issues detected.

## Scenario Audit

| # | Scenario | Status | Gap Description |
|---|----------|--------|-----------------|
| 1 | TUI Launch | **Partial** | Module loads, App renders, banner + picker display. But double StatusBar (App + HomeView both render one). |
| 2 | Vim modes | **Broken** | `i` dispatches `SET_MODE: "insert"` to store, App's StatusBar updates correctly, but the mode NEVER reaches HomeView. The `home` view adapter in register-views.tsx reads `props?.mode` which is always undefined (defaults to "normal"). ChatInput stays permanently inactive. |
| 3 | Command execution | **Broken** | Depends on vim mode working. Even if mode were fixed, ChatInput uses `@inkjs/ui TextInput` which is uncontrolled (`defaultValue` only, no `value` prop) -- store's `SET_INPUT` from picker/autocomplete cannot push values into it. The `onSubmit` path (HomeView -> dispatch) is correctly wired. |
| 4 | Record navigation | **Missing** | No command or keybinding ever dispatches `PUSH_VIEW { name: "record-list" }` or `"record-detail"`. The `record list` command returns text output that displays in the HomeView viewport; it does not push a structured view. `useRouter().push()` is exported but never called by any component. |
| 5 | Ask multi-turn | **Partial** | The `ask` command returns text via dispatch. The text renders in the home viewport. The `ask-chat` view exists and works (MessageList, multi-turn conversation), but there is no code path that PUSHES the `ask-chat` view. The user sees raw text output, not the chat interface. |
| 6 | Agent create + run | **Works (CLI only)** | `agent create slug --template seo-agent` and `agent start slug` execute correctly through dispatch. The TUI shows text output in home viewport. The `agent-list` view exists and renders correctly, but is unreachable. |
| 7 | Help screen | **Works** | Pressing `?` in normal mode dispatches `PUSH_VIEW { name: "help" }`. HelpView renders HelpScreen with command groups and keybindings. Escape pops back. This is the ONLY fully working navigation path. |
| 8 | Chat | **Dead Code** | ChatView is fully implemented (channel sidebar, message list, input). ChatService wraps the backend chat system. But there is no keybinding, command, or UI element that navigates to the chat view. |
| 9 | Calendar | **Dead Code** | CalendarView renders a week grid. CalendarService wraps scheduler. No path to reach it. |
| 10 | Orchestration | **Dead Code** | OrchestrationView renders goals, tasks, budget bars. Service wraps executor + budget tracker. No path to reach it. |
| 11 | Generative UI | **Dead Code** | Full A2UI schema renderer (row/column/card/text/table/list/progress/spacer), JSON Pointer bindings, error boundary, validation. Complete and tested. But nothing ever triggers `PUSH_VIEW { name: "generative", props: { schema } }`. No agent emit path exists. |
| 12 | Back navigation | **Works** | Escape in normal mode dispatches `POP_VIEW`. View stack pops correctly. Never pops below home. |
| 13 | Non-interactive mode | **Works** | `wuphf ask "query"` from CLI dispatches correctly, outputs to stdout, exits with code. Piped stdin also works. |

## Critical Gaps (Must Fix for Demo)

### 1. Mode state does not flow to views (SHOW-STOPPER)

**Problem**: When user presses `i`, the store updates to `mode: "insert"`, the App StatusBar correctly shows "INSERT", but the HomeView ChatInput stays inactive because the mode is not passed through the view system.

**Root cause**: `App.tsx` passes `viewStack` and `dispatch` to Router, but not `mode`. The home adapter in `register-views.tsx` reads `props?.mode` which is always undefined because the initial ViewEntry `{ name: "home" }` has no props.

**Fix**: Either (a) inject the store's mode into the Router/view context so all views can read it, or (b) have the home adapter subscribe to the store directly.

### 2. No navigation to any view except help (SHOW-STOPPER)

**Problem**: `useRouter().push()` is defined but never called. Only the `?` keybinding pushes a view. Views for agents, chat, calendar, orchestration, and generative are fully implemented but unreachable.

**Fix**: Add keybindings for common views (e.g., `a` for agents, `c` for chat, `o` for orchestration, `C` for calendar) AND/OR have the dispatch layer push views when appropriate (e.g., `agent list` pushes agent-list view, `ask` pushes ask-chat view).

### 3. Double StatusBar rendering

**Problem**: App.tsx renders AppStatusBar (reads mode from store). Every view also renders its own StatusBar with hardcoded values. The user sees two status bars.

**Fix**: Remove StatusBar from all individual views. The App-level StatusBar already tracks mode, breadcrumbs, and hints correctly.

### 4. Store input state is disconnected from TextInput

**Problem**: Keybindings dispatches `SET_INPUT` for autocomplete and picker quick-select, but `@inkjs/ui TextInput` only accepts `defaultValue` (uncontrolled). The store's input state never reaches the actual TextInput.

**Impact**: Tab-autocomplete in keybindings.ts writes to the store, but the TextInput doesn't see it. Picker quick-select dispatches `SET_INPUT` + `SET_MODE`, but the TextInput in HomeView has its own local state.

### 5. Help screen references non-existent commands

**Problem**: `help-screen.tsx` lists commands `objects`, `records`, `insights`, `entities`, `link`, `setup`, `register`, `scan` -- none of which are registered in `dispatch.ts`. The actual commands are `object list`, `record list`, `insight list`, etc.

**Fix**: Update HelpScreen to reference actual registered commands, or add shorthand aliases to dispatch.

## Experience Gaps (Would Make It Magical)

### 1. No real-time agent activity feed
The agent loop emits `phase_change`, `tool_call`, `message`, and `done` events. The AgentService subscribes to `phase_change`. But no TUI component subscribes to the service, so the user never sees an agent working in real-time. For a "Zero Humans Company" demo, watching agents think/act/discover in real-time is the core experience.

### 2. Services never trigger re-renders
All four services (agent, chat, calendar, orchestration) have `subscribe()` methods, but no view adapter in `register-views.tsx` calls them. Views render static snapshots at mount time. Even if the user could navigate to agent-list, it would show a frozen snapshot.

### 3. No command shortcuts / aliases
Users must type exact multi-word commands like `object list`, `record list contacts`, `insight list`. Common CRM operations should have single-word shortcuts: `objects`, `records`, `insights`, `agents`.

### 4. No transition animations or visual flourishes
No spinners for loading states. No fade-in/out for view transitions. No streaming text effect for agent responses. The `SET_LOADING` action exists in the store but is never dispatched from anywhere.

### 5. No welcome message or onboarding
First launch shows the WUPHF banner and a picker list of ALL registered commands (50+). There's no welcome message, no "try these first" guidance, no setup wizard.

### 6. Picker displays all 50+ commands indiscriminately
The home picker shows every registered command from dispatch.ts (ask, artifact, attribute create, attribute delete, ...). For a demo, this should be curated to 5-10 high-impact commands.

### 7. No agent-to-agent chat visible in TUI
The gossip layer (publish/query via WUPHF API) and chat system (channels, messages, routing) exist, but there's no trigger that makes agents send messages to each other, and no UI path to observe it.

### 8. Generative UI has no trigger path
The A2UI renderer is complete and tested. But no code in the agent loop, dispatch, or any service ever constructs an `A2UIComponent` schema and pushes a generative view. The entire generative UI system is plumbing with no faucet.

## Dead Code

| Module | What Exists | Why Dead |
|--------|-------------|----------|
| `views/chat.tsx` + `ChatService` | Full chat UI with channels, messages, send | No navigation path reaches it |
| `views/calendar.tsx` + `CalendarService` | Week grid with scheduled events | No navigation path reaches it |
| `views/orchestration.tsx` + `OrchestrationService` | Goals, tasks, budget bars | No navigation path reaches it |
| `views/generative.tsx` + generative/* | Full A2UI schema renderer | No code ever emits A2UI JSON |
| `views/record-list.tsx`, `record-detail.tsx` | Structured record browser | Commands return text, never push these views |
| `views/ask-chat.tsx` | Multi-turn AI conversation | `ask` command returns text to home viewport |
| `views/agent-list.tsx` + AgentCard | Agent cards with status dots | `agent list` returns text to home viewport |
| `useRouter().push()` | View stack navigation | Never called by any component |
| Service `subscribe()` methods | Real-time reactivity | Never called by any view adapter |
| `store.SET_LOADING` action | Loading indicator support | Never dispatched |
| `store.SET_CONTENT` action | Content update | Only reducer exists, never dispatched from views |
| `keybindings.ts` tab autocomplete | Autocomplete against COMMANDS list | Writes to store, but TextInput is uncontrolled and can't receive it |
| `keybindings.ts` picker quick-select SET_INPUT | Pre-fill input from picker | Same TextInput issue |
| `gossip.ts` + `adoption.ts` | Agent knowledge propagation + credibility | No agent loop step invokes gossip |
| `suggested-responses.ts` (chat) | Auto-generated reply suggestions | Exists in chat module, never surfaced |

## Architecture Issues

### 1. Dual state management (store vs local component state)
The store tracks `mode`, `inputValue`, `pickerCursor`, `scrollOffset`, `content`. But views maintain their own local state for the same things. HomeView has local `cursor`, `inputValue`, `scrollOffset`. This creates inconsistency -- keybindings writes to the store, components read from their local state.

### 2. View props are the only data channel
The router passes `props?: Record<string, unknown>` to each view. There's no context for mode, no subscription to services, no way for a view to access the store. This forces all data through the initial ViewEntry props, which are static.

### 3. Uncontrolled TextInput limits integration
`@inkjs/ui TextInput` only supports `defaultValue`, not `value`. This makes it impossible for the store/keybindings layer to programmatically set the input text. Either need a controlled text input or a different integration pattern.

### 4. Command dispatch returns text, not structured views
When `ask`, `object list`, `record list`, or `agent list` execute, they return `{ output: string }`. The home adapter shows this string in a Viewport. For rich TUI views, the dispatch layer should return structured data that triggers view pushes (e.g., `ask` should push `ask-chat` view with an onAsk handler).

## Recommended Next Steps

### Priority 1: Make the TUI actually usable (1-2 days)
1. **Fix mode flow**: Create a `TuiContext` that provides `mode`, `inputValue`, and `dispatch` to all views via React context. Have App.tsx wrap Router in this context.
2. **Fix double StatusBar**: Remove StatusBar from all individual views. App-level StatusBar handles it.
3. **Add view navigation keybindings**: In normal mode, map `a` -> agents, `c` -> chat, `C` -> calendar, `o` -> orchestration. These PUSH_VIEW the corresponding view.
4. **Add command aliases**: Register `objects` -> `object list`, `records <slug>` -> `record list <slug>`, `agents` -> push agent-list view, `insights` -> `insight list`.
5. **Fix help screen**: Update command references to match actual dispatch registry.

### Priority 2: Connect views to live data (1-2 days)
6. **Subscribe view adapters to services**: In register-views.tsx, each adapter should call `service.subscribe()` in a `useEffect` to trigger re-renders when data changes.
7. **Make `ask` push ask-chat view**: When user types `ask <query>`, push the ask-chat view instead of showing raw text.
8. **Make `agent list` push agent-list view**: Similarly for record list -> record-list view.
9. **Wire loading indicators**: Dispatch `SET_LOADING` before async operations, clear after.

### Priority 3: Make it magical for demo (2-3 days)
10. **Agent activity live feed**: Subscribe to agent loop events (phase_change, tool_call, message) and surface them in a status area or activity log.
11. **Curated home picker**: Show 5-7 high-impact commands (ask, agents, objects, records, search) instead of 50+.
12. **Welcome message**: First-launch experience that introduces the CLI and suggests next steps.
13. **Streaming agent output**: When an agent runs, stream its thinking to the TUI in real-time.
14. **Wire gossip layer**: Have agent loop invoke gossip.publish() after insights, gossip.query() at build_context.
15. **Trigger generative UI**: Have agent loop detect structured output and push generative view with A2UI schema.
