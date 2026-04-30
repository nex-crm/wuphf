package channelui

// canonicalRosterSlugs is the eight-agent office roster in display
// order. It is the single source of truth for "the built-in slugs we
// know about". Accessed via CanonicalRosterSlugs() so callers receive
// a defensive copy and can't reorder or extend the canonical sequence
// in-place.
//
// The order matters: it drives left-to-right rendering in the usage
// strip and top-to-bottom rendering in the sidebar fallback. Adding a
// new built-in role is a one-line edit here.
var canonicalRosterSlugs = []string{
	"ceo",
	"pm",
	"fe",
	"be",
	"ai",
	"designer",
	"cmo",
	"cro",
}

// CanonicalRosterSlugs returns a copy of the eight-agent built-in
// roster in display order. Callers may freely mutate the returned
// slice without affecting the canonical sequence.
func CanonicalRosterSlugs() []string {
	out := make([]string, len(canonicalRosterSlugs))
	copy(out, canonicalRosterSlugs)
	return out
}
