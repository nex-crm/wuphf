package tui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
)

// InitPhase represents a step in the onboarding flow.
type InitPhase string

const (
	InitIdle           InitPhase = "idle"
	InitAPIKey         InitPhase = "api_key"
	InitProviderChoice InitPhase = "provider_choice" // kept for backward compat, skipped in flow
	InitOneAPIKey      InitPhase = "one_api_key"     // kept for backward compat, skipped in flow
	InitPackChoice     InitPhase = "pack_choice"     // kept for backward compat, skipped in flow
	InitDone           InitPhase = "done"
)

// InitFlowModel is the state machine for the /init onboarding flow.
type InitFlowModel struct {
	phase    InitPhase
	apiKey   string
	provider string
	pack     string

	// Text input buffer for API key entry
	keyInput []rune
	keyError string
}

// NewInitFlow creates an idle InitFlowModel.
func NewInitFlow() InitFlowModel {
	return InitFlowModel{phase: InitIdle}
}

// Phase returns the current phase.
func (f InitFlowModel) Phase() InitPhase {
	return f.phase
}

// IsActive returns true if the flow is in progress (not idle and not done).
func (f InitFlowModel) IsActive() bool {
	return f.phase != InitIdle && f.phase != InitDone
}

// Start begins the init flow. API key → pack choice → done.
// Provider is always claude-code.
func (f InitFlowModel) Start() (InitFlowModel, tea.Cmd) {
	cfg, _ := config.Load()
	if cfg.APIKey != "" {
		f.apiKey = cfg.APIKey
	}
	f.provider = "claude-code"
	if f.apiKey == "" {
		f.phase = InitAPIKey
		return f, f.emitPhase(InitAPIKey)
	}
	// Already have API key — skip to pack choice
	f.phase = InitPackChoice
	return f, f.emitPhase(InitPackChoice)
}

// Update advances the flow based on incoming messages.
func (f InitFlowModel) Update(msg tea.Msg) (InitFlowModel, tea.Cmd) {
	switch m := msg.(type) {
	case InitFlowMsg:
		f.phase = InitPhase(m.Phase)
		if v, ok := m.Data["api_key"]; ok {
			f.apiKey = v
		}
		if v, ok := m.Data["provider"]; ok {
			f.provider = v
		}
		if v, ok := m.Data["pack"]; ok {
			f.pack = v
		}

	case PickerSelectMsg:
		switch f.phase {
		case InitProviderChoice:
			f.provider = m.Value
			f.phase = InitPackChoice
			return f, f.emitPhase(InitPackChoice)
		case InitPackChoice:
			f.pack = m.Value
			return f.finish()
		}

	case tea.KeyMsg:
		if f.requiresTextInput() {
			return f.updateAPIKeyInput(m)
		}
	}
	return f, nil
}

func (f InitFlowModel) requiresTextInput() bool {
	return f.phase == InitAPIKey
}

// updateAPIKeyInput handles keystrokes during the API key entry phase.
func (f InitFlowModel) updateAPIKeyInput(msg tea.KeyMsg) (InitFlowModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		key := string(f.keyInput)
		if strings.TrimSpace(key) == "" {
			f.keyError = "API key cannot be empty."
			return f, nil
		}
		f.apiKey = key
		f.keyError = ""
		f.keyInput = nil
		f.provider = "claude-code"
		f.phase = InitPackChoice
		return f, f.emitPhase(InitPackChoice)
	case "backspace":
		if len(f.keyInput) > 0 {
			f.keyInput = f.keyInput[:len(f.keyInput)-1]
			f.keyError = ""
		}
		return f, nil
	case "esc":
		// Cancel the flow
		f.phase = InitIdle
		f.keyInput = nil
		f.keyError = ""
		return f, nil
	default:
		runes := []rune(msg.String())
		if len(runes) == 1 && runes[0] >= 32 {
			f.keyInput = append(f.keyInput, runes[0])
			f.keyError = ""
		}
		return f, nil
	}
}

// finish saves config and transitions to done.
func (f InitFlowModel) finish() (InitFlowModel, tea.Cmd) {
	cfg, _ := config.Load()
	if f.apiKey != "" {
		cfg.APIKey = f.apiKey
	}
	cfg.LLMProvider = f.provider
	cfg.Pack = f.pack

	// Resolve team lead slug from pack
	if p := agent.GetPack(f.pack); p != nil {
		cfg.TeamLeadSlug = p.LeadSlug
	}

	_ = config.Save(cfg)

	f.phase = InitDone
	return f, f.emitPhase(InitDone)
}

// emitPhase returns a tea.Cmd that emits an InitFlowMsg for the given phase.
func (f InitFlowModel) emitPhase(phase InitPhase) tea.Cmd {
	data := map[string]string{
		"api_key":  f.apiKey,
		"provider": f.provider,
		"pack":     f.pack,
	}
	return func() tea.Msg {
		return InitFlowMsg{Phase: string(phase), Data: data}
	}
}

// ProviderOptions returns the picker options for LLM provider selection.
func ProviderOptions() []PickerOption {
	claudeDesc := "Claude via claude CLI (recommended)"
	if _, err := exec.LookPath("claude"); err != nil {
		claudeDesc = "Claude via claude CLI (not found in PATH!)"
	}
	options := []PickerOption{
		{Label: "Claude Code (default)", Value: "claude-code", Description: claudeDesc},
		{Label: "Gemini", Value: "gemini", Description: "Google Gemini via API key"},
	}
	if !config.ResolveNoNex() {
		options = append(options, PickerOption{Label: "Nex Ask", Value: "nex-ask", Description: "Nex hosted AI (uses WUPHF_API_KEY)"})
	}
	return options
}

// PackOptions returns the picker options for agent pack selection.
func PackOptions() []PickerOption {
	options := make([]PickerOption, len(agent.Packs))
	for i, p := range agent.Packs {
		label := p.Name
		if i == 0 {
			label += " (default)"
		}
		options[i] = PickerOption{
			Label:       label,
			Value:       p.Slug,
			Description: p.Description,
		}
	}
	return options
}

// View renders the current phase and instructions.
func (f InitFlowModel) View() string {
	heading, instructions := f.phaseText()
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(NexPurple))
	muteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor))

	view := labelStyle.Render(heading) + "\n" + muteStyle.Render(instructions)

	if f.requiresTextInput() {
		view += "\n" + f.renderAPIKeyInput()
	}

	return view
}

// renderAPIKeyInput renders the text input for API key entry.
func (f InitFlowModel) renderAPIKeyInput() string {
	input := string(f.keyInput)
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	label := "API Key: "
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(NexBlue)).Bold(true).Render(label)

	display := prompt + input + cursorStyle.Render(" ")

	if f.keyError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(Error))
		display += "\n" + errStyle.Render(f.keyError)
	}

	return display
}

func (f InitFlowModel) phaseText() (heading, instructions string) {
	switch f.phase {
	case InitIdle:
		return "Setup", "Run /init to begin."
	case InitAPIKey:
		return "Enter Nex API Key", "Paste your WUPHF/Nex API key. WUPHF uses One for integrations and manages it automatically through your Nex identity."
	case InitProviderChoice:
		return "Choose LLM Provider", "Select your preferred AI provider. Integrations are handled automatically through Nex using One."
	case InitPackChoice:
		return "Choose Agent Pack", "Select the team of agents to work with."
	case InitDone:
		packName := f.pack
		if p := agent.GetPack(f.pack); p != nil {
			packName = p.Name
		}
		return "Setup Complete", "Provider: " + f.provider + " | Pack: " + packName + ". " + config.OneSetupBlurb()
	default:
		return "Setup", "Run /init to begin."
	}
}
