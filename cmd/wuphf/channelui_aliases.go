package main

import "github.com/nex-crm/wuphf/cmd/wuphf/channelui"

// Type aliases bridge the old package-main names to the new channelui
// package while the channel cluster is incrementally extracted. These
// aliases preserve every existing field access, method receiver, and
// composite-literal usage in the rest of cmd/wuphf so each extraction
// PR can move types without churning every callsite.
//
// Each alias carries a per-entry `// Deprecated:` doc comment so that
// staticcheck's SA1019 rule fires when *new* callers reach for the
// lowercase form. This is a forcing function: the alias file was
// supposed to disappear in a follow-up cleanup PR but, in the absence
// of compiler-level pressure, refactor stacks like this one tend to
// leave bridge files lingering for years. Treat any new SA1019 hit on
// these symbols as a bug.
//
// The aliases will be removed once the channel cluster fully lives in
// channelui (final cleanup PR).
type (
	// Deprecated: use channelui.OfficeApp directly.
	officeApp = channelui.OfficeApp
	// Deprecated: use channelui.BrokerReaction directly.
	brokerReaction = channelui.BrokerReaction
	// Deprecated: use channelui.BrokerMessageUsage directly.
	brokerMessageUsage = channelui.BrokerMessageUsage
	// Deprecated: use channelui.BrokerMessage directly.
	brokerMessage = channelui.BrokerMessage
	// Deprecated: use channelui.RenderedLine directly.
	renderedLine = channelui.RenderedLine
	// Deprecated: use channelui.ThreadedMessage directly.
	threadedMessage = channelui.ThreadedMessage
	// Deprecated: use channelui.LayoutDimensions directly.
	layoutDimensions = channelui.LayoutDimensions
	// Deprecated: use channelui.OfficeMember directly.
	officeMemberInfo = channelui.OfficeMember
	// Deprecated: use channelui.Member directly.
	channelMember = channelui.Member
	// Deprecated: use channelui.ChannelInfo directly.
	channelInfo = channelui.ChannelInfo
	// Deprecated: use channelui.InterviewOption directly.
	channelInterviewOption = channelui.InterviewOption
	// Deprecated: use channelui.Interview directly.
	channelInterview = channelui.Interview
	// Deprecated: use channelui.UsageTotals directly.
	channelUsageTotals = channelui.UsageTotals
	// Deprecated: use channelui.UsageState directly.
	channelUsageState = channelui.UsageState
	// Deprecated: use channelui.Task directly.
	channelTask = channelui.Task
	// Deprecated: use channelui.Action directly.
	channelAction = channelui.Action
	// Deprecated: use channelui.Signal directly.
	channelSignal = channelui.Signal
	// Deprecated: use channelui.Decision directly.
	channelDecision = channelui.Decision
	// Deprecated: use channelui.Watchdog directly.
	channelWatchdog = channelui.Watchdog
	// Deprecated: use channelui.SchedulerJob directly.
	channelSchedulerJob = channelui.SchedulerJob
	// Deprecated: use channelui.Skill directly.
	channelSkill = channelui.Skill
	// Deprecated: use channelui.CalendarRange directly.
	calendarRange = channelui.CalendarRange
	// Deprecated: use channelui.CalendarEvent directly.
	calendarEvent = channelui.CalendarEvent
	// Deprecated: use channelui.RecoverySurgeryOption directly.
	recoverySurgeryOption = channelui.RecoverySurgeryOption
	// Deprecated: use channelui.InterviewPhase directly.
	channelInterviewPhase = channelui.InterviewPhase
	// Deprecated: use channelui.MemberActivity directly.
	memberActivity = channelui.MemberActivity
	// Deprecated: use channelui.OfficeCharacter directly.
	officeCharacter = channelui.OfficeCharacter
	// Deprecated: use channelui.ChannelConfirmAction directly.
	channelConfirmAction = channelui.ChannelConfirmAction
	// Deprecated: use channelui.ChannelConfirm directly.
	channelConfirm = channelui.ChannelConfirm
	// Deprecated: use channelui.ComposerPopupOption directly.
	composerPopupOption = channelui.ComposerPopupOption
	// Deprecated: use channelui.OfficeSidebarApp directly.
	officeSidebarApp = channelui.OfficeSidebarApp
	// Deprecated: use channelui.DoctorSeverity directly.
	doctorSeverity = channelui.DoctorSeverity
	// Deprecated: use channelui.DoctorCheck directly.
	doctorCheck = channelui.DoctorCheck
	// Deprecated: use channelui.DoctorReport directly.
	channelDoctorReport = channelui.DoctorReport
	// Deprecated: use channelui.WorkspaceReadinessLevel directly.
	workspaceReadinessLevel = channelui.WorkspaceReadinessLevel
	// Deprecated: use channelui.WorkspaceReadinessState directly.
	workspaceReadinessState = channelui.WorkspaceReadinessState
	// Deprecated: use channelui.WorkspaceUIState directly.
	workspaceUIState = channelui.WorkspaceUIState
	// Deprecated: use channelui.MemberRuntimeSummary directly.
	memberRuntimeSummary = channelui.MemberRuntimeSummary
	// Deprecated: use channelui.RuntimeArtifactSnapshot directly.
	runtimeArtifactSnapshot = channelui.RuntimeArtifactSnapshot
	// Deprecated: use channelui.TaskLogRecord directly.
	taskLogRecord = channelui.TaskLogRecord
	// Deprecated: use channelui.TaskLogArtifact directly.
	taskLogArtifact = channelui.TaskLogArtifact
	// Deprecated: use channelui.WorkflowRunArtifact directly.
	workflowRunArtifact = channelui.WorkflowRunArtifact
)

// Function aliases keep the lowercase names callable from package main
// while the helpers physically live in channelui. Each carries a
// `// Deprecated:` doc so that staticcheck SA1019 surfaces fresh
// callers in CI. Removed in the cleanup PR.
var (
	// Deprecated: use channelui.CountReplies directly.
	countReplies = channelui.CountReplies
	// Deprecated: use channelui.BuildReplyChildren directly.
	buildReplyChildren = channelui.BuildReplyChildren
	// Deprecated: use channelui.ParseTimestamp directly.
	parseTimestamp = channelui.ParseTimestamp
	// Deprecated: use channelui.FormatShortTime directly.
	formatShortTime = channelui.FormatShortTime
	// Deprecated: use channelui.ComputeLayout directly.
	computeLayout = channelui.ComputeLayout
	// Deprecated: use channelui.RenderVerticalBorder directly.
	renderVerticalBorder = channelui.RenderVerticalBorder

	// Deprecated: use channelui.MaxInt directly.
	maxInt = channelui.MaxInt
	// Deprecated: use channelui.ClampScroll directly.
	clampScroll = channelui.ClampScroll
	// Deprecated: use channelui.OverlayBottomLines directly.
	overlayBottomLines = channelui.OverlayBottomLines
	// Deprecated: use channelui.FindMessageByID directly.
	findMessageByID = channelui.FindMessageByID
	// Deprecated: use channelui.ContainsString directly.
	containsString = channelui.ContainsString
	// Deprecated: use channelui.ShortClock directly.
	shortClock = channelui.ShortClock
	// Deprecated: use channelui.FormatMinutes directly.
	formatMinutes = channelui.FormatMinutes
	// Deprecated: use channelui.FallbackString directly.
	fallbackString = channelui.FallbackString
	// Deprecated: use channelui.ParseChannelTime directly.
	parseChannelTime = channelui.ParseChannelTime
	// Deprecated: use channelui.SameDay directly.
	sameDay = channelui.SameDay
	// Deprecated: use channelui.PrettyWhen directly.
	prettyWhen = channelui.PrettyWhen
	// Deprecated: use channelui.PrettyRelativeTime directly.
	prettyRelativeTime = channelui.PrettyRelativeTime
	// Deprecated: use channelui.RenderTimingSummary directly.
	renderTimingSummary = channelui.RenderTimingSummary

	// Deprecated: use channelui.AppendWrapped directly.
	appendWrapped = channelui.AppendWrapped
	// Deprecated: use channelui.TruncateText directly.
	truncateText = channelui.TruncateText
	// Deprecated: use channelui.MutedText directly.
	mutedText = channelui.MutedText
	// Deprecated: use channelui.RenderDateSeparator directly.
	renderDateSeparator = channelui.RenderDateSeparator
	// Deprecated: use channelui.HumanMessageLabel directly.
	humanMessageLabel = channelui.HumanMessageLabel
	// Deprecated: use channelui.RenderUnreadDivider directly.
	renderUnreadDivider = channelui.RenderUnreadDivider
	// Deprecated: use channelui.DisplayDecisionSummary directly.
	displayDecisionSummary = channelui.DisplayDecisionSummary

	// Deprecated: use channelui.DisplayName directly.
	displayName = channelui.DisplayName
	// Deprecated: use channelui.RoleLabel directly.
	roleLabel = channelui.RoleLabel

	// Deprecated: use channelui.AppIcon directly.
	appIcon = channelui.AppIcon

	// Deprecated: use channelui.MinInt directly.
	minInt = channelui.MinInt
	// Deprecated: use channelui.RenderRuntimeEventCard directly.
	renderRuntimeEventCard = channelui.RenderRuntimeEventCard

	// Deprecated: use channelui.BuildNeedsYouLines directly.
	buildNeedsYouLines = channelui.BuildNeedsYouLines
	// Deprecated: use channelui.BuildNeedsYouLinesForRequest directly.
	buildNeedsYouLinesForRequest = channelui.BuildNeedsYouLinesForRequest
	// Deprecated: use channelui.SelectNeedsYouRequest directly.
	selectNeedsYouRequest = channelui.SelectNeedsYouRequest
	// Deprecated: use channelui.IsOpenInterviewStatus directly.
	isOpenInterviewStatus = channelui.IsOpenInterviewStatus

	// Deprecated: use channelui.ReverseSignals directly.
	reverseSignals = channelui.ReverseSignals
	// Deprecated: use channelui.ReverseDecisions directly.
	reverseDecisions = channelui.ReverseDecisions
	// Deprecated: use channelui.ActiveWatchdogs directly.
	activeWatchdogs = channelui.ActiveWatchdogs
	// Deprecated: use channelui.ReverseWatchdogs directly.
	reverseWatchdogs = channelui.ReverseWatchdogs
	// Deprecated: use channelui.RecentExternalActions directly.
	recentExternalActions = channelui.RecentExternalActions
	// Deprecated: use channelui.AgentSlugForDisplay directly.
	agentSlugForDisplay = channelui.AgentSlugForDisplay
	// Deprecated: use channelui.DisplaySignalKind directly.
	displaySignalKind = channelui.DisplaySignalKind

	// Deprecated: use channelui.BuildRequestLines directly.
	buildRequestLines = channelui.BuildRequestLines
	// Deprecated: use channelui.BuildSkillLines directly.
	buildSkillLines = channelui.BuildSkillLines
	// Deprecated: use channelui.BuildPolicyLines directly.
	buildPolicyLines = channelui.BuildPolicyLines
	// Deprecated: use channelui.BuildTaskLines directly.
	buildTaskLines = channelui.BuildTaskLines

	// Deprecated: use channelui.CalendarEventColors directly.
	calendarEventColors = channelui.CalendarEventColors
	// Deprecated: use channelui.CollectCalendarEvents directly.
	collectCalendarEvents = channelui.CollectCalendarEvents
	// Deprecated: use channelui.TaskCalendarEvents directly.
	taskCalendarEvents = channelui.TaskCalendarEvents
	// Deprecated: use channelui.RequestCalendarEvents directly.
	requestCalendarEvents = channelui.RequestCalendarEvents
	// Deprecated: use channelui.DedupeCalendarEvents directly.
	dedupeCalendarEvents = channelui.DedupeCalendarEvents
	// Deprecated: use channelui.FilterCalendarEvents directly.
	filterCalendarEvents = channelui.FilterCalendarEvents
	// Deprecated: use channelui.PrettyCalendarWhen directly.
	prettyCalendarWhen = channelui.PrettyCalendarWhen
	// Deprecated: use channelui.CalendarBucketLabel directly.
	calendarBucketLabel = channelui.CalendarBucketLabel
	// Deprecated: use channelui.ChooseCalendarChannel directly.
	chooseCalendarChannel = channelui.ChooseCalendarChannel
	// Deprecated: use channelui.CalendarParticipantsForTask directly.
	calendarParticipantsForTask = channelui.CalendarParticipantsForTask
	// Deprecated: use channelui.CalendarParticipantSlugsForTask directly.
	calendarParticipantSlugsForTask = channelui.CalendarParticipantSlugsForTask
	// Deprecated: use channelui.CalendarParticipantsForRequest directly.
	calendarParticipantsForRequest = channelui.CalendarParticipantsForRequest
	// Deprecated: use channelui.CalendarParticipantSlugsForRequest directly.
	calendarParticipantSlugsForRequest = channelui.CalendarParticipantSlugsForRequest
	// Deprecated: use channelui.CalendarParticipantsForJob directly.
	calendarParticipantsForJob = channelui.CalendarParticipantsForJob
	// Deprecated: use channelui.CalendarParticipantSlugsForJob directly.
	calendarParticipantSlugsForJob = channelui.CalendarParticipantSlugsForJob
	// Deprecated: use channelui.CalendarParticipantNames directly.
	calendarParticipantNames = channelui.CalendarParticipantNames
	// Deprecated: use channelui.CalendarParticipantSlugs directly.
	calendarParticipantSlugs = channelui.CalendarParticipantSlugs
	// Deprecated: use channelui.NextCalendarEventByParticipant directly.
	nextCalendarEventByParticipant = channelui.NextCalendarEventByParticipant
	// Deprecated: use channelui.OrderedCalendarParticipants directly.
	orderedCalendarParticipants = channelui.OrderedCalendarParticipants
	// Deprecated: use channelui.SchedulerTargetTaskID directly.
	schedulerTargetTaskID = channelui.SchedulerTargetTaskID
	// Deprecated: use channelui.SchedulerTargetRequestID directly.
	schedulerTargetRequestID = channelui.SchedulerTargetRequestID
	// Deprecated: use channelui.SchedulerTargetThreadID directly.
	schedulerTargetThreadID = channelui.SchedulerTargetThreadID

	// Deprecated: use channelui.NormalizeSidebarSlug directly.
	normalizeSidebarSlug = channelui.NormalizeSidebarSlug
	// Deprecated: use channelui.BuildCalendarLines directly.
	buildCalendarLines = channelui.BuildCalendarLines
	// Deprecated: use channelui.BuildCalendarToolbar directly.
	buildCalendarToolbar = channelui.BuildCalendarToolbar
	// Deprecated: use channelui.RenderCalendarEventCard directly.
	renderCalendarEventCard = channelui.RenderCalendarEventCard
	// Deprecated: use channelui.RenderCalendarParticipantCard directly.
	renderCalendarParticipantCard = channelui.RenderCalendarParticipantCard
	// Deprecated: use channelui.RenderCalendarActionCard directly.
	renderCalendarActionCard = channelui.RenderCalendarActionCard
	// Deprecated: use channelui.RenderedCardLines directly.
	renderedCardLines = channelui.RenderedCardLines
	// Deprecated: use channelui.RenderedCardLinesWithPrompt directly.
	renderedCardLinesWithPrompt = channelui.RenderedCardLinesWithPrompt

	// Deprecated: use channelui.RenderReactions directly.
	renderReactions = channelui.RenderReactions
	// Deprecated: use channelui.MessageUsageTotal directly.
	messageUsageTotal = channelui.MessageUsageTotal
	// Deprecated: use channelui.RenderMessageUsageMeta directly.
	renderMessageUsageMeta = channelui.RenderMessageUsageMeta
	// Deprecated: use channelui.DefaultHumanMessageTitle directly.
	defaultHumanMessageTitle = channelui.DefaultHumanMessageTitle
	// Deprecated: use channelui.SliceRenderedLines directly.
	sliceRenderedLines = channelui.SliceRenderedLines
	// Deprecated: use channelui.FormatTokenCount directly.
	formatTokenCount = channelui.FormatTokenCount

	// Deprecated: use channelui.CloneRenderedLines directly.
	cloneRenderedLines = channelui.CloneRenderedLines
	// Deprecated: use channelui.CloneThreadedMessages directly.
	cloneThreadedMessages = channelui.CloneThreadedMessages
	// Deprecated: use channelui.RenderTimeBucket directly.
	renderTimeBucket = channelui.RenderTimeBucket

	// Deprecated: use channelui.ThreadRootMessageID directly.
	threadRootMessageID = channelui.ThreadRootMessageID
	// Deprecated: use channelui.HasThreadReplies directly.
	hasThreadReplies = channelui.HasThreadReplies
	// Deprecated: use channelui.CountThreadReplies directly.
	countThreadReplies = channelui.CountThreadReplies
	// Deprecated: use channelui.ThreadParticipants directly.
	threadParticipants = channelui.ThreadParticipants
	// Deprecated: use channelui.FlattenThreadMessages directly.
	flattenThreadMessages = channelui.FlattenThreadMessages

	// Deprecated: use channelui.TrimRecoverySentence directly.
	trimRecoverySentence = channelui.TrimRecoverySentence
	// Deprecated: use channelui.RenderAwayStrip directly.
	renderAwayStrip = channelui.RenderAwayStrip
	// Deprecated: use channelui.BuildRecoverySurgeryOptions directly.
	buildRecoverySurgeryOptions = channelui.BuildRecoverySurgeryOptions
	// Deprecated: use channelui.BuildRecoveryPromptForMessage directly.
	buildRecoveryPromptForMessage = channelui.BuildRecoveryPromptForMessage
	// Deprecated: use channelui.BuildRecoveryPromptForRequest directly.
	buildRecoveryPromptForRequest = channelui.BuildRecoveryPromptForRequest
	// Deprecated: use channelui.BuildRecoveryPromptForTask directly.
	buildRecoveryPromptForTask = channelui.BuildRecoveryPromptForTask
	// Deprecated: use channelui.RenderRecoveryActionCard directly.
	renderRecoveryActionCard = channelui.RenderRecoveryActionCard
	// Deprecated: use channelui.PrefixedCardLines directly.
	prefixedCardLines = channelui.PrefixedCardLines
	// Deprecated: use channelui.RecoveryActiveTasks directly.
	recoveryActiveTasks = channelui.RecoveryActiveTasks
	// Deprecated: use channelui.RecoveryRecentThreads directly.
	recoveryRecentThreads = channelui.RecoveryRecentThreads

	// Deprecated: use channelui.InterviewOptionRequiresText directly.
	interviewOptionRequiresText = channelui.InterviewOptionRequiresText
	// Deprecated: use channelui.InterviewOptionTextHint directly.
	interviewOptionTextHint = channelui.InterviewOptionTextHint
	// Deprecated: use channelui.SelectedInterviewOption directly.
	selectedInterviewOption = channelui.SelectedInterviewOption

	// Deprecated: use channelui.HighlightMentions directly.
	highlightMentions = channelui.HighlightMentions
	// Deprecated: use channelui.FlattenThreadReplies directly.
	flattenThreadReplies = channelui.FlattenThreadReplies
	// Deprecated: use channelui.RenderThreadReplies directly.
	renderThreadReplies = channelui.RenderThreadReplies
	// Deprecated: use channelui.RenderThreadReply directly.
	renderThreadReply = channelui.RenderThreadReply
	// Deprecated: use channelui.RenderThreadMessage directly.
	renderThreadMessage = channelui.RenderThreadMessage
	// Deprecated: use channelui.SummarizeUnreadMessages directly.
	summarizeUnreadMessages = channelui.SummarizeUnreadMessages

	// Deprecated: use channelui.FilterMessagesForViewerScope directly.
	filterMessagesForViewerScope = channelui.FilterMessagesForViewerScope
	// Deprecated: use channelui.NormalizeMailboxScope directly.
	normalizeMailboxScope = channelui.NormalizeMailboxScope
	// Deprecated: use channelui.MailboxMessageMatchesViewerScope directly.
	mailboxMessageMatchesViewerScope = channelui.MailboxMessageMatchesViewerScope
	// Deprecated: use channelui.MailboxMessageBelongsToViewerOutbox directly.
	mailboxMessageBelongsToViewerOutbox = channelui.MailboxMessageBelongsToViewerOutbox
	// Deprecated: use channelui.MailboxMessageBelongsToViewerInbox directly.
	mailboxMessageBelongsToViewerInbox = channelui.MailboxMessageBelongsToViewerInbox
	// Deprecated: use channelui.MailboxMessageRepliesToViewerThread directly.
	mailboxMessageRepliesToViewerThread = channelui.MailboxMessageRepliesToViewerThread
	// Deprecated: use channelui.NormalizeDraftSlug directly.
	normalizeDraftSlug = channelui.NormalizeDraftSlug
	// Deprecated: use channelui.ParseExpertiseInput directly.
	parseExpertiseInput = channelui.ParseExpertiseInput
	// Deprecated: use channelui.LiveActivityFromMembers directly.
	liveActivityFromMembers = channelui.LiveActivityFromMembers

	// Deprecated: use channelui.TruncateLabel directly.
	truncateLabel = channelui.TruncateLabel
	// Deprecated: use channelui.SidebarAgentColors directly.
	sidebarAgentColors = channelui.SidebarAgentColors
	// Deprecated: use channelui.ClassifyActivity directly.
	classifyActivity = channelui.ClassifyActivity
	// Deprecated: use channelui.DefaultSidebarRoster directly.
	defaultSidebarRoster = channelui.DefaultSidebarRoster
	// Deprecated: use channelui.RenderOfficeCharacter directly.
	renderOfficeCharacter = channelui.RenderOfficeCharacter
	// Deprecated: use channelui.OfficeAside directly.
	officeAside = channelui.OfficeAside
	// Deprecated: use channelui.ActiveSidebarTask directly.
	activeSidebarTask = channelui.ActiveSidebarTask
	// Deprecated: use channelui.ApplyTaskActivity directly.
	applyTaskActivity = channelui.ApplyTaskActivity
	// Deprecated: use channelui.TaskBubbleText directly.
	taskBubbleText = channelui.TaskBubbleText
	// Deprecated: use channelui.RenderThoughtBubble directly.
	renderThoughtBubble = channelui.RenderThoughtBubble
	// Deprecated: use channelui.PadSidebarContent directly.
	padSidebarContent = channelui.PadSidebarContent
	// Deprecated: use channelui.SidebarPlainRow directly.
	sidebarPlainRow = channelui.SidebarPlainRow
	// Deprecated: use channelui.SidebarStyledRow directly.
	sidebarStyledRow = channelui.SidebarStyledRow

	// Deprecated: use channelui.ConfirmationForResetDM directly.
	confirmationForResetDM = channelui.ConfirmationForResetDM
	// Deprecated: use channelui.ConfirmationForInterviewAnswer directly.
	confirmationForInterviewAnswer = channelui.ConfirmationForInterviewAnswer
	// Deprecated: use channelui.RenderConfirmCard directly.
	renderConfirmCard = channelui.RenderConfirmCard
	// Deprecated: use channelui.RenderComposerPopup directly.
	renderComposerPopup = channelui.RenderComposerPopup
	// Deprecated: use channelui.TypingAgentsFromMembers directly.
	typingAgentsFromMembers = channelui.TypingAgentsFromMembers

	// Deprecated: use channelui.TaskStatusLine directly.
	taskStatusLine = channelui.TaskStatusLine
	// Deprecated: use channelui.SummarizeLiveActivity directly.
	summarizeLiveActivity = channelui.SummarizeLiveActivity
	// Deprecated: use channelui.SanitizeActivityLine directly.
	sanitizeActivityLine = channelui.SanitizeActivityLine
	// Deprecated: use channelui.SummarizeSentence directly.
	summarizeSentence = channelui.SummarizeSentence
	// Deprecated: use channelui.BlockedWorkTasks directly.
	blockedWorkTasks = channelui.BlockedWorkTasks
	// Deprecated: use channelui.RecentDirectExecutionActions directly.
	recentDirectExecutionActions = channelui.RecentDirectExecutionActions
	// Deprecated: use channelui.ExecutionMetaLine directly.
	executionMetaLine = channelui.ExecutionMetaLine
	// Deprecated: use channelui.LatestRelevantAction directly.
	latestRelevantAction = channelui.LatestRelevantAction
	// Deprecated: use channelui.DescribeActionState directly.
	describeActionState = channelui.DescribeActionState
	// Deprecated: use channelui.ActivityPill directly.
	activityPill = channelui.ActivityPill
	// Deprecated: use channelui.ActionStatePill directly.
	actionStatePill = channelui.ActionStatePill

	// Deprecated: use channelui.ArtifactLifecyclePill directly.
	artifactLifecyclePill = channelui.ArtifactLifecyclePill
	// Deprecated: use channelui.ArtifactAccentColor directly.
	artifactAccentColor = channelui.ArtifactAccentColor
	// Deprecated: use channelui.ParseArtifactTimestamp directly.
	parseArtifactTimestamp = channelui.ParseArtifactTimestamp
	// Deprecated: use channelui.RecentHumanArtifactRequests directly.
	recentHumanArtifactRequests = channelui.RecentHumanArtifactRequests
	// Deprecated: use channelui.RecentExecutionArtifactActions directly.
	recentExecutionArtifactActions = channelui.RecentExecutionArtifactActions
	// Deprecated: use channelui.ArtifactClock directly.
	artifactClock = channelui.ArtifactClock
	// Deprecated: use channelui.ArtifactTime directly.
	artifactTime = channelui.ArtifactTime

	// Deprecated: use channelui.OfficeSidebarApps directly.
	officeSidebarApps = channelui.OfficeSidebarApps
	// Deprecated: use channelui.VisibleSidebarApps directly.
	visibleSidebarApps = channelui.VisibleSidebarApps
	// Deprecated: use channelui.ContainsSlug directly.
	containsSlug = channelui.ContainsSlug
	// Deprecated: use channelui.PluralizeWord directly.
	pluralizeWord = channelui.PluralizeWord
	// Deprecated: use channelui.ExtractTagsFromText directly.
	extractTagsFromText = channelui.ExtractTagsFromText
	// Deprecated: use channelui.ChannelExists directly.
	channelExists = channelui.ChannelExists
	// Deprecated: use channelui.NormalizeCursorPos directly.
	normalizeCursorPos = channelui.NormalizeCursorPos
	// Deprecated: use channelui.InsertComposerRunes directly.
	insertComposerRunes = channelui.InsertComposerRunes

	// Deprecated: use channelui.ReplaceMentionInInput directly.
	replaceMentionInInput = channelui.ReplaceMentionInInput
	// Deprecated: use channelui.IsComposerWordRune directly.
	isComposerWordRune = channelui.IsComposerWordRune
	// Deprecated: use channelui.MoveCursorBackwardWord directly.
	moveCursorBackwardWord = channelui.MoveCursorBackwardWord
	// Deprecated: use channelui.MoveCursorForwardWord directly.
	moveCursorForwardWord = channelui.MoveCursorForwardWord
	// Deprecated: use channelui.MoveComposerCursor directly.
	moveComposerCursor = channelui.MoveComposerCursor

	// Deprecated: use channelui.FilterInsightMessages directly.
	filterInsightMessages = channelui.FilterInsightMessages
	// Deprecated: use channelui.LatestHumanFacingMessage directly.
	latestHumanFacingMessage = channelui.LatestHumanFacingMessage
	// Deprecated: use channelui.CountUniqueAgents directly.
	countUniqueAgents = channelui.CountUniqueAgents

	// Deprecated: use channelui.AppendUniqueMessages directly.
	appendUniqueMessages = channelui.AppendUniqueMessages
	// Deprecated: use channelui.PopupActionIndex directly.
	popupActionIndex = channelui.PopupActionIndex
	// Deprecated: use channelui.FormatUSD directly.
	formatUsd = channelui.FormatUSD

	// Deprecated: use channelui.InferMood directly.
	inferMood = channelui.InferMood
	// Deprecated: use channelui.RenderInterviewCard directly.
	renderInterviewCard = channelui.RenderInterviewCard

	// Deprecated: use channelui.MergeOfficeMembers directly.
	mergeOfficeMembers = channelui.MergeOfficeMembers
	// Deprecated: use channelui.OfficeMembersFromManifest directly.
	officeMembersFromManifest = channelui.OfficeMembersFromManifest
	// Deprecated: use channelui.ChannelInfosFromManifest directly.
	channelInfosFromManifest = channelui.ChannelInfosFromManifest
	// Deprecated: use channelui.OfficeMembersFallback directly.
	officeMembersFallback = channelui.OfficeMembersFallback
	// Deprecated: use channelui.ChannelInfosFallback directly.
	channelInfosFallback = channelui.ChannelInfosFallback

	// Deprecated: use channelui.MapString directly.
	mapString = channelui.MapString
	// Deprecated: use channelui.OpenBrowserURL directly.
	openBrowserURL = channelui.OpenBrowserURL
	// Deprecated: use channelui.IsDarwin directly.
	isDarwin = channelui.IsDarwin
	// Deprecated: use channelui.IsLinux directly.
	isLinux = channelui.IsLinux
	// Deprecated: use channelui.IsWindows directly.
	isWindows = channelui.IsWindows
	// Deprecated: use channelui.ResolveInitialOfficeApp directly.
	resolveInitialOfficeApp = channelui.ResolveInitialOfficeApp

	// Deprecated: use channelui.RenderUsageStrip directly.
	renderUsageStrip = channelui.RenderUsageStrip
	// Deprecated: use channelui.SidebarShortcutLabel directly.
	sidebarShortcutLabel = channelui.SidebarShortcutLabel

	// Deprecated: use channelui.DoctorSeverityForCapability directly.
	doctorSeverityForCapability = channelui.DoctorSeverityForCapability
	// Deprecated: use channelui.RenderDoctorCard directly.
	renderDoctorCard = channelui.RenderDoctorCard
	// Deprecated: use channelui.RenderDoctorLabel directly.
	renderDoctorLabel = channelui.RenderDoctorLabel
	// Deprecated: use channelui.RenderDoctorLifecycle directly.
	renderDoctorLifecycle = channelui.RenderDoctorLifecycle

	// Deprecated: use channelui.SummarizeAwayRecovery directly.
	summarizeAwayRecovery = channelui.SummarizeAwayRecovery
	// Deprecated: use channelui.RuntimeRequestIsOpen directly.
	runtimeRequestIsOpen = channelui.RuntimeRequestIsOpen
	// Deprecated: use channelui.FirstWorkspaceString directly.
	firstWorkspaceString = channelui.FirstWorkspaceString
	// Deprecated: use channelui.SidebarViewLabel directly.
	sidebarViewLabel = channelui.SidebarViewLabel
	// Deprecated: use channelui.FirstDoctorNextStep directly.
	firstDoctorNextStep = channelui.FirstDoctorNextStep

	// Deprecated: use channelui.RuntimeTasksFromChannel directly.
	runtimeTasksFromChannel = channelui.RuntimeTasksFromChannel
	// Deprecated: use channelui.RuntimeRequestsFromChannel directly.
	runtimeRequestsFromChannel = channelui.RuntimeRequestsFromChannel
	// Deprecated: use channelui.RuntimeMessagesFromChannel directly.
	runtimeMessagesFromChannel = channelui.RuntimeMessagesFromChannel
	// Deprecated: use channelui.CountRunningRuntimeTasks directly.
	countRunningRuntimeTasks = channelui.CountRunningRuntimeTasks
	// Deprecated: use channelui.CountIsolatedRuntimeTasks directly.
	countIsolatedRuntimeTasks = channelui.CountIsolatedRuntimeTasks

	// Deprecated: use channelui.ResolveWorkspaceAwaySummary directly.
	resolveWorkspaceAwaySummary = channelui.ResolveWorkspaceAwaySummary
	// Deprecated: use channelui.DeriveWorkspaceReadiness directly.
	deriveWorkspaceReadiness = channelui.DeriveWorkspaceReadiness

	// Deprecated: use channelui.BuildRecoveryLines directly.
	buildRecoveryLines = channelui.BuildRecoveryLines
	// Deprecated: use channelui.BuildRecoveryActionLines directly.
	buildRecoveryActionLines = channelui.BuildRecoveryActionLines
	// Deprecated: use channelui.BuildRecoverySurgeryLines directly.
	buildRecoverySurgeryLines = channelui.BuildRecoverySurgeryLines

	// Deprecated: use channelui.DeriveMemberRuntimeSummary directly.
	deriveMemberRuntimeSummary = channelui.DeriveMemberRuntimeSummary
	// Deprecated: use channelui.BuildLiveWorkLines directly.
	buildLiveWorkLines = channelui.BuildLiveWorkLines
	// Deprecated: use channelui.BuildWaitStateLines directly.
	buildWaitStateLines = channelui.BuildWaitStateLines
	// Deprecated: use channelui.BuildDirectExecutionLines directly.
	buildDirectExecutionLines = channelui.BuildDirectExecutionLines
	// Deprecated: use channelui.RenderRuntimeStrip directly.
	renderRuntimeStrip = channelui.RenderRuntimeStrip
	// Deprecated: use channelui.OneOnOneRuntimeLine directly.
	oneOnOneRuntimeLine = channelui.OneOnOneRuntimeLine

	// Deprecated: use channelui.RenderArtifactSection directly.
	renderArtifactSection = channelui.RenderArtifactSection
	// Deprecated: use channelui.RenderArtifactHeader directly.
	renderArtifactHeader = channelui.RenderArtifactHeader
	// Deprecated: use channelui.ArtifactExtraLines directly.
	artifactExtraLines = channelui.ArtifactExtraLines

	// Deprecated: use channelui.SummarizeJSONField directly.
	summarizeJSONField = channelui.SummarizeJSONField
	// Deprecated: use channelui.TaskLogRoot directly.
	taskLogRoot = channelui.TaskLogRoot

	// Deprecated: use channelui.AppendChannelCrashLog directly.
	appendChannelCrashLog = channelui.AppendChannelCrashLog
	// Deprecated: use channelui.ChannelCrashLogPath directly.
	channelCrashLogPath = channelui.ChannelCrashLogPath

	// Deprecated: use channelui.RecentArtifactTasks directly.
	recentArtifactTasks = channelui.RecentArtifactTasks
	// Deprecated: use channelui.BuildRequestRuntimeArtifact directly.
	buildRequestRuntimeArtifact = channelui.BuildRequestRuntimeArtifact
	// Deprecated: use channelui.BuildActionRuntimeArtifact directly.
	buildActionRuntimeArtifact = channelui.BuildActionRuntimeArtifact
	// Deprecated: use channelui.RequestArtifactProgress directly.
	requestArtifactProgress = channelui.RequestArtifactProgress
	// Deprecated: use channelui.RequestArtifactReviewHint directly.
	requestArtifactReviewHint = channelui.RequestArtifactReviewHint
	// Deprecated: use channelui.NormalizeRequestArtifactState directly.
	normalizeRequestArtifactState = channelui.NormalizeRequestArtifactState
	// Deprecated: use channelui.ActionArtifactSummary directly.
	actionArtifactSummary = channelui.ActionArtifactSummary
	// Deprecated: use channelui.ActionArtifactProgress directly.
	actionArtifactProgress = channelui.ActionArtifactProgress
	// Deprecated: use channelui.ActionArtifactResumeHint directly.
	actionArtifactResumeHint = channelui.ActionArtifactResumeHint
	// Deprecated: use channelui.NormalizeActionArtifactState directly.
	normalizeActionArtifactState = channelui.NormalizeActionArtifactState
	// Deprecated: use channelui.LatestArtifactTimestamp directly.
	latestArtifactTimestamp = channelui.LatestArtifactTimestamp

	// Deprecated: use channelui.SummarizeTaskLogRecord directly.
	summarizeTaskLogRecord = channelui.SummarizeTaskLogRecord
	// Deprecated: use channelui.BuildTaskRuntimeArtifact directly.
	buildTaskRuntimeArtifact = channelui.BuildTaskRuntimeArtifact
	// Deprecated: use channelui.BuildOrphanTaskLogRuntimeArtifact directly.
	buildOrphanTaskLogRuntimeArtifact = channelui.BuildOrphanTaskLogRuntimeArtifact
	// Deprecated: use channelui.BuildWorkflowRuntimeArtifact directly.
	buildWorkflowRuntimeArtifact = channelui.BuildWorkflowRuntimeArtifact
	// Deprecated: use channelui.BuildTaskArtifactSummary directly.
	buildTaskArtifactSummary = channelui.BuildTaskArtifactSummary
	// Deprecated: use channelui.BuildTaskArtifactProgress directly.
	buildTaskArtifactProgress = channelui.BuildTaskArtifactProgress
	// Deprecated: use channelui.BuildTaskArtifactReviewHint directly.
	buildTaskArtifactReviewHint = channelui.BuildTaskArtifactReviewHint
	// Deprecated: use channelui.BuildTaskArtifactResumeHint directly.
	buildTaskArtifactResumeHint = channelui.BuildTaskArtifactResumeHint
	// Deprecated: use channelui.NormalizeTaskArtifactState directly.
	normalizeTaskArtifactState = channelui.NormalizeTaskArtifactState
	// Deprecated: use channelui.WorkflowArtifactProgress directly.
	workflowArtifactProgress = channelui.WorkflowArtifactProgress
	// Deprecated: use channelui.NormalizeWorkflowArtifactState directly.
	normalizeWorkflowArtifactState = channelui.NormalizeWorkflowArtifactState
)

// Workspace readiness level consts.
const (
	// Deprecated: use channelui.WorkspaceReadinessReady directly.
	workspaceReadinessReady = channelui.WorkspaceReadinessReady
	// Deprecated: use channelui.WorkspaceReadinessWarn directly.
	workspaceReadinessWarn = channelui.WorkspaceReadinessWarn
	// Deprecated: use channelui.WorkspaceReadinessPreview directly.
	workspaceReadinessPreview = channelui.WorkspaceReadinessPreview
)

// Doctor severity consts mirror channelui's exported names.
const (
	// Deprecated: use channelui.DoctorOK directly.
	doctorOK = channelui.DoctorOK
	// Deprecated: use channelui.DoctorWarn directly.
	doctorWarn = channelui.DoctorWarn
	// Deprecated: use channelui.DoctorFail directly.
	doctorFail = channelui.DoctorFail
	// Deprecated: use channelui.DoctorInfo directly.
	doctorInfo = channelui.DoctorInfo
)

// Channel-confirm action typed-string consts.
const (
	// Deprecated: use channelui.ChannelConfirmActionResetTeam directly.
	confirmActionResetTeam = channelui.ChannelConfirmActionResetTeam
	// Deprecated: use channelui.ChannelConfirmActionResetDM directly.
	confirmActionResetDM = channelui.ChannelConfirmActionResetDM
	// Deprecated: use channelui.ChannelConfirmActionSwitchMode directly.
	confirmActionSwitchMode = channelui.ChannelConfirmActionSwitchMode
	// Deprecated: use channelui.ChannelConfirmActionRecoverFocus directly.
	confirmActionRecoverFocus = channelui.ChannelConfirmActionRecoverFocus
	// Deprecated: use channelui.ChannelConfirmActionSubmitRequest directly.
	confirmActionSubmitRequest = channelui.ChannelConfirmActionSubmitRequest
)

// Sidebar theme color constants.
const (
	// Deprecated: use channelui.SidebarBG directly.
	sidebarBG = channelui.SidebarBG
	// Deprecated: use channelui.SidebarMuted directly.
	sidebarMuted = channelui.SidebarMuted
	// Deprecated: use channelui.SidebarDivider directly.
	sidebarDivider = channelui.SidebarDivider
	// Deprecated: use channelui.SidebarActive directly.
	sidebarActive = channelui.SidebarActive

	// Deprecated: use channelui.DotTalking directly.
	dotTalking = channelui.DotTalking
	// Deprecated: use channelui.DotThinking directly.
	dotThinking = channelui.DotThinking
	// Deprecated: use channelui.DotCoding directly.
	dotCoding = channelui.DotCoding
	// Deprecated: use channelui.DotIdle directly.
	dotIdle = channelui.DotIdle
)

// Interview-phase typed-string consts.
const (
	// Deprecated: use channelui.InterviewPhaseChoose directly.
	interviewPhaseChoose = channelui.InterviewPhaseChoose
	// Deprecated: use channelui.InterviewPhaseDraft directly.
	interviewPhaseDraft = channelui.InterviewPhaseDraft
	// Deprecated: use channelui.InterviewPhaseReview directly.
	interviewPhaseReview = channelui.InterviewPhaseReview
)

// Calendar-range typed-string consts.
const (
	// Deprecated: use channelui.CalendarRangeDay directly.
	calendarRangeDay = channelui.CalendarRangeDay
	// Deprecated: use channelui.CalendarRangeWeek directly.
	calendarRangeWeek = channelui.CalendarRangeWeek
)

// Office-app constant aliases. Typed-string consts copy across packages
// while preserving type identity via the type alias above.
const (
	// Deprecated: use channelui.OfficeAppMessages directly.
	officeAppMessages = channelui.OfficeAppMessages
	// Deprecated: use channelui.OfficeAppInbox directly.
	officeAppInbox = channelui.OfficeAppInbox
	// Deprecated: use channelui.OfficeAppOutbox directly.
	officeAppOutbox = channelui.OfficeAppOutbox
	// Deprecated: use channelui.OfficeAppRecovery directly.
	officeAppRecovery = channelui.OfficeAppRecovery
	// Deprecated: use channelui.OfficeAppTasks directly.
	officeAppTasks = channelui.OfficeAppTasks
	// Deprecated: use channelui.OfficeAppRequests directly.
	officeAppRequests = channelui.OfficeAppRequests
	// Deprecated: use channelui.OfficeAppPolicies directly.
	officeAppPolicies = channelui.OfficeAppPolicies
	// Deprecated: use channelui.OfficeAppCalendar directly.
	officeAppCalendar = channelui.OfficeAppCalendar
	// Deprecated: use channelui.OfficeAppArtifacts directly.
	officeAppArtifacts = channelui.OfficeAppArtifacts
	// Deprecated: use channelui.OfficeAppSkills directly.
	officeAppSkills = channelui.OfficeAppSkills
)
