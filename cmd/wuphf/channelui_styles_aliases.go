package main

import "github.com/nex-crm/wuphf/cmd/wuphf/channelui"

// Constant aliases for the slack-themed palette. Typed-string constants
// can be re-declared by copying the value, which preserves type identity
// (the underlying string type matches across packages). Removed in PR 9.
const (
	slackSidebarBg   = channelui.SlackSidebarBg
	slackMainBg      = channelui.SlackMainBg
	slackThreadBg    = channelui.SlackThreadBg
	slackBorder      = channelui.SlackBorder
	slackActive      = channelui.SlackActive
	slackHover       = channelui.SlackHover
	slackText        = channelui.SlackText
	slackMuted       = channelui.SlackMuted
	slackTimestamp   = channelui.SlackTimestamp
	slackDivider     = channelui.SlackDivider
	slackMentionBg   = channelui.SlackMentionBg
	slackMentionText = channelui.SlackMentionText
	slackOnline      = channelui.SlackOnline
	slackAway        = channelui.SlackAway
	slackBusy        = channelui.SlackBusy
	slackInputBorder = channelui.SlackInputBorder
	slackInputFocus  = channelui.SlackInputFocus
)

// Map and function aliases for the channel-side style helpers. Maps
// share storage by reference, so callers continue to mutate / read the
// same backing data they did before the move. Removed in PR 9.
var (
	agentColorMap   = channelui.AgentColorMap
	statusDotColors = channelui.StatusDotColors

	sidebarStyle         = channelui.SidebarStyle
	mainPanelStyle       = channelui.MainPanelStyle
	threadPanelStyle     = channelui.ThreadPanelStyle
	statusBarStyle       = channelui.StatusBarStyle
	channelHeaderStyle   = channelui.ChannelHeaderStyle
	composerBorderStyle  = channelui.ComposerBorderStyle
	timestampStyle       = channelui.TimestampStyle
	mutedTextStyle       = channelui.MutedTextStyle
	agentNameStyle       = channelui.AgentNameStyle
	activeChannelStyle   = channelui.ActiveChannelStyle
	dateSeparatorStyle   = channelui.DateSeparatorStyle
	threadIndicatorStyle = channelui.ThreadIndicatorStyle

	agentAvatar    = channelui.AgentAvatar
	mascotAccent   = channelui.MascotAccent
	mascotEyes     = channelui.MascotEyes
	mascotMouth    = channelui.MascotMouth
	mascotTop      = channelui.MascotTop
	mascotProp     = channelui.MascotProp
	mascotLines    = channelui.MascotLines
	agentCharacter = channelui.AgentCharacter

	accentPill      = channelui.AccentPill
	subtlePill      = channelui.SubtlePill
	taskStatusPill  = channelui.TaskStatusPill
	requestKindPill = channelui.RequestKindPill
)
