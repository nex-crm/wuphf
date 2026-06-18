package team

import (
	"regexp"
	"strings"
)

// slack_silence.go is the deterministic "stay quiet" chokepoint for Slack
// outbound, ported verbatim from the Hermes agent's two-layer silence handling
// (gateway/response_filters.py + gateway/delivery.py). An agent can DELIBERATELY
// choose not to speak (an explicit NO_REPLY control token), and any hallucinated
// "(silent)" narration a model emits INSTEAD of staying quiet is scrubbed. Both
// are enforced in code at the single send-point (FormatOutbound), so no prompt
// regression can ever leak status chatter — the convention note asks the LLM to
// be quiet; this guarantees it.

// slackSilenceMarkers is Hermes's LIVE_GATEWAY_SILENT_MARKERS: the exact whole-
// message tokens an agent posts to say "I deliberately have nothing to send."
// Compared case-insensitively and whitespace-collapsed against the FULL trimmed
// message (never a substring), so prose that merely contains the word is sent.
var slackSilenceMarkers = map[string]struct{}{
	"[SILENT]": {},
	"SILENT":   {},
	"NO_REPLY": {},
	"NO REPLY": {},
}

// slackSilenceNarration is Hermes's _SILENCE_NARRATION regex (delivery.py:31),
// translated to Go syntax: a whole message that is ONLY a silence token with
// optional markdown wrappers — "*(silent)*", "_silent_", "(no reply)", bare
// "silent"/"silence"/"no response"/"no reply" — or only the 🔇 emoji / a bare
// dot run / an ellipsis. Anchored ^…$ so it never matches real prose.
var slackSilenceNarration = regexp.MustCompile(
	"(?i)" +
		"^[\\s*_~`]*\\(?\\s*(silent|silence|no\\s+response|no\\s+reply)\\s*\\.?\\)?[\\s*_~`]*$" +
		"|^[\\s*_~`]*[\\x{1F507}.\\x{2026}]+[\\s*_~`]*$",
)

// slackOutboundIsSilent reports whether agent-authored content is an intentional
// non-reply that must NOT be posted to Slack. content is the RAW office message
// text (before display-name rewrite / tag rendering). Mirrors Hermes's rules:
// blank is silence; otherwise the >64-char guard keeps the check scoped to short
// throwaway markers (a genuine deliverable is never this short), then the exact
// NO_REPLY marker check and the narration scrubber.
func slackOutboundIsSilent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	if len(trimmed) > 64 {
		return false
	}
	// Exact NO_REPLY marker: uppercase + collapse internal whitespace, then match
	// the whole string (Hermes _canonical_silence_candidate).
	canonical := strings.ToUpper(strings.Join(strings.Fields(trimmed), " "))
	if _, ok := slackSilenceMarkers[canonical]; ok {
		return true
	}
	// Hallucinated silence-narration scrubber.
	return slackSilenceNarration.MatchString(trimmed)
}
