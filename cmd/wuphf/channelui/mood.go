package channelui

import "strings"

// InferMood classifies a message body into one of a small set of
// vibe labels ("energized", "skeptical", "concerned", "tense",
// "relieved", "focused"). Empty / unmatched input returns "". Used
// for the meta-line tint on office messages.
func InferMood(text string) string {
	lower := strings.ToLower(text)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "love this") || strings.Contains(lower, "excited") || strings.Contains(lower, "let's go") || strings.Contains(lower, "great wedge"):
		return "energized"
	case strings.Contains(lower, "hmm") || strings.Contains(lower, "skept") || strings.Contains(lower, "push back") || strings.Contains(lower, "bloodbath") || strings.Contains(lower, "crowded"):
		return "skeptical"
	case strings.Contains(lower, "worr") || strings.Contains(lower, "risk") || strings.Contains(lower, "concern"):
		return "concerned"
	case strings.Contains(lower, "blocked") || strings.Contains(lower, "stuck") || strings.Contains(lower, "hard part"):
		return "tense"
	case strings.Contains(lower, "done") || strings.Contains(lower, "shipped") || strings.Contains(lower, "works"):
		return "relieved"
	case strings.Contains(lower, "need") || strings.Contains(lower, "should") || strings.Contains(lower, "must") || strings.Contains(lower, "v1"):
		return "focused"
	default:
		return ""
	}
}
