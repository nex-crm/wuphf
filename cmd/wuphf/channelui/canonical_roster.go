package channelui

// CanonicalRosterSlugs is the eight-agent office roster in display
// order. It is the single source of truth for "the built-in slugs we
// know about" — usage strips, default sidebar fallback, and any other
// helper that needs to walk the office in canonical order should
// consume this slice instead of redeclaring the list. Adding a new
// built-in role is a one-line edit here.
//
// The order matters: it drives left-to-right rendering in the usage
// strip and top-to-bottom rendering in the sidebar fallback.
var CanonicalRosterSlugs = []string{
	"ceo",
	"pm",
	"fe",
	"be",
	"ai",
	"designer",
	"cmo",
	"cro",
}
