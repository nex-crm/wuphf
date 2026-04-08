# Web UX Parity Audit

> TUI source audit vs `web/index.html` — generated 2026-04-08

---

## 1. Feature Inventory

| # | Feature | TUI File(s) | Web Status | Priority | Effort |
|---|---------|-------------|------------|----------|--------|
| **Messaging** | | | | | |
| 1 | Send messages to channel | `channel.go` | Done | P0 | - |
| 2 | Message grouping (same sender within 5 min) | `channel_messages.go` | Missing | P0 | S |
| 3 | Date separators (Today / Yesterday / date) | `channel_messages.go` | Missing | P1 | S |
| 4 | Markdown rendering (bold, code, headers, lists) | `channel_window.go`, `renderMarkdown` | Partial | P0 | M |
| 5 | @mention highlighting | `channel_render.go` `highlightMentions` | Missing | P1 | S |
| 6 | [STATUS] message rendering (compact italic) | `channel_messages.go` | Missing | P1 | S |
| 7 | Human message cards (human_decision, human_action) | `channel_window.go` | Missing | P1 | M |
| 8 | Automation message cards (nex/automation kind) | `channel_window.go` | Missing | P2 | M |
| 9 | Routing/stage system message cards | `channel_window.go` | Missing | P2 | S |
| 10 | Mood inference on messages | `channel_window.go` `inferMood` | Missing | P2 | S |
| 11 | Reactions display (emoji + count pills) | `channel_render.go` `renderReactions` | Missing | P1 | S |
| 12 | Unread divider ("N new since you looked") | `channel_render.go` `renderUnreadDivider` | Missing | P1 | M |
| 13 | A2UI block rendering (structured UI in messages) | `channel_window.go` `renderA2UIBlocks` | Missing | P1 | L |
| 14 | Agent character face in message header | `channel_styles.go` `agentCharacter` | Missing | P2 | S |
| **Threading** | | | | | |
| 15 | Thread panel (parent + replies + input) | `channel_thread.go` | Done | P0 | - |
| 16 | Thread reply count indicator on messages | `channel_messages.go` `countReplies` | Done | P0 | - |
| 17 | Nested thread replies (depth-aware rendering) | `channel_thread.go` `flattenThreadReplies` | Missing | P1 | M |
| 18 | Expand/collapse thread inline | `channel.go` `/expand`, `/collapse` | Missing | P1 | M |
| 19 | Thread default expand/collapse toggle | `channel.go` `threadsDefaultExpand` | Missing | P2 | S |
| 20 | Thread reply to indicator ("reply to @name") | `channel_thread.go` | Missing | P2 | S |
| **Sidebar** | | | | | |
| 21 | Channel list with active highlight | `channel_sidebar.go` | Done | P0 | - |
| 22 | Agent roster with status dots | `channel_sidebar.go` | Partial | P0 | M |
| 23 | Agent activity classification (talking/shipping/plotting/lurking) | `channel_sidebar.go` `classifyActivity` | Missing | P0 | M |
| 24 | Agent live activity text (from Claude Code pane) | `channel_sidebar.go` `summarizeLiveActivity` | Missing | P1 | M |
| 25 | ASCII pixel art avatars in sidebar | `avatars_wuphf.go`, `channel_sidebar.go` | Missing | P2 | L |
| 26 | Thought bubble for agents | `channel_sidebar.go` `renderThoughtBubble` | Missing | P2 | M |
| 27 | Task-aware agent status (working/reviewing/blocked/queued) | `channel_sidebar.go` `applyTaskActivity` | Missing | P1 | M |
| 28 | Apps section (Tasks, Requests, Policies, Calendar, etc.) | `channel_sidebar.go` `officeSidebarApps` | Missing | P0 | M |
| 29 | Quick jump (Ctrl+G channels, Ctrl+O apps, 1-9 shortcuts) | `channel_sidebar.go`, `channel.go` | Missing | P2 | M |
| 30 | Workspace summary line in sidebar | `channel_workspace_state.go` `sidebarSummaryLine` | Missing | P1 | S |
| 31 | Workspace hint line in sidebar | `channel_workspace_state.go` `sidebarHintLine` | Missing | P1 | S |
| 32 | PgUp/PgDn roster scrolling | `channel_sidebar.go` | Missing | P2 | S |
| **Composer** | | | | | |
| 33 | Basic text input + send | `channel_composer.go` | Done | P0 | - |
| 34 | Slash command autocomplete (/ prefix) | `channel.go`, `internal/tui` | Missing | P0 | L |
| 35 | @mention autocomplete (@ prefix) | `channel.go`, `internal/tui` | Missing | P0 | L |
| 36 | Typing indicator (agents currently typing) | `channel_composer.go` `typingAgentsFromMembers` | Partial | P1 | M |
| 37 | Live activity indicator (per-agent Claude Code status) | `channel_composer.go` `liveActivityFromMembers` | Missing | P1 | M |
| 38 | Composer context hints (reply mode, interview mode, etc.) | `channel_context.go` `composerHint` | Missing | P1 | S |
| 39 | Reply-to mode (Ctrl+R or /reply) | `channel.go` | Missing | P0 | M |
| 40 | Composer input history (Up/Down recall) | `channel_history.go` | Missing | P2 | M |
| 41 | Multi-line input (Ctrl+J newline) | `channel_composer.go` | Partial | P1 | S |
| 42 | Esc pause all agents | `channel.go` | Missing | P2 | M |
| **Requests / Interviews** | | | | | |
| 43 | Request list view (/requests app) | `channel_render.go` `buildRequestLines` | Partial | P0 | M |
| 44 | Interview answer flow (choose/draft/review phases) | `channel_interview.go` | Missing | P0 | L |
| 45 | Interview option selection (Up/Down, Enter) | `channel.go` | Partial | P0 | M |
| 46 | Confirmation dialog for interview submission | `channel_confirm.go` `confirmationForInterviewAnswer` | Missing | P0 | M |
| 47 | "Needs you" banner (blocking requests) | `channel_needs_you.go` | Missing | P0 | M |
| 48 | Request kind pills (approval, confirm, secret, etc.) | `channel_styles.go` `requestKindPill` | Missing | P1 | S |
| 49 | Request timing display (due, follow-up, reminder) | `channel_render.go` `renderTimingSummary` | Missing | P1 | S |
| **Tasks** | | | | | |
| 50 | Task board view (/tasks app) | `channel_render.go` `buildTaskLines` | Missing | P0 | M |
| 51 | Task status pills (moving/review/blocked/done/open) | `channel_styles.go` `taskStatusPill` | Missing | P1 | S |
| 52 | Task action commands (claim, release, complete, block) | `channel.go` `/task` | Missing | P1 | M |
| 53 | Task worktree info display | `channel_render.go` | Missing | P2 | S |
| 54 | Task click-to-focus (jump to thread) | `channel_insert_search.go` | Missing | P1 | M |
| **Apps / Views** | | | | | |
| 55 | Recovery view (/recover app) | `channel_workspace_state.go` | Missing | P0 | L |
| 56 | Policies view (signals, decisions, watchdogs, actions) | `channel_render.go` `buildPolicyLines` | Missing | P1 | L |
| 57 | Calendar view (scheduled work, teammate calendars) | `channel_render.go` `buildCalendarLines` | Missing | P1 | L |
| 58 | Artifacts view (task logs, workflow runs, approvals) | `channel_artifacts.go` | Missing | P2 | L |
| 59 | Skills view (reusable skills and workflows) | `channel_render.go` `buildSkillLines` | Missing | P2 | L |
| 60 | Inbox view (1:1 mode -- agent inbox lane) | `channel_mailboxes.go` `buildInboxLines` | Missing | P1 | M |
| 61 | Outbox view (1:1 mode -- agent outbox lane) | `channel_mailboxes.go` `buildOutboxLines` | Missing | P1 | M |
| **Workspace / Navigation** | | | | | |
| 62 | Channel switching | `channel_switcher.go`, `channel.go` | Done | P0 | - |
| 63 | Unified switcher (/switcher -- channels, apps, DMs, tasks, threads) | `channel_switcher.go` | Missing | P0 | L |
| 64 | Search picker (/search -- cross-entity search) | `channel_insert_search.go` `buildSearchPickerOptions` | Missing | P1 | L |
| 65 | Insert picker (/insert -- insert references into composer) | `channel_insert_search.go` `buildInsertPickerOptions` | Missing | P2 | M |
| 66 | Rewind picker (/rewind -- recovery prompt from message) | `channel_insert_search.go` `buildRecoveryPromptPickerOptions` | Missing | P2 | M |
| **Session Management** | | | | | |
| 67 | 1:1 direct session mode | `channel.go`, `channel_confirm.go` | Missing | P0 | L |
| 68 | Session mode switch (office <-> 1:1) with confirmation | `channel_confirm.go` `confirmationForSessionSwitch` | Missing | P0 | M |
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
| 77 | Live work section ("Live work now" cards) | `channel_activity.go` `buildLiveWorkLines` | Missing | P0 | M |
| 78 | Execution timeline (direct execution actions) | `channel_activity.go` `buildDirectExecutionLines` | Missing | P1 | M |
| 79 | Wait state display ("Nothing is moving") | `channel_activity.go` `buildWaitStateLines` | Missing | P2 | S |
| 80 | Blocked work display | `channel_activity.go` `blockedWorkTasks` | Missing | P1 | M |
| 81 | Runtime strip (status pills: active/blocked/need you) | `channel_activity.go` `renderRuntimeStrip` | Missing | P1 | M |
| **Diagnostics / Setup** | | | | | |
| 82 | Doctor panel (/doctor -- readiness checks) | `channel_doctor.go` | Missing | P1 | L |
| 83 | Init flow (/init -- setup wizard) | `channel.go`, `internal/tui/init_flow.go` | Missing | P1 | L |
| 84 | Integration connect (/integrate) | `channel.go` `channelIntegrationSpecs` | Missing | P2 | L |
| 85 | Telegram connect flow | `channel.go` | Missing | P2 | L |
| 86 | Provider switching (/provider) | `channel.go` | Missing | P2 | M |
| **Visual / UX Polish** | | | | | |
| 87 | Splash screen (The Office intro animation) | `channel_splash.go` | Missing | P2 | L |
| 88 | Pixel art character sprites per agent | `avatars_wuphf.go` | Missing | P2 | L |
| 89 | Confirmation dialogs (generic) | `channel_confirm.go` `renderConfirmCard` | Missing | P0 | M |
| 90 | Notice/toast system | `channel.go` `notice` | Missing | P1 | S |
| 91 | Status bar (bottom bar with context info) | `channel_workspace_state.go` `defaultStatusLine` | Missing | P1 | M |
| 92 | Channel header with meta info (teammates, running tasks) | `channel_workspace_state.go` `headerMeta` | Missing | P1 | S |
| 93 | Theme switcher (editorial, Slack, Windows 98) | `web/index.html` theme-switcher | Done | P2 | - |
| 94 | Disconnect detection + reconnect banner | `web/index.html` | Done | P0 | - |
| 95 | Keyboard shortcuts (Ctrl+G, Ctrl+O, Ctrl+R, PgUp/Down, etc.) | `channel.go` key handling | Missing | P1 | L |
| **Slash Commands** | | | | | |
| 96 | /init | `channel.go` | Missing | P1 | L |
| 97 | /doctor | `channel.go` | Missing | P1 | L |
| 98 | /switcher | `channel.go` | Missing | P0 | L |
| 99 | /recover | `channel.go` | Missing | P0 | L |
| 100 | /tasks | `channel.go` | Missing | P0 | M |
| 101 | /requests | `channel.go` | Missing | P0 | M |
| 102 | /policies | `channel.go` | Missing | P1 | M |
| 103 | /calendar | `channel.go` | Missing | P1 | L |
| 104 | /artifacts | `channel.go` | Missing | P2 | L |
| 105 | /skills | `channel.go` | Missing | P2 | L |
| 106 | /reply | `channel.go` | Missing | P0 | M |
| 107 | /search | `channel.go` | Missing | P1 | L |
| 108 | /insert | `channel.go` | Missing | P2 | M |
| 109 | /1o1 | `channel.go` | Missing | P0 | L |
| 110 | /reset | `channel.go` | Missing | P1 | M |
| 111 | /agents | `channel.go` | Missing | P1 | M |
| 112 | /channels | `channel.go` | Missing | P1 | M |
| 113 | /threads | `channel.go` | Missing | P1 | M |
| 114 | /expand, /collapse | `channel.go` | Missing | P1 | S |
| 115 | /cancel | `channel.go` | Missing | P1 | S |
| 116 | /task (claim/release/complete/block) | `channel.go` | Missing | P1 | M |
| 117 | /skill (create/invoke/manage) | `channel.go` | Missing | P2 | L |
| 118 | /connect (Telegram/Slack/Discord) | `channel.go` | Missing | P2 | L |
| 119 | /rewind | `channel.go` | Missing | P2 | M |
| 120 | /quit | `channel.go` | N/A | - | - |

---

## 2. Gap Analysis

### Critical Missing (P0)

**Message Grouping (#2)**
- TUI: Groups consecutive messages from the same sender within a 5-minute window into a single visual block. Only the first message in a group shows the avatar and name.
- Web: Every message renders with full avatar and name header.
- Build: Group messages by `from` field + 5-min timestamp window. Only render avatar/name for group head.
- API: Uses existing `/messages` endpoint, client-side grouping.

**Agent Activity Classification (#23)**
- TUI: Uses `classifyActivity()` to derive agent state from `lastTime`, `lastMessage`, and `liveActivity`. States: talking (< 10s), shipping (< 30s + tool keywords), plotting (< 30s), lurking (idle).
- Web: Shows a static green pulse dot for all agents.
- Build: Port `classifyActivity` logic to JS. Poll `/members` endpoint for `lastTime`/`lastMessage`/`liveActivity` fields.
- API: `/members` already returns these fields.

**Slash Command Autocomplete (#34)**
- TUI: Typing `/` opens an autocomplete popup with all available commands, filterable by typing.
- Web: No command system at all.
- Build: Implement a command palette dropdown triggered by `/` in composer. Match against `channelSlashCommands` list.
- API: Client-side. Command execution dispatches to various broker endpoints.

**@mention Autocomplete (#35)**
- TUI: Typing `@` opens a mention popup with all channel members.
- Web: No mention system.
- Build: Implement mention dropdown triggered by `@` in composer. Insert `@slug` into text.
- API: `/office-members` for member list.

**Interview Answer Flow (#44)**
- TUI: Three-phase flow: choose (pick option), draft (type note), review (confirmation). Options rendered as a selectable list in the composer area.
- Web: Shows requests in a basic list with raw "answer" buttons.
- Build: Full interview modal/panel with option selection, text input for notes, and confirmation step.
- API: `/requests` for listing, `/requests/answer` for submission with `choice_id` + `choice_text` + `custom_text`.

**"Needs You" Banner (#47)**
- TUI: When a blocking request exists, a prominent banner appears at the top of the message area with the request question, who asked, and action hints.
- Web: No equivalent.
- Build: Render a dismissible banner above messages when `selectNeedsYouRequest()` finds a blocking/required request.
- API: `/requests` -- filter for `blocking: true` or `required: true` with open status.

**Task Board (#50)**
- TUI: Full task view with status pills, owner, channel, execution mode, review state, worktree path, timing, and action hints.
- Web: No task view.
- Build: Add `/tasks` as a broker endpoint consumer. Render task cards with status, owner, and action buttons.
- API: `/tasks` endpoint.

**Recovery View (#55)**
- TUI: Shows session recovery with focus, changes-since-away, next steps, unread summary.
- Web: No equivalent.
- Build: Add recovery panel/view that polls `/state` and renders `SessionRecovery` data.
- API: `/state` endpoint returns `recovery` object.

**1:1 Direct Session Mode (#67)**
- TUI: Full mode switch from office to direct session with a single agent. Changes sidebar, composer, message feed, and routing.
- Web: No concept of 1:1 mode.
- Build: Add session mode toggle. When in 1:1, filter messages, hide other agents, change composer label.
- API: `/session` endpoint for mode switching. Broker manages routing.

**Unified Switcher (#63)**
- TUI: Opens a fuzzy-search picker with channels, apps, DMs, active tasks, pending requests, and recent threads. Single keystroke access via Ctrl+K or `/switcher`.
- Web: Only basic channel clicking in sidebar.
- Build: Command palette / modal with all switchable targets.
- API: Aggregates data from `/channels`, `/tasks`, `/requests`, `/messages`.

**Confirmation Dialogs (#89)**
- TUI: Generic confirmation card used for session switches, resets, interview submissions. Shows title, detail, confirm/cancel labels.
- Web: No confirmation system.
- Build: Modal dialog component for destructive/important actions.
- API: Client-side UI pattern.

### Important Missing (P1)

**Apps Sidebar Section (#28)** -- The TUI sidebar has an "Apps" section listing Tasks, Requests, Policies, Calendar, Artifacts, Skills. The web has no app navigation.

**Nested Thread Replies (#17)** -- TUI renders replies at increasing indentation depth with `depth` tracking and `ParentLabel`. Web shows flat replies.

**Live Work Cards (#77)** -- TUI shows "Live work now" section below messages with per-agent activity cards, including tool usage detection and task progress. Critical for knowing what agents are actually doing.

**Blocked Work Display (#80)** -- TUI surfaces blocked tasks prominently. Web has no visibility into blockers.

**Runtime Strip (#81)** -- TUI shows a compact status strip with pill counts (N active, N blocked, N need you). Gives instant situational awareness.

**Composer Context Hints (#38)** -- TUI changes composer hints based on context (reply mode, interview mode, direct mode, etc.). Web shows static placeholder.

**Reply-to Mode (#39)** -- TUI supports `/reply <id>` to set reply context. Web only supports thread panel replies.

**Doctor Panel (#82)** -- TUI shows full runtime readiness checks (broker, API key, integrations, capabilities). Web has no diagnostics.

---

## 3. Recommended Build Order

### Wave 1: Core Functionality Gaps (blocking basic usage)
*These features are required for the web to be a usable daily driver.*

1. **Message grouping** (#2) -- S effort, high visual impact
2. **Agent activity classification** (#23) -- M effort, sidebar comes alive
3. **"Needs you" banner** (#47) -- M effort, can't miss blocking requests
4. **Confirmation dialogs** (#89) -- M effort, prerequisite for many actions
5. **Interview answer flow** (#44) -- L effort, core decision-making loop
6. **Slash command autocomplete** (#34) -- L effort, unlocks all commands
7. **@mention autocomplete** (#35) -- L effort, core interaction pattern
8. **Task board view** (#50, #100) -- M effort, visibility into work
9. **Request list view** (#43, #101) -- M effort, decision queue
10. **Apps sidebar section** (#28) -- M effort, navigation backbone
11. **Live work cards** (#77) -- M effort, know what agents are doing
12. **Reply-to mode** (#39) -- M effort, threaded conversations
13. **Date separators** (#3) -- S effort, message readability

### Wave 2: Power User Features
*Features that make the web competitive with the TUI for daily use.*

14. **Unified switcher** (#63, #98) -- L effort, power-user navigation
15. **Recovery view** (#55, #99) -- L effort, context recovery after away
16. **1:1 direct session mode** (#67, #109) -- L effort, focused agent work
17. **Session mode switch** (#68) -- M effort, enables 1:1
18. **@mention highlighting** (#5) -- S effort, visual clarity
19. **Unread divider** (#12) -- M effort, know what's new
20. **Nested thread replies** (#17) -- M effort, proper conversation depth
21. **Expand/collapse threads** (#18, #114) -- M effort, manage thread noise
22. **Reactions display** (#11) -- S effort, social signals
23. **[STATUS] messages** (#6) -- S effort, agent status context
24. **Blocked work display** (#80) -- M effort, surface blockers
25. **Runtime strip** (#81) -- M effort, instant situational awareness
26. **Composer context hints** (#38) -- S effort, guide user actions
27. **Sidebar summary + hint lines** (#30, #31) -- S effort, workspace awareness
28. **Channel header meta** (#92) -- S effort, context at a glance
29. **Notice/toast system** (#90) -- S effort, feedback for actions
30. **Status bar** (#91) -- M effort, persistent context
31. **Search picker** (#64, #107) -- L effort, cross-entity search
32. **Keyboard shortcuts** (#95) -- L effort, power-user speed
33. **Typing indicator with real agent data** (#36) -- M effort, liveness
34. **Policies view** (#56, #102) -- L effort, signals/decisions/watchdogs
35. **Calendar view** (#57, #103) -- L effort, scheduled work

### Wave 3: Polish and Parity
*Nice-to-have features for complete feature parity.*

36. **A2UI block rendering** (#13) -- L effort, structured UI in messages
37. **Agent create/edit wizard** (#73, #74, #75) -- L effort, team management
38. **Enable/disable agent** (#76) -- M effort, team control
39. **Doctor panel** (#82) -- L effort, diagnostics
40. **Init flow** (#83) -- L effort, first-run setup
41. **Artifacts view** (#58, #104) -- L effort, execution artifacts
42. **Skills view** (#59, #105) -- L effort, skill management
43. **Inbox/Outbox views** (#60, #61) -- M effort, 1:1 mailboxes
44. **Insert picker** (#65, #108) -- M effort, reference insertion
45. **Rewind picker** (#66, #119) -- M effort, recovery prompts
46. **Composer input history** (#40) -- M effort, recall previous inputs
47. **Pixel art avatars** (#88) -- L effort, visual delight (CSS/canvas port)
48. **Thought bubbles** (#26) -- M effort, agent personality
49. **Quick jump shortcuts** (#29) -- M effort, keyboard speed
50. **Splash screen** (#87) -- L effort, "The Office" intro for web
51. **Human message cards** (#7) -- M effort, decision/action cards
52. **Automation message cards** (#8) -- M effort, nex automation
53. **Mood inference** (#10) -- S effort, message sentiment
54. **Agent character face** (#14) -- S effort, inline mascot
55. **Wait state display** (#79) -- S effort, quiet lane notice
56. **Reset session** (#69, #110) -- M effort, session management
57. **Reset DM** (#70) -- S effort, clear direct transcript
58. **Integration connect** (#84) -- L effort, third-party connections
59. **Telegram connect** (#85) -- L effort, messaging bridge
60. **Provider switching** (#86) -- M effort, LLM provider
61. **Esc pause all** (#42) -- M effort, agent control
62. **Task action commands** (#52, #116) -- M effort, task lifecycle
63. **Skill commands** (#117) -- L effort, skill CRUD
64. **Connect commands** (#118) -- L effort, channel bridges
65. **Execution timeline** (#78) -- M effort, action history
66. **Task-aware agent status** (#27) -- M effort, richer sidebar
67. **Agent live activity text** (#24) -- M effort, Claude Code status
68. **PgUp/PgDn roster scrolling** (#32) -- S effort, overflow handling
69. **Thread default expand toggle** (#19) -- S effort, preference
70. **Thread reply-to indicator** (#20) -- S effort, thread context
71. **Task worktree info** (#53) -- S effort, workspace display
72. **Task click-to-focus** (#54) -- M effort, navigation
73. **Request timing display** (#49) -- S effort, due dates
74. **Request kind pills** (#48) -- S effort, visual type indicator
75. **Multi-line input** (#41) -- S effort, already partially works
76. **Live activity indicator** (#37) -- M effort, agent tool usage

---

## Summary

| Status | Count |
|--------|-------|
| Done | 11 |
| Partial | 5 |
| Missing | 104 |
| N/A | 1 |
| **Total** | **121** |

The web UI covers ~9% of TUI features. The critical gap is the absence of the entire app/view system (tasks, requests, policies, calendar, artifacts, skills, recovery), the command palette, @mentions, 1:1 mode, and the real-time agent activity display that makes the TUI feel "alive."

Wave 1 (13 items) would bring the web to ~30% parity and make it usable for basic office interaction. Wave 2 (22 items) reaches ~55% and makes it viable as a daily driver. Wave 3 (41 items) achieves full parity.
