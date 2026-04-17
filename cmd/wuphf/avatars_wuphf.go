package main

import (
	"fmt"
	"strings"
)

// ── Pixel sprite engine ─────────────────────────────────────────
//
// Each sprite is a 2D grid of palette indices rendered with Unicode
// half-block characters (▀▄█). Two pixel rows fit in one terminal row
// using foreground/background colors, giving double vertical resolution.
//
// Palette:
//   0 = transparent
//   1 = outline (dark)
//   2 = skin tone
//   3 = accent (agent color)
//   4 = hair/hat
//   5 = prop/accessory
//   6 = white/highlight

type pixelSprite [][]int

const (
	pxClear     = 0
	pxLine      = 1
	pxSkin      = 2
	pxAccent    = 3
	pxHair      = 4
	pxProp      = 5
	pxHighlight = 6
)

// ── Unique character sprites ────────────────────────────────────
//
// Each character has a completely different silhouette, pose, and props.
// 14x14 grids — rendered to 14x7 terminal characters.
// Faces are deliberately squarish/blocky (Minecraft-ish).

// CEO: leaning back, sunglasses, confident stance, coffee in hand
var spriteCEO = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 1, 1, 2, 2, 1, 1, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 1, 1, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{0, 0, 2, 2, 3, 3, 3, 3, 3, 3, 2, 5, 1, 0},
	{0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 5, 1, 0},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 0, 1, 5, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// PM: standing straight, clipboard in hand, organized look
var spritePM = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{0, 0, 2, 3, 3, 3, 3, 3, 3, 3, 5, 5, 1, 0},
	{0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 5, 5, 1, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 5, 5, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// FE: hunched over laptop, typing furiously, hoodie
var spriteFE = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 1, 4, 2, 2, 2, 2, 2, 2, 4, 1, 0, 0},
	{0, 0, 1, 4, 2, 1, 2, 2, 1, 2, 4, 1, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{0, 2, 2, 5, 5, 5, 5, 5, 5, 5, 5, 2, 2, 0},
	{0, 0, 1, 5, 6, 6, 6, 6, 6, 6, 5, 1, 0, 0},
	{0, 0, 0, 5, 5, 5, 5, 5, 5, 5, 5, 0, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// BE: arms crossed, slightly grumpy, server rack behind
var spriteBE = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 1, 1, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{0, 0, 1, 2, 3, 3, 3, 3, 3, 3, 2, 1, 0, 0},
	{0, 0, 1, 3, 2, 3, 3, 3, 3, 2, 3, 1, 0, 0},
	{0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// AI: antenna on head, glowing eyes, slightly robotic
var spriteAI = pixelSprite{
	{0, 0, 0, 0, 0, 0, 5, 5, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 5, 1, 2, 2, 1, 5, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{0, 0, 2, 2, 3, 3, 3, 3, 3, 3, 2, 2, 0, 0},
	{0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 1, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// Designer: beret, holding pencil, creative pose
var spriteDesigner = pixelSprite{
	{0, 0, 0, 5, 5, 5, 5, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 3, 3, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 5, 0},
	{0, 0, 2, 2, 3, 3, 3, 3, 3, 3, 2, 2, 5, 0},
	{0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 1, 5, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// CMO: energetic pose, arms up, megaphone
var spriteCMO = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 3, 3, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{5, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{5, 5, 2, 2, 3, 3, 3, 3, 3, 3, 2, 2, 0, 0},
	{5, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 1, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

// CRO: sharp look, briefcase, power stance
var spriteCRO = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 4, 4, 4, 4, 4, 4, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 6, 3, 3, 3, 3, 6, 3, 1, 0, 0},
	{0, 1, 2, 3, 6, 3, 3, 3, 3, 6, 3, 2, 1, 0},
	{0, 0, 2, 2, 3, 3, 3, 3, 3, 3, 2, 2, 0, 0},
	{0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 1, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 5, 5, 5, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 5, 1, 5, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 5, 5, 5, 0, 0},
}

// spriteForSlug returns the unique sprite for a known role,
// or a seeded variation of the generic sprite for dynamic agents.
// frame alternates 0/1 for animation.
func spriteForSlug(slug string, frame ...int) pixelSprite {
	f := 0
	if len(frame) > 0 {
		f = frame[0] % 2
	}

	var sprite pixelSprite
	switch slug {
	case "ceo":
		sprite = cloneSprite(spriteCEO)
	case "pm":
		sprite = cloneSprite(spritePM)
	case "fe":
		sprite = cloneSprite(spriteFE)
	case "be":
		sprite = cloneSprite(spriteBE)
	case "ai":
		sprite = cloneSprite(spriteAI)
	case "designer":
		sprite = cloneSprite(spriteDesigner)
	case "cmo":
		sprite = cloneSprite(spriteCMO)
	case "cro":
		sprite = cloneSprite(spriteCRO)
	default:
		// Unknown slug — compose from modular layers so every agent is unique.
		sprite = proceduralSpriteForSlug(slug)
	}

	if f == 1 {
		animateFrame(sprite, slug)
	}
	return sprite
}

// animateFrame applies micro-animations for frame 1 (frame 0 is the base).
// Each character has a unique animation that conveys personality:
//
//	CEO:      raises coffee cup (arm moves up)
//	PM:       taps clipboard (hand shifts)
//	FE:      screen flickers (highlight changes)
//	BE:      tightens crossed arms
//	AI:      antenna blinks (accent toggles)
//	Designer: pencil moves (prop shifts position)
//	CMO:      megaphone raised higher
//	CRO:      briefcase swings
func animateFrame(sprite pixelSprite, slug string) {
	if len(sprite) < 14 {
		return
	}
	switch slug {
	case "ceo":
		// Coffee cup raised: move prop pixels up one row
		sprite[7][11] = pxProp
		sprite[7][12] = pxLine
		sprite[8][11] = pxAccent
		sprite[8][12] = pxAccent
		sprite[9][11] = pxSkin
		sprite[9][12] = pxClear
		// Mouth open (talking)
		sprite[5][7] = pxClear
	case "pm":
		// Clipboard check: arm extends, clipboard shifts
		sprite[8][12] = pxProp
		sprite[8][13] = pxLine
		sprite[9][12] = pxProp
		sprite[9][13] = pxLine
		// Eyebrow raise
		sprite[2][5] = pxLine
		sprite[2][8] = pxLine
	case "fe":
		// Screen glow flickers
		sprite[9][5] = pxHighlight
		sprite[9][8] = pxHighlight
		sprite[8][6] = pxHighlight
		sprite[8][7] = pxHighlight
		// Typing: hands shift
		sprite[8][4] = pxSkin
		sprite[8][9] = pxSkin
	case "be":
		// Arms cross tighter + frown
		sprite[8][4] = pxAccent
		sprite[8][9] = pxAccent
		sprite[9][5] = pxSkin
		sprite[9][8] = pxSkin
		// Frown deepens
		sprite[4][6] = pxLine
		sprite[4][7] = pxLine
	case "ai":
		// Antenna pulses (accent <-> highlight)
		sprite[0][6] = pxHighlight
		sprite[0][7] = pxHighlight
		// Eyes glow brighter
		sprite[4][5] = pxHighlight
		sprite[4][9] = pxHighlight
	case "designer":
		// Pencil moves (drawing motion)
		sprite[8][12] = pxClear
		sprite[9][12] = pxProp
		sprite[10][12] = pxProp
		sprite[10][13] = pxLine
		// Smirk
		sprite[5][7] = pxLine
	case "cmo":
		// Megaphone raised higher
		sprite[6][0] = pxProp
		sprite[6][1] = pxProp
		sprite[7][0] = pxClear
		sprite[7][1] = pxSkin
		sprite[8][0] = pxClear
		sprite[8][1] = pxClear
		// Mouth open (yelling)
		sprite[4][6] = pxClear
		sprite[4][7] = pxClear
	case "cro":
		// Briefcase swings forward
		sprite[11][9] = pxClear
		sprite[11][10] = pxProp
		sprite[11][11] = pxProp
		sprite[11][12] = pxProp
		sprite[12][10] = pxLine
		sprite[12][11] = pxProp
		sprite[12][12] = pxLine
		sprite[13][10] = pxProp
		sprite[13][11] = pxProp
		sprite[13][12] = pxProp
	default:
		// Generic: wave (arm up)
		sprite[7][1] = pxSkin
		sprite[8][0] = pxSkin
		sprite[8][1] = pxClear
	}
}

func cloneSprite(src pixelSprite) pixelSprite {
	out := make(pixelSprite, len(src))
	for i := range src {
		out[i] = append([]int(nil), src[i]...)
	}
	return out
}

// ── Palette ─────────────────────────────────────────────────────

func parseHexColor(hex string) [3]int {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return [3]int{140, 140, 150}
	}
	r, g, b := 0, 0, 0
	_, _ = fmt.Sscanf(hex[0:2], "%x", &r)
	_, _ = fmt.Sscanf(hex[2:4], "%x", &g)
	_, _ = fmt.Sscanf(hex[4:6], "%x", &b)
	return [3]int{r, g, b}
}

func spritePaletteForSlug(slug string) map[int][3]int {
	// Unknown slugs get a fully procedural palette (hash picks skin/hair/accent
	// independently) to match proceduralSpriteForSlug.
	if isProceduralSlug(slug) {
		return proceduralPaletteForSlug(slug)
	}
	accent := parseHexColor(agentColorMap[slug])
	if accent == ([3]int{}) {
		accent = [3]int{88, 166, 255}
	}
	// Hair color: darker version of accent
	hair := [3]int{
		max(0, accent[0]-60),
		max(0, accent[1]-60),
		max(0, accent[2]-60),
	}
	return map[int][3]int{
		pxLine:      {36, 32, 30},    // dark outline
		pxSkin:      {235, 215, 190}, // warm skin
		pxAccent:    accent,
		pxHair:      hair,
		pxProp:      {180, 170, 155}, // neutral prop color
		pxHighlight: {255, 255, 255}, // white highlights
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── Half-block renderer ─────────────────────────────────────────

func renderSpriteToANSI(sprite pixelSprite, palette map[int][3]int) []string {
	reset := "\x1b[0m"
	lines := make([]string, 0, (len(sprite)+1)/2)
	for r := 0; r < len(sprite); r += 2 {
		top := sprite[r]
		var bottom []int
		if r+1 < len(sprite) {
			bottom = sprite[r+1]
		}
		var b strings.Builder
		for c := 0; c < len(top); c++ {
			topVal := top[c]
			botVal := 0
			if bottom != nil && c < len(bottom) {
				botVal = bottom[c]
			}
			topRGB, topOK := palette[topVal]
			botRGB, botOK := palette[botVal]
			switch {
			case topVal != 0 && botVal != 0 && topOK && botOK:
				b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm\u2580%s",
					topRGB[0], topRGB[1], topRGB[2],
					botRGB[0], botRGB[1], botRGB[2], reset))
			case topVal != 0 && topOK:
				b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\u2580%s",
					topRGB[0], topRGB[1], topRGB[2], reset))
			case botVal != 0 && botOK:
				b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\u2584%s",
					botRGB[0], botRGB[1], botRGB[2], reset))
			default:
				b.WriteByte(' ')
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// ── Public API ──────────────────────────────────────────────────

// renderWuphfSplashAvatar renders a full-body character for the splash screen.
// frame alternates 0/1 for animation.
func renderWuphfSplashAvatar(seed, slug string, frame int) []string {
	_ = seed
	sprite := spriteForSlug(slug, frame)
	return renderSpriteToANSI(sprite, spritePaletteForSlug(slug))
}

// renderWuphfAvatar renders a small face portrait for inline use.
func renderWuphfAvatar(seed, slug string, frame int) []string {
	_ = seed
	// Use just the head portion (rows 0-5) of the full sprite
	full := spriteForSlug(slug, frame)
	if len(full) > 6 {
		head := full[:6]
		return renderSpriteToANSI(head, spritePaletteForSlug(slug))
	}
	return renderSpriteToANSI(full, spritePaletteForSlug(slug))
}
