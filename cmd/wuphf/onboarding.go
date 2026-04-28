package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── Onboarding state ────────────────────────────────────────────

type onboardingStep int

const (
	stepWelcome onboardingStep = iota
	stepSetup
	stepTask
)

// simpleInput is a minimal single-line text input (no external bubbles dep).
type simpleInput struct {
	value    []rune
	pos      int
	width    int
	password bool
}

func newSimpleInput(width int) simpleInput {
	return simpleInput{width: width}
}

func newPasswordInput(width int) simpleInput {
	return simpleInput{width: width, password: true}
}

func (s *simpleInput) SetValue(v string) {
	s.value = []rune(v)
	s.pos = len(s.value)
}

func (s simpleInput) Value() string { return string(s.value) }

func (s *simpleInput) HandleKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if s.pos > 0 {
			s.value = append(s.value[:s.pos-1], s.value[s.pos:]...)
			s.pos--
		}
	case tea.KeyLeft:
		if s.pos > 0 {
			s.pos--
		}
	case tea.KeyRight:
		if s.pos < len(s.value) {
			s.pos++
		}
	case tea.KeyHome, tea.KeyCtrlA:
		s.pos = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		s.pos = len(s.value)
	case tea.KeyRunes:
		runes := []rune(msg.String())
		s.value = append(s.value[:s.pos], append(runes, s.value[s.pos:]...)...)
		s.pos += len(runes)
	}
}

func (s simpleInput) View(focused bool) string {
	w := s.width
	if w < 10 {
		w = 10
	}
	borderColor := slackInputBorder
	if focused {
		borderColor = slackInputFocus
	}

	var display string
	if s.password {
		masked := strings.Repeat("*", len(s.value))
		display = masked
	} else {
		display = string(s.value)
	}

	// Build cursor-in-string view, clipped to width-4.
	innerW := w - 4
	if innerW < 4 {
		innerW = 4
	}
	runes := []rune(display)
	cursorStyle := lipgloss.NewStyle().Reverse(true)

	var buf strings.Builder
	if len(runes) == 0 {
		buf.WriteString(cursorStyle.Render(" "))
	} else {
		// Determine visible window around cursor.
		start := 0
		if s.pos > innerW-1 {
			start = s.pos - (innerW - 1)
		}
		end := start + innerW
		if end > len(runes) {
			end = len(runes)
		}
		for i := start; i < end; i++ {
			if i == s.pos {
				buf.WriteString(cursorStyle.Render(string(runes[i])))
			} else {
				buf.WriteRune(runes[i])
			}
		}
		if s.pos == len(runes) {
			buf.WriteString(cursorStyle.Render(" "))
		}
	}

	return lipgloss.NewStyle().
		Width(w).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Background(lipgloss.Color("#17161C")).
		Padding(0, 1).
		Render(buf.String())
}

// ── HTTP message types ──────────────────────────────────────────

type prereqResult struct {
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	Found      bool   `json:"found"`
	Version    string `json:"version"`
	InstallURL string `json:"install_url"`
}

type taskTemplate struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	OwnerSlug   string `json:"owner_slug"`
}

type onboardingStateResp struct {
	Onboarded bool `json:"onboarded"`
}

// ── Bubbletea Msg types ─────────────────────────────────────────

type prereqsLoadedMsg struct{ results []prereqResult }
type keyValidatedMsg struct{ status string }
type templatesLoadedMsg struct{ templates []taskTemplate }
type completeMsg struct{ err error }
type onboardingProgressMsg struct{ err error }

// localRuntimeDetectedMsg lands when the off-loop runtime probe finishes.
// kind is empty when nothing was reachable; non-empty values are
// "ollama"/"mlx-lm"/"exo" plus their /v1 base URL.
type localRuntimeDetectedMsg struct {
	kind string
	addr string
}

// ── Model ───────────────────────────────────────────────────────

type onboardingModel struct {
	step   onboardingStep
	width  int
	height int

	// Step 1
	companyInput  simpleInput
	descInput     simpleInput
	priorityInput simpleInput
	focusIndex    int // 0=company, 1=desc, 2=priority

	// Step 2
	prereqs            []prereqResult
	anthropicKey       simpleInput
	openAIKey          simpleInput
	keyFocus           int    // 0=anthropic, 1=openai
	keyStatus          string // "idle", "checking", "valid", "invalid", "unverified"
	prereqsOk          bool
	continueUnverified bool

	// Step 3
	templates   []taskTemplate
	selectedTpl int // -1 = freeform
	taskInput   simpleInput

	// State
	brokerURL string
	err       string
	done      bool
}

func newOnboardingModel(brokerURL string, w, h int) onboardingModel {
	inputW := 40
	if w > 0 {
		inputW = w/2 - 4
		if inputW < 20 {
			inputW = 20
		}
		if inputW > 60 {
			inputW = 60
		}
	}

	m := onboardingModel{
		width:         w,
		height:        h,
		brokerURL:     brokerURL,
		companyInput:  newSimpleInput(inputW),
		descInput:     newSimpleInput(inputW),
		priorityInput: newSimpleInput(inputW),
		anthropicKey:  newPasswordInput(inputW),
		openAIKey:     newPasswordInput(inputW),
		keyStatus:     "idle",
		selectedTpl:   -1,
		taskInput:     newSimpleInput(inputW),
	}
	return m
}

func (m onboardingModel) Init() tea.Cmd {
	return nil
}

func (m onboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Recompute input widths.
		inputW := m.width/2 - 4
		if inputW < 20 {
			inputW = 20
		}
		if inputW > 60 {
			inputW = 60
		}
		m.companyInput.width = inputW
		m.descInput.width = inputW
		m.priorityInput.width = inputW
		m.anthropicKey.width = inputW
		m.openAIKey.width = inputW
		m.taskInput.width = inputW
		return m, nil

	case prereqsLoadedMsg:
		m.prereqs = msg.results
		m.prereqsOk = allRequiredPrereqsOk(msg.results)
		return m, nil

	case keyValidatedMsg:
		m.keyStatus = msg.status
		return m, nil

	case templatesLoadedMsg:
		m.templates = msg.templates
		return m, nil

	case completeMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.done = true
		return m, tea.Quit

	case onboardingProgressMsg:
		// progress saved, nothing to do
		return m, nil

	case localRuntimeDetectedMsg:
		// Replace the placeholder "Detecting…" message with either the
		// concrete --provider suggestion or the generic "paste a key"
		// fallback, depending on what the probe found.
		if msg.kind != "" {
			m.err = fmt.Sprintf("Detected %s running on %s. Exit with Ctrl+C and re-run: `wuphf --provider %s`. Or paste an Anthropic key below.", msg.kind, msg.addr, msg.kind)
		} else {
			m.err = "Paste an Anthropic API key (https://console.anthropic.com/settings/keys), or install Claude Code (https://claude.com/claude-code) and rerun — no key needed."
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m onboardingModel) handleKey(msg tea.KeyMsg) (onboardingModel, tea.Cmd) {
	// Global quit.
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch m.step {
	case stepWelcome:
		return m.handleWelcomeKey(msg)
	case stepSetup:
		return m.handleSetupKey(msg)
	case stepTask:
		return m.handleTaskKey(msg)
	}
	return m, nil
}

func (m onboardingModel) handleWelcomeKey(msg tea.KeyMsg) (onboardingModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab, tea.KeyDown:
		m.focusIndex = (m.focusIndex + 1) % 3
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.focusIndex = (m.focusIndex + 2) % 3
		return m, nil
	case tea.KeyEnter:
		// Validate step 1.
		if strings.TrimSpace(m.companyInput.Value()) == "" {
			m.err = "Company name is required."
			return m, nil
		}
		m.err = ""
		m.step = stepSetup
		// Submit progress and fetch prereqs.
		answers := map[string]interface{}{
			"company":  m.companyInput.Value(),
			"desc":     m.descInput.Value(),
			"priority": m.priorityInput.Value(),
		}
		return m, tea.Batch(
			m.submitProgressCmd("welcome", answers),
			m.fetchPrereqsCmd(),
			m.fetchTemplatesCmd(),
		)
	default:
		switch m.focusIndex {
		case 0:
			m.companyInput.HandleKey(msg)
		case 1:
			m.descInput.HandleKey(msg)
		case 2:
			m.priorityInput.HandleKey(msg)
		}
	}
	return m, nil
}

func (m onboardingModel) handleSetupKey(msg tea.KeyMsg) (onboardingModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab, tea.KeyDown:
		m.keyFocus = (m.keyFocus + 1) % 2
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.keyFocus = (m.keyFocus + 1) % 2
		return m, nil
	case tea.KeyCtrlR:
		// Re-check prereqs.
		return m, m.fetchPrereqsCmd()
	case tea.KeyEnter:
		// Must have prereqs ok OR continueUnverified.
		if !m.prereqsOk && !m.continueUnverified {
			m.err = "Required tools missing. Install them or press 'c' to continue anyway."
			return m, nil
		}
		// If a runtime CLI (claude, codex, cursor, windsurf) is installed, its
		// login handles provider auth — no raw API key is needed. Skip the
		// Anthropic key check entirely in that case.
		if hasInstalledRuntimeCLI(m.prereqs) {
			m.err = ""
			m.step = stepTask
			return m, nil
		}
		key := strings.TrimSpace(m.anthropicKey.Value())
		if key == "" {
			// Self-hosters often have ollama / mlx-lm / exo already running
			// locally. Detect that and tell them how to use it instead of
			// nagging for a cloud API key — saves the most-engaged segment of
			// PH visitors from a frustrating dead-end.
			//
			// The probe is dispatched as a tea.Cmd so the bubbletea Update
			// loop stays responsive — TCP-RST-on-firewalled-port can block
			// for the full per-candidate timeout, and freezing the TUI right
			// after the user pressed Enter is the worst possible UX moment.
			// Until the probe lands we show a hold message; the
			// localRuntimeDetectedMsg case overwrites it with either the
			// concrete suggestion or the generic fallback.
			m.err = "Checking for a local LLM runtime…"
			return m, detectLocalRuntimeCmd()
		}
		// If key not yet validated, validate now.
		if m.keyStatus == "idle" || m.keyStatus == "unverified" {
			m.keyStatus = "checking"
			return m, m.validateKeyCmd(key)
		}
		if m.keyStatus == "checking" {
			return m, nil
		}
		if m.keyStatus == "invalid" {
			m.err = "API key appears invalid. Double-check it and try again."
			return m, nil
		}
		// valid or unverified after c was pressed.
		m.err = ""
		m.step = stepTask
		return m, nil
	case tea.KeyRunes:
		if msg.String() == "c" && !m.prereqsOk {
			m.continueUnverified = true
			m.err = ""
			return m, nil
		}
		if msg.String() == "v" {
			key := strings.TrimSpace(m.anthropicKey.Value())
			if key != "" {
				m.keyStatus = "checking"
				return m, m.validateKeyCmd(key)
			}
		}
		// Route to focused input.
		switch m.keyFocus {
		case 0:
			m.anthropicKey.HandleKey(msg)
			m.keyStatus = "idle" // reset validation on edit
		case 1:
			m.openAIKey.HandleKey(msg)
		}
	default:
		switch m.keyFocus {
		case 0:
			m.anthropicKey.HandleKey(msg)
			m.keyStatus = "idle"
		case 1:
			m.openAIKey.HandleKey(msg)
		}
	}
	return m, nil
}

func (m onboardingModel) handleTaskKey(msg tea.KeyMsg) (onboardingModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyRunes:
		s := msg.String()
		// Number key: select template.
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			idx := int(s[0] - '1')
			if idx < len(m.templates) {
				m.selectedTpl = idx
				return m, nil
			}
		}
		if s == "s" {
			// Skip task.
			return m, m.completeOnboardingCmd("", true)
		}
		// Otherwise type into freeform input.
		m.selectedTpl = -1
		m.taskInput.HandleKey(msg)
		return m, nil
	case tea.KeyEnter:
		var taskText string
		if m.selectedTpl >= 0 && m.selectedTpl < len(m.templates) {
			taskText = m.templates[m.selectedTpl].Title
		} else {
			taskText = strings.TrimSpace(m.taskInput.Value())
		}
		if taskText == "" {
			// Skip.
			return m, m.completeOnboardingCmd("", true)
		}
		return m, m.completeOnboardingCmd(taskText, false)
	default:
		m.taskInput.HandleKey(msg)
	}
	return m, nil
}

// View renders the current step.
func (m onboardingModel) View() string {
	if m.done {
		return ""
	}
	w := m.width
	h := m.height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}

	bg := lipgloss.Color("#0D0D12")
	fg := lipgloss.Color("#E8E8EA")
	fullStyle := lipgloss.NewStyle().
		Width(w).Height(h).
		Background(bg).Foreground(fg)

	var content string
	switch m.step {
	case stepWelcome:
		content = m.viewWelcome(w, h)
	case stepSetup:
		content = m.viewSetup(w, h)
	case stepTask:
		content = m.viewTask(w, h)
	}

	return fullStyle.Render(content)
}

// ── Step views ──────────────────────────────────────────────────

func (m onboardingModel) viewWelcome(w, h int) string {
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EAB308")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E8EA")).Bold(true)

	var lines []string

	lines = append(lines, "")
	lines = append(lines, accentStyle.Render("  WUPHF — Let's set up your office"))
	lines = append(lines, mutedStyle.Render("  The cast is ready. We just need a few details."))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("  Company or project name"))
	lines = append(lines, "  "+m.companyInput.View(m.focusIndex == 0))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("  What do you do?"))
	lines = append(lines, "  "+m.descInput.View(m.focusIndex == 1))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("  Top priority right now"))
	lines = append(lines, "  "+m.priorityInput.View(m.focusIndex == 2))
	lines = append(lines, "")
	if m.err != "" {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(m.err))
	}
	lines = append(lines, mutedStyle.Render("  Tab to move between fields  ·  Enter to continue"))

	return centerBlock(lines, w, h)
}

func (m onboardingModel) viewSetup(w, h int) string {
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EAB308")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E8EA")).Bold(true)
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#2BAC76"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, accentStyle.Render("  Tools check"))
	lines = append(lines, mutedStyle.Render("  "+strings.Repeat("\u2500", maxInt(10, w/2-4))))

	if len(m.prereqs) == 0 {
		lines = append(lines, mutedStyle.Render("  Checking tools..."))
	} else {
		for _, p := range m.prereqs {
			var statusStr string
			if p.Found {
				ver := p.Version
				if ver != "" {
					ver = "  " + ver
				}
				statusStr = okStyle.Render("\u2713 "+p.Name) + dimStyle.Render(ver)
			} else {
				label := failStyle.Render("\u2717 " + p.Name)
				hint := ""
				if p.InstallURL != "" {
					hint = dimStyle.Render("  install at " + p.InstallURL)
				}
				req := ""
				if p.Required {
					req = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render(" (required)")
				}
				statusStr = label + req + hint
			}
			lines = append(lines, "  "+statusStr)
		}
	}

	lines = append(lines, "")
	lines = append(lines, accentStyle.Render("  Provider keys"))
	lines = append(lines, mutedStyle.Render("  "+strings.Repeat("\u2500", maxInt(10, w/2-4))))

	cliAuthActive := hasInstalledRuntimeCLI(m.prereqs)
	if cliAuthActive {
		lines = append(lines, mutedStyle.Render("  A runtime CLI is installed. Its login handles provider auth —"))
		lines = append(lines, mutedStyle.Render("  keys below are optional and only used as a fallback."))
		lines = append(lines, labelStyle.Render("  Anthropic (optional)"))
	} else {
		lines = append(lines, labelStyle.Render("  Anthropic (required)"))
	}

	keyStatusStr := ""
	switch m.keyStatus {
	case "checking":
		keyStatusStr = mutedStyle.Render("  checking…")
	case "valid":
		keyStatusStr = okStyle.Render("  \u2713 verified")
	case "invalid":
		keyStatusStr = failStyle.Render("  \u2717 invalid key")
	case "unverified":
		keyStatusStr = mutedStyle.Render("  unverified (continuing)")
	}
	keyLine := "  " + m.anthropicKey.View(m.keyFocus == 0)
	if keyStatusStr != "" {
		keyLine += keyStatusStr
	}
	lines = append(lines, keyLine)

	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("  OpenAI (optional)"))
	lines = append(lines, "  "+m.openAIKey.View(m.keyFocus == 1))

	lines = append(lines, "")

	// Re-check hint.
	lines = append(lines, mutedStyle.Render("  Ctrl+R re-check tools  ·  v validate key  ·  Enter continue"))

	readyMsg := ""
	if !m.prereqsOk && !m.continueUnverified && len(m.prereqs) > 0 {
		readyMsg = failStyle.Render("  Required tools missing — press c to continue anyway")
	} else if cliAuthActive {
		readyMsg = okStyle.Render("  Ready — Enter to continue (CLI login handles auth)")
	} else if m.keyStatus == "valid" || m.keyStatus == "unverified" || m.continueUnverified {
		readyMsg = okStyle.Render("  Ready — Enter to continue")
	}
	if readyMsg != "" {
		lines = append(lines, readyMsg)
	}

	if m.err != "" {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(m.err))
	}

	return centerBlock(lines, w, h)
}

func (m onboardingModel) viewTask(w, h int) string {
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EAB308")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E8EA")).Bold(true)
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#1264A3")).Bold(true).Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).Padding(0, 1)
	agentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, accentStyle.Render("  What's the first thing you want done?"))
	lines = append(lines, "")

	for i, tpl := range m.templates {
		num := fmt.Sprintf("[%d]", i+1)
		owner := ""
		if tpl.OwnerSlug != "" {
			owner = agentStyle.Render("  \u2192 " + tpl.OwnerSlug)
		}
		label := fmt.Sprintf("%s %s%s", num, tpl.Title, owner)
		if m.selectedTpl == i {
			lines = append(lines, "  "+activeStyle.Render(label))
		} else {
			lines = append(lines, "  "+inactiveStyle.Render(label))
		}
	}

	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("  Or describe it yourself:"))
	lines = append(lines, "  "+m.taskInput.View(m.selectedTpl == -1))
	lines = append(lines, "")

	if m.err != "" {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(m.err))
	}
	lines = append(lines, mutedStyle.Render("  s to skip  ·  Enter to start working"))

	return centerBlock(lines, w, h)
}

// ── Helpers ─────────────────────────────────────────────────────

func centerBlock(lines []string, w, h int) string {
	topPad := (h - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	var out []string
	for i := 0; i < topPad; i++ {
		out = append(out, "")
	}
	out = append(out, lines...)
	return strings.Join(out, "\n")
}

func allRequiredPrereqsOk(prereqs []prereqResult) bool {
	for _, p := range prereqs {
		if p.Required && !p.Found {
			return false
		}
	}
	return true
}

// localRuntimeCandidates lists the supported local OpenAI-compatible runtimes
// in priority order. The order is the user-facing tiebreak when more than one
// is reachable: ollama wins because it's the most common install, mlx-lm
// second because it's Apple-Silicon-only, exo last because it's niche.
var localRuntimeCandidates = []struct {
	kind string
	base string
}{
	{kind: "ollama", base: "http://127.0.0.1:11434/v1"},
	{kind: "mlx-lm", base: "http://127.0.0.1:8080/v1"},
	{kind: "exo", base: "http://127.0.0.1:52415/v1"},
}

// probeLocalRuntime checks a single OpenAI-compatible /v1/models endpoint
// and returns true iff the response is a 2xx with an OpenAI-shaped body —
// `{"object":"list", "data":<array>}`. The shape check is load-bearing
// because port 8080 is shared with countless dev servers, so a 200 OK on
// a path-permissive server (Spring Boot, Tomcat, Docker images) would
// otherwise produce a bad "--provider mlx-lm" suggestion that fails
// opaquely on use.
//
// The `data` key must be present (not missing, not null) but MAY be an
// empty array. Freshly-installed ollama (`ollama serve` before any
// `ollama pull`) returns `{"object":"list","data":[]}` and is a real
// runtime — rejecting it would silently break the most common ollama
// state and is worse than the exotic case of an unrelated server that
// happens to return `data:[]`. We use a pointer to []RawMessage so we
// can distinguish "key missing or null" (Data == nil) from "key
// present, empty array" (*Data has length 0).
func probeLocalRuntime(ctx context.Context, client *http.Client, base string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	var probe struct {
		Object string             `json:"object"`
		Data   *[]json.RawMessage `json:"data"`
	}
	// Cap the read so a misconfigured server can't stream us megabytes of
	// HTML. Real /v1/models bodies are < 4KB even with dozens of models.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return false
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Object == "list" && probe.Data != nil
}

// detectReachableLocalRuntime probes the canonical loopback endpoints for the
// supported local OpenAI-compatible runtimes (ollama, mlx-lm, exo) in priority
// order and returns the first one that answers with an OpenAI-shaped body.
// Used by the onboarding wizard to suggest a concrete --provider flag when
// the user has no Anthropic key and no runtime CLI installed but does have a
// local LLM running.
//
// Sequential (not parallel) so the suggestion is deterministic when more than
// one runtime is up. A 250ms-per-candidate timeout keeps the worst case at
// 750ms total when nothing is reachable; the caller dispatches this off the
// bubbletea Update loop so the TUI stays responsive.
func detectReachableLocalRuntime() (kind, addr string) {
	client := &http.Client{Timeout: 250 * time.Millisecond}
	for _, c := range localRuntimeCandidates {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		ok := probeLocalRuntime(ctx, client, c.base)
		cancel()
		if ok {
			return c.kind, c.base
		}
	}
	return "", ""
}

// hasInstalledRuntimeCLI reports whether at least one runtime CLI (claude,
// codex, cursor, windsurf) was detected on PATH. When true, the CLI handles
// provider auth via its own login and the user does not need to paste a raw
// API key.
func hasInstalledRuntimeCLI(prereqs []prereqResult) bool {
	for _, p := range prereqs {
		switch p.Name {
		case "claude", "codex", "opencode", "cursor", "windsurf":
			if p.Found {
				return true
			}
		}
	}
	return false
}

// ── Commands (HTTP calls) ────────────────────────────────────────

func (m onboardingModel) fetchPrereqsCmd() tea.Cmd {
	url := m.brokerURL + "/onboarding/prereqs"
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			return prereqsLoadedMsg{results: []prereqResult{
				{Name: "git", Required: true, Found: false},
				{Name: "node", Required: false, Found: false},
				{Name: "claude", Required: false, Found: false, InstallURL: "claude.ai/code"},
			}}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return prereqsLoadedMsg{results: defaultPrereqs()}
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return prereqsLoadedMsg{results: defaultPrereqs()}
		}
		var out struct {
			Prereqs []prereqResult `json:"prereqs"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			// Try flat array.
			var flat []prereqResult
			if err2 := json.Unmarshal(body, &flat); err2 != nil {
				return prereqsLoadedMsg{results: defaultPrereqs()}
			}
			return prereqsLoadedMsg{results: flat}
		}
		return prereqsLoadedMsg{results: out.Prereqs}
	}
}

func defaultPrereqs() []prereqResult {
	return []prereqResult{
		{Name: "node", Required: true, Found: false, InstallURL: "https://nodejs.org"},
		{Name: "git", Required: true, Found: false, InstallURL: "https://git-scm.com"},
		{Name: "claude", Required: false, Found: false, InstallURL: "https://claude.ai/code"},
		{Name: "codex", Required: false, Found: false, InstallURL: "https://github.com/openai/codex"},
		{Name: "opencode", Required: false, Found: false, InstallURL: "https://opencode.ai"},
	}
}

// detectLocalRuntimeCmd runs detectReachableLocalRuntime off the bubbletea
// Update loop and posts a localRuntimeDetectedMsg back to it. Worst-case
// runtime is bounded by the loop in detectReachableLocalRuntime (250ms per
// candidate × 3 candidates = 750ms), so the TUI never blocks.
func detectLocalRuntimeCmd() tea.Cmd {
	return func() tea.Msg {
		kind, addr := detectReachableLocalRuntime()
		return localRuntimeDetectedMsg{kind: kind, addr: addr}
	}
}

func (m onboardingModel) validateKeyCmd(key string) tea.Cmd {
	url := m.brokerURL + "/onboarding/validate-key"
	return func() tea.Msg {
		payload, _ := json.Marshal(map[string]string{"key": key, "provider": "anthropic"})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return keyValidatedMsg{status: "unverified"}
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return keyValidatedMsg{status: "unverified"}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out struct {
			Status string `json:"status"`
			Valid  bool   `json:"valid"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return keyValidatedMsg{status: "unverified"}
		}
		if out.Status != "" {
			return keyValidatedMsg{status: out.Status}
		}
		if out.Valid {
			return keyValidatedMsg{status: "valid"}
		}
		return keyValidatedMsg{status: "invalid"}
	}
}

func (m onboardingModel) fetchTemplatesCmd() tea.Cmd {
	url := m.brokerURL + "/onboarding/templates"
	return func() tea.Msg {
		req, err := newBrokerRequest(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			return templatesLoadedMsg{templates: defaultTemplates()}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return templatesLoadedMsg{templates: defaultTemplates()}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out struct {
			Templates []taskTemplate `json:"templates"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			var flat []taskTemplate
			if err2 := json.Unmarshal(body, &flat); err2 != nil {
				return templatesLoadedMsg{templates: defaultTemplates()}
			}
			return templatesLoadedMsg{templates: flat}
		}
		if len(out.Templates) > 0 {
			return templatesLoadedMsg{templates: out.Templates}
		}
		return templatesLoadedMsg{templates: defaultTemplates()}
	}
}

func defaultTemplates() []taskTemplate {
	return []taskTemplate{
		{ID: "landing-page", Title: "Draft the landing page", OwnerSlug: "executor"},
		{ID: "repo-structure", Title: "Set up repo structure", OwnerSlug: "executor"},
		{ID: "product-spec", Title: "Write the product spec", OwnerSlug: "planner"},
		{ID: "readme", Title: "Write the README", OwnerSlug: "planner"},
		{ID: "competitive-audit", Title: "Audit the competition", OwnerSlug: "ceo"},
	}
}

func (m onboardingModel) submitProgressCmd(step string, answers map[string]interface{}) tea.Cmd {
	url := m.brokerURL + "/onboarding/progress"
	return func() tea.Msg {
		payload, _ := json.Marshal(map[string]interface{}{
			"step":    step,
			"answers": answers,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return onboardingProgressMsg{err: err}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return onboardingProgressMsg{err: err}
		}
		resp.Body.Close()
		return onboardingProgressMsg{}
	}
}

func (m onboardingModel) completeOnboardingCmd(task string, skipTask bool) tea.Cmd {
	url := m.brokerURL + "/onboarding/complete"
	return func() tea.Msg {
		payload, _ := json.Marshal(map[string]interface{}{
			"first_task": task,
			"skip_task":  skipTask,
		})
		req, err := newBrokerRequest(context.Background(), http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			// If broker isn't up yet, treat as complete (graceful).
			return completeMsg{err: nil}
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			// Broker not yet running — still complete gracefully.
			return completeMsg{err: nil}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return completeMsg{err: fmt.Errorf("broker error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
		}
		return completeMsg{err: nil}
	}
}

// ── Onboarding state check (used by runChannelView) ─────────────

type onboardingState struct {
	Onboarded bool `json:"onboarded"`
}

func fetchOnboardingState(brokerURL string) (onboardingState, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := newBrokerRequest(context.Background(), http.MethodGet, brokerURL+"/onboarding/state", nil)
	if err != nil {
		return onboardingState{Onboarded: true}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		// Broker not running — assume onboarded to not block startup.
		return onboardingState{Onboarded: true}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// Endpoint doesn't exist yet — treat as onboarded.
		return onboardingState{Onboarded: true}, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return onboardingState{Onboarded: true}, err
	}
	var s onboardingState
	if err := json.Unmarshal(body, &s); err != nil {
		return onboardingState{Onboarded: true}, err
	}
	return s, nil
}

// ── Sidebar onboarding checklist ─────────────────────────────────

type onboardingChecklistItem struct {
	Label string
	Done  bool
}

type onboardingChecklist struct {
	Items     []onboardingChecklistItem
	Dismissed bool
}

// renderOnboardingChecklist renders the "Getting started" section for the sidebar.
// Returns empty string if checklist should not be shown.
func renderOnboardingChecklist(checklist onboardingChecklist, width int) string {
	if checklist.Dismissed {
		return ""
	}
	done := 0
	remaining := false
	for _, item := range checklist.Items {
		if item.Done {
			done++
		} else {
			remaining = true
		}
	}
	if !remaining {
		return ""
	}
	total := len(checklist.Items)

	bg := lipgloss.Color(sidebarBG)
	sectionBandStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D4D4D8")).
		Background(lipgloss.Color("#20242A")).
		Bold(true).
		Padding(0, 1)
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#2BAC76"))
	todoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sidebarMuted))
	panel := lipgloss.NewStyle().Background(bg)

	header := fmt.Sprintf("Getting started  %d/%d", done, total)
	headerLine := sidebarStyledRow(sectionBandStyle, header, width)

	innerW := width - 2
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sidebarDivider))
	divider := sidebarPlainRow(dividerStyle.Render(strings.Repeat("\u2500", maxInt(1, innerW))), width)

	var rows []string
	rows = append(rows, headerLine)
	rows = append(rows, divider)
	for _, item := range checklist.Items {
		var marker, label string
		if item.Done {
			marker = doneStyle.Render("[x]")
			label = doneStyle.Render(item.Label)
		} else {
			marker = todoStyle.Render("[ ]")
			label = todoStyle.Render(item.Label)
		}
		text := marker + " " + label
		// Pad to width.
		visW := ansi.StringWidth(text)
		if visW < innerW-1 {
			text += strings.Repeat(" ", innerW-1-visW)
		}
		rows = append(rows, panel.Render(" "+text))
	}
	rows = append(rows, "")

	return strings.Join(rows, "\n")
}
