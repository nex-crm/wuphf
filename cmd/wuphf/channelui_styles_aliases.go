package main

import "github.com/nex-crm/wuphf/cmd/wuphf/channelui"

// Constant aliases for the slack-themed palette. Typed-string constants
// can be re-declared by copying the value, which preserves type identity
// (the underlying string type matches across packages). Each entry
// carries a `// Deprecated:` doc comment so staticcheck SA1019 fires
// when *new* callers reach for the lowercase form. Removed in cleanup PR.
const (
	// Deprecated: use channelui.SlackSidebarBg directly.
	slackSidebarBg = channelui.SlackSidebarBg
	// Deprecated: use channelui.SlackMainBg directly.
	slackMainBg = channelui.SlackMainBg
	// Deprecated: use channelui.SlackThreadBg directly.
	slackThreadBg = channelui.SlackThreadBg
	// Deprecated: use channelui.SlackBorder directly.
	slackBorder = channelui.SlackBorder
	// Deprecated: use channelui.SlackActive directly.
	slackActive = channelui.SlackActive
	// Deprecated: use channelui.SlackHover directly.
	slackHover = channelui.SlackHover
	// Deprecated: use channelui.SlackText directly.
	slackText = channelui.SlackText
	// Deprecated: use channelui.SlackMuted directly.
	slackMuted = channelui.SlackMuted
	// Deprecated: use channelui.SlackTimestamp directly.
	slackTimestamp = channelui.SlackTimestamp
	// Deprecated: use channelui.SlackDivider directly.
	slackDivider = channelui.SlackDivider
	// Deprecated: use channelui.SlackMentionBg directly.
	slackMentionBg = channelui.SlackMentionBg
	// Deprecated: use channelui.SlackMentionText directly.
	slackMentionText = channelui.SlackMentionText
	// Deprecated: use channelui.SlackOnline directly.
	slackOnline = channelui.SlackOnline
	// Deprecated: use channelui.SlackAway directly.
	slackAway = channelui.SlackAway
	// Deprecated: use channelui.SlackBusy directly.
	slackBusy = channelui.SlackBusy
	// Deprecated: use channelui.SlackInputBorder directly.
	slackInputBorder = channelui.SlackInputBorder
	// Deprecated: use channelui.SlackInputFocus directly.
	slackInputFocus = channelui.SlackInputFocus
)

// Map and function aliases for the channel-side style helpers. Maps
// share storage by reference, so callers continue to mutate / read the
// same backing data they did before the move. Removed in cleanup PR.
var (
	// Deprecated: use channelui.AgentColorMap directly.
	agentColorMap = channelui.AgentColorMap
	// Deprecated: use channelui.StatusDotColors directly.
	statusDotColors = channelui.StatusDotColors
	// Deprecated: use channelui.AgentColor directly.
	agentColor = channelui.AgentColor

	// Deprecated: use channelui.SidebarStyle directly.
	sidebarStyle = channelui.SidebarStyle
	// Deprecated: use channelui.MainPanelStyle directly.
	mainPanelStyle = channelui.MainPanelStyle
	// Deprecated: use channelui.ThreadPanelStyle directly.
	threadPanelStyle = channelui.ThreadPanelStyle
	// Deprecated: use channelui.StatusBarStyle directly.
	statusBarStyle = channelui.StatusBarStyle
	// Deprecated: use channelui.ChannelHeaderStyle directly.
	channelHeaderStyle = channelui.ChannelHeaderStyle
	// Deprecated: use channelui.ComposerBorderStyle directly.
	composerBorderStyle = channelui.ComposerBorderStyle
	// Deprecated: use channelui.TimestampStyle directly.
	timestampStyle = channelui.TimestampStyle
	// Deprecated: use channelui.MutedTextStyle directly.
	mutedTextStyle = channelui.MutedTextStyle
	// Deprecated: use channelui.AgentNameStyle directly.
	agentNameStyle = channelui.AgentNameStyle
	// Deprecated: use channelui.ActiveChannelStyle directly.
	activeChannelStyle = channelui.ActiveChannelStyle
	// Deprecated: use channelui.DateSeparatorStyle directly.
	dateSeparatorStyle = channelui.DateSeparatorStyle
	// Deprecated: use channelui.ThreadIndicatorStyle directly.
	threadIndicatorStyle = channelui.ThreadIndicatorStyle

	// Deprecated: use channelui.AgentAvatar directly.
	agentAvatar = channelui.AgentAvatar
	// Deprecated: use channelui.MascotAccent directly.
	mascotAccent = channelui.MascotAccent
	// Deprecated: use channelui.MascotEyes directly.
	mascotEyes = channelui.MascotEyes
	// Deprecated: use channelui.MascotMouth directly.
	mascotMouth = channelui.MascotMouth
	// Deprecated: use channelui.MascotTop directly.
	mascotTop = channelui.MascotTop
	// Deprecated: use channelui.MascotProp directly.
	mascotProp = channelui.MascotProp
	// Deprecated: use channelui.MascotLines directly.
	mascotLines = channelui.MascotLines
	// Deprecated: use channelui.AgentCharacter directly.
	agentCharacter = channelui.AgentCharacter

	// Deprecated: use channelui.AccentPill directly.
	accentPill = channelui.AccentPill
	// Deprecated: use channelui.SubtlePill directly.
	subtlePill = channelui.SubtlePill
	// Deprecated: use channelui.TaskStatusPill directly.
	taskStatusPill = channelui.TaskStatusPill
	// Deprecated: use channelui.RequestKindPill directly.
	requestKindPill = channelui.RequestKindPill
)
