package main

import "github.com/nex-crm/wuphf/cmd/wuphf/channelui"

// Type aliases bridge the old package-main names to the new channelui
// package while the channel cluster is incrementally extracted. These
// aliases preserve every existing field access, method receiver, and
// composite-literal usage in the rest of cmd/wuphf so each extraction
// PR can move types without churning every callsite.
//
// The aliases will be removed once the channel cluster fully lives in
// channelui (final cleanup PR).
type (
	brokerReaction         = channelui.BrokerReaction
	brokerMessageUsage     = channelui.BrokerMessageUsage
	brokerMessage          = channelui.BrokerMessage
	renderedLine           = channelui.RenderedLine
	threadedMessage        = channelui.ThreadedMessage
	layoutDimensions       = channelui.LayoutDimensions
	officeMemberInfo       = channelui.OfficeMember
	channelMember          = channelui.Member
	channelInfo            = channelui.ChannelInfo
	channelInterviewOption = channelui.InterviewOption
	channelInterview       = channelui.Interview
	channelUsageTotals     = channelui.UsageTotals
	channelUsageState      = channelui.UsageState
	channelTask            = channelui.Task
	channelAction          = channelui.Action
	channelSignal          = channelui.Signal
	channelDecision        = channelui.Decision
	channelWatchdog        = channelui.Watchdog
	channelSchedulerJob    = channelui.SchedulerJob
	channelSkill           = channelui.Skill
)

// Function aliases keep the lowercase names callable from package main
// while the helpers physically live in channelui. Removed in PR 9.
var (
	countReplies         = channelui.CountReplies
	buildReplyChildren   = channelui.BuildReplyChildren
	parseTimestamp       = channelui.ParseTimestamp
	formatShortTime      = channelui.FormatShortTime
	computeLayout        = channelui.ComputeLayout
	renderVerticalBorder = channelui.RenderVerticalBorder

	maxInt              = channelui.MaxInt
	clampScroll         = channelui.ClampScroll
	overlayBottomLines  = channelui.OverlayBottomLines
	findMessageByID     = channelui.FindMessageByID
	containsString      = channelui.ContainsString
	shortClock          = channelui.ShortClock
	formatMinutes       = channelui.FormatMinutes
	fallbackString      = channelui.FallbackString
	parseChannelTime    = channelui.ParseChannelTime
	sameDay             = channelui.SameDay
	prettyWhen          = channelui.PrettyWhen
	prettyRelativeTime  = channelui.PrettyRelativeTime
	renderTimingSummary = channelui.RenderTimingSummary

	appendWrapped          = channelui.AppendWrapped
	truncateText           = channelui.TruncateText
	mutedText              = channelui.MutedText
	renderDateSeparator    = channelui.RenderDateSeparator
	humanMessageLabel      = channelui.HumanMessageLabel
	renderUnreadDivider    = channelui.RenderUnreadDivider
	displayDecisionSummary = channelui.DisplayDecisionSummary

	displayName = channelui.DisplayName
	roleLabel   = channelui.RoleLabel
)
