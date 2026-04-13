package tui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
)

var initFlowLookPathFn = exec.LookPath

type initReadinessCheck struct {
	Label  string
	Status string
	Detail string
}

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

// Start begins the init flow. API key → provider choice → pack choice → done.
func (f InitFlowModel) Start() (InitFlowModel, tea.Cmd) {
	f.apiKey = strings.TrimSpace(config.ResolveAPIKey(""))
	f.provider = config.ResolveLLMProvider("")
	if f.apiKey == "" {
		f.phase = InitAPIKey
		return f, f.emitPhase(InitAPIKey)
	}
	f.phase = InitProviderChoice
	return f, f.emitPhase(InitProviderChoice)
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
		if strings.TrimSpace(f.provider) == "" {
			f.provider = "claude-code"
		}
		f.phase = InitProviderChoice
		return f, f.emitPhase(InitProviderChoice)
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
	if _, err := initFlowLookPathFn("claude"); err != nil {
		claudeDesc = "Claude via claude CLI (not found in PATH!)"
	}
	codexDesc := "Codex via codex CLI"
	if _, err := initFlowLookPathFn("codex"); err != nil {
		codexDesc = "Codex via codex CLI (not found in PATH!)"
	}
	options := []PickerOption{
		{Label: "Claude Code (default)", Value: "claude-code", Description: claudeDesc},
		{Label: "Codex CLI", Value: "codex", Description: codexDesc},
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

	if readiness := f.renderReadinessSummary(); readiness != "" {
		view += "\n\n" + readiness
	}

	if f.requiresTextInput() {
		view += "\n\n" + f.renderAPIKeyInput()
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

func (f InitFlowModel) renderReadinessSummary() string {
	checks := f.readinessChecks()
	if len(checks) == 0 {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(NexBlue))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor))

	lines := []string{
		titleStyle.Render("Setup Readiness"),
		mutedStyle.Render("Use /doctor for the full capability report."),
	}
	for _, check := range checks {
		lines = append(lines, f.renderReadinessCheck(check))
	}
	return strings.Join(lines, "\n")
}

func (f InitFlowModel) renderReadinessCheck(check initReadinessCheck) string {
	statusStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E5E7EB")).
		Background(lipgloss.Color(readinessStatusColor(check.Status))).
		Padding(0, 1)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ValueColor))
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(MutedColor))

	return statusStyle.Render(strings.ToUpper(check.Status)) + " " +
		labelStyle.Render(check.Label) + " " +
		detailStyle.Render(check.Detail)
}

func readinessStatusColor(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready":
		return "#166534"
	case "next":
		return "#1D4ED8"
	default:
		return "#991B1B"
	}
}

func (f InitFlowModel) readinessChecks() []initReadinessCheck {
	effectiveAPIKey := strings.TrimSpace(f.apiKey)
	if effectiveAPIKey == "" {
		effectiveAPIKey = strings.TrimSpace(config.ResolveAPIKey(""))
	}
	provider := strings.TrimSpace(f.provider)
	if provider == "" {
		provider = "claude-code"
	}

	checks := []initReadinessCheck{
		{
			Label:  "Nex identity",
			Status: readinessStatusForBool(effectiveAPIKey != ""),
			Detail: apiKeyReadinessDetail(effectiveAPIKey != ""),
		},
		{
			Label:  "tmux office runtime",
			Status: readinessStatusForBool(binaryAvailable("tmux")),
			Detail: binaryReadinessDetail("tmux", "WUPHF can open the office panes.", "Install tmux before launching the office."),
		},
		{
			Label:  "LLM runtime",
			Status: providerRuntimeStatus(provider),
			Detail: providerRuntimeDetail(provider),
		},
		{
			Label:  "Agent pack",
			Status: packReadinessStatus(f.pack),
			Detail: packReadinessDetail(f.pack),
		},
		{
			Label:  "Integrations",
			Status: "ready",
			Detail: config.OneSetupSummary(),
		},
	}

	// Provider API key readiness
	providerKeys := []struct {
		label   string
		resolve func() string
	}{
		{"Gemini API key", config.ResolveGeminiAPIKey},
		{"Anthropic API key", config.ResolveAnthropicAPIKey},
		{"OpenAI API key", config.ResolveOpenAIAPIKey},
		{"Minimax API key", config.ResolveMinimaxAPIKey},
	}
	for _, pk := range providerKeys {
		set := pk.resolve() != ""
		detail := "Not configured. Set via /config set or env var."
		if set {
			detail = "Configured."
		}
		checks = append(checks, initReadinessCheck{
			Label:  pk.label,
			Status: readinessStatusForOptional(set),
			Detail: detail,
		})
	}

	return checks
}

func readinessStatusForBool(ok bool) string {
	if ok {
		return "ready"
	}
	return "missing"
}

func readinessStatusForOptional(set bool) string {
	if set {
		return "ready"
	}
	return "next"
}

func apiKeyReadinessDetail(ok bool) string {
	if ok {
		return "WUPHF/Nex API key is configured."
	}
	return "Paste your WUPHF/Nex API key to enable memory and managed integrations."
}

func packReadinessStatus(pack string) string {
	if strings.TrimSpace(pack) == "" {
		return "next"
	}
	return "ready"
}

func packReadinessDetail(pack string) string {
	if p := agent.GetPack(strings.TrimSpace(pack)); p != nil {
		return "Selected " + p.Name + "."
	}
	if strings.TrimSpace(pack) != "" {
		return "Selected " + pack + "."
	}
	return "Choose which team should open after setup."
}

func providerRuntimeStatus(provider string) string {
	switch strings.TrimSpace(provider) {
	case "", "claude-code":
		return readinessStatusForBool(binaryAvailable("claude"))
	case "codex":
		return readinessStatusForBool(binaryAvailable("codex"))
	default:
		return "ready"
	}
}

func providerRuntimeDetail(provider string) string {
	switch strings.TrimSpace(provider) {
	case "", "claude-code":
		return binaryReadinessDetail("claude", "Claude CLI is ready for teammate sessions.", "Install claude or pick another provider.")
	case "codex":
		return binaryReadinessDetail("codex", "Codex CLI is ready for teammate sessions.", "Install codex or pick another provider.")
	case "gemini":
		return "Gemini uses an API key. No local CLI is required."
	case "nex-ask":
		return "Managed through Nex. WUPHF will route requests through your Nex identity."
	default:
		return provider + " is selected."
	}
}

func binaryReadinessDetail(name, readyDetail, missingDetail string) string {
	if binaryAvailable(name) {
		return readyDetail
	}
	return missingDetail
}

func binaryAvailable(name string) bool {
	_, err := initFlowLookPathFn(name)
	return err == nil
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
