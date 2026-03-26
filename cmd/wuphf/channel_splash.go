package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/nex-crm/wuphf/internal/company"
)

type splashTickMsg time.Time
type splashDoneMsg struct{}

type splashModel struct {
	members []company.MemberSpec
	width   int
	height  int
	frame   int
	phase   int
	shown   int
	bells   int
	startAt time.Time
}

func newSplashModel() splashModel {
	manifest := company.DefaultManifest()
	loaded, err := company.LoadManifest()
	if err == nil && len(loaded.Members) > 0 {
		manifest = loaded
	}
	return splashModel{
		members: manifest.Members,
		startAt: time.Now(),
	}
}

func splashTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return splashTickMsg(t) })
}

func (m splashModel) Init() tea.Cmd { return splashTick() }

func (m splashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m, func() tea.Msg { return splashDoneMsg{} }
	case splashTickMsg:
		m.frame++
		elapsed := time.Since(m.startAt)
		switch {
		case elapsed < 200*time.Millisecond:
			m.phase = 0
			m.shown = 0
		case elapsed < time.Duration(len(m.members))*350*time.Millisecond+200*time.Millisecond:
			m.phase = 0
			m.shown = int((elapsed - 200*time.Millisecond) / (350 * time.Millisecond))
			if m.shown > len(m.members) {
				m.shown = len(m.members)
			}
			if m.shown > m.bells && m.shown <= len(m.members) {
				m.bells = m.shown
				return m, tea.Batch(splashTick(), func() tea.Msg {
					fmt.Print("\a")
					return nil
				})
			}
		case elapsed < time.Duration(len(m.members))*350*time.Millisecond+1200*time.Millisecond:
			m.phase = 1
		case elapsed < time.Duration(len(m.members))*350*time.Millisecond+2500*time.Millisecond:
			m.phase = 2
		default:
			m.phase = 3
			return m, func() tea.Msg { return splashDoneMsg{} }
		}
		return m, splashTick()
	case splashDoneMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m splashModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	fullStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Background(lipgloss.Color("#0D0D12")).
		Foreground(lipgloss.Color("#E8E8EA"))
	if m.phase == 3 {
		return fullStyle.Render("")
	}
	content := ""
	switch m.phase {
	case 0, 1:
		content = m.renderCast()
	case 2:
		content = m.renderTitle()
	}
	return fullStyle.Render(content)
}

func (m splashModel) renderCast() string {
	if len(m.members) == 0 {
		return ""
	}
	count := m.shown
	if count > len(m.members) {
		count = len(m.members)
	}
	if count < 1 {
		return ""
	}

	const slotW = 16
	const spacing = 2

	type avatarBlock struct {
		lines []string
		name  string
	}

	blocks := make([]avatarBlock, 0, count)
	maxAvatarH := 0
	for i := 0; i < count; i++ {
		member := m.members[i]
		seed := member.Name
		if seed == "" {
			seed = member.Slug
		}
		lines := renderWuphfSplashAvatar(seed, member.Slug, m.frame)
		if len(lines) > maxAvatarH {
			maxAvatarH = len(lines)
		}
		name := member.Name
		if name == "" {
			name = member.Slug
		}
		blocks = append(blocks, avatarBlock{lines: lines, name: name})
	}

	totalW := count*(slotW+spacing) - spacing
	leftPad := (m.width - totalW) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	var lines []string
	topPad := (m.height - maxAvatarH - 4) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}

	for row := 0; row < maxAvatarH; row++ {
		var parts []string
		for _, block := range blocks {
			offset := maxAvatarH - len(block.lines)
			rendered := strings.Repeat(" ", slotW)
			if row >= offset {
				line := block.lines[row-offset]
				if ansi.StringWidth(line) < slotW {
					line += strings.Repeat(" ", slotW-ansi.StringWidth(line))
				}
				rendered = line
			}
			parts = append(parts, rendered)
		}
		lines = append(lines, strings.Repeat(" ", leftPad)+strings.Join(parts, strings.Repeat(" ", spacing)))
	}

	lines = append(lines, "")
	var nameParts []string
	for i, block := range blocks {
		name := truncateLabel(block.name, slotW)
		padL := (slotW - len([]rune(name))) / 2
		padR := slotW - len([]rune(name)) - padL
		if padL < 0 {
			padL = 0
		}
		if padR < 0 {
			padR = 0
		}
		label := strings.Repeat(" ", padL) + name + strings.Repeat(" ", padR)
		agentColor := sidebarAgentColors[m.members[i].Slug]
		if agentColor == "" {
			agentColor = "#64748B"
		}
		nameParts = append(nameParts, lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Bold(true).Render(label))
	}
	lines = append(lines, strings.Repeat(" ", leftPad)+strings.Join(nameParts, strings.Repeat(" ", spacing)))
	return strings.Join(lines, "\n")
}

func (m splashModel) renderTitle() string {
	title := []string{
		"██╗    ██╗██╗   ██╗██████╗ ██╗  ██╗███████╗",
		"██║    ██║██║   ██║██╔══██╗██║  ██║██╔════╝",
		"██║ █╗ ██║██║   ██║██████╔╝███████║█████╗  ",
		"██║███╗██║██║   ██║██╔═══╝ ██╔══██║██╔══╝  ",
		"╚███╔███╔╝╚██████╔╝██║     ██║  ██║██║     ",
		" ╚══╝╚══╝  ╚═════╝ ╚═╝     ╚═╝  ╚═╝╚═╝     ",
	}
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EAB308")).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7A7E")).Italic(true)
	titleW := 0
	for _, l := range title {
		if len(l) > titleW {
			titleW = len(l)
		}
	}
	var lines []string
	topPad := (m.height - len(title) - 4) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}
	for _, l := range title {
		pad := (m.width - titleW) / 2
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, strings.Repeat(" ", pad)+titleStyle.Render(l))
	}
	subtitle := "Somehow still operational."
	pad := (m.width - len(subtitle)) / 2
	if pad < 0 {
		pad = 0
	}
	lines = append(lines, "")
	lines = append(lines, strings.Repeat(" ", pad)+subtitleStyle.Render(subtitle))
	if m.frame%8 == 0 {
		fmt.Print("\a")
	}
	return strings.Join(lines, "\n")
}
