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
	officeApp               = channelui.OfficeApp
	brokerReaction          = channelui.BrokerReaction
	brokerMessageUsage      = channelui.BrokerMessageUsage
	brokerMessage           = channelui.BrokerMessage
	renderedLine            = channelui.RenderedLine
	threadedMessage         = channelui.ThreadedMessage
	layoutDimensions        = channelui.LayoutDimensions
	officeMemberInfo        = channelui.OfficeMember
	channelMember           = channelui.Member
	channelInfo             = channelui.ChannelInfo
	channelInterviewOption  = channelui.InterviewOption
	channelInterview        = channelui.Interview
	channelUsageTotals      = channelui.UsageTotals
	channelUsageState       = channelui.UsageState
	channelTask             = channelui.Task
	channelAction           = channelui.Action
	channelSignal           = channelui.Signal
	channelDecision         = channelui.Decision
	channelWatchdog         = channelui.Watchdog
	channelSchedulerJob     = channelui.SchedulerJob
	channelSkill            = channelui.Skill
	calendarRange           = channelui.CalendarRange
	calendarEvent           = channelui.CalendarEvent
	recoverySurgeryOption   = channelui.RecoverySurgeryOption
	channelInterviewPhase   = channelui.InterviewPhase
	memberActivity          = channelui.MemberActivity
	officeCharacter         = channelui.OfficeCharacter
	channelConfirmAction    = channelui.ChannelConfirmAction
	channelConfirm          = channelui.ChannelConfirm
	composerPopupOption     = channelui.ComposerPopupOption
	officeSidebarApp        = channelui.OfficeSidebarApp
	doctorSeverity          = channelui.DoctorSeverity
	doctorCheck             = channelui.DoctorCheck
	channelDoctorReport     = channelui.DoctorReport
	workspaceReadinessLevel = channelui.WorkspaceReadinessLevel
	workspaceReadinessState = channelui.WorkspaceReadinessState
	workspaceUIState        = channelui.WorkspaceUIState
	memberRuntimeSummary    = channelui.MemberRuntimeSummary
	runtimeArtifactSnapshot = channelui.RuntimeArtifactSnapshot
	taskLogRecord           = channelui.TaskLogRecord
	taskLogArtifact         = channelui.TaskLogArtifact
	workflowRunArtifact     = channelui.WorkflowRunArtifact
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

	appIcon = channelui.AppIcon

	minInt                 = channelui.MinInt
	renderRuntimeEventCard = channelui.RenderRuntimeEventCard

	buildNeedsYouLines           = channelui.BuildNeedsYouLines
	buildNeedsYouLinesForRequest = channelui.BuildNeedsYouLinesForRequest
	selectNeedsYouRequest        = channelui.SelectNeedsYouRequest
	isOpenInterviewStatus        = channelui.IsOpenInterviewStatus

	reverseSignals        = channelui.ReverseSignals
	reverseDecisions      = channelui.ReverseDecisions
	activeWatchdogs       = channelui.ActiveWatchdogs
	reverseWatchdogs      = channelui.ReverseWatchdogs
	recentExternalActions = channelui.RecentExternalActions
	agentSlugForDisplay   = channelui.AgentSlugForDisplay
	displaySignalKind     = channelui.DisplaySignalKind

	buildRequestLines = channelui.BuildRequestLines
	buildSkillLines   = channelui.BuildSkillLines
	buildPolicyLines  = channelui.BuildPolicyLines
	buildTaskLines    = channelui.BuildTaskLines

	calendarEventColors                = channelui.CalendarEventColors
	collectCalendarEvents              = channelui.CollectCalendarEvents
	taskCalendarEvents                 = channelui.TaskCalendarEvents
	requestCalendarEvents              = channelui.RequestCalendarEvents
	dedupeCalendarEvents               = channelui.DedupeCalendarEvents
	filterCalendarEvents               = channelui.FilterCalendarEvents
	prettyCalendarWhen                 = channelui.PrettyCalendarWhen
	calendarBucketLabel                = channelui.CalendarBucketLabel
	chooseCalendarChannel              = channelui.ChooseCalendarChannel
	calendarParticipantsForTask        = channelui.CalendarParticipantsForTask
	calendarParticipantSlugsForTask    = channelui.CalendarParticipantSlugsForTask
	calendarParticipantsForRequest     = channelui.CalendarParticipantsForRequest
	calendarParticipantSlugsForRequest = channelui.CalendarParticipantSlugsForRequest
	calendarParticipantsForJob         = channelui.CalendarParticipantsForJob
	calendarParticipantSlugsForJob     = channelui.CalendarParticipantSlugsForJob
	calendarParticipantNames           = channelui.CalendarParticipantNames
	calendarParticipantSlugs           = channelui.CalendarParticipantSlugs
	nextCalendarEventByParticipant     = channelui.NextCalendarEventByParticipant
	orderedCalendarParticipants        = channelui.OrderedCalendarParticipants
	schedulerTargetTaskID              = channelui.SchedulerTargetTaskID
	schedulerTargetRequestID           = channelui.SchedulerTargetRequestID
	schedulerTargetThreadID            = channelui.SchedulerTargetThreadID

	normalizeSidebarSlug          = channelui.NormalizeSidebarSlug
	buildCalendarLines            = channelui.BuildCalendarLines
	buildCalendarToolbar          = channelui.BuildCalendarToolbar
	renderCalendarEventCard       = channelui.RenderCalendarEventCard
	renderCalendarParticipantCard = channelui.RenderCalendarParticipantCard
	renderCalendarActionCard      = channelui.RenderCalendarActionCard
	renderedCardLines             = channelui.RenderedCardLines
	renderedCardLinesWithPrompt   = channelui.RenderedCardLinesWithPrompt

	renderReactions          = channelui.RenderReactions
	messageUsageTotal        = channelui.MessageUsageTotal
	renderMessageUsageMeta   = channelui.RenderMessageUsageMeta
	defaultHumanMessageTitle = channelui.DefaultHumanMessageTitle
	sliceRenderedLines       = channelui.SliceRenderedLines
	formatTokenCount         = channelui.FormatTokenCount

	cloneRenderedLines    = channelui.CloneRenderedLines
	cloneThreadedMessages = channelui.CloneThreadedMessages
	renderTimeBucket      = channelui.RenderTimeBucket

	threadRootMessageID   = channelui.ThreadRootMessageID
	hasThreadReplies      = channelui.HasThreadReplies
	countThreadReplies    = channelui.CountThreadReplies
	threadParticipants    = channelui.ThreadParticipants
	flattenThreadMessages = channelui.FlattenThreadMessages

	trimRecoverySentence          = channelui.TrimRecoverySentence
	renderAwayStrip               = channelui.RenderAwayStrip
	buildRecoverySurgeryOptions   = channelui.BuildRecoverySurgeryOptions
	buildRecoveryPromptForMessage = channelui.BuildRecoveryPromptForMessage
	buildRecoveryPromptForRequest = channelui.BuildRecoveryPromptForRequest
	buildRecoveryPromptForTask    = channelui.BuildRecoveryPromptForTask
	renderRecoveryActionCard      = channelui.RenderRecoveryActionCard
	prefixedCardLines             = channelui.PrefixedCardLines
	recoveryActiveTasks           = channelui.RecoveryActiveTasks
	recoveryRecentThreads         = channelui.RecoveryRecentThreads

	interviewOptionRequiresText = channelui.InterviewOptionRequiresText
	interviewOptionTextHint     = channelui.InterviewOptionTextHint
	selectedInterviewOption     = channelui.SelectedInterviewOption

	highlightMentions       = channelui.HighlightMentions
	flattenThreadReplies    = channelui.FlattenThreadReplies
	renderThreadReplies     = channelui.RenderThreadReplies
	renderThreadReply       = channelui.RenderThreadReply
	renderThreadMessage     = channelui.RenderThreadMessage
	summarizeUnreadMessages = channelui.SummarizeUnreadMessages

	filterMessagesForViewerScope        = channelui.FilterMessagesForViewerScope
	normalizeMailboxScope               = channelui.NormalizeMailboxScope
	mailboxMessageMatchesViewerScope    = channelui.MailboxMessageMatchesViewerScope
	mailboxMessageBelongsToViewerOutbox = channelui.MailboxMessageBelongsToViewerOutbox
	mailboxMessageBelongsToViewerInbox  = channelui.MailboxMessageBelongsToViewerInbox
	mailboxMessageRepliesToViewerThread = channelui.MailboxMessageRepliesToViewerThread
	normalizeDraftSlug                  = channelui.NormalizeDraftSlug
	parseExpertiseInput                 = channelui.ParseExpertiseInput
	liveActivityFromMembers             = channelui.LiveActivityFromMembers

	truncateLabel         = channelui.TruncateLabel
	sidebarAgentColors    = channelui.SidebarAgentColors
	classifyActivity      = channelui.ClassifyActivity
	defaultSidebarRoster  = channelui.DefaultSidebarRoster
	renderOfficeCharacter = channelui.RenderOfficeCharacter
	officeAside           = channelui.OfficeAside
	activeSidebarTask     = channelui.ActiveSidebarTask
	applyTaskActivity     = channelui.ApplyTaskActivity
	taskBubbleText        = channelui.TaskBubbleText
	renderThoughtBubble   = channelui.RenderThoughtBubble
	padSidebarContent     = channelui.PadSidebarContent
	sidebarPlainRow       = channelui.SidebarPlainRow
	sidebarStyledRow      = channelui.SidebarStyledRow

	confirmationForResetDM         = channelui.ConfirmationForResetDM
	confirmationForInterviewAnswer = channelui.ConfirmationForInterviewAnswer
	renderConfirmCard              = channelui.RenderConfirmCard
	renderComposerPopup            = channelui.RenderComposerPopup
	typingAgentsFromMembers        = channelui.TypingAgentsFromMembers

	taskStatusLine               = channelui.TaskStatusLine
	summarizeLiveActivity        = channelui.SummarizeLiveActivity
	sanitizeActivityLine         = channelui.SanitizeActivityLine
	summarizeSentence            = channelui.SummarizeSentence
	blockedWorkTasks             = channelui.BlockedWorkTasks
	recentDirectExecutionActions = channelui.RecentDirectExecutionActions
	executionMetaLine            = channelui.ExecutionMetaLine
	latestRelevantAction         = channelui.LatestRelevantAction
	describeActionState          = channelui.DescribeActionState
	activityPill                 = channelui.ActivityPill
	actionStatePill              = channelui.ActionStatePill

	artifactLifecyclePill          = channelui.ArtifactLifecyclePill
	artifactAccentColor            = channelui.ArtifactAccentColor
	parseArtifactTimestamp         = channelui.ParseArtifactTimestamp
	recentHumanArtifactRequests    = channelui.RecentHumanArtifactRequests
	recentExecutionArtifactActions = channelui.RecentExecutionArtifactActions
	artifactClock                  = channelui.ArtifactClock
	artifactTime                   = channelui.ArtifactTime

	officeSidebarApps   = channelui.OfficeSidebarApps
	visibleSidebarApps  = channelui.VisibleSidebarApps
	containsSlug        = channelui.ContainsSlug
	pluralizeWord       = channelui.PluralizeWord
	extractTagsFromText = channelui.ExtractTagsFromText
	channelExists       = channelui.ChannelExists
	normalizeCursorPos  = channelui.NormalizeCursorPos
	insertComposerRunes = channelui.InsertComposerRunes

	replaceMentionInInput  = channelui.ReplaceMentionInInput
	isComposerWordRune     = channelui.IsComposerWordRune
	moveCursorBackwardWord = channelui.MoveCursorBackwardWord
	moveCursorForwardWord  = channelui.MoveCursorForwardWord
	moveComposerCursor     = channelui.MoveComposerCursor

	filterInsightMessages    = channelui.FilterInsightMessages
	latestHumanFacingMessage = channelui.LatestHumanFacingMessage
	countUniqueAgents        = channelui.CountUniqueAgents

	appendUniqueMessages = channelui.AppendUniqueMessages
	popupActionIndex     = channelui.PopupActionIndex
	formatUsd            = channelui.FormatUSD

	inferMood           = channelui.InferMood
	renderInterviewCard = channelui.RenderInterviewCard

	mergeOfficeMembers        = channelui.MergeOfficeMembers
	officeMembersFromManifest = channelui.OfficeMembersFromManifest
	channelInfosFromManifest  = channelui.ChannelInfosFromManifest
	officeMembersFallback     = channelui.OfficeMembersFallback
	channelInfosFallback      = channelui.ChannelInfosFallback

	mapString               = channelui.MapString
	openBrowserURL          = channelui.OpenBrowserURL
	isDarwin                = channelui.IsDarwin
	isLinux                 = channelui.IsLinux
	isWindows               = channelui.IsWindows
	resolveInitialOfficeApp = channelui.ResolveInitialOfficeApp

	renderUsageStrip     = channelui.RenderUsageStrip
	sidebarShortcutLabel = channelui.SidebarShortcutLabel

	doctorSeverityForCapability = channelui.DoctorSeverityForCapability
	renderDoctorCard            = channelui.RenderDoctorCard
	renderDoctorLabel           = channelui.RenderDoctorLabel
	renderDoctorLifecycle       = channelui.RenderDoctorLifecycle

	summarizeAwayRecovery = channelui.SummarizeAwayRecovery
	runtimeRequestIsOpen  = channelui.RuntimeRequestIsOpen
	firstWorkspaceString  = channelui.FirstWorkspaceString
	sidebarViewLabel      = channelui.SidebarViewLabel
	firstDoctorNextStep   = channelui.FirstDoctorNextStep

	runtimeTasksFromChannel    = channelui.RuntimeTasksFromChannel
	runtimeRequestsFromChannel = channelui.RuntimeRequestsFromChannel
	runtimeMessagesFromChannel = channelui.RuntimeMessagesFromChannel
	countRunningRuntimeTasks   = channelui.CountRunningRuntimeTasks
	countIsolatedRuntimeTasks  = channelui.CountIsolatedRuntimeTasks

	resolveWorkspaceAwaySummary = channelui.ResolveWorkspaceAwaySummary
	deriveWorkspaceReadiness    = channelui.DeriveWorkspaceReadiness

	buildRecoveryLines        = channelui.BuildRecoveryLines
	buildRecoveryActionLines  = channelui.BuildRecoveryActionLines
	buildRecoverySurgeryLines = channelui.BuildRecoverySurgeryLines

	deriveMemberRuntimeSummary = channelui.DeriveMemberRuntimeSummary
	buildLiveWorkLines         = channelui.BuildLiveWorkLines
	buildWaitStateLines        = channelui.BuildWaitStateLines
	buildDirectExecutionLines  = channelui.BuildDirectExecutionLines
	renderRuntimeStrip         = channelui.RenderRuntimeStrip
	oneOnOneRuntimeLine        = channelui.OneOnOneRuntimeLine

	renderArtifactSection = channelui.RenderArtifactSection
	renderArtifactHeader  = channelui.RenderArtifactHeader
	artifactExtraLines    = channelui.ArtifactExtraLines

	summarizeJSONField = channelui.SummarizeJSONField
	taskLogRoot        = channelui.TaskLogRoot

	appendChannelCrashLog = channelui.AppendChannelCrashLog
	channelCrashLogPath   = channelui.ChannelCrashLogPath

	recentArtifactTasks           = channelui.RecentArtifactTasks
	buildRequestRuntimeArtifact   = channelui.BuildRequestRuntimeArtifact
	buildActionRuntimeArtifact    = channelui.BuildActionRuntimeArtifact
	requestArtifactProgress       = channelui.RequestArtifactProgress
	requestArtifactReviewHint     = channelui.RequestArtifactReviewHint
	normalizeRequestArtifactState = channelui.NormalizeRequestArtifactState
	actionArtifactSummary         = channelui.ActionArtifactSummary
	actionArtifactProgress        = channelui.ActionArtifactProgress
	actionArtifactResumeHint      = channelui.ActionArtifactResumeHint
	normalizeActionArtifactState  = channelui.NormalizeActionArtifactState
	latestArtifactTimestamp       = channelui.LatestArtifactTimestamp

	summarizeTaskLogRecord            = channelui.SummarizeTaskLogRecord
	buildTaskRuntimeArtifact          = channelui.BuildTaskRuntimeArtifact
	buildOrphanTaskLogRuntimeArtifact = channelui.BuildOrphanTaskLogRuntimeArtifact
	buildWorkflowRuntimeArtifact      = channelui.BuildWorkflowRuntimeArtifact
	buildTaskArtifactSummary          = channelui.BuildTaskArtifactSummary
	buildTaskArtifactProgress         = channelui.BuildTaskArtifactProgress
	buildTaskArtifactReviewHint       = channelui.BuildTaskArtifactReviewHint
	buildTaskArtifactResumeHint       = channelui.BuildTaskArtifactResumeHint
	normalizeTaskArtifactState        = channelui.NormalizeTaskArtifactState
	workflowArtifactProgress          = channelui.WorkflowArtifactProgress
	normalizeWorkflowArtifactState    = channelui.NormalizeWorkflowArtifactState
)

// Workspace readiness level consts.
const (
	workspaceReadinessReady   = channelui.WorkspaceReadinessReady
	workspaceReadinessWarn    = channelui.WorkspaceReadinessWarn
	workspaceReadinessPreview = channelui.WorkspaceReadinessPreview
)

// Doctor severity consts mirror channelui's exported names.
const (
	doctorOK   = channelui.DoctorOK
	doctorWarn = channelui.DoctorWarn
	doctorFail = channelui.DoctorFail
	doctorInfo = channelui.DoctorInfo
)

// Channel-confirm action typed-string consts.
const (
	confirmActionResetTeam     = channelui.ChannelConfirmActionResetTeam
	confirmActionResetDM       = channelui.ChannelConfirmActionResetDM
	confirmActionSwitchMode    = channelui.ChannelConfirmActionSwitchMode
	confirmActionRecoverFocus  = channelui.ChannelConfirmActionRecoverFocus
	confirmActionSubmitRequest = channelui.ChannelConfirmActionSubmitRequest
)

// Sidebar theme color constants.
const (
	sidebarBG      = channelui.SidebarBG
	sidebarMuted   = channelui.SidebarMuted
	sidebarDivider = channelui.SidebarDivider
	sidebarActive  = channelui.SidebarActive

	dotTalking  = channelui.DotTalking
	dotThinking = channelui.DotThinking
	dotCoding   = channelui.DotCoding
	dotIdle     = channelui.DotIdle
)

// Interview-phase typed-string consts.
const (
	interviewPhaseChoose = channelui.InterviewPhaseChoose
	interviewPhaseDraft  = channelui.InterviewPhaseDraft
	interviewPhaseReview = channelui.InterviewPhaseReview
)

// Calendar-range typed-string consts.
const (
	calendarRangeDay  = channelui.CalendarRangeDay
	calendarRangeWeek = channelui.CalendarRangeWeek
)

// Office-app constant aliases. Typed-string consts copy across packages
// while preserving type identity via the type alias above.
const (
	officeAppMessages  = channelui.OfficeAppMessages
	officeAppInbox     = channelui.OfficeAppInbox
	officeAppOutbox    = channelui.OfficeAppOutbox
	officeAppRecovery  = channelui.OfficeAppRecovery
	officeAppTasks     = channelui.OfficeAppTasks
	officeAppRequests  = channelui.OfficeAppRequests
	officeAppPolicies  = channelui.OfficeAppPolicies
	officeAppCalendar  = channelui.OfficeAppCalendar
	officeAppArtifacts = channelui.OfficeAppArtifacts
	officeAppSkills    = channelui.OfficeAppSkills
)
