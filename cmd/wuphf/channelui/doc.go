// Package channelui hosts the pure rendering and data-projection layer
// for the wuphf "channel" TUI surface — broker-shape data types, slug-
// to-display-name resolution, layout / wrap / time helpers, message
// flatteners, mailbox / artifact / runtime / recovery / calendar
// renderers, and the lipgloss-backed pill / card primitives. The Bubble
// Tea program model (channelModel.Update / View / tea.Cmd builders)
// stays in package main; channelui only exposes pure helpers it can
// consume.
//
// The package lives under cmd/wuphf/ rather than internal/ because it is
// binary-private; the broker-side internal/channel package owns the
// cross-process channel store types and is intentionally distinct from
// this UI layer.
//
// # Dependencies
//
// channelui is a pure rendering / data-projection layer. The intentional
// boundary is:
//
//   - Allowed: lipgloss / glamour / charmbracelet/x/ansi (terminal
//     rendering), and the project's own internal/avatar,
//     internal/company, internal/team, internal/config (data-shape
//     and roster sources). Each of these was opted in deliberately
//     when a hoisted helper needed it; further internal packages
//     should be added with similar deliberation.
//   - Disallowed: bubbletea (no tea.Cmd / tea.Msg / tea.Model code in
//     channelui — those stay in package main where the wuphf TUI
//     program lives), internal/tui (picker / mention / autocomplete
//     widgets stay package-main consumers), HTTP clients and broker
//     polling, anything that owns long-lived global state (the
//     channel render cache, splash program model). Hoist these only
//     by separating their pure parts first.
//
// # Layout
//
// The package is grouped by concern. As of the latest extraction PR the
// inhabitants are:
//
//   - types.go             — broker-shape data types (BrokerMessage,
//     RenderedLine, ThreadedMessage, Member, ChannelInfo, Interview,
//     Task, Action, Signal, Decision, Watchdog, SchedulerJob, Skill,
//     UsageState, UsageTotals) plus method receivers like IsDM and
//     TitleOrQuestion.
//   - composer_history.go  — Snapshot / History primitives the composer
//     uses to remember submitted drafts and stash in-flight input.
//   - directory.go         — OfficeMember roster singleton, DisplayName /
//     RoleLabel slug-to-label resolution, plus a
//     WithOfficeDirectoryForTest fixture helper.
//   - layout.go            — ComputeLayout panel-size calculator and
//     RenderVerticalBorder.
//   - styles.go            — Slack-themed palette constants
//     (SlackSidebarBg, SlackMuted, …), AgentColorMap / StatusDotColors,
//     lipgloss style constructors, mascot helpers, and pill renderers
//     (AccentPill, SubtlePill, TaskStatusPill, RequestKindPill).
//   - helpers.go           — pure stdlib-only utilities (MaxInt,
//     ClampScroll, OverlayBottomLines, FindMessageByID, ContainsString,
//     ShortClock, FormatMinutes, FallbackString, ParseChannelTime,
//     SameDay, PrettyWhen, PrettyRelativeTime, RenderTimingSummary).
//   - render_helpers.go    — lipgloss-backed render utilities
//     (AppendWrapped, TruncateText, MutedText, RenderDateSeparator,
//     RenderUnreadDivider, HumanMessageLabel, DisplayDecisionSummary,
//     MinInt, RenderRuntimeEventCard).
//   - messages.go          — broker-message walkers (CountReplies,
//     BuildReplyChildren, ParseTimestamp, FormatShortTime).
//   - needs_you.go         — "needs your attention" strip renderer plus
//     SelectNeedsYouRequest / IsOpenInterviewStatus selectors.
//   - list_helpers.go      — pure list filters and reversals
//     (ReverseSignals, ReverseDecisions, ActiveWatchdogs,
//     ReverseWatchdogs, RecentExternalActions), plus
//     AgentSlugForDisplay and DisplaySignalKind.
//   - build_lines_simple.go — leaf "build*Lines" rendering helpers
//     for the requests and skills apps (BuildRequestLines,
//     BuildSkillLines).
//   - build_lines_policy_task.go — "build*Lines" rendering helpers for
//     the policies and tasks apps (BuildPolicyLines, BuildTaskLines).
//   - calendar.go          — calendar agenda data layer for the
//     calendar app: CalendarRange / CalendarEvent types,
//     CollectCalendarEvents and its task/request fan-outs,
//     DedupeCalendarEvents, FilterCalendarEvents, the calendar-time
//     formatters (PrettyCalendarWhen, CalendarBucketLabel),
//     ChooseCalendarChannel, the participant resolvers
//     (CalendarParticipants*, CalendarParticipantSlugs*,
//     CalendarParticipantNames, NextCalendarEventByParticipant,
//     OrderedCalendarParticipants), CalendarEventColors, and the
//     SchedulerTarget* helpers that map a job to its task / request /
//     thread.
//   - calendar_render.go   — calendar rendering layer:
//     BuildCalendarLines (entry), BuildCalendarToolbar,
//     RenderCalendarEventCard, RenderCalendarParticipantCard,
//     RenderCalendarActionCard, the RenderedCardLines /
//     RenderedCardLinesWithPrompt card-to-RenderedLine adapters, and
//     NormalizeSidebarSlug (used to canonicalize channel slugs for
//     equality).
//   - messages_render.go   — leaf message-render helpers:
//     RenderReactions (emoji pill row), MessageUsageTotal /
//     RenderMessageUsageMeta (token-usage strip on assistant
//     messages), DefaultHumanMessageTitle (fallback titles for
//     human_* kinds), SliceRenderedLines (viewport windowing) and
//     FormatTokenCount (compact "1.2M tok" formatter).
//   - cache_helpers.go     — leaf render-cache helpers:
//     CloneRenderedLines / CloneThreadedMessages (defensive copies
//     for cached snapshots) and RenderTimeBucket (per-second
//     bucket for direct DMs and the messages app, per-30s
//     elsewhere).
//   - threads.go           — thread navigation + walker helpers:
//     ThreadRootMessageID walks ReplyTo to the root,
//     HasThreadReplies reports whether any message replies to a
//     given id, CountThreadReplies / ThreadParticipants recurse
//     the children-by-ID adjacency map to count descendants and
//     collect distinct reply-author display names in walk-order,
//     and FlattenThreadMessages produces the office-feed thread
//     layout (timestamp-sorted ThreadedMessage list with depth /
//     parent-label / collapse state populated; honors expanded[id]
//     == false to collapse a root and surface HiddenReplies +
//     ThreadParticipants for the collapsed-summary line).
//   - recovery.go          — recovery leaf helpers:
//     TrimRecoverySentence, RenderAwayStrip,
//     RecoverySurgeryOption struct + BuildRecoverySurgeryOptions
//     (cards for "draft a decision brief / restore task context /
//     summarize since"), the BuildRecoveryPromptFor* prompt
//     builders, RenderRecoveryActionCard (the card body styler),
//     PrefixedCardLines, RecoveryActiveTasks (filter+sort by
//     UpdatedAt), and RecoveryRecentThreads (newest thread roots).
//   - interview.go         — interview-flow leaf helpers:
//     InterviewPhase typed-string + Choose/Draft/Review consts,
//     InterviewOptionRequiresText, InterviewOptionTextHint, and
//     SelectedInterviewOption.
//   - mentions.go          — HighlightMentions wraps every "@slug"
//     in a colored bold style based on a slug-to-color map (private
//     mentionPattern regex moved alongside).
//   - thread_render.go     — pure thread-side-panel rendering:
//     FlattenThreadReplies (depth-first walk of descendants),
//     RenderThreadReplies, RenderThreadReply (per-reply
//     header+body), and RenderThreadMessage (compact parent-style
//     layout). The channelModel-bound entry renderThreadPanel and
//     the tui-dependent renderThreadInput stay in package main.
//   - unread.go            — SummarizeUnreadMessages renders a
//     short "N new from <names>" label naming up to three distinct
//     senders for the away-strip.
//   - mailbox.go           — viewer-scope mailbox filter cluster:
//     FilterMessagesForViewerScope (entry), NormalizeMailboxScope
//     (canonicalize "inbox"/"outbox"/"agent"), the per-message
//     predicates (MailboxMessageMatchesViewerScope,
//     MailboxMessageBelongsToViewer{Inbox,Outbox}), and
//     MailboxMessageRepliesToViewerThread (cycle-safe ReplyTo walk).
//   - member_draft.go      — member-draft leaf helpers:
//     NormalizeDraftSlug, ParseExpertiseInput (comma-split + dedup),
//     LiveActivityFromMembers (slug → live-activity map).
//   - sidebar_presence.go  — sidebar member-presence helpers:
//     TruncateLabel, the SidebarBG/Muted/Divider/Active +
//     DotTalking/Thinking/Coding/Idle theme consts,
//     SidebarAgentColors map, MemberActivity / OfficeCharacter
//     types, ClassifyActivity, DefaultSidebarRoster,
//     RenderOfficeCharacter (uses internal/avatar),
//     OfficeAside (per-slug catchphrase), ActiveSidebarTask,
//     ApplyTaskActivity, TaskBubbleText, RenderThoughtBubble
//     (▗ … ▖ … ▘ pill), PadSidebarContent, SidebarPlainRow,
//     SidebarStyledRow.
//   - confirm.go           — confirm-card data + leaf renderers:
//     ChannelConfirmAction typed-string + the five action consts,
//     ChannelConfirm struct, ConfirmationForResetDM,
//     ConfirmationForInterviewAnswer, RenderConfirmCard.
//     team-bound ConfirmationForSessionSwitch and the
//     channelModel-bound ConfirmationForReset stay in package main.
//   - composer_popup.go    — autocomplete popup leaves:
//     ComposerPopupOption struct, RenderComposerPopup (rounded
//     popup with selection accent + footer hint),
//     TypingAgentsFromMembers (display names of recently-active
//     teammates).
//   - activity.go          — runtime-strip / live-work leaf helpers:
//     TaskStatusLine, SummarizeLiveActivity / SanitizeActivityLine /
//     SummarizeSentence (pane-snapshot summarization),
//     BlockedWorkTasks, RecentDirectExecutionActions,
//     ExecutionMetaLine, LatestRelevantAction, DescribeActionState,
//     ActivityPill (member-activity → colored pill),
//     ActionStatePill (action kind → colored pill).
//   - artifacts.go         — artifact-card leaf helpers:
//     ArtifactLifecyclePill, ArtifactAccentColor (state →
//     border color), ParseArtifactTimestamp,
//     RecentHumanArtifactRequests (filter+sort decision-kind
//     interviews), RecentExecutionArtifactActions
//     (request_/external_/interrupt_/human_ kinds, newest first),
//     ArtifactClock (HH:MM with fallback), ArtifactTime
//     (RFC3339 emit string).
//   - sidebar_apps.go      — sidebar app-stack data:
//     OfficeSidebarApp struct, OfficeSidebarApps (canonical
//     8-row stack), VisibleSidebarApps (max-rows fit that always
//     keeps the active app visible).
//   - text_misc.go         — small string utilities:
//     ContainsSlug, PluralizeWord, ExtractTagsFromText (from
//     "@slug" mentions), ChannelExists.
//   - composer_input.go    — composer cursor/insertion primitives:
//     NormalizeCursorPos (clamp to [0, len]), InsertComposerRunes
//     (rune-aware insert at pos returning new pos).
//   - composer_cursor.go   — composer cursor motion + mention helpers:
//     ReplaceMentionInInput (substitute the in-progress "@…" token),
//     IsComposerWordRune, MoveCursorBackwardWord / MoveCursorForwardWord
//     (alt+b / alt+w word jumps), and MoveComposerCursor (key-string
//     dispatch for left/right/home/end/word motions, with a recognized
//     bool so callers can fall through unrecognized keys).
//   - message_filters.go   — message-walking filters and selectors:
//     FilterInsightMessages (automation / nex senders for the insight
//     side panels), LatestHumanFacingMessage (newest human_*-kind
//     pointer or nil), CountUniqueAgents (distinct senders excluding
//     "you" / "nex" / kind=="automation").
//   - misc_helpers.go      — small pure helpers:
//     AppendUniqueMessages (dedup-by-trimmed-ID merge, returns the
//     added count), PopupActionIndex (parses the numeric token of a
//     "popup_action_N" value), FormatUSD (two-decimal "$X.YZ"
//     dollar-cost formatter).
//   - mood.go              — InferMood classifies a message body
//     into one of "energized" / "skeptical" / "concerned" / "tense" /
//     "relieved" / "focused" (or "" on empty / unmatched). Tints
//     the meta-line on office messages.
//   - interview_card.go    — RenderInterviewCard renders the
//     amber rounded interview-request card: header pill row
//     (kind label + optional phase pill + blocking/private
//     accents), title, question body, optional context, optional
//     timing summary, the option list with the selected option
//     arrowed, the "Something else" custom row, and the
//     accept/type hint footer.
//   - manifest.go          — company-manifest projection +
//     roster fallback: MergeOfficeMembers (channel/order-aware
//     merge of broker members with office-roster metadata,
//     preserving members who haven't posted yet),
//     OfficeMembersFromManifest / ChannelInfosFromManifest
//     (project a company.Manifest into the channel UI shapes),
//     and OfficeMembersFallback / ChannelInfosFallback (load the
//     manifest from disk — falling back to DefaultManifest on
//     error — when the broker hasn't reported a roster yet).
//   - platform.go          — IsDarwin / IsLinux / IsWindows
//     (runtime.GOOS predicates) and OpenBrowserURL (spawns
//     "open" / "xdg-open" / "cmd /c start" with a background
//     context for fire-and-forget browser handoff).
//   - map_string.go        — MapString safely reads a string
//     field from a map[string]any (used to parse JSON-decoded
//     broker responses where the shape isn't statically known);
//     non-string values fall back to fmt.Sprintf("%v").
//   - initial_app.go       — ResolveInitialOfficeApp normalizes
//     a CLI-flag string into a known OfficeApp value with the
//     legacy "insights" alias mapped to OfficeAppPolicies and an
//     OfficeAppMessages fallback.
//   - usage_strip.go       — RenderUsageStrip renders the
//     "Spend by teammate" pill row beneath the office feed
//     (avatar + token count + dollar cost per agent, ordered by
//     channel-member appearance, then canonical roster, then map
//     iteration order; "" when no agents tracked or width < 40).
//     SidebarShortcutLabel returns the "1".."9" digit shortcut
//     for sidebar item indexes 0..8 (or "" when out of range).
//   - doctor.go            — DoctorSeverity typed-string + the four
//     consts, DoctorCheck / DoctorReport types with the counts /
//     StatusLine / Counts methods, DoctorSeverityForCapability
//     (capability descriptor → severity), and the rendering
//     helpers RenderDoctorCard (rounded slate card with status
//     pill + per-check rows), RenderDoctorLabel (severity pill),
//     RenderDoctorLifecycle (lifecycle pill). Imports
//     internal/team for the CapabilityRegistry / Lifecycle types.
//     The team-coupled tea.Cmd entry runDoctorChecks /
//     inspectDoctor and the test mock-point detectRuntimeCapabilitiesFn
//     stay in package main.
//   - workspace_helpers.go — pure / team-typed leaves used by the
//     workspace state machinery: SummarizeAwayRecovery (the
//     "while away" one-liner combining unread count + recovery
//     focus + first next step), RuntimeRequestIsOpen
//     (open/pending/draft/empty status predicate), the
//     FirstWorkspaceString chain helper, FirstDoctorNextStep
//     (first non-empty NextStep on a fail/warn check), and
//     SidebarViewLabel (OfficeApp → short sidebar summary label).
//   - runtime_projection.go — channel UI ↔ team.Runtime* projection
//     and tally helpers: RuntimeTasksFromChannel /
//     RuntimeRequestsFromChannel / RuntimeMessagesFromChannel
//     (project the channel UI types into the runtime-snapshot
//     shapes; the messages projection walks newest-first and is
//     bounded by limit), CountRunningRuntimeTasks (excludes
//     terminal statuses), CountIsolatedRuntimeTasks (counts tasks
//     in a "local_worktree" execution mode or with a non-empty
//     WorktreePath / WorktreeBranch).
//   - task_workflow_builders.go — task / workflow / orphan-log
//     runtime-artifact projection: TaskLogRecord (on-disk JSON
//     line shape), TaskLogArtifact (UI projection of the latest
//     record + log file metadata), WorkflowRunArtifact (on-disk
//     workflow run row), SummarizeTaskLogRecord (error/result/
//     params one-liner), BuildTaskRuntimeArtifact /
//     BuildOrphanTaskLogRuntimeArtifact / BuildWorkflowRuntimeArtifact,
//     and the BuildTask*/Workflow*/Normalize* helpers
//     (BuildTaskArtifactSummary / Progress / ReviewHint /
//     ResumeHint, NormalizeTaskArtifactState,
//     WorkflowArtifactProgress, NormalizeWorkflowArtifactState).
//     Disk-IO readers (recentTaskLogArtifacts on channelModel,
//     recentWorkflowRunArtifacts, readTaskLogArtifact,
//     readWorkflowRunArtifact) stay in package main since they
//     are tied to the disk layout and the channelModel state.
//   - artifact_builders.go — request / action runtime-artifact
//     projection helpers: RuntimeArtifactSnapshot type + Count /
//     Filter methods, RecentArtifactTasks (filter + newest-first
//     sort + limit), BuildRequestRuntimeArtifact and
//     BuildActionRuntimeArtifact, the Request*/Action* progress /
//     review / resume / state normalizers (RequestArtifactProgress,
//     RequestArtifactReviewHint, NormalizeRequestArtifactState,
//     ActionArtifactSummary, ActionArtifactProgress,
//     ActionArtifactResumeHint, NormalizeActionArtifactState),
//     and LatestArtifactTimestamp (newest-parseable RFC3339 or
//     ""). The package-main task / workflow / orphan-log builders
//     stay there since they touch package-main types like
//     taskLogArtifact and workflowRunArtifact.
//   - crash_log.go         — AppendChannelCrashLog (RFC3339-stamped
//     append to the crash log; mode 0o700 dir + 0o600 file) and
//     ChannelCrashLogPath (~/.wuphf/logs/channel-crash.log with
//     a working-directory fallback).
//   - artifact_helpers.go  — execution-artifact stdlib leaves:
//     SummarizeJSONField (TruncateText'd one-line summary of a
//     json.RawMessage; unquotes JSON strings, compacts objects /
//     arrays, falls through to trimmed raw on parse errors;
//     "" for empty / "null"), TaskLogRoot (WUPHF_TASK_LOG_ROOT
//     env var → ~/.wuphf/office/tasks → ".wuphf/office/tasks"
//     fallback for the headless task-tool log root).
//   - artifact_renderers.go — execution-artifacts subsection
//     renderers: RenderArtifactSection (date separator + per-
//     artifact card with TaskID / RequestID click-target wiring
//     on the first line of Task/TaskLog/Request artifacts),
//     RenderArtifactHeader ("<clock pill> <lifecycle pill>
//     <bold title>" + accent color picked by kind/state), and
//     ArtifactExtraLines (the optional progress / output / owner
//     / channel / worktree / path / related-id / blocking /
//     review / resume rows).
//   - runtime_builders.go  — runtime-strip + live-work builders:
//     MemberRuntimeSummary struct, DeriveMemberRuntimeSummary
//     (per-member activity classification + meta detail + thought
//     bubble), BuildLiveWorkLines (the "Live work now" + "Recent
//     external actions" + wait-state cluster),
//     BuildWaitStateLines (Blocked work or "Nothing is moving"),
//     BuildDirectExecutionLines (1:1 execution timeline),
//     RenderRuntimeStrip (two-line summary pill row + detail
//     line shown above the office feed), OneOnOneRuntimeLine
//     (compact descriptor for the channel header in 1:1 mode).
//   - recovery_builders.go — recovery-view section builders:
//     BuildRecoveryLines (the full recovery view — while-away
//     card, runtime status card, readiness card, next-step +
//     highlights strips, and the action / surgery rows; offline
//     preview message when broker is detached and runtime is
//     empty), BuildRecoveryActionLines (the "Resume human
//     decisions" / "Resume active tasks" / "Return to recent
//     threads" sections wired with click-target metadata), and
//     BuildRecoverySurgeryLines (the "Transcript surgery"
//     composer-prefill cards).
//   - workspace_state.go    — WorkspaceReadinessLevel typed-string
//   - the three Ready/Warn/Preview consts,
//     WorkspaceReadinessState struct, WorkspaceUIState struct, and
//     the workspace-state machinery: ResolveWorkspaceAwaySummary,
//     DeriveWorkspaceReadiness, ReadinessCard / NeedsYouLines /
//     HeaderMeta / DefaultStatusLine / SidebarSummaryLine /
//     SidebarHintLine method receivers. Imports internal/config
//     for the memory-backend-kind consts and internal/team for
//     RuntimeSnapshot / SessionRecovery / MemoryBackendStatus.
//     The channelModel.currentWorkspaceUIState() builder stays in
//     package main since it touches private channelModel fields.
//
// The original incremental extraction stack maintained lowercase-name
// alias files (channelui_aliases.go, channelui_styles_aliases.go) so
// in-flight package-main callers kept compiling between PRs. Those
// aliases were retired in the cleanup pass that ships with this
// package; every package-main caller now references channelui.X
// directly.
package channelui
