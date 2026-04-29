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
//
// Subsequent extraction PRs will land the workspace / recovery / cache
// cluster, the sidebar / splash, the broker integrations, and finally
// the channelModel itself. cmd/wuphf maintains lowercase-name aliases
// (channelui_aliases.go, channelui_styles_aliases.go) so existing
// callers keep compiling unchanged during the migration; those alias
// files are deleted in the final cleanup PR.
package channelui
