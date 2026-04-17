package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
	"github.com/nex-crm/wuphf/internal/operations"
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
	InitIdle             InitPhase = "idle"
	InitAPIKey           InitPhase = "api_key"
	InitProviderChoice   InitPhase = "provider_choice" // kept for backward compat, skipped in flow
	InitOneAPIKey        InitPhase = "one_api_key"     // kept for backward compat, skipped in flow
	InitMemoryChoice     InitPhase = "memory_choice"
	InitGBrainOpenAIKey  InitPhase = "gbrain_openai_key"
	InitGBrainAnthropKey InitPhase = "gbrain_anthropic_key"
	InitNexRegister      InitPhase = "nex_register"
	InitBlueprintChoice  InitPhase = "blueprint_choice"
	InitPackChoice       InitPhase = "pack_choice" // legacy alias
	InitDone             InitPhase = "done"
)

// InitFlowModel is the state machine for the /init onboarding flow.
type InitFlowModel struct {
	phase     InitPhase
	apiKey    string
	provider  string
	memory    string
	blueprint string

	// Text input buffer for key / email entry
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

// Start begins the init flow.
// Order: API key → provider choice → memory choice → blueprint choice → done.
// Memory comes before blueprint because it's a higher-level architectural
// decision (where org knowledge lives) that the blueprint will then act on top of.
func (f InitFlowModel) Start() (InitFlowModel, tea.Cmd) {
	f.apiKey = strings.TrimSpace(config.ResolveAPIKey(""))
	f.provider = config.ResolveLLMProvider("")
	f.memory = config.ResolveMemoryBackend("")
	if cfg, err := config.Load(); err == nil {
		f.blueprint = cfg.ActiveBlueprint()
	}
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
		if v, ok := m.Data["memory"]; ok {
			f.memory = v
		}
		if v, ok := m.Data["blueprint"]; ok {
			f.blueprint = v
		}
		if v, ok := m.Data["pack"]; ok && strings.TrimSpace(f.blueprint) == "" {
			f.blueprint = v
		}

	case PickerSelectMsg:
		switch f.phase {
		case InitProviderChoice:
			f.provider = m.Value
			f.phase = InitMemoryChoice
			return f, f.emitPhase(InitMemoryChoice)
		case InitMemoryChoice:
			f.memory = m.Value
			return f.advanceAfterMemoryChoice()
		case InitBlueprintChoice, InitPackChoice:
			f.blueprint = m.Value
			return f.finish()
		}

	case tea.KeyMsg:
		if f.requiresTextInput() {
			return f.updateTextInput(m)
		}
	}
	return f, nil
}

// advanceAfterMemoryChoice transitions to the right key/registration phase
// based on which memory backend the user chose, or straight to blueprint
// if no additional setup is needed.
func (f InitFlowModel) advanceAfterMemoryChoice() (InitFlowModel, tea.Cmd) {
	switch f.memory {
	case config.MemoryBackendGBrain:
		// GBrain requires an OpenAI key for embeddings.
		if config.ResolveOpenAIAPIKey() == "" {
			f.phase = InitGBrainOpenAIKey
			return f, f.emitPhase(InitGBrainOpenAIKey)
		}
		// Key already configured, skip to blueprint.
		f.phase = InitBlueprintChoice
		return f, f.emitPhase(InitBlueprintChoice)
	case config.MemoryBackendNex:
		// Nex requires a Nex identity. If no API key is configured, prompt
		// the user to register via email.
		if config.ResolveAPIKey("") == "" {
			f.phase = InitNexRegister
			return f, f.emitPhase(InitNexRegister)
		}
		f.phase = InitBlueprintChoice
		return f, f.emitPhase(InitBlueprintChoice)
	default:
		f.phase = InitBlueprintChoice
		return f, f.emitPhase(InitBlueprintChoice)
	}
}

func (f InitFlowModel) requiresTextInput() bool {
	switch f.phase {
	case InitAPIKey, InitGBrainOpenAIKey, InitGBrainAnthropKey, InitNexRegister:
		return true
	}
	return false
}

// updateTextInput handles keystrokes during any text-entry phase
// (API key, GBrain OpenAI/Anthropic keys, Nex email registration).
func (f InitFlowModel) updateTextInput(msg tea.KeyMsg) (InitFlowModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := strings.TrimSpace(string(f.keyInput))
		return f.submitTextInput(value)
	case "backspace":
		if len(f.keyInput) > 0 {
			f.keyInput = f.keyInput[:len(f.keyInput)-1]
			f.keyError = ""
		}
		return f, nil
	case "esc":
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

func (f InitFlowModel) submitTextInput(value string) (InitFlowModel, tea.Cmd) {
	switch f.phase {
	case InitAPIKey:
		if value == "" {
			f.keyError = "API key cannot be empty."
			return f, nil
		}
		f.apiKey = value
		f.keyError = ""
		f.keyInput = nil
		if strings.TrimSpace(f.provider) == "" {
			f.provider = "claude-code"
		}
		f.phase = InitProviderChoice
		return f, f.emitPhase(InitProviderChoice)

	case InitGBrainOpenAIKey:
		if value == "" {
			f.keyError = "OpenAI API key is required for GBrain."
			return f, nil
		}
		// Persist immediately so gbrain can use it.
		cfg, _ := config.Load()
		cfg.OpenAIAPIKey = value
		_ = config.Save(cfg)
		f.keyError = ""
		f.keyInput = nil
		// Optional: ask for Anthropic key too.
		if config.ResolveAnthropicAPIKey() == "" {
			f.phase = InitGBrainAnthropKey
			return f, f.emitPhase(InitGBrainAnthropKey)
		}
		f.phase = InitBlueprintChoice
		return f, f.emitPhase(InitBlueprintChoice)

	case InitGBrainAnthropKey:
		// Anthropic key is optional; empty means skip.
		if value != "" {
			cfg, _ := config.Load()
			cfg.AnthropicAPIKey = value
			_ = config.Save(cfg)
		}
		f.keyError = ""
		f.keyInput = nil
		f.phase = InitBlueprintChoice
		return f, f.emitPhase(InitBlueprintChoice)

	case InitNexRegister:
		if value == "" {
			f.keyError = "Email is required to register with Nex."
			return f, nil
		}
		// Shell out to nex-cli register synchronously. The TUI will block
		// briefly (nex-cli should be fast), then proceed.
		_, err := nex.Register(context.Background(), value)
		if err != nil {
			f.keyError = "Registration failed: " + err.Error()
			return f, nil
		}
		f.keyError = ""
		f.keyInput = nil
		// Reload API key since register should have written it.
		f.apiKey = strings.TrimSpace(config.ResolveAPIKey(""))
		f.phase = InitBlueprintChoice
		return f, f.emitPhase(InitBlueprintChoice)
	}
	return f, nil
}

// finish saves config and transitions to done.
func (f InitFlowModel) finish() (InitFlowModel, tea.Cmd) {
	cfg, _ := config.Load()
	if f.apiKey != "" {
		cfg.APIKey = f.apiKey
	}
	cfg.LLMProvider = f.provider
	if normalized := config.NormalizeMemoryBackend(f.memory); normalized != "" {
		cfg.MemoryBackend = normalized
	}
	if strings.TrimSpace(f.blueprint) != "" {
		cfg.SetActiveBlueprint(f.blueprint)
	}

	_ = config.Save(cfg)

	f.phase = InitDone
	return f, f.emitPhase(InitDone)
}

// emitPhase returns a tea.Cmd that emits an InitFlowMsg for the given phase.
func (f InitFlowModel) emitPhase(phase InitPhase) tea.Cmd {
	data := map[string]string{
		"api_key":   f.apiKey,
		"provider":  f.provider,
		"memory":    f.memory,
		"blueprint": f.blueprint,
		"pack":      f.blueprint,
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

// MemoryOptions returns the picker options for organizational memory backend
// selection. Order matches the recommended default-first, then opt-out.
func MemoryOptions() []PickerOption {
	return []PickerOption{
		{
			Label:       "Nex (recommended)",
			Value:       config.MemoryBackendNex,
			Description: "Hosted org memory: entity briefs, shared notes, and search backed by your Nex identity.",
		},
		{
			Label:       "GBrain",
			Value:       config.MemoryBackendGBrain,
			Description: "Local-first knowledge graph CLI. Good when you want everything on your machine.",
		},
		{
			Label:       "No shared memory",
			Value:       config.MemoryBackendNone,
			Description: "Skip the memory layer. Agents only know what's in the current conversation.",
		},
	}
}

// BlueprintOptions returns the picker options for operation blueprint selection.
func BlueprintOptions() []PickerOption {
	if repoRoot := resolveInitRepoRoot(); repoRoot != "" {
		if blueprints, err := operations.ListBlueprints(repoRoot); err == nil && len(blueprints) > 0 {
			options := make([]PickerOption, len(blueprints))
			for i, bp := range blueprints {
				label := bp.Name
				if i == 0 {
					label += " (default)"
				}
				desc := strings.TrimSpace(bp.Description)
				if desc == "" {
					desc = strings.TrimSpace(bp.Objective)
				}
				options[i] = PickerOption{
					Label:       label,
					Value:       bp.ID,
					Description: desc,
				}
			}
			return options
		}
	}
	return legacyPackOptions()
}

// PackOptions is a legacy alias retained for compatibility with older callers.
func PackOptions() []PickerOption { return BlueprintOptions() }

func legacyPackOptions() []PickerOption {
	packs := agent.ListLegacyPacks()
	options := make([]PickerOption, len(packs))
	for i, p := range packs {
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

// renderAPIKeyInput renders the text input for the current text-entry phase.
func (f InitFlowModel) renderAPIKeyInput() string {
	input := string(f.keyInput)
	cursorStyle := lipgloss.NewStyle().Reverse(true)

	var label string
	switch f.phase {
	case InitGBrainOpenAIKey:
		label = "OpenAI Key: "
	case InitGBrainAnthropKey:
		label = "Anthropic Key (Enter to skip): "
	case InitNexRegister:
		label = "Email: "
	default:
		label = "API Key: "
	}
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
	memory := strings.TrimSpace(f.memory)
	if memory == "" {
		memory = config.ResolveMemoryBackend("")
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
			Label:  "Memory backend",
			Status: memoryReadinessStatus(memory),
			Detail: memoryReadinessDetail(memory),
		},
		{
			Label:  "Operation template",
			Status: blueprintReadinessStatus(f.blueprint),
			Detail: blueprintReadinessDetail(f.blueprint),
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

func blueprintReadinessStatus(blueprint string) string {
	if strings.TrimSpace(blueprint) == "" {
		return "next"
	}
	return "ready"
}

func blueprintReadinessDetail(blueprint string) string {
	if name := blueprintDisplayName(strings.TrimSpace(blueprint)); name != "" {
		if strings.TrimSpace(blueprint) != "" {
			return "Selected " + name + "."
		}
	}
	return "Choose which operation template or blueprint should open after setup."
}

func memoryReadinessStatus(backend string) string {
	switch config.NormalizeMemoryBackend(backend) {
	case config.MemoryBackendNex, config.MemoryBackendGBrain:
		return "ready"
	case config.MemoryBackendNone:
		return "next"
	default:
		return "next"
	}
}

func memoryReadinessDetail(backend string) string {
	switch config.NormalizeMemoryBackend(backend) {
	case config.MemoryBackendNex:
		return "Hosted org memory via Nex."
	case config.MemoryBackendGBrain:
		return "Local knowledge graph via GBrain CLI."
	case config.MemoryBackendNone:
		return "No shared memory. Agents only know what's in the current conversation."
	default:
		return "Pick a memory backend so the office can remember what it learns."
	}
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
	case InitMemoryChoice:
		return "Choose Memory Backend", "Where should the office remember what it learns? Nex is hosted org memory, GBrain is a local knowledge graph, or skip for no shared memory."
	case InitGBrainOpenAIKey:
		return "Enter OpenAI API Key", "GBrain uses OpenAI for embeddings. Paste your OpenAI API key (starts with sk-)."
	case InitGBrainAnthropKey:
		return "Enter Anthropic API Key (optional)", "GBrain can optionally use Anthropic for reasoning. Press Enter to skip, or paste your key."
	case InitNexRegister:
		return "Register with Nex", "Enter your email to create or connect your Nex identity. This enables shared memory, entity briefs, and integrations."
	case InitBlueprintChoice, InitPackChoice:
		return "Choose Operation Template", "Select the blueprint or template that will seed your startup."
	case InitDone:
		blueprintName := blueprintDisplayName(f.blueprint)
		memoryName := config.MemoryBackendLabel(f.memory)
		return "Setup Complete", "Provider: " + f.provider + " | Memory: " + memoryName + " | Blueprint: " + blueprintName + ". " + config.OneSetupBlurb()
	default:
		return "Setup", "Run /init to begin."
	}
}

func blueprintDisplayName(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if repoRoot := resolveInitRepoRoot(); repoRoot != "" {
		if blueprint, err := operations.LoadBlueprint(repoRoot, id); err == nil {
			if name := strings.TrimSpace(blueprint.Name); name != "" {
				return name
			}
		}
	}
	return id
}

func resolveInitRepoRoot() string {
	current, err := os.Getwd()
	if err != nil {
		return ""
	}
	current = filepath.Clean(current)
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		if _, err := os.Stat(filepath.Join(current, "templates")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}
