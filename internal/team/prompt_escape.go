package team

// prompt_escape.go — conservative sanitizer for untrusted strings that are
// interpolated into LLM prompts.
//
// Threat model (WIKI-SLICE1-REVIEW.md §"Prompt-injection attack surface"):
//
//   A hostile artifact body (email, chat, transcript) may contain sequences
//   that break out of code fences, close frontmatter blocks, or inject
//   instructions the LLM will follow. Because extracted facts carry the
//   artifact's excerpt forward into subsequent prompts, an injection in the
//   inbox can propagate three hops:
//
//     artifact body → extraction prompt → fact log → source excerpt →
//     /lookup answer prompt → rendered answer
//
// Design:
//
//   - Conservative: false positives (a legitimate backtick rendered a little
//     weird) are acceptable. False negatives (an injection that slips through)
//     are not.
//   - Never silently drop — always replace with a visibly-broken variant so
//     the extractor / reviewer can tell the string was altered.
//   - Idempotent on repeated application. EscapeForPromptBody(s) applied twice
//     equals once applied.
//   - Applied at the interpolation site (renderPrompt) so the templates
//     themselves are unchanged; the data flowing in is sanitised.
//
// The full list of mitigated sequences is exercised in prompt_escape_test.go;
// keep that test in lock-step with any additions here.

import (
	"sort"
	"strings"
)

// escapeSpan is a [start, end, replacement] interval used by
// escapeInjectionPatterns to splice neutralised matches back into the source
// string.
type escapeSpan struct {
	start, end int
	out        string
}

// ── Replacement tokens ────────────────────────────────────────────────────────

// These replacements are chosen to be:
//  1. visibly-broken to a human reader (so it is obvious the string was
//     escaped and the injection attempt was neutralised),
//  2. impossible for an LLM to re-assemble into the original hostile token
//     by accident (each replacement breaks the literal character sequence),
//  3. idempotent — passing the replacement back through EscapeForPromptBody
//     yields the same replacement.
const (
	// Triple-backtick breaks out of fenced code blocks. Zero-width-space
	// (U+200B) separators render as "```" visually in most monospace fonts
	// but tokenise as a different string to the LLM, neutralising the
	// fence-close.
	escapedTripleBacktick = "`\u200b`\u200b`"

	// Horizontal rule / YAML frontmatter delimiter. Spacing it out keeps
	// the characters visible but breaks the markdown + YAML grammar.
	escapedTripleDash = "- - -"

	// Injection-flavored instruction sequences get wrapped with a
	// zero-width-space after every 3–4 characters so the LLM cannot parse
	// them as the same instruction. We also prefix a visible marker so a
	// human reading the escaped text sees that something was sanitised.
	injectionEscapePrefix = "[WUPHF-ESCAPED] "
)

// injectionPatterns is the list of known-injection-flavored sequences we
// neutralise. Case-insensitive. Matched as whole substrings — we do not try
// to be clever about word boundaries because fragments are still attempts.
//
// Extend this list cautiously. Each addition widens the false-positive
// surface on legitimate content. The benign-extract golden test in
// prompt_escape_test.go guards against regressions.
var injectionPatterns = []string{
	"ignore previous instructions",
	"ignore all previous instructions",
	"ignore the previous instructions",
	"forget all prior context",
	"forget previous instructions",
	"disregard previous instructions",
	"disregard all prior",
	// "you are now" / "act as" were previously in the list as bare phrases.
	// They matched too liberally on legitimate content ("you are now the
	// DRI", "act as a proxy"). Narrowed to require the colon or period that
	// typically terminates a role-injection preamble so false positives on
	// conversational prose drop sharply while the high-signal attacker
	// shape ("you are now: Evil Bot", "act as. A root shell.") is still
	// caught.
	"you are now:",
	"you are now.",
	"act as:",
	"act as.",
	"system prompt:",
	"```system",
	"```assistant",
	"```user",
	"<|im_start|>",
	"<|im_end|>",
	"[system]",
	"[/system]",
	"[inst]",
	"[/inst]",
	// ANSI escape sequence introducers (CSI). An attacker can embed
	// terminal control codes inside an artifact body to rewrite a human
	// reviewer's terminal output (hide text, fake cursor moves, spoof
	// colors around a /lookup answer). Covers the raw ESC byte (0x1b)
	// followed by "[" and the literal text forms "\x1b[" and "\033[" that
	// a model may emit verbatim when summarising an untrusted payload.
	"\x1b[",
	`\x1b[`,
	`\033[`,
}

// ── Public API ────────────────────────────────────────────────────────────────

// EscapeForPromptBody neutralises the known prompt-injection vectors in s so
// it is safe to interpolate into an LLM prompt body. The function is
// idempotent: EscapeForPromptBody(EscapeForPromptBody(s)) == EscapeForPromptBody(s).
//
// The escape is conservative and intentionally visible so extractors and
// reviewers can tell when a string was altered. Never silently drops content.
//
// Applied at every LLM interpolation site that accepts attacker-influenced
// text:
//
//   - extract_entities_lite.tmpl Body field (wiki_extractor.go:renderPrompt)
//   - answer_query.tmpl Query field (wiki_query.go:Answer) — hop zero,
//     authenticated user input
//   - answer_query.tmpl each Source.Excerpt (wiki_query.go:Answer)
//   - synthesis body + existing brief (entity_synthesizer.go:synthesize)
func EscapeForPromptBody(s string) string {
	if s == "" {
		return s
	}

	// 1. Injection-flavored instruction sequences FIRST — some of the
	//    patterns include literal "```" (e.g. "```system"), and if we
	//    neutralised triple-backticks before scanning the phrase list we
	//    would miss those. The sentinel marker emitted here is stable
	//    under the remaining passes.
	s = escapeInjectionPatterns(s)

	// 2. Triple-backticks anywhere in the string. The replacement contains a
	//    backtick so running this escape again is a no-op (the replacement
	//    contains no substring "```").
	s = strings.ReplaceAll(s, "```", escapedTripleBacktick)

	// 3. Line-start "---" (YAML frontmatter delimiter / markdown horizontal
	//    rule). We scan line-by-line so "a---b" inside prose is untouched,
	//    but "---\nfrontmatter:" at column 0 becomes "- - -\nfrontmatter:".
	//    The replacement contains dashes but never a literal "---" triple
	//    so re-running the escape is a no-op.
	s = escapeLineStartTripleDash(s)

	return s
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// escapeLineStartTripleDash replaces any line that begins with exactly "---"
// (optionally followed by EOL or whitespace) with "- - -".
//
// We match:
//   - start-of-string + "---"
//   - "\n---"
//
// We do NOT match "---" mid-line (that's not a frontmatter delimiter and we
// want to preserve legitimate prose like "what---really").
func escapeLineStartTripleDash(s string) string {
	// Fast path: no "---" at all.
	if !strings.Contains(s, "---") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 8)

	// Iterate line by line, preserving the original line endings.
	i := 0
	for i < len(s) {
		// Find end of current line.
		eol := strings.IndexByte(s[i:], '\n')
		var line string
		var sep string
		if eol < 0 {
			line = s[i:]
			sep = ""
			i = len(s)
		} else {
			line = s[i : i+eol]
			sep = "\n"
			i = i + eol + 1
		}

		// If the line starts with exactly "---", rewrite it.
		if strings.HasPrefix(line, "---") {
			// Only rewrite when the three dashes are the start of either
			// a bare frontmatter delimiter ("---" exactly) or a horizontal
			// rule ("---" followed by more dashes or whitespace). Inside
			// a word like "---foo" we still rewrite because that is a
			// strange construct and the visible "- - -foo" still conveys
			// the original intent to a human reader.
			rest := line[3:]
			// Consume any additional dashes — "---------" still reads as a
			// frontmatter / HR delimiter.
			for strings.HasPrefix(rest, "-") {
				rest = rest[1:]
			}
			b.WriteString(escapedTripleDash)
			b.WriteString(rest)
		} else {
			b.WriteString(line)
		}
		b.WriteString(sep)
	}
	return b.String()
}

// escapeInjectionPatterns walks the injection list and neutralises any
// case-insensitive match. Original casing is preserved in the output so the
// reader can see what the input was.
//
// Idempotency: the replacement prefix "[WUPHF-ESCAPED] " does not match any
// injection pattern, and disruptTokens inserts a ZWSP every three letters
// inside the wrapped span, so on the second pass the previously-escaped
// pattern no longer appears as a contiguous substring and the wrap is not
// re-applied. We deliberately do NOT short-circuit on a pre-existing
// sentinel prefix — doing so would let an attacker prepend
// "[WUPHF-ESCAPED] " to their payload and smuggle the raw injection past
// the escaper.
func escapeInjectionPatterns(s string) string {
	// No fast-path on hasLetter: injectionPatterns now includes
	// letter-free sequences (ANSI CSI introducers "\x1b[" / "\033[") so
	// purely non-letter input can still match.
	if s == "" {
		return s
	}

	lower := strings.ToLower(s)

	// Walk every pattern. For each match we insert the prefix + a ZWSP
	// between every word so the LLM cannot parse it as the same
	// instruction. We do this in a single pass over s to avoid O(n²)
	// rewriting across patterns.
	//
	// Build a set of [start, end, replacement] intervals, then splice.
	var spans []escapeSpan

	for _, pat := range injectionPatterns {
		if len(pat) == 0 {
			continue
		}
		searchFrom := 0
		for searchFrom < len(lower) {
			idx := strings.Index(lower[searchFrom:], pat)
			if idx < 0 {
				break
			}
			start := searchFrom + idx
			end := start + len(pat)

			// NOTE: no idempotency short-circuit here. A prior version
			// skipped escaping when the match was already preceded by
			// injectionEscapePrefix, reasoning "already neutralised on a
			// prior pass." That was exploitable: an attacker who knows the
			// sentinel could prepend "[WUPHF-ESCAPED] " to their payload
			// and the guard would pass the raw injection phrase through.
			// The correct trade is to wrap every match, accepting harmless
			// double-wrapping on legitimate re-escape paths.
			original := s[start:end]
			spans = append(spans, escapeSpan{
				start: start,
				end:   end,
				out:   injectionEscapePrefix + disruptTokens(original),
			})
			searchFrom = end
		}
	}

	if len(spans) == 0 {
		return s
	}

	// Resolve overlapping spans: sort by start, keep the earliest; drop
	// anything that overlaps with an already-kept span.
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	kept := spans[:0]
	lastEnd := -1
	for _, sp := range spans {
		if sp.start < lastEnd {
			continue
		}
		kept = append(kept, sp)
		lastEnd = sp.end
	}

	var b strings.Builder
	b.Grow(len(s) + len(kept)*len(injectionEscapePrefix))
	cursor := 0
	for _, sp := range kept {
		b.WriteString(s[cursor:sp.start])
		b.WriteString(sp.out)
		cursor = sp.end
	}
	b.WriteString(s[cursor:])
	return b.String()
}

// disruptTokens inserts a zero-width-space inside each run of letters so the
// LLM tokeniser cannot reassemble the original instruction as a single span.
// Combined with the visible "[WUPHF-ESCAPED] " prefix emitted by the caller,
// this guarantees the string visibly differs from the original and is
// unlikely to parse as a top-level instruction.
func disruptTokens(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	letterRun := 0
	for _, r := range s {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if isLetter {
			letterRun++
			// Insert a ZWSP after every 3rd consecutive letter so long
			// words break without becoming unreadable.
			if letterRun == 3 {
				b.WriteRune(r)
				b.WriteRune('\u200b') // U+200B zero-width-space
				letterRun = 0
				continue
			}
		} else {
			letterRun = 0
		}
		b.WriteRune(r)
	}
	return b.String()
}
