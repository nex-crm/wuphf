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

// ── Splash phases ───────────────────────────────────────────────
//
// The splash is a scripted sequence inspired by The Office intro:
//
//   Phase 0: Cast entrance — characters appear one by one
//   Phase 1: Full cast visible, brief beat
//   Phase 2: PM rushes in late from the right
//   Phase 3: CRASH — PM bumps CEO, coffee spills, bell rings
//   Phase 4: CEO grumpy face, coffee stain visible
//   Phase 5: CEO forces a fake smile for the "camera"
//   Phase 6: WUPHF title card
//   Phase 7: Transition to channel view

const (
	splashEntrance  = 0
	splashFullCast  = 1
	splashRushIn    = 2
	splashCrash     = 3
	splashGrumpy    = 4
	splashFakeSmile = 5
	splashTitle     = 6
	splashDone      = 7
)

type splashTickMsg time.Time
type splashDoneMsg struct{}

type splashModel struct {
	members   []company.MemberSpec
	width     int
	height    int
	frame     int
	phase     int
	shown     int
	bells     int
	rushX     int  // PM's X offset during rush-in (starts off-screen)
	startAt   time.Time
	phaseAt   time.Time // when current phase started
	crashBell bool
}

func newSplashModel() splashModel {
	manifest := company.DefaultManifest()
	loaded, err := company.LoadManifest()
	if err == nil && len(loaded.Members) > 0 {
		manifest = loaded
	}
	now := time.Now()
	return splashModel{
		members: manifest.Members,
		startAt: now,
		phaseAt: now,
		rushX:   40, // start off-screen right
	}
}

func splashTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return splashTickMsg(t) })
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
		phaseElapsed := time.Since(m.phaseAt)

		entranceDuration := time.Duration(len(m.members))*300*time.Millisecond + 200*time.Millisecond

		switch m.phase {
		case splashEntrance:
			if elapsed < 200*time.Millisecond {
				m.shown = 0
			} else if elapsed < entranceDuration {
				m.shown = int((elapsed - 200*time.Millisecond) / (300 * time.Millisecond))
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
			} else {
				m.shown = len(m.members)
				m.phase = splashFullCast
				m.phaseAt = time.Now()
			}

		case splashFullCast:
			if phaseElapsed > 600*time.Millisecond {
				m.phase = splashRushIn
				m.phaseAt = time.Now()
			}

		case splashRushIn:
			// PM slides in from the right toward CEO
			m.rushX -= 6 // fast slide
			if m.rushX <= 0 {
				m.rushX = 0
				m.phase = splashCrash
				m.phaseAt = time.Now()
				// Double bell on crash!
				if !m.crashBell {
					m.crashBell = true
					return m, tea.Batch(splashTick(), func() tea.Msg {
						fmt.Print("\a\a")
						return nil
					})
				}
			}

		case splashCrash:
			if phaseElapsed > 500*time.Millisecond {
				m.phase = splashGrumpy
				m.phaseAt = time.Now()
			}

		case splashGrumpy:
			if phaseElapsed > 800*time.Millisecond {
				m.phase = splashFakeSmile
				m.phaseAt = time.Now()
			}

		case splashFakeSmile:
			if phaseElapsed > 1200*time.Millisecond {
				m.phase = splashTitle
				m.phaseAt = time.Now()
			}

		case splashTitle:
			if phaseElapsed > 1500*time.Millisecond {
				m.phase = splashDone
				return m, func() tea.Msg { return splashDoneMsg{} }
			}
			// Bell chord on title
			if m.frame%12 == 0 {
				fmt.Print("\a")
			}

		case splashDone:
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
		Width(m.width).Height(m.height).
		Background(bg).Foreground(lipgloss.Color("#E8E8EA"))

	if m.phase == splashDone {
		return fullStyle.Render("")
	}

	switch m.phase {
	case splashTitle:
		return fullStyle.Render(m.renderTitle())
	default:
		return fullStyle.Render(m.renderCast())
	}
}

// ── Cast rendering with collision gag ───────────────────────────

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
		slug  string
	}

	pmIsRushing := m.phase == splashRushIn
	pmHasCrashed := m.phase >= splashCrash

	// Determine which CEO sprite variant to use
	ceoVariant := "normal"
	switch m.phase {
	case splashCrash:
		ceoVariant = "spill"
	case splashGrumpy:
		ceoVariant = "grumpy"
	case splashFakeSmile:
		ceoVariant = "fakesmile"
	}

	// Separate members into: CEO (top row), PM (arrives late), rest (bottom row)
	var ceoBlock, pmBlock *avatarBlock
	rest := make([]avatarBlock, 0, count)
	for i := 0; i < count; i++ {
		member := m.members[i]
		name := member.Name
		if name == "" {
			name = member.Slug
		}
		var spriteLines []string

		switch {
		case member.Slug == "ceo":
			if ceoVariant != "normal" {
				spriteLines = renderCEOVariant(ceoVariant, m.frame)
			} else {
				spriteLines = renderWuphfSplashAvatar(member.Name, member.Slug, m.frame)
			}
			ceoBlock = &avatarBlock{lines: spriteLines, name: name, slug: member.Slug}

		case member.Slug == "pm":
			spriteLines = renderWuphfSplashAvatar(member.Name, member.Slug, m.frame)
			pmBlock = &avatarBlock{lines: spriteLines, name: name, slug: member.Slug}

		default:
			spriteLines = renderWuphfSplashAvatar(member.Name, member.Slug, m.frame)
			rest = append(rest, avatarBlock{lines: spriteLines, name: name, slug: member.Slug})
		}
	}

	if ceoBlock == nil {
		return ""
	}

	// ── Top row: CEO (+ PM after crash or during rush-in) ──
	topBlocks := []avatarBlock{*ceoBlock}
	// After crash, PM sits next to CEO permanently
	if pmHasCrashed && pmBlock != nil {
		topBlocks = append(topBlocks, *pmBlock)
	}

	topMaxH := 0
	for _, b := range topBlocks {
		if len(b.lines) > topMaxH {
			topMaxH = len(b.lines)
		}
	}
	if pmBlock != nil && len(pmBlock.lines) > topMaxH {
		topMaxH = len(pmBlock.lines)
	}

	topW := len(topBlocks)*(slotW+spacing) - spacing
	topLeftPad := (m.width - topW) / 2
	if topLeftPad < 0 {
		topLeftPad = 0
	}

	// ── Bottom row: rest of the cast ──
	bottomMaxH := 0
	for _, b := range rest {
		if len(b.lines) > bottomMaxH {
			bottomMaxH = len(b.lines)
		}
	}
	bottomW := 0
	if len(rest) > 0 {
		bottomW = len(rest)*(slotW+spacing) - spacing
	}
	bottomLeftPad := (m.width - bottomW) / 2
	if bottomLeftPad < 0 {
		bottomLeftPad = 0
	}

	// Calculate vertical layout
	gapBetweenRows := 2 // gap between top row and bottom row
	nameLabelH := 2     // blank line + name
	subtitleH := 3      // blank + subtitle + padding
	totalH := topMaxH + nameLabelH + gapBetweenRows + bottomMaxH + nameLabelH + subtitleH
	topPad := (m.height - totalH) / 2
	if topPad < 2 {
		topPad = 2
	}

	var lines []string
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}

	// ── Render top row sprites ──
	for row := 0; row < topMaxH; row++ {
		var parts []string
		for _, block := range topBlocks {
			offset := topMaxH - len(block.lines)
			rendered := strings.Repeat(" ", slotW)
			if row >= offset {
				line := block.lines[row-offset]
				w := ansi.StringWidth(line)
				if w < slotW {
					line += strings.Repeat(" ", slotW-w)
				}
				rendered = line
			}
			parts = append(parts, rendered)
		}
		castLine := strings.Repeat(" ", topLeftPad) + strings.Join(parts, strings.Repeat(" ", spacing))

		// During rush-in, overlay PM sliding from right toward CEO
		if pmIsRushing && pmBlock != nil {
			pmOffset := topMaxH - len(pmBlock.lines)
			if row >= pmOffset {
				pmLine := pmBlock.lines[row-pmOffset]
				// PM slides from rushX toward slot right of CEO
				targetX := topLeftPad + slotW + spacing
				pmX := targetX + m.rushX
				if pmX+slotW > m.width {
					pmX = m.width - slotW
				}
				if pmX < targetX {
					pmX = targetX
				}
				lineW := ansi.StringWidth(castLine)
				if pmX > lineW {
					castLine += strings.Repeat(" ", pmX-lineW)
				}
				castLine += pmLine
			}
		}

		lines = append(lines, castLine)
	}

	// Coffee spill particles above CEO during crash
	if m.phase == splashCrash {
		spillStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8B4513"))
		if topPad > 2 {
			particleLine := strings.Repeat(" ", topLeftPad+2)
			particleLine += spillStyle.Render("  ~~ \u2615 ~~  ")
			lines[topPad-2] = particleLine
		}
	}

	// Angry speech bubble above CEO during grumpy/fakesmile phases
	if m.phase == splashGrumpy || m.phase == splashFakeSmile {
		angryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		bubbleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
		if topPad > 4 {
			bubble := []string{
				bubbleStyle.Render("  \u250c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2510"),
				bubbleStyle.Render("  \u2502 ") + angryStyle.Render("STAY IN YOUR LANE") + bubbleStyle.Render("  \u2502"),
				bubbleStyle.Render("  \u2514\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u252c\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2518"),
				bubbleStyle.Render("              \u2570\u2500\u256f"),
			}
			for i, bline := range bubble {
				idx := topPad - 5 + i
				if idx >= 0 && idx < len(lines) {
					lines[idx] = strings.Repeat(" ", topLeftPad) + bline
				}
			}
		}
	}

	// Top row name labels
	lines = append(lines, "")
	var topNames []string
	for _, block := range topBlocks {
		topNames = append(topNames, m.renderNameLabel(block.slug, block.name, slotW))
	}
	topNameLine := strings.Repeat(" ", topLeftPad) + strings.Join(topNames, strings.Repeat(" ", spacing))

	// PM name during rush-in
	if pmIsRushing && pmBlock != nil {
		pmLabel := m.renderNameLabel(pmBlock.slug, pmBlock.name, slotW)
		targetX := topLeftPad + slotW + spacing
		pmX := targetX + m.rushX
		if pmX+slotW > m.width {
			pmX = m.width - slotW
		}
		if pmX < targetX {
			pmX = targetX
		}
		nameW := ansi.StringWidth(topNameLine)
		if pmX > nameW {
			topNameLine += strings.Repeat(" ", pmX-nameW)
		}
		topNameLine += pmLabel
	}
	lines = append(lines, topNameLine)

	// ── Gap between rows ──
	for i := 0; i < gapBetweenRows; i++ {
		lines = append(lines, "")
	}

	// ── Render bottom row sprites ──
	if len(rest) > 0 {
		for row := 0; row < bottomMaxH; row++ {
			var parts []string
			for _, block := range rest {
				offset := bottomMaxH - len(block.lines)
				rendered := strings.Repeat(" ", slotW)
				if row >= offset {
					line := block.lines[row-offset]
					w := ansi.StringWidth(line)
					if w < slotW {
						line += strings.Repeat(" ", slotW-w)
					}
					rendered = line
				}
				parts = append(parts, rendered)
			}
			lines = append(lines, strings.Repeat(" ", bottomLeftPad)+strings.Join(parts, strings.Repeat(" ", spacing)))
		}

		// Bottom row name labels
		lines = append(lines, "")
		var bottomNames []string
		for _, block := range rest {
			bottomNames = append(bottomNames, m.renderNameLabel(block.slug, block.name, slotW))
		}
		lines = append(lines, strings.Repeat(" ", bottomLeftPad)+strings.Join(bottomNames, strings.Repeat(" ", spacing)))
	}

	// ── Subtitle ──
	subtitle := ""
	subtitleColor := "#7A7A7E"
	switch m.phase {
	case splashRushIn:
		subtitle = "*running footsteps*"
	case splashCrash:
		subtitle = "!! CRASH !!"
		subtitleColor = "#EF4444"
	case splashGrumpy:
		subtitle = ""
	case splashFakeSmile:
		subtitle = ""
	}
	if subtitle != "" {
		lines = append(lines, "")
		pad := (m.width - len(subtitle)) / 2
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, strings.Repeat(" ", pad)+lipgloss.NewStyle().Foreground(lipgloss.Color(subtitleColor)).Italic(true).Render(subtitle))
	}

	return strings.Join(lines, "\n")
}

func (m splashModel) renderNameLabel(slug, name string, slotW int) string {
	name = truncateLabel(name, slotW)
	padL := (slotW - len([]rune(name))) / 2
	padR := slotW - len([]rune(name)) - padL
	if padL < 0 {
		padL = 0
	}
	if padR < 0 {
		padR = 0
	}
	label := strings.Repeat(" ", padL) + name + strings.Repeat(" ", padR)
	agentColor := sidebarAgentColors[slug]
	if agentColor == "" {
		agentColor = "#64748B"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Bold(true).Render(label)
}


// ── CEO sprite variants for the collision gag ───────────────────

func renderCEOVariant(variant string, frame int) []string {
	var sprite pixelSprite
	switch variant {
	case "spill":
		sprite = spriteCEOSpill()
	case "grumpy":
		sprite = spriteCEOGrumpy()
	case "fakesmile":
		if frame%2 == 0 {
			sprite = spriteCEOFakeSmile()
		} else {
			// Alternate: smile twitches back to grumpy briefly
			sprite = spriteCEOFakeSmileTwitch()
		}
	default:
		sprite = spriteCEO
	}
	return renderSpriteToANSI(sprite, spritePaletteForSlug("ceo"))
}

// CEO shocked — coffee cup flying off to the side, mouth wide open, eyes wide
func spriteCEOSpill() pixelSprite {
	return pixelSprite{
		{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 5, 0},
		{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 5, 5},
		{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 1, 2, 2, 1, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 2, 2, 1, 1, 2, 2, 1, 0, 0, 0}, // mouth open (shocked)
		{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
		{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
		{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
		{0, 0, 2, 2, 3, 5, 3, 3, 3, 3, 2, 2, 0, 0}, // coffee stain on shirt
		{0, 0, 1, 2, 1, 5, 5, 3, 3, 1, 2, 1, 0, 0},
		{0, 0, 0, 1, 0, 1, 1, 1, 1, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	}
}

// CEO grumpy — angry eyebrows, tight frown, coffee stain still visible
func spriteCEOGrumpy() pixelSprite {
	return pixelSprite{
		{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
		{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
		{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 1, 2, 2, 1, 1, 1, 0, 0, 0}, // sunglasses
		{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
		{0, 0, 0, 0, 1, 2, 1, 1, 2, 1, 0, 0, 0, 0}, // tight frown
		{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
		{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
		{0, 0, 2, 2, 3, 5, 3, 3, 3, 3, 2, 2, 0, 0}, // stain
		{0, 0, 1, 2, 1, 5, 5, 3, 3, 1, 2, 1, 0, 0}, // stain
		{0, 0, 0, 1, 0, 1, 1, 1, 1, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	}
}

// CEO fake smile — forced wide grin, eyebrows up, stain still there
func spriteCEOFakeSmile() pixelSprite {
	return pixelSprite{
		{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
		{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
		{0, 0, 1, 1, 2, 2, 2, 2, 2, 2, 1, 1, 0, 0}, // eyebrows up
		{0, 0, 0, 1, 1, 1, 2, 2, 1, 1, 1, 0, 0, 0}, // sunglasses
		{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
		{0, 0, 0, 0, 1, 6, 6, 6, 6, 1, 0, 0, 0, 0}, // wide forced grin (white teeth)
		{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
		{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
		{0, 0, 2, 2, 3, 5, 3, 3, 3, 3, 2, 2, 0, 0}, // stain still there
		{0, 0, 1, 2, 1, 5, 5, 3, 3, 1, 2, 1, 0, 0},
		{0, 0, 0, 1, 0, 1, 1, 1, 1, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	}
}

// CEO fake smile twitching — smile flickers, one eyebrow drops
func spriteCEOFakeSmileTwitch() pixelSprite {
	return pixelSprite{
		{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
		{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
		{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 1, 0, 0}, // one eyebrow up, one down
		{0, 0, 0, 1, 1, 1, 2, 2, 1, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
		{0, 0, 0, 0, 1, 6, 6, 6, 2, 1, 0, 0, 0, 0}, // smile twitching (half grin)
		{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
		{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
		{0, 0, 2, 2, 3, 5, 3, 3, 3, 3, 2, 2, 0, 0},
		{0, 0, 1, 2, 1, 5, 5, 3, 3, 1, 2, 1, 0, 0},
		{0, 0, 0, 1, 0, 1, 1, 1, 1, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	}
}

// ── Title card ──────────────────────────────────────────────────

func (m splashModel) renderTitle() string {
	title := []string{
		"\u2588\u2588\u2557    \u2588\u2588\u2557\u2588\u2588\u2557   \u2588\u2588\u2557\u2588\u2588\u2588\u2588\u2588\u2588\u2557 \u2588\u2588\u2557  \u2588\u2588\u2557\u2588\u2588\u2588\u2588\u2588\u2588\u2588\u2557",
		"\u2588\u2588\u2551    \u2588\u2588\u2551\u2588\u2588\u2551   \u2588\u2588\u2551\u2588\u2588\u2554\u2550\u2550\u2588\u2588\u2557\u2588\u2588\u2551  \u2588\u2588\u2551\u2588\u2588\u2554\u2550\u2550\u2550\u2550\u255d",
		"\u2588\u2588\u2551 \u2588\u2557 \u2588\u2588\u2551\u2588\u2588\u2551   \u2588\u2588\u2551\u2588\u2588\u2588\u2588\u2588\u2588\u2554\u255d\u2588\u2588\u2588\u2588\u2588\u2588\u2588\u2551\u2588\u2588\u2588\u2588\u2588\u2557  ",
		"\u2588\u2588\u2551\u2588\u2588\u2588\u2557\u2588\u2588\u2551\u2588\u2588\u2551   \u2588\u2588\u2551\u2588\u2588\u2554\u2550\u2550\u2550\u255d \u2588\u2588\u2554\u2550\u2550\u2588\u2588\u2551\u2588\u2588\u2554\u2550\u2550\u255d  ",
		"\u255a\u2588\u2588\u2588\u2554\u2588\u2588\u2588\u2554\u255d\u255a\u2588\u2588\u2588\u2588\u2588\u2588\u2554\u255d\u2588\u2588\u2551     \u2588\u2588\u2551  \u2588\u2588\u2551\u2588\u2588\u2551     ",
		" \u255a\u2550\u2550\u255d\u255a\u2550\u2550\u255d  \u255a\u2550\u2550\u2550\u2550\u2550\u255d \u255a\u2550\u255d     \u255a\u2550\u255d  \u255a\u2550\u255d\u255a\u2550\u255d     ",
	}
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EAB308")).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7A7E")).Italic(true)
	titleW := 0
	for _, l := range title {
		w := len([]rune(l))
		if w > titleW {
			titleW = w
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
	return strings.Join(lines, "\n")
}
