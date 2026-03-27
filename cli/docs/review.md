# WUPHF CLI TUI -- Integration Review

## Build Summary

| Metric       | Count |
|-------------|-------|
| Source files | 110   |
| Test files   | 24 (23 `.test.ts` + 1 `.test.tsx`) |
| Tests passing| 328 (was 283 -- fixed test glob to include `.tsx` tests) |
| New LOC      | ~6,430 (source) + ~2,662 (tests) |
| TypeScript   | Clean compile, zero errors |

## Integration Gaps Found and Fixed

### Gap 1: Views Not Registered with Router (FIXED)
**Problem:** `router.tsx` defines a `registerView()` function and a `viewRegistry` map, but no code ever calls `registerView()`. All 8 views (home, help, record-list, record-detail, ask-chat, agent-list, chat, calendar) would render as `PlaceholderView` (a "not yet implemented" stub).

**Fix:** Created `src/tui/register-views.tsx` which imports all 8 view components and registers them with the router via adapter wrappers that translate generic props to typed component props. Imported as a side-effect module from `app.tsx`.

### Gap 2: Home View Not Wired to dispatch() (FIXED)
**Problem:** `HomeView` accepts `onCommand` and `onSubmit` callbacks but nothing connected these to `dispatch()` from `src/commands/dispatch.ts`. Commands typed in the TUI would have no effect.

**Fix:** The `register-views.tsx` adapter for "home" wires both callbacks through to `dispatch()`. The command picker's items are populated from `commandHelp` (the dispatch registry's exported command list).

### Gap 3: ask-chat View Not Wired to dispatch() (FIXED)
**Problem:** `AskChatView` has an `onAsk` callback but nothing connected it to the API.

**Fix:** The adapter wires `onAsk` to `dispatch("ask <question>")`.

### Gap 4: App.tsx Used Inline StatusBar Instead of Component (FIXED)
**Problem:** `app.tsx` defined its own inline `StatusBar` function instead of using the reusable `StatusBar` component from `src/tui/components/status-bar.tsx`.

**Fix:** Replaced the inline 25-line status bar with a thin adapter (`AppStatusBar`) that delegates to the component version, passing breadcrumbs from the view stack and contextual hints.

### Gap 5: Test Glob Missing .tsx Files (FIXED)
**Problem:** `package.json` test script glob (`tests/**/*.test.ts`) did not match `.test.tsx` files. The 20 generative renderer tests in `tests/tui/generative/renderer.test.tsx` were never executed by `npm test`.

**Fix:** Updated the test glob to also include `.test.tsx` patterns.

### Gap 6: Unused Imports in app.tsx (FIXED)
**Problem:** `parseKey` and `useTheme` were imported but never used.

**Fix:** Removed the unused imports.

## Integration Status

### Wired (Working)

| From | To | How |
|------|----|-----|
| Entry point (`src/index.ts`) | TUI (`src/tui/index.tsx`) | Dynamic import via `shouldLaunchTui()` |
| `src/cli.ts` | Entry point | `--tui` / `--classic` flags |
| `app.tsx` | Router | Direct import, passes viewStack + dispatch |
| `app.tsx` | View registry | Side-effect import of `register-views.js` |
| Router | View components | `registerView()` + `viewRegistry.get()` |
| Router | Components | Breadcrumb uses theme, views use Picker/Viewport/StatusBar |
| Home view | dispatch() | `onCommand` and `onSubmit` wired through adapter |
| Ask-chat view | dispatch() | `onAsk` wired through adapter |
| Keybindings | Store | `handleKey()` calls `store.dispatch()` directly |
| Store | Views | `subscribe()` triggers `setState()` in App |

### Standalone (Not Yet Wired to TUI)

| Module | Status | What's Missing |
|--------|--------|---------------|
| `src/agent/` | Has loop, tools, sessions, queues | No TUI trigger starts the agent loop. `agent-list` view receives agents as props but nothing fetches actual agent state. |
| `src/chat/` | Has channels, messages, router, suggested-responses | `chat` view receives channels/messages as props but nothing connects to `ChannelManager` or `MessageStore`. |
| `src/calendar/` | Has scheduler, store | `calendar` view receives events as props but nothing fetches from the scheduler. |
| `src/orchestration/` | Has budget, router, executor, templates | Fully standalone -- no TUI integration point exists yet. |
| `src/tui/generative/` | Has renderer, bindings, registry | Standalone -- no view or mechanism invokes `GenerativeRenderer` from the main TUI flow. |

## Remaining TODOs for Next Session

1. **Agent runtime integration**: Create a service layer that bridges `src/agent/AgentLoop` to the TUI. The `agent-list` view should fetch real agent states, and navigation should allow starting/stopping/steering agents.

2. **Chat integration**: Wire `ChannelManager` and `MessageStore` to the `chat` view. Messages should flow from agents through the chat system to the TUI.

3. **Calendar integration**: Wire `Scheduler` to the `calendar` view. Display real heartbeat schedules.

4. **Orchestration panel**: Create a TUI view for orchestration (task pool, active goals, budget status).

5. **Generative TUI integration**: Add a mechanism for agents to emit A2UI JSON schemas that the `GenerativeRenderer` renders inline within views.

6. **Knowledge propagation**: Implement Ask/Remember API and wire gossip/adoption scoring to agent interactions.

7. **Insert-mode command execution end-to-end**: The keybindings layer pushes history on Enter in insert mode, but the actual command text needs to flow through `dispatch()` and render the result in the viewport. Currently the comment says "actual command execution is handled by the component layer" but that path goes through the registered home view adapter only when the home view is active.

8. **Error surfacing**: `dispatch()` returns errors in `CommandResult.error` but the adapter in `register-views.tsx` currently swallows them. Errors should be displayed in the content area or as a notification.

## Architecture Quality Assessment

**Strengths:**
- Clean separation of concerns: store, keybindings, router, views, components are all independent
- The store + reducer pattern is solid and well-tested (21 store tests)
- The dispatch layer is comprehensive (55 commands) with proper error typing
- View components are properly typed with explicit props interfaces
- The `registerView` pattern allows lazy registration without circular imports

**Weaknesses:**
- The 5-agent parallel build created clean modules that don't talk to each other. Each agent built their piece correctly in isolation, but no agent wired the seams.
- The `register-views.tsx` adapters are thin shims that cast `Record<string, unknown>` to typed props -- this works but loses type safety at the boundary. A union type for view props in the store would be stronger.
- `app.tsx` still has its own inline insert-mode input line (lines 106-114) that duplicates what `ChatInput` does. This should be consolidated.
- The keybindings layer is tightly coupled to the store's action types rather than going through a command abstraction. Adding new commands requires touching both `keybindings.ts` and the view layer.
