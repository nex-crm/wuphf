package tui

import (
	"os"
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
	f.apiKey = strings.TrimSpace(initFlowResolvedMemoryKey())
	f.provider = config.ResolveLLMProvider("")
	if initFlowNeedsMemoryKey(f.apiKey) {
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
		key := strings.TrimSpace(string(f.keyInput))
		if key == "" {
			f.keyError = initFlowEmptyKeyError()
			return f, nil
		}
		if err := initFlowValidateMemoryKey(key); err != "" {
			f.keyError = err
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
	switch config.ResolveMemoryBackend("") {
	case config.MemoryBackendNex:
		if f.apiKey != "" {
			cfg.APIKey = f.apiKey
		}
	case config.MemoryBackendGBrain:
		if key := strings.TrimSpace(f.apiKey); key != "" {
			if strings.HasPrefix(strings.ToLower(key), "sk-ant-") {
				cfg.AnthropicAPIKey = key
			} else {
				cfg.OpenAIAPIKey = key
			}
		}
	default:
	}
	if selected := config.NormalizeMemoryBackend(os.Getenv("WUPHF_MEMORY_BACKEND")); selected != "" {
		cfg.MemoryBackend = selected
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
	label := initFlowAPIKeyLabel()
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
	backend := config.ResolveMemoryBackend("")
	effectiveAPIKey := strings.TrimSpace(f.apiKey)
	if effectiveAPIKey == "" {
		effectiveAPIKey = strings.TrimSpace(initFlowResolvedMemoryKey())
	}
	provider := strings.TrimSpace(f.provider)
	if provider == "" {
		provider = "claude-code"
	}

	checks := []initReadinessCheck{
		{
			Label:  "Memory backend",
			Status: initFlowMemoryBackendStatus(backend, effectiveAPIKey),
			Detail: initFlowMemoryBackendDetail(backend, effectiveAPIKey),
		},
		{
			Label:  initFlowMemoryCredentialLabel(backend),
			Status: initFlowMemoryCredentialStatus(backend, effectiveAPIKey),
			Detail: initFlowMemoryCredentialDetail(backend, effectiveAPIKey),
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
			Status: initFlowIntegrationsStatus(backend),
			Detail: initFlowIntegrationsDetail(backend),
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
	backend := config.ResolveMemoryBackend("")
	switch f.phase {
	case InitIdle:
		return "Setup", "Run /init to begin."
	case InitAPIKey:
		switch backend {
		case config.MemoryBackendNex:
			return "Enter Nex API Key", "Paste your WUPHF/Nex API key. Nex memory and managed integrations both depend on it."
		case config.MemoryBackendGBrain:
			return "Enter GBrain Provider Key", "Paste an OpenAI or Anthropic API key for GBrain. OpenAI is the full path: embeddings and vector search depend on it. Anthropic alone works in reduced mode."
		default:
			return "Setup", "External memory is disabled for this run, so no memory key is required."
		}
	case InitProviderChoice:
		switch backend {
		case config.MemoryBackendNex:
			return "Choose LLM Provider", "Select your preferred AI provider. Nex memory uses your WUPHF/Nex API key, and WUPHF-managed integrations remain Nex-backed."
		case config.MemoryBackendGBrain:
			return "Choose LLM Provider", "Select your preferred AI provider. GBrain uses the provider key you configured separately here. OpenAI unlocks embeddings and vector search; Anthropic alone is reduced mode."
		default:
			return "Choose LLM Provider", "Select your preferred AI provider. External memory is disabled for this run."
		}
	case InitPackChoice:
		return "Choose Agent Pack", "Select the team of agents to work with."
	case InitDone:
		packName := f.pack
		if p := agent.GetPack(f.pack); p != nil {
			packName = p.Name
		}
		summary := "Memory: " + config.MemoryBackendLabel(backend) + " | Provider: " + f.provider + " | Pack: " + packName + "."
		switch backend {
		case config.MemoryBackendNex:
			return "Setup Complete", summary + " " + config.OneSetupBlurb()
		case config.MemoryBackendGBrain:
			if strings.TrimSpace(config.ResolveOpenAIAPIKey()) != "" {
				return "Setup Complete", summary + " GBrain will use your OpenAI key for embeddings and vector search."
			}
			return "Setup Complete", summary + " GBrain is configured in reduced mode. Add an OpenAI key if you want embeddings and vector search."
		default:
			return "Setup Complete", summary + " External memory is disabled for this run."
		}
	default:
		return "Setup", "Run /init to begin."
	}
}

func initFlowSelectedMemoryBackend() string {
	return config.ResolveMemoryBackend("")
}

func initFlowResolvedMemoryKey() string {
	switch initFlowSelectedMemoryBackend() {
	case config.MemoryBackendNex:
		return strings.TrimSpace(config.ResolveAPIKey(""))
	case config.MemoryBackendGBrain:
		if key := strings.TrimSpace(config.ResolveOpenAIAPIKey()); key != "" {
			return key
		}
		return strings.TrimSpace(config.ResolveAnthropicAPIKey())
	default:
		return ""
	}
}

func initFlowNeedsMemoryKey(current string) bool {
	switch initFlowSelectedMemoryBackend() {
	case config.MemoryBackendNex, config.MemoryBackendGBrain:
		return strings.TrimSpace(current) == ""
	default:
		return false
	}
}

func initFlowAPIKeyLabel() string {
	switch initFlowSelectedMemoryBackend() {
	case config.MemoryBackendNex:
		return "Nex API Key: "
	case config.MemoryBackendGBrain:
		return "Provider Key: "
	default:
		return "API Key: "
	}
}

func initFlowEmptyKeyError() string {
	switch initFlowSelectedMemoryBackend() {
	case config.MemoryBackendNex:
		return "Nex API key cannot be empty."
	case config.MemoryBackendGBrain:
		return "Enter an OpenAI or Anthropic API key for GBrain. OpenAI is required for embeddings and vector search."
	default:
		return "API key cannot be empty."
	}
}

func initFlowValidateMemoryKey(key string) string {
	if initFlowSelectedMemoryBackend() != config.MemoryBackendGBrain {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(lower, "sk-ant-") || strings.HasPrefix(lower, "sk-") {
		return ""
	}
	return "Paste an OpenAI key (sk-...) or an Anthropic key (sk-ant-...)."
}

func initFlowMemoryBackendStatus(backend, key string) string {
	if backend == config.MemoryBackendNone {
		return "ready"
	}
	return readinessStatusForBool(strings.TrimSpace(key) != "")
}

func initFlowMemoryBackendDetail(backend, key string) string {
	switch backend {
	case config.MemoryBackendNex:
		if strings.TrimSpace(key) != "" {
			return "Nex selected. WUPHF/Nex API key is configured."
		}
		return "Nex selected. WUPHF/Nex API key is required."
	case config.MemoryBackendGBrain:
		if strings.TrimSpace(key) != "" {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "sk-ant-") {
				return "GBrain selected. Anthropic-only mode is configured; embeddings and vector search still require OpenAI."
			}
			return "GBrain selected. OpenAI is configured, including embeddings and vector search."
		}
		return "GBrain selected. Configure a provider key before GBrain can run. OpenAI is required for embeddings and vector search."
	default:
		return "Local-only selected. No external memory key is required."
	}
}

func initFlowMemoryCredentialLabel(backend string) string {
	switch backend {
	case config.MemoryBackendNex:
		return "Nex API key"
	case config.MemoryBackendGBrain:
		return "GBrain provider key"
	default:
		return "Memory credentials"
	}
}

func initFlowMemoryCredentialStatus(backend, key string) string {
	if backend == config.MemoryBackendNone {
		return "ready"
	}
	return readinessStatusForBool(strings.TrimSpace(key) != "")
}

func initFlowMemoryCredentialDetail(backend, key string) string {
	switch backend {
	case config.MemoryBackendNex:
		if strings.TrimSpace(key) != "" {
			return "Configured for Nex-backed context and managed integrations."
		}
		return "Paste your WUPHF/Nex API key to enable Nex-backed context."
	case config.MemoryBackendGBrain:
		if strings.TrimSpace(key) != "" {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "sk-ant-") {
				return "Anthropic key configured for GBrain reduced mode. Add OpenAI for embeddings and vector search."
			}
			return "OpenAI key configured for GBrain, including embeddings and vector search."
		}
		return "Paste an OpenAI or Anthropic API key before using GBrain. OpenAI is required for embeddings and vector search."
	default:
		return "No external memory credentials required."
	}
}

func initFlowIntegrationsStatus(backend string) string {
	if backend == config.MemoryBackendNex {
		return "ready"
	}
	return "next"
}

func initFlowIntegrationsDetail(backend string) string {
	switch backend {
	case config.MemoryBackendNex:
		return config.OneSetupSummary()
	case config.MemoryBackendGBrain:
		return "WUPHF-managed integrations currently require the Nex memory backend and a WUPHF/Nex API key."
	default:
		return "Managed integrations are off in local-only mode. Select Nex if you want WUPHF-managed integration setup."
	}
}
