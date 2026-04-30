package channelui

import "strings"

// ContainsSlug reports whether items contains want by exact match.
// Mirrors ContainsString but kept as a distinct name to make slug
// comparisons read clearly at the callsite.
func ContainsSlug(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// PluralizeWord returns singular for count == 1, otherwise plural
// when non-empty, otherwise singular + "s". Used for "1 reply" /
// "5 replies", "1 task run" / "5 task runs", etc.
func PluralizeWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	if strings.TrimSpace(plural) != "" {
		return plural
	}
	return singular + "s"
}

// ExtractTagsFromText returns the slugs after each "@…" mention in
// text, stripping trailing punctuation (".,!?;:"). Whitespace-split,
// so "Hi @ceo, can you?" yields []{"ceo"}. Punctuation-only mentions
// like "@," collapse to an empty slug after trimming and are skipped.
func ExtractTagsFromText(text string) []string {
	var tags []string
	for _, word := range strings.Fields(text) {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			tag := strings.TrimRight(word[1:], ".,!?;:")
			if tag == "" {
				continue
			}
			tags = append(tags, tag)
		}
	}
	return tags
}

// ChannelExists reports whether channels contains a ChannelInfo with
// matching Slug.
func ChannelExists(channels []ChannelInfo, slug string) bool {
	for _, ch := range channels {
		if ch.Slug == slug {
			return true
		}
	}
	return false
}
