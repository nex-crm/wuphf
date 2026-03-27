# WUPHF CLI TUI -- Issue Report

## Test Environment
- Date: 2026-03-17
- Node: v25.4.0
- tsx: v4.21.0
- TypeScript: 5.7.x (compiles clean, zero errors)
- Tests passing: 499/499 (full suite), 154/154 (TUI-specific)
- Branch: nazz/feat/graph-cli

## Severity Legend
- P0: Crashes / won't launch
- P1: Feature completely broken
- P2: Feature works but buggy
- P3: UX polish / cosmetic

---

## Issues Found

### P0 -- Critical

| # | Issue | Location | Description | Fix |
|---|-------|----------|-------------|-----|
| -- | (none) | -- | TUI launches, compiles clean, all imports resolve, no crashes detected | -- |

### P1 -- Major

| # | Issue | Location | Description | Fix |
|---|-------|----------|-------------|-----|
| 1 | TextInput does not clear after submit | `src/tui/views/home.tsx:132-137` | `@inkjs/ui` TextInput uses `defaultValue` which is only read on initial mount (useReducer initial state). When `setInputValue("")` is called after submit, the React state updates but TextInput's internal reducer state is never reset. The input field keeps showing the old text. | Replace `defaultValue={inputValue}` with a key-based remount pattern: add a `key={submitCount}` prop to TextInput and increment a counter on each submit, forcing a fresh mount. Alternatively, use `@inkjs/ui`'s lower-level `useTextInput` hook to get direct control over the state reset. |
| 2 | TextInput does not clear in ChatInput component | `src/tui/components/chat-input.tsx:32-37` | Same root cause as #1. ChatInput passes `defaultValue={value}` to TextInput. When parent sets value to "" after submit, TextInput ignores it. Affects ask-chat view and chat view. | Same fix as #1: use key-based remount or switch to useTextInput hook. |
| 3 | Error messages reference nonexistent `wuphf setup` command | `src/lib/errors.ts:9`, `src/lib/client.ts:69`, `src/plugin/shared.ts:274` | AuthError default message says "Run 'wuphf setup'" and the client 401 handler says "Run 'wuphf setup' to re-authenticate." The actual command is `wuphf init`. Users following the error guidance will get "Unknown command: setup" (although `setup` IS aliased to `init` in dispatch.ts, the error message is still misleading for TUI users who would use `/init`). | Change all three references from `wuphf setup` to `wuphf init`. |

### P2 -- Minor

| # | Issue | Location | Description | Fix |
|---|-------|----------|-------------|-----|
| 4 | Typing just "/" dispatches "ask /" to API | `src/tui/register-views.tsx:100` | `parseSlashInput("/")` returns `{isSlash: true, command: "", args: ""}`. Since `parsed.command` is `""` (falsy), the `if (parsed.isSlash && parsed.command)` check fails, and the code falls through to the natural-language handler which dispatches `ask /` to the API. This wastes an API call and returns a confusing response. | Add an explicit check: `if (trimmed === "/") return` or check `parsed.isSlash && !parsed.command` to show "Unknown command. Type /help for commands." |
| 5 | `/quit` and `/q` may not terminate process | `src/tui/app.tsx:68-78`, `src/tui/slash-commands.ts:311` | `process.exit` is overridden to call Ink's `exit()` (unmount only). The `/quit` command calls `process.exit(0)` which unmounts Ink but does NOT terminate the Node process. If any service singletons (AgentService, ChatService, etc.) hold file handles or timers, the process hangs. | After calling `exit()` in the process.exit override, also schedule `originalExit(code)` with a short delay to ensure actual termination: `setTimeout(() => originalExit(_code ?? 0), 100)`. |
| 6 | `handleSubmit` async errors silently dropped | `src/tui/views/home.tsx:94-98` | `handleSubmit` in ConversationView calls `onSubmit(value)` without awaiting the result. The `onSubmit` prop is typed as `(input: string) => void` but register-views passes an async function. If the async handler throws, it becomes an unhandled promise rejection. | Change the type to `(input: string) => void | Promise<void>` and wrap the call: `Promise.resolve(onSubmit(value)).catch(...)`. Or make handleSubmit async. |
| 7 | Double loading state set in slash command flow | `src/tui/register-views.tsx:116-117`, `src/tui/slash-commands.ts:135` | When executing `/ask`, handleSubmit sets loading with hint "Running /ask..." then the slash command's execute function immediately overrides with "thinking...". The first hint flashes briefly. | Remove the loading set from the generic handler in register-views (lines 116-117) since individual slash commands manage their own loading. |
| 8 | `onSubmit` type mismatch in ConversationViewProps | `src/tui/views/home.tsx:18` | `onSubmit` is typed as `(input: string) => void` but always receives an async handler from register-views. TypeScript allows this (Promise<void> is assignable to void) but it masks the async nature. | Change to `(input: string) => void \| Promise<void>`. |

### P3 -- Polish

| # | Issue | Location | Description | Fix |
|---|-------|----------|-------------|-----|
| 9 | No Escape key hint on home view | `src/tui/app.tsx:93-99` | When on home view, pressing Escape does nothing (correctly -- nothing to pop). But the status bar shows "Esc=back" which is misleading when there's no view to go back to. | Only show "Esc=back" in the status bar hint when `viewStack.length > 1`. |
| 10 | `generative` view missing from integration test | `tests/tui/register-views-integration.test.ts:280` | The `expectedViews` list is missing "generative". The view IS registered in register-views.tsx but not verified in the test. | Add `"generative"` to the expectedViews array. |
| 11 | CalendarView left/right week navigation not wired | `src/tui/views/calendar.tsx:103` | The legend says "Use left/right to change week" but no keybinding dispatches week offset changes. The view only renders the current week. | Wire left/right arrow keys in the keybinding handler for calendar view to dispatch week offset changes. |
| 12 | Help screen shows old keybinding hints for sub-views | `src/tui/components/help-screen.tsx` | The help screen references keybindings like `a` for agents, `c` for chat, `C` for calendar, `o` for orchestration -- these only work in sub-view normal mode, not in the conversation home. Users in conversation mode should use `/agents`, `/chat`, etc. | Add a "Conversation Mode" section to the help screen listing slash commands. |
| 13 | Message history scrolling is approximate | `src/tui/views/home.tsx:92` | `displayMessages = messages.slice(-visibleRows)` assumes ~1 line per message. Multi-line messages or long messages that wrap will push content off-screen. | Track actual rendered line count or use a more sophisticated windowing approach. |
| 14 | No visual feedback when pressing Enter on empty input | `src/tui/views/home.tsx:95` | Empty input is silently ignored. User might think the input is broken. | Show a subtle cursor flash or do nothing (current behavior is acceptable, just noting). |

---

## Test Results Summary

| Category | Tests | Pass | Fail | Notes |
|----------|-------|------|------|-------|
| TypeScript compilation | 1 | 1 | 0 | Zero type errors with `tsc --noEmit` |
| TUI Launch | 1 | 1 | 0 | Launches successfully, exits cleanly on timeout |
| --version | 1 | 1 | 0 | Prints "0.1.22" |
| --help | 1 | 1 | 0 | Shows full command listing with categories |
| CLI dispatch: ask | 1 | 0 | 1 | Exit code 2 (auth error -- expected without valid key) |
| CLI dispatch: search | 1 | 0 | 1 | Exit code 2 (auth error -- expected without valid key) |
| CLI dispatch: object list | 1 | 0 | 1 | Exit code 2 (auth error -- expected without valid key) |
| CLI dispatch: agent list | 1 | 1 | 0 | Returns empty list (no auth needed, in-memory) |
| CLI dispatch: agent templates | 1 | 1 | 0 | Lists 5 templates |
| CLI dispatch: detect | 1 | 1 | 0 | Detected 6 platforms |
| CLI dispatch: init | 1 | 1 | 0 | Already authenticated, installs for 6 platforms |
| CLI dispatch: config show | 1 | 1 | 0 | Shows masked API key and workspace info |
| CLI dispatch: config path | 1 | 1 | 0 | Shows config file path |
| CLI dispatch: graph | 1 | 0 | 1 | Exit code 2 (auth error -- expected) |
| CLI dispatch: unknown cmd | 1 | 1 | 0 | Shows "Unknown command" error |
| Piped stdin | 2 | 2 | 0 | Dispatches piped input; empty stdin shows help |
| Import chain | 7 | 7 | 0 | All TUI module imports resolve without circular deps |
| Slash command parsing | 8 | 8 | 0 | All edge cases pass |
| Slash command registry | 18 | 18 | 0 | All 17 commands registered + unknown check |
| Slash command execution | 12 | 12 | 0 | Help, navigation, loading, dispatch wiring |
| TUI Store | 30 | 30 | 0 | All reducer actions, subscribe/unsubscribe |
| Keybindings | 23 | 23 | 0 | Normal mode, insert mode, key parsing |
| Component rendering | 27 | 27 | 0 | Picker, StatusBar, Viewport, AgentCard, MessageList, HelpScreen |
| Register-views integration | 24 | 24 | 0 | Home adapter, ask-chat adapter, service subscriptions |
| View rendering | 7 | 7 | 0 | Orchestration view (goals, tasks, budget bars) |
| **Full test suite** | **499** | **499** | **0** | **All tests pass** |

---

## Recommendations

Priority-ordered list of fixes:

1. **[P1] Fix TextInput clear-on-submit** (Issues #1, #2)
   - This is the highest-impact UX bug. Users cannot compose a second message after the first submit because the input field retains the old text.
   - Fix: In `home.tsx`, add a `submitCount` state that increments on each submit, and pass `key={submitCount}` to the TextInput. This forces React to remount the TextInput with a fresh internal state each time.
   - Apply the same pattern in `chat-input.tsx`.

2. **[P1] Fix "wuphf setup" references** (Issue #3)
   - Simple find-and-replace in three files. Change to "wuphf init" (or "/init" for TUI context).

3. **[P2] Guard against "/" alone** (Issue #4)
   - Add early return in `handleSubmit` when `parsed.isSlash` is true but `parsed.command` is empty.

4. **[P2] Fix process exit** (Issue #5)
   - Add `setTimeout(() => originalExit(_code ?? 0), 100)` after `exit()` in the process.exit override.

5. **[P2] Fix async error handling** (Issue #6)
   - Make `handleSubmit` in home.tsx async and add try/catch around `onSubmit(value)`.

6. **[P3] Polish status bar hints** (Issue #9)
   - Conditionally show "Esc=back" only when viewStack.length > 1.

7. **[P3] Add generative to integration test** (Issue #10)
   - One-line addition to the expectedViews array.

8. **[P3] Help screen update** (Issue #12)
   - Add slash command reference to the help screen for conversation mode users.
