# Web UX Parity Audit

> TUI source audit vs `web/index.html` — updated 2026-04-09

---

## 1. Feature Inventory

| # | Feature | TUI File(s) | Web Status | Priority | Effort |
|---|---------|-------------|------------|----------|--------|
| **Messaging** | | | | | |
| 1 | Send messages to channel | `channel.go` | Done | P0 | - |
| 2 | Message grouping (same sender within 5 min) | `channel_messages.go` | Done | P0 | - |
| 3 | Date separators (Today / Yesterday / date) | `channel_messages.go` | Done | P1 | - |
| 4 | Markdown rendering (bold, code, headers, lists) | `channel_window.go`, `renderMarkdown` | Done | P0 | - |
| 5 | @mention highlighting | `channel_render.go` `highlightMentions` | Done | P1 | - |
| 6 | [STATUS] message rendering (compact italic) | `channel_messages.go` | Done | P1 | - |
| 7 | Human message cards (human_decision, human_action) | `channel_window.go` | Done | P1 | - |
| 8 | Automation message cards (nex/automation kind) | `channel_window.go` | Done | P2 | - |
| 9 | Routing/stage system message cards | `channel_window.go` | Done | P2 | - |
| 10 | Mood inference on messages | `channel_window.go` `inferMood` | Done | P2 | - |
| 11 | Reactions display (emoji + count pills) | `channel_render.go` `renderReactions` | Done | P1 | - |
| 12 | Unread divider ("N new since you looked") | `channel_render.go` `renderUnreadDivider` | Done | P1 | - |
| 13 | A2UI block rendering (structured UI in messages) | `channel_window.go` `renderA2UIBlocks` | Missing | P1 | L |
| 14 | Agent character face in message header | `channel_styles.go` `agentCharacter` | Done | P2 | - |
| **Threading** | | | | | |
| 15 | Thread panel (parent + replies + input) | `channel_thread.go` | Done | P0 | - |
| 16 | Thread reply count indicator on messages | `channel_messages.go` `countReplies` | Done | P0 | - |
| 17 | Nested thread replies (depth-aware rendering) | `channel_thread.go` `flattenThreadReplies` | Done | P1 | - |
| 18 | Expand/collapse thread inline | `channel.go` `/expand`, `/collapse` | N/A | - | - |
| 19 | Thread default expand/collapse toggle | `channel.go` `threadsDefaultExpand` | N/A | - | - |
| 20 | Thread reply to indicator ("reply to @name") | `channel_thread.go` | N/A | - | - |
| **Sidebar** | | | | | |
| 21 | Channel list with active highlight | `channel_sidebar.go` | Done | P0 | - |
| 22 | Agent roster with status dots | `channel_sidebar.go` | Done | P0 | - |
| 23 | Agent activity classification (talking/shipping/plotting/lurking) | `channel_sidebar.go` `classifyActivity` | Done | P0 | - |
| 24 | Agent live activity text (from Claude Code pane) | `channel_sidebar.go` `summarizeLiveActivity` | Missing | P1 | M |
| 25 | ASCII pixel art avatars in sidebar | `avatars_wuphf.go`, `channel_sidebar.go` | Done | P2 | - |
| 26 | Thought bubble for agents | `channel_sidebar.go` `renderThoughtBubble` | Missing | P2 | M |
| 27 | Task-aware agent status (working/reviewing/blocked/queued) | `channel_sidebar.go` `applyTaskActivity` | Done | P1 | - |
| 28 | Apps section (Tasks, Requests, Policies, Calendar, etc.) | `channel_sidebar.go` `officeSidebarApps` | Done | P0 | - |
| 29 | Quick jump (Ctrl+G channels, Ctrl+O apps, 1-9 shortcuts) | `channel_sidebar.go`, `channel.go` | Missing | P2 | M |
| 30 | Workspace summary line in sidebar | `channel_workspace_state.go` `sidebarSummaryLine` | Done | P1 | - |
| 31 | Workspace hint line in sidebar | `channel_workspace_state.go` `sidebarHintLine` | Done | P1 | - |
| 32 | PgUp/PgDn roster scrolling | `channel_sidebar.go` | N/A | - | - |
| **Composer** | | | | | |
| 33 | Basic text input + send | `channel_composer.go` | Done | P0 | - |
| 34 | Slash command autocomplete (/ prefix) | `channel.go`, `internal/tui` | Done | P0 | - |
| 35 | @mention autocomplete (@ prefix) | `channel.go`, `internal/tui` | Done | P0 | - |
| 36 | Typing indicator (agents currently typing) | `channel_composer.go` `typingAgentsFromMembers` | Done | P1 | - |
| 37 | Live activity indicator (per-agent Claude Code status) | `channel_composer.go` `liveActivityFromMembers` | Missing | P1 | M |
| 38 | Composer context hints (reply mode, interview mode, etc.) | `channel_context.go` `composerHint` | Done | P1 | - |
| 39 | Reply-to mode (Ctrl+R or /reply) | `channel.go` | Done | P0 | - |
| 40 | Composer input history (Up/Down recall) | `channel_history.go` | Done | P2 | - |
| 41 | Multi-line input (Ctrl+J newline) | `channel_composer.go` | Done | P1 | - |
| 42 | Esc pause all agents | `channel.go` | Missing | P2 | M |
| **Requests / Interviews** | | | | | |
| 43 | Request list view (/requests app) | `channel_render.go` `buildRequestLines` | Done | P0 | - |
| 44 | Interview answer flow (choose/draft/review phases) | `channel_interview.go` | Done | P0 | - |
| 45 | Interview option selection (Up/Down, Enter) | `channel.go` | Done | P0 | - |
| 46 | Confirmation dialog for interview submission | `channel_confirm.go` `confirmationForInterviewAnswer` | Done | P0 | - |
| 47 | "Needs you" banner (blocking requests) | `channel_needs_you.go` | Done | P0 | - |
| 48 | Request kind pills (approval, confirm, secret, etc.) | `channel_styles.go` `requestKindPill` | Done | P1 | - |
| 49 | Request timing display (due, follow-up, reminder) | `channel_render.go` `renderTimingSummary` | Missing | P1 | S |
| **Tasks** | | | | | |
| 50 | Task board view (/tasks app) | `channel_render.go` `buildTaskLines` | Done | P0 | - |
| 51 | Task status pills (moving/review/blocked/done/open) | `channel_styles.go` `taskStatusPill` | Done | P1 | - |
| 52 | Task action commands (claim, release, complete, block) | `channel.go` `/task` | Missing | P1 | M |
| 53 | Task worktree info display | `channel_render.go` | Missing | P2 | S |
| 54 | Task click-to-focus (jump to thread) | `channel_insert_search.go` | Missing | P1 | M |
| **Apps / Views** | | | | | |
| 55 | Recovery view (/recover app) | `channel_workspace_state.go` | Done | P0 | - |
| 56 | Policies view (signals, decisions, watchdogs, actions) | `channel_render.go` `buildPolicyLines` | Done | P1 | - |
| 57 | Calendar view (scheduled work, teammate calendars) | `channel_render.go` `buildCalendarLines` | Done | P1 | - |
| 58 | Artifacts view (task logs, workflow runs, approvals) | `channel_artifacts.go` | Missing | P2 | L |
| 59 | Skills view (reusable skills and workflows) | `channel_render.go` `buildSkillLines` | Done | P2 | - |
| 60 | Inbox view (1:1 mode -- agent inbox lane) | `channel_mailboxes.go` `buildInboxLines` | Done | P1 | - |
| 61 | Outbox view (1:1 mode -- agent outbox lane) | `channel_mailboxes.go` `buildOutboxLines` | Done | P1 | - |
| **Workspace / Navigation** | | | | | |
| 62 | Channel switching | `channel_switcher.go`, `channel.go` | Done | P0 | - |
| 63 | Unified switcher (/switcher -- channels, apps, DMs, tasks, threads) | `channel_switcher.go` | Done | P0 | - |
| 64 | Search picker (/search -- cross-entity search) | `channel_insert_search.go` `buildSearchPickerOptions` | Missing | P1 | L |
| 65 | Insert picker (/insert -- insert references into composer) | `channel_insert_search.go` `buildInsertPickerOptions` | Missing | P2 | M |
| 66 | Rewind picker (/rewind -- recovery prompt from message) | `channel_insert_search.go` `buildRecoveryPromptPickerOptions` | Missing | P2 | M |
| **Session Management** | | | | | |
| 67 | 1:1 direct session mode | `channel.go`, `channel_confirm.go` | Done | P0 | - |
| 68 | Session mode switch (office <-> 1:1) with confirmation | `channel_confirm.go` `confirmationForSessionSwitch` | Done | P0 | - |
| 69 | Reset session (/reset) with confirmation | `channel_confirm.go` `confirmationForReset` | Missing | P1 | M |
| 70 | Reset DM (/reset-dm) | `channel_confirm.go` `confirmationForResetDM` | Missing | P2 | S |
| **Agent Management** | | | | | |
| 71 | Agent detail panel (name, role, stats, skills) | `web/index.html` agent-panel | Done | P0 | - |
| 72 | Agent panel -- real activity data | `channel_activity.go` | Done | P0 | - |
| 73 | Create new agent (/agent prompt) | `channel_member_draft.go` | Missing | P1 | L |
| 74 | Edit existing agent | `channel_member_draft.go` `startEditMemberDraft` | Missing | P1 | L |
| 75 | Agent draft wizard (slug/name/role/expertise/personality/permission) | `channel_member_draft.go` | Missing | P1 | L |
| 76 | Enable/disable agent in channel | `channel.go` `/agent` | Missing | P1 | M |
| **Activity / Runtime** | | | | | |
| 77 | Live work section ("Live work now" cards) | `channel_activity.go` `buildLiveWorkLines` | Done | P0 | - |
| 78 | Execution timeline (direct execution actions) | `channel_activity.go` `buildDirectExecutionLines` | Missing | P1 | M |
| 79 | Wait state display ("Nothing is moving") | `channel_activity.go` `buildWaitStateLines` | Missing | P2 | S |
| 80 | Blocked work display | `channel_activity.go` `blockedWorkTasks` | Missing | P1 | M |
| 81 | Runtime strip (status pills: active/blocked/need you) | `channel_activity.go` `renderRuntimeStrip` | Done | P1 | - |
| **Diagnostics / Setup** | | | | | |
| 82 | Doctor panel (/doctor -- readiness checks) | `channel_doctor.go` | Missing | P1 | L |
| 83 | Init flow (/init -- setup wizard) | `channel.go`, `internal/tui/init_flow.go` | Missing | P1 | L |
| 84 | Integration connect (/integrate) | `channel.go` `channelIntegrationSpecs` | Missing | P2 | L |
| 85 | Telegram connect flow | `channel.go` | Missing | P2 | L |
| 86 | Provider switching (/provider) | `channel.go` | Missing | P2 | M |
| **Visual / UX Polish** | | | | | |
| 87 | Splash screen (The Office intro animation) | `channel_splash.go` | Done | P2 | - |
| 88 | Pixel art character sprites per agent | `avatars_wuphf.go` | Done | P2 | - |
| 89 | Confirmation dialogs (generic) | `channel_confirm.go` `renderConfirmCard` | Done | P0 | - |
| 90 | Notice/toast system | `channel.go` `notice` | Done | P1 | - |
| 91 | Status bar (bottom bar with context info) | `channel_workspace_state.go` `defaultStatusLine` | Missing | P1 | M |
| 92 | Channel header with meta info (teammates, running tasks) | `channel_workspace_state.go` `headerMeta` | Done | P1 | - |
| 93 | Theme switcher (editorial, Slack, Windows 98) | `web/index.html` theme-switcher | Done | P2 | - |
| 94 | Disconnect detection + reconnect banner | `web/index.html` | Done | P0 | - |
| 95 | Keyboard shortcuts (Ctrl+K, Ctrl+R, Ctrl+Shift+T) | `channel.go` key handling | Done | P1 | - |
| **Slash Commands** | | | | | |
| 96 | /init | `channel.go` | Missing | P1 | L |
| 97 | /doctor | `channel.go` | Missing | P1 | L |
| 98 | /switcher (via Ctrl+K) | `channel.go` | Done | P0 | - |
| 99 | /recover | `channel.go` | Done | P0 | - |
| 100 | /tasks | `channel.go` | Done | P0 | - |
| 101 | /requests | `channel.go` | Done | P0 | - |
| 102 | /policies | `channel.go` | Done | P1 | - |
| 103 | /calendar | `channel.go` | Done | P1 | - |
| 104 | /artifacts | `channel.go` | Missing | P2 | L |
| 105 | /skills | `channel.go` | Done | P2 | - |
| 106 | /reply | `channel.go` | Done | P0 | - |
| 107 | /search | `channel.go` | Done | P1 | - |
| 108 | /insert | `channel.go` | Missing | P2 | M |
| 109 | /1o1 | `channel.go` | Done | P0 | - |
| 110 | /reset | `channel.go` | Missing | P1 | M |
| 111 | /agents | `channel.go` | Missing | P1 | M |
| 112 | /channels | `channel.go` | Done | P1 | - |
| 113 | /threads | `channel.go` | Missing | P1 | M |
| 114 | /expand, /collapse | `channel.go` | N/A | - | - |
| 115 | /cancel | `channel.go` | Missing | P1 | S |
| 116 | /task (claim/release/complete/block) | `channel.go` | Missing | P1 | M |
| 117 | /skill (create/invoke/manage) | `channel.go` | Missing | P2 | L |
| 118 | /connect (Telegram/Slack/Discord) | `channel.go` | Missing | P2 | L |
| 119 | /rewind | `channel.go` | Missing | P2 | M |
| 120 | /quit | `channel.go` | N/A | - | - |

---

## 2. Summary

| Status | Count |
|--------|-------|
| Done | 82 |
| Missing | 33 |
| N/A | 6 |
| **Total** | **121** |

The web UI covers ~68% of TUI features, up from 9% at initial audit. All P0 features are complete. Remaining gaps are mostly P1/P2 power-user features: agent management wizard, search/insert pickers, diagnostics, task actions, and integration connectors.

---

## 3. Remaining Work (prioritized)

### P1 Missing (should-have for daily driver)
1. **A2UI block rendering** (#13) — L effort, structured UI blocks in messages
2. **Agent live activity text** (#24) — M effort, Claude Code status in sidebar
3. **Task action commands** (#52, #116) — M effort, claim/release/complete from web
4. **Task click-to-focus** (#54) — M effort, jump to task thread
5. **Search picker** (#64) — L effort, cross-entity search
6. **Reset session** (#69, #110) — M effort, /reset with confirmation
7. **Agent create/edit** (#73-75) — L effort, team management wizard
8. **Enable/disable agent** (#76) — M effort, toggle agent participation
9. **Execution timeline** (#78) — M effort, action history
10. **Blocked work display** (#80) — M effort, surface blocked tasks
11. **Doctor panel** (#82, #97) — L effort, runtime diagnostics
12. **Init flow** (#83, #96) — L effort, first-run setup
13. **Status bar** (#91) — M effort, persistent bottom bar
14. **Request timing** (#49) — S effort, due dates on requests
15. **/cancel** (#115) — S effort, cancel running task
16. **/threads** (#113) — M effort, thread list view
17. **/agents** (#111) — M effort, agent roster view

### P2 Missing (nice-to-have for full parity)
18. Thought bubbles (#26) — M
19. Quick jump shortcuts (#29) — M
20. Esc pause all (#42) — M
21. Task worktree info (#53) — S
22. Artifacts view (#58, #104) — L
23. Insert picker (#65, #108) — M
24. Rewind picker (#66, #119) — M
25. Wait state display (#79) — S
26. Integration connect (#84, #85, #118) — L
27. Provider switching (#86) — M
28. Skill commands (#117) — L
