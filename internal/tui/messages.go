package tui

import "time"

// Agent messages

type AgentTextMsg struct {
	AgentSlug string
	Text      string
}

type AgentDoneMsg struct {
	AgentSlug string
	Elapsed   time.Duration
}

type AgentErrorMsg struct {
	AgentSlug string
	Err       error
}

type AgentThinkingMsg struct {
	AgentSlug string
	Text      string
}

type AgentToolUseMsg struct {
	AgentSlug string
	ToolName  string
	ToolInput string
}

type AgentToolResultMsg struct {
	AgentSlug string
	Content   string
}

// API messages

type APIResultMsg struct {
	Data any
	Err  error
}

// Phase changes

type PhaseChangeMsg struct {
	AgentSlug string
	From      string
	To        string
}

// Slash command results

type NavTarget struct {
	View   string
	Params map[string]string
}

type SlashResultMsg struct {
	Output string
	Err    error
	Nav    *NavTarget
}

// UI interaction

type SpinnerTickMsg struct {
	Time time.Time
}

type PickerSelectMsg struct {
	Value string
	Label string
}

type ConfirmMsg struct {
	Confirmed bool
}

// Init flow

type InitFlowMsg struct {
	Phase string
	Data  map[string]string
}

type ProviderChoiceMsg struct {
	Provider string
}

type SetupApplyMsg struct {
	Notice string
	Err    error
}

// View switching

type ViewSwitchMsg struct {
	Target ViewName
}
