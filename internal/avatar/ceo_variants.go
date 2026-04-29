package avatar

// CEO sprite variants for the splash collision gag. These live alongside
// the canonical CEO sprite so all sprite data stays in one package.

// SpriteCEOSpill is the shocked-coffee-spill pose: cup flying off, mouth
// wide open, eyes wide.
func SpriteCEOSpill() Sprite {
	return Sprite{
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

// SpriteCEOGrumpy is the angry-eyebrows tight-frown pose with the coffee
// stain still visible.
func SpriteCEOGrumpy() Sprite {
	return Sprite{
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

// SpriteCEOFakeSmile is the forced-wide-grin pose (eyebrows up, stain
// still there) used when the CEO performs for the camera.
func SpriteCEOFakeSmile() Sprite {
	return Sprite{
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

// SpriteCEOFakeSmileTwitch is the half-grin failure mode where the
// performance breaks: smile flickers, one eyebrow drops.
func SpriteCEOFakeSmileTwitch() Sprite {
	return Sprite{
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

// RenderCEOVariant picks the right CEO sprite for the splash collision
// gag and renders it with the canonical CEO palette. variant must be
// one of "spill", "grumpy", "fakesmile", or any other value (treated as
// the canonical CEO sprite). frame alternates 0/1 to drive the
// fakesmile twitch animation.
func RenderCEOVariant(variant string, frame int) []string {
	var sprite Sprite
	switch variant {
	case "spill":
		sprite = SpriteCEOSpill()
	case "grumpy":
		sprite = SpriteCEOGrumpy()
	case "fakesmile":
		if frame%2 == 0 {
			sprite = SpriteCEOFakeSmile()
		} else {
			sprite = SpriteCEOFakeSmileTwitch()
		}
	default:
		sprite = SpriteCEO()
	}
	return RenderToANSI(sprite, PaletteForSlug("ceo"))
}
