package emoji

import "strings"

// ToShortcode replaces common emoji with their Slack-style shortcode equivalents.
func ToShortcode(s string) string {
	s = strings.ReplaceAll(s, "🎉", ":tada:")
	s = strings.ReplaceAll(s, "🚀", ":rocket:")
	s = strings.ReplaceAll(s, "✅", ":check:")
	return s
}
