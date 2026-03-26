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

// ── Splash portraits (6 lines tall) ───────────────────────────────
//
// The splash should read like a clean cast photo, not a stack of ASCII limbs.
// Each teammate gets the same portrait card shape with small role-specific
// trait differences. Fixed slot width keeps the lineup tidy.

type bigSprite struct {
	Lines [6]string // 6 lines tall, each 9 chars wide
}

func bigSpriteForSlug(slug string, pose int) bigSprite {
	accent := map[string]string{
		"ceo":      "⌐",
		"pm":       "□",
		"fe":       "=",
		"be":       "#",
		"ai":       "*",
		"designer": "~",
		"cmo":      "!",
		"cro":      "$",
		"nex":      "◎",
	}
	eyes := map[string]string{
		"ceo":      "■ ■",
		"ai":       "◉ ◉",
		"nex":      "◉ ◉",
		"designer": "◕ ◕",
		"cmo":      "✶ ✶",
	}
	prop := map[string]string{
		"ceo":      "$",
		"pm":       "]",
		"fe":       "=",
		"be":       "#",
		"ai":       "*",
		"designer": "~",
		"cmo":      "!",
		"cro":      "%",
		"nex":      "o",
	}
	mouths := [4]string{" ‿ ", " o ", " ▿ ", " ᴗ "}

	roleAccent := accent[slug]
	if roleAccent == "" {
		roleAccent = "•"
	}
	eyeLine := eyes[slug]
	if eyeLine == "" {
		eyeLine = "• •"
	}
	propGlyph := prop[slug]
	if propGlyph == "" {
		propGlyph = "·"
	}
	p := pose % len(mouths)
	return bigSprite{
		Lines: [6]string{
			"╭───────╮",
			splashPortraitLine("  " + roleAccent + " " + roleAccent + "  "),
			splashPortraitLine("  " + eyeLine + "  "),
			splashPortraitLine("  " + mouths[p] + "  "),
			splashPortraitLine("   " + propGlyph + "   "),
			"╰───────╯",
		},
	}
}

func splashPortraitLine(inner string) string {
	runes := []rune(inner)
	if len(runes) > 7 {
		inner = string(runes[:7])
	} else if len(runes) < 7 {
		inner = inner + strings.Repeat(" ", 7-len(runes))
	}
	return "│" + inner + "│"
}

// ── Splash model ──────────────────────────────────────────────────

type splashTickMsg time.Time
type splashDoneMsg struct{}

type splashModel struct {
	members []company.MemberSpec
	width   int
	height  int
	frame   int    // animation frame counter
	phase   int    // 0=building, 1=full cast, 2=title, 3=done
	shown   int    // how many characters revealed so far
	bells   int    // bell counter
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
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

func (m splashModel) Init() tea.Cmd {
	return splashTick()
}

func (m splashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Any key skips the splash
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
			// Bell on each new character reveal
			if m.shown > m.bells && m.shown <= len(m.members) {
				m.bells = m.shown
				return m, tea.Batch(splashTick(), func() tea.Msg {
					fmt.Print("\a") // terminal bell
					return nil
				})
			}
		case elapsed < time.Duration(len(m.members))*350*time.Millisecond+1200*time.Millisecond:
			m.phase = 1 // full cast visible
		case elapsed < time.Duration(len(m.members))*350*time.Millisecond+2500*time.Millisecond:
			m.phase = 2 // title card
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

	bg := lipgloss.Color("#0D0D12")
	fullStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Background(bg).
		Foreground(lipgloss.Color("#E8E8EA"))

	if m.phase == 3 {
		return fullStyle.Render("")
	}

	var content string

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

	// Calculate sprite width and spacing
	spriteW := 9
	spacing := 2
	totalW := count*(spriteW+spacing) - spacing
	if totalW <= 0 {
		totalW = 1
	}

	// Center horizontally
	leftPad := (m.width - totalW) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	// Build each character's big sprite
	var spriteColumns []bigSprite
	for i := 0; i < count; i++ {
		member := m.members[i]
		// Assign a different pose to each character
		pose := i % 4
		spriteColumns = append(spriteColumns, bigSpriteForSlug(member.Slug, pose))
	}

	// Render sprite lines (8 lines tall)
	spriteHeight := 6
	var lines []string

	// Vertical centering: put sprites in the middle of the screen
	topPad := (m.height - spriteHeight - 4) / 2 // -4 for labels + spacing
	if topPad < 0 {
		topPad = 0
	}
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}

	// Render each sprite line across all characters
	for row := 0; row < spriteHeight; row++ {
		var lineParts []string
		for i, sp := range spriteColumns {
			agentColor := sidebarAgentColors[m.members[i].Slug]
			if agentColor == "" {
				agentColor = "#64748B"
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor))

			// Entrance animation: reveal whole portraits, not disembodied rows.
			visibleAge := m.shown - i
			if visibleAge <= 0 {
				lineParts = append(lineParts, strings.Repeat(" ", spriteW))
				continue
			}
			rendered := style.Render(sp.Lines[row])
			w := ansi.StringWidth(rendered)
			if w < spriteW {
				rendered += strings.Repeat(" ", spriteW-w)
			}
			lineParts = append(lineParts, rendered)
		}
		line := strings.Repeat(" ", leftPad) + strings.Join(lineParts, strings.Repeat(" ", spacing))
		lines = append(lines, line)
	}

	// Name labels under each character
	lines = append(lines, "") // spacing
	var nameParts []string
	for i := 0; i < count; i++ {
		member := m.members[i]
		agentColor := sidebarAgentColors[member.Slug]
		if agentColor == "" {
			agentColor = "#64748B"
		}
		name := member.Name
		if name == "" {
			name = member.Slug
		}
		// Center name under sprite
		if len(name) > spriteW {
			name = name[:spriteW-1] + "\u2026"
		}
		padL := (spriteW - len(name)) / 2
		padR := spriteW - len(name) - padL
		if padL < 0 {
			padL = 0
		}
		if padR < 0 {
			padR = 0
		}
		label := strings.Repeat(" ", padL) + name + strings.Repeat(" ", padR)
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(agentColor)).
			Bold(true)
		nameParts = append(nameParts, style.Render(label))
	}
	lines = append(lines, strings.Repeat(" ", leftPad)+strings.Join(nameParts, strings.Repeat(" ", spacing)))

	return strings.Join(lines, "\n")
}

func (m splashModel) renderTitle() string {
	// Big WUPHF title
	title := []string{
		"██╗    ██╗██╗   ██╗██████╗ ██╗  ██╗███████╗",
		"██║    ██║██║   ██║██╔══██╗██║  ██║██╔════╝",
		"██║ █╗ ██║██║   ██║██████╔╝███████║█████╗  ",
		"██║███╗██║██║   ██║██╔═══╝ ██╔══██║██╔══╝  ",
		"╚███╔███╔╝╚██████╔╝██║     ██║  ██║██║     ",
		" ╚══╝╚══╝  ╚═════╝ ╚═╝     ╚═╝  ╚═╝╚═╝     ",
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EAB308")).
		Bold(true)
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7A7A7E")).
		Italic(true)

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

	// Subtitle
	subtitle := "Somehow still operational."
	pad := (m.width - len(subtitle)) / 2
	if pad < 0 {
		pad = 0
	}
	lines = append(lines, "")
	lines = append(lines, strings.Repeat(" ", pad)+subtitleStyle.Render(subtitle))

	// Final bell chord
	if m.frame%8 == 0 {
		fmt.Print("\a")
	}

	return strings.Join(lines, "\n")
}
