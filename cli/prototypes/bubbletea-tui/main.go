package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Stream JSON types ──────────────────────────────────────────────

type streamEvent struct {
	Type    string        `json:"type"`
	Subtype string        `json:"subtype,omitempty"`
	Message *assistantMsg `json:"message,omitempty"`
	Result  string        `json:"result,omitempty"`
}

type assistantMsg struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ── Bubbletea messages ─────────────────────────────────────────────

// agentTextMsg replaces the current partial text (full snapshot, not delta)
type agentTextMsg struct{ text string }
type agentDoneMsg struct{ elapsed time.Duration }
type agentErrorMsg struct{ err error }

// ── Styles ─────────────────────────────────────────────────────────

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7C3AED")).
			Padding(0, 1)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60A5FA")).
			Bold(true)

	agentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")).
			Padding(1, 2).
			Width(26)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7C3AED")).
				Padding(0, 1)
)

// ── Model ──────────────────────────────────────────────────────────

type chatLine struct {
	role string // "user", "agent", "system"
	text string
}

type model struct {
	input   textinput.Model
	lines   []chatLine
	thinking bool
	partial string            // accumulates streaming text
	stream  chan tea.Msg       // receives chunks from goroutine
	width   int
	height  int
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type a message... (/help, /quit)"
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 80

	return model{
		input: ti,
		lines: []chatLine{
			{role: "system", text: "Welcome to Nex TUI prototype. Type a message to chat with Claude."},
		},
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// waitForStream returns a Cmd that reads one message from the stream channel.
func waitForStream(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 34
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.input.SetValue("")

			switch text {
			case "/quit":
				return m, tea.Quit
			case "/help":
				m.lines = append(m.lines, chatLine{role: "system", text: "Commands: /help, /quit. Type anything else to chat with Claude."})
				return m, nil
			}

			// Start agent
			m.lines = append(m.lines, chatLine{role: "user", text: text})
			m.thinking = true
			m.partial = ""
			ch := make(chan tea.Msg, 64)
			m.stream = ch
			go runClaude(text, ch)
			return m, waitForStream(ch)
		}

	case agentTextMsg:
		m.partial = msg.text // replace — each event is a full snapshot
		return m, waitForStream(m.stream)

	case agentDoneMsg:
		m.thinking = false
		response := strings.TrimSpace(m.partial)
		if response == "" {
			response = "(empty response)"
		}
		m.lines = append(m.lines, chatLine{role: "agent", text: response})
		m.lines = append(m.lines, chatLine{role: "system", text: fmt.Sprintf("Response in %s", msg.elapsed.Round(time.Millisecond))})
		m.partial = ""
		m.stream = nil
		return m, nil

	case agentErrorMsg:
		m.thinking = false
		m.lines = append(m.lines, chatLine{role: "system", text: "Error: " + msg.err.Error()})
		m.partial = ""
		m.stream = nil
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// ── Sidebar ──
	agentStatus := "idle"
	statusDot := "○"
	if m.thinking {
		agentStatus = "thinking..."
		statusDot = "●"
	}
	sidebar := sidebarStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Agents"),
			"",
			fmt.Sprintf(" %s Team Lead  %s", statusDot, dimStyle.Render(agentStatus)),
		),
	)

	// ── Chat area ──
	chatWidth := m.width - 28
	if chatWidth < 40 {
		chatWidth = 40
	}

	var chatLines []string
	for _, l := range m.lines {
		switch l.role {
		case "user":
			chatLines = append(chatLines, userStyle.Render("You: ")+l.text)
		case "agent":
			// Wrap long agent responses
			wrapped := wrapText(l.text, chatWidth-10)
			first := true
			for _, wl := range strings.Split(wrapped, "\n") {
				if first {
					chatLines = append(chatLines, agentStyle.Render("Claude: ")+wl)
					first = false
				} else {
					chatLines = append(chatLines, "        "+wl)
				}
			}
		case "system":
			chatLines = append(chatLines, dimStyle.Render("  "+l.text))
		}
	}

	// Show streaming partial text
	if m.thinking && m.partial != "" {
		wrapped := wrapText(strings.TrimSpace(m.partial), chatWidth-10)
		wLines := strings.Split(wrapped, "\n")
		// Show last few lines of streaming text
		maxPreview := 6
		if len(wLines) > maxPreview {
			wLines = wLines[len(wLines)-maxPreview:]
		}
		for i, wl := range wLines {
			if i == 0 {
				chatLines = append(chatLines, agentStyle.Render("Claude: ")+dimStyle.Render(wl))
			} else {
				chatLines = append(chatLines, dimStyle.Render("        "+wl))
			}
		}
		chatLines = append(chatLines, dimStyle.Render("        _"))
	} else if m.thinking {
		chatLines = append(chatLines, dimStyle.Render("  thinking..."))
	}

	// Calculate available height
	inputHeight := 3
	titleHeight := 2
	availableHeight := m.height - inputHeight - titleHeight - 2
	if availableHeight < 3 {
		availableHeight = 3
	}

	// Scroll: show last N lines
	if len(chatLines) > availableHeight {
		chatLines = chatLines[len(chatLines)-availableHeight:]
	}
	for len(chatLines) < availableHeight {
		chatLines = append(chatLines, "")
	}

	chatArea := lipgloss.NewStyle().Width(chatWidth).Render(strings.Join(chatLines, "\n"))

	// ── Title bar ──
	title := titleStyle.Render("Nex TUI -- Bubbletea Prototype")

	// ── Input ──
	inputBox := inputBorderStyle.Width(chatWidth - 4).Render(m.input.View())

	// ── Layout ──
	mainCol := lipgloss.JoinVertical(lipgloss.Left, title, "", chatArea, inputBox)
	full := lipgloss.JoinHorizontal(lipgloss.Top, mainCol, " ", sidebar)

	return full
}

// ── Claude spawner (goroutine) ─────────────────────────────────────

func runClaude(prompt string, ch chan tea.Msg) {
	defer close(ch)
	start := time.Now()

	cmd := exec.Command("claude",
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "5",
		"--no-session-persistence",
		"--include-partial-messages",
	)

	// Strip CLAUDECODE env vars to avoid nesting issues
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key := strings.SplitN(e, "=", 2)[0]
		switch key {
		case "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SESSION", "CLAUDE_CODE_PARENT_SESSION":
			continue
		default:
			filtered = append(filtered, e)
		}
	}
	cmd.Env = filtered

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ch <- agentErrorMsg{err: fmt.Errorf("stdout pipe: %w", err)}
		return
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		ch <- agentErrorMsg{err: fmt.Errorf("start claude: %w", err)}
		return
	}

	var finalText string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "assistant":
			if ev.Message != nil {
				// Each partial message is a full snapshot of text so far
				var sb strings.Builder
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						sb.WriteString(block.Text)
					}
				}
				if sb.Len() > 0 {
					finalText = sb.String()
					ch <- agentTextMsg{text: finalText}
				}
			}
		case "result":
			if ev.Result != "" {
				finalText = ev.Result
				ch <- agentTextMsg{text: finalText}
			}
		}
	}

	_ = cmd.Wait()
	elapsed := time.Since(start)
	ch <- agentDoneMsg{elapsed: elapsed}
}

// ── Helpers ────────────────────────────────────────────────────────

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > width {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return strings.Join(lines, "\n")
}

// ── Main ───────────────────────────────────────────────────────────

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
