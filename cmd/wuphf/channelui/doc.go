// Package channelui hosts the Bubble Tea TUI for the wuphf "channel"
// surface — channel feed, sidebar, thread panel, composer, splash, and
// the broker-backed model that drives them.
//
// The package lives under cmd/wuphf/ rather than internal/ because it is
// binary-private; the broker-side internal/channel package owns the
// cross-process channel store types and is intentionally distinct from
// this UI layer.
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
//   - threads.go           — thread navigation helpers:
//     ThreadRootMessageID walks ReplyTo to the root,
//     HasThreadReplies reports whether any message replies to a
//     given id.
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
//
// Subsequent extraction PRs will land the workspace / recovery / cache
// cluster, the sidebar / splash, the broker integrations, and finally
// the channelModel itself. cmd/wuphf maintains lowercase-name aliases
// (channelui_aliases.go, channelui_styles_aliases.go) so existing
// callers keep compiling unchanged during the migration; those alias
// files are deleted in the final cleanup PR.
package channelui
