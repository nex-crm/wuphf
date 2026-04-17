package main

import "hash/fnv"

// Procedural pixel-art avatar builder for unknown agent slugs.
// Mirrors web/src/lib/proceduralAvatar.ts — the layer set, color pools, and
// hash strategy match so the same slug looks identical in the CLI and browser.
//
// Layers are stamped in order over the shared body base:
//   1. hair (rows 0-2)
//   2. headwear (rows 0-2, overrides hair under a hat)
//   3. facial feature (rows 3-5)
//   4. neck accessory (rows 6-9)

const (
	proceduralRows = 14
	proceduralCols = 14
	layerNoop      = -1 // sentinel: leave the underlying pixel untouched
)

// baseBody is the always-drawn skeleton — face, torso, arms. No hair.
var baseBody = pixelSprite{
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 1, 2, 2, 1, 2, 1, 0, 0, 0},
	{0, 0, 0, 1, 2, 2, 2, 2, 2, 2, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 2, 2, 2, 2, 1, 0, 0, 0, 0},
	{0, 0, 1, 3, 3, 3, 3, 3, 3, 3, 3, 1, 0, 0},
	{0, 1, 2, 3, 3, 3, 3, 3, 3, 3, 3, 2, 1, 0},
	{0, 0, 2, 2, 3, 3, 3, 3, 3, 3, 2, 2, 0, 0},
	{0, 0, 1, 2, 1, 3, 3, 3, 3, 1, 2, 1, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0},
}

type layerBuilder func() [][]int

func emptyLayer() [][]int {
	layer := make([][]int, proceduralRows)
	for r := range layer {
		layer[r] = make([]int, proceduralCols)
		for c := range layer[r] {
			layer[r][c] = layerNoop
		}
	}
	return layer
}

// ── Hair ───────────────────────────────────────────────────────────────

func hairClassic() [][]int {
	l := emptyLayer()
	for c := 4; c <= 9; c++ {
		l[1][c] = pxHair
	}
	return l
}

func hairSpiky() [][]int {
	l := emptyLayer()
	l[0][4] = pxHair
	l[0][6] = pxHair
	l[0][8] = pxHair
	for c := 4; c <= 9; c++ {
		l[1][c] = pxHair
	}
	return l
}

func hairSidePart() [][]int {
	l := emptyLayer()
	for c := 4; c <= 9; c++ {
		l[1][c] = pxHair
	}
	l[1][7] = pxSkin
	return l
}

func hairBald() [][]int { return emptyLayer() }

func hairAfro() [][]int {
	l := emptyLayer()
	for c := 3; c <= 10; c++ {
		l[0][c] = pxHair
	}
	for c := 2; c <= 11; c++ {
		l[1][c] = pxHair
	}
	l[2][3] = pxHair
	l[2][10] = pxHair
	return l
}

func hairMohawk() [][]int {
	l := emptyLayer()
	l[0][6] = pxHair
	l[0][7] = pxHair
	l[1][6] = pxHair
	l[1][7] = pxHair
	return l
}

func hairLongSides() [][]int {
	l := emptyLayer()
	for c := 4; c <= 9; c++ {
		l[1][c] = pxHair
	}
	l[2][3] = pxHair
	l[2][10] = pxHair
	l[3][3] = pxHair
	l[3][10] = pxHair
	return l
}

var hairStyles = []layerBuilder{
	hairClassic, hairSpiky, hairSidePart, hairBald,
	hairAfro, hairMohawk, hairLongSides,
}

// ── Headwear ───────────────────────────────────────────────────────────

func headwearNone() [][]int { return emptyLayer() }

func headwearCap() [][]int {
	l := emptyLayer()
	for c := 3; c <= 10; c++ {
		l[0][c] = pxProp
		l[1][c] = pxProp
	}
	for c := 2; c <= 11; c++ {
		l[2][c] = pxLine
	}
	return l
}

func headwearHeadband() [][]int {
	l := emptyLayer()
	for c := 4; c <= 9; c++ {
		l[2][c] = pxAccent
	}
	return l
}

func headwearBeanie() [][]int {
	l := emptyLayer()
	for c := 4; c <= 9; c++ {
		l[0][c] = pxAccent
	}
	for c := 3; c <= 10; c++ {
		l[1][c] = pxAccent
	}
	return l
}

func headwearVisor() [][]int {
	l := emptyLayer()
	for c := 2; c <= 11; c++ {
		l[2][c] = pxProp
	}
	return l
}

// Weight "none" more heavily so most agents aren't wearing hats.
var headwearStyles = []layerBuilder{
	headwearNone, headwearNone,
	headwearCap, headwearHeadband, headwearBeanie, headwearVisor,
}

// ── Facial feature ─────────────────────────────────────────────────────

func faceNone() [][]int { return emptyLayer() }

func faceGlasses() [][]int {
	l := emptyLayer()
	l[3][4] = pxLine
	l[3][5] = pxProp
	l[3][6] = pxLine
	l[3][7] = pxLine
	l[3][8] = pxProp
	l[3][9] = pxLine
	return l
}

func faceMustache() [][]int {
	l := emptyLayer()
	for c := 5; c <= 8; c++ {
		l[5][c] = pxHair
	}
	return l
}

func faceBeard() [][]int {
	l := emptyLayer()
	l[4][4] = pxHair
	l[4][9] = pxHair
	for c := 5; c <= 8; c++ {
		l[5][c] = pxHair
	}
	return l
}

var faceStyles = []layerBuilder{
	faceNone, faceNone, faceNone,
	faceGlasses, faceMustache, faceBeard,
}

// ── Neck accessory ─────────────────────────────────────────────────────

func neckNone() [][]int { return emptyLayer() }

func neckTie() [][]int {
	l := emptyLayer()
	l[6][6] = pxHighlight
	l[6][7] = pxHighlight
	l[7][6] = pxHighlight
	l[7][7] = pxHighlight
	return l
}

func neckBadge() [][]int {
	l := emptyLayer()
	l[9][4] = pxHighlight
	return l
}

var neckStyles = []layerBuilder{neckNone, neckNone, neckTie, neckBadge}

// ── Color pools ────────────────────────────────────────────────────────

var accentPool = []string{
	"#E8A838", "#58A6FF", "#A371F7", "#3FB950",
	"#D2A8FF", "#F778BA", "#FFA657", "#79C0FF",
	"#FF7B72", "#56D4DD", "#FFD866", "#C9D1D9",
}

var hairPool = [][3]int{
	{36, 32, 30},
	{74, 52, 38},
	{139, 90, 43},
	{200, 155, 90},
	{170, 60, 45},
	{200, 200, 200},
	{236, 178, 46},
	{180, 120, 200},
}

var skinPool = [][3]int{
	{248, 220, 190},
	{235, 195, 160},
	{210, 165, 125},
	{175, 125, 90},
	{140, 95, 65},
	{100, 65, 45},
}

// ── Hash ───────────────────────────────────────────────────────────────

func proceduralHash(slug string) uint32 {
	if slug == "" {
		slug = "unknown"
	}
	h := fnv.New32a()
	h.Write([]byte(slug))
	return h.Sum32()
}

// pickIndex mixes the slug hash with a salt so each layer picks independently.
func pickIndex(hash, salt uint32, modulo int) int {
	h := hash ^ (salt * 0x9e3779b1)
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return int(h % uint32(modulo))
}

// ── Public API ─────────────────────────────────────────────────────────

// proceduralSpriteForSlug returns a composed sprite for a slug that has no
// hand-designed mascot. Stable per slug.
func proceduralSpriteForSlug(slug string) pixelSprite {
	hash := proceduralHash(slug)

	sprite := cloneSprite(baseBody)
	layers := [][][]int{
		hairStyles[pickIndex(hash, 1, len(hairStyles))](),
		headwearStyles[pickIndex(hash, 2, len(headwearStyles))](),
		faceStyles[pickIndex(hash, 3, len(faceStyles))](),
		neckStyles[pickIndex(hash, 4, len(neckStyles))](),
	}
	for _, layer := range layers {
		for r := 0; r < proceduralRows; r++ {
			for c := 0; c < proceduralCols; c++ {
				if layer[r][c] != layerNoop {
					sprite[r][c] = layer[r][c]
				}
			}
		}
	}
	return sprite
}

// proceduralPaletteForSlug returns the per-slug palette (skin, hair, accent
// all picked independently by hash) for an agent without a mascot entry.
func proceduralPaletteForSlug(slug string) map[int][3]int {
	hash := proceduralHash(slug)
	accentHex := accentPool[pickIndex(hash, 5, len(accentPool))]
	hair := hairPool[pickIndex(hash, 6, len(hairPool))]
	skin := skinPool[pickIndex(hash, 7, len(skinPool))]
	return map[int][3]int{
		pxLine:      {36, 32, 30},
		pxSkin:      skin,
		pxAccent:    parseHexColor(accentHex),
		pxHair:      hair,
		pxProp:      {180, 170, 155},
		pxHighlight: {255, 255, 255},
	}
}

// proceduralAccentForSlug returns the hex accent color for a procedural slug,
// matching the one used inside proceduralPaletteForSlug.
func proceduralAccentForSlug(slug string) string {
	hash := proceduralHash(slug)
	return accentPool[pickIndex(hash, 5, len(accentPool))]
}

// isProceduralSlug reports true when the slug has no hand-designed mascot.
func isProceduralSlug(slug string) bool {
	switch slug {
	case "ceo", "pm", "fe", "be", "ai", "designer", "cmo", "cro":
		return false
	}
	return true
}
