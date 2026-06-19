package team

// custom_app_guard.go — the deterministic App Builder efficiency harness.
//
// AI_RULES.md TELLS the App Builder not to ship wasteful interactions, but
// guidance is advisory: an agent ignored it and shipped a Gmail digest that
// re-fetched 25 emails and re-ran an ai() summary on EVERY browser-tab refocus —
// exactly the pattern that endangers integration rate limits and burns LLM
// tokens. This guard makes the rule ENFORCED. It runs inside the host-owned
// publish build (resolvePublishHTML, which the agent cannot bypass — the host
// already overwrites the protected files and builds server-side) and REJECTS a
// publish whose source contains the canonical waste patterns, so a token-burner
// can never reach the sealed bundle.
//
// Properties:
//   - DETERMINISTIC: pure text rules, no model judgment, same input → same verdict.
//   - HIGH-PRECISION: the flagged shapes (tab focus/visibility reactions, sub-30s
//     polling) are essentially never legitimate in a sealed, single-screen tool.
//     A real refresh cadence (a daily timer) uses a COMPUTED delay and is never
//     flagged; a setInterval with a non-literal period is left alone.
//   - "UNLESS ASKED FOR" is an explicit, auditable opt-in: the agent puts a
//     `wuphf-allow:` marker on the offending line (or the line directly above),
//     recording on the record that the human requested this cadence. The runtime
//     per-app budget (broker_apps_integrations.go) is the defense-in-depth
//     backstop for anything these static rules don't catch.

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// appGuardPollFloorMs is the minimum setInterval period an app may use without
	// an explicit opt-in. A sealed internal tool that polls faster than this is the
	// wasteful pattern; a deliberate refresh (a daily timer) uses a computed delay
	// and is never flagged.
	appGuardPollFloorMs = 30000

	// wuphfAllowMarker is the deterministic opt-in. Placed on the offending line or
	// the line directly above, it records that the human asked for this cadence and
	// suppresses the violation, e.g.
	//   // wuphf-allow: poll — user asked for a 10s live ticker
	wuphfAllowMarker = "wuphf-allow"
)

// appGuardViolation is one rejected pattern: where it is and why.
type appGuardViolation struct {
	File    string
	Line    int
	Rule    string
	Message string
}

const (
	appGuardFocusMsg = "re-runs work when the browser tab regains focus. A visibilitychange / window focus|blur|pageshow listener fires every time the user switches tabs, so the app re-fetches data and re-runs ai() — burning integration rate limits and LLM tokens. Load once on mount and refresh only on an explicit user action (a button) or a deliberate schedule (a timer with a computed delay). If the human explicitly asked for focus-triggered refresh, put `// wuphf-allow: focus-refresh — <why>` on the listener line."
	appGuardPollMsg  = "polls faster than 30s with setInterval. In a sealed internal tool a tight poll hammers integration rate limits and LLM tokens. Use a longer cadence, a computed-delay timer, or refresh on user action. If the human asked for a fast live refresh, put `// wuphf-allow: poll — <why>` on the setInterval line."
)

var (
	// visibilitychange only ever fires at the tab/document level, so flag any
	// listener or handler for it regardless of the receiver.
	reVisibilityListener = regexp.MustCompile("addEventListener\\s*\\(\\s*[\"'`]visibilitychange[\"'`]")
	reVisibilityHandler  = regexp.MustCompile(`\bon(visibilitychange|pageshow|pagehide|freeze|resume)\s*=`)
	// focus/blur/pageshow/pagehide are tab-level ONLY on window/document; an input
	// element's focus listener is legitimate UI, so require that receiver here.
	reWindowFocusListener = regexp.MustCompile("\\b(window|document|globalThis)\\s*\\.\\s*addEventListener\\s*\\(\\s*[\"'`](focus|blur|pageshow|pagehide)[\"'`]")
	reWindowFocusHandler  = regexp.MustCompile(`\b(window|document|globalThis)\s*\.\s*on(focus|blur)\s*=`)
	reSetInterval         = regexp.MustCompile(`\bsetInterval\s*\(`)
)

// checkAppSourceEfficiency scans the agent-authored source files and returns the
// waste-pattern violations (deterministic, stable order). An empty result means
// the app is clean to publish. Protected host-contract files and build artifacts
// are skipped — the host owns those, and they are overwritten with canonical
// bytes anyway.
func checkAppSourceEfficiency(files map[string]string) []appGuardViolation {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []appGuardViolation
	for _, rel := range keys {
		if !appGuardShouldScan(rel) {
			continue
		}
		out = append(out, scanAppFile(rel, files[rel])...)
	}
	return out
}

// appGuardShouldScan reports whether a project-relative path is agent-authored
// source the guard should lint: a .ts/.tsx/.js/.jsx/.mjs/.cjs file, not a type
// declaration, not a protected host-contract file, and not under a build/VCS dir.
func appGuardShouldScan(rel string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	clean := path.Clean(strings.TrimPrefix(rel, "./"))
	if _, protected := customAppProtectedFiles[clean]; protected {
		return false
	}
	for _, seg := range strings.Split(clean, "/") {
		switch seg {
		case "node_modules", "dist", ".vite", ".git":
			return false
		}
	}
	lower := strings.ToLower(clean) // extensions are matched case-insensitively
	if strings.HasSuffix(lower, ".d.ts") {
		return false
	}
	switch path.Ext(lower) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return true
	}
	return false
}

// scanAppFile applies the rule set line by line. For each line it builds a
// "code view" (comments removed) plus an in-string mask, so a pattern that lives
// ENTIRELY inside a string literal or comment ("see addEventListener('focus')…")
// never false-flags, while the event-name string inside a REAL call
// (addEventListener("visibilitychange")) is still matched. The raw lines are kept
// to honor the wuphf-allow opt-in marker, which lives in a comment.
func scanAppFile(rel, content string) []appGuardViolation {
	rawLines := strings.Split(content, "\n")
	codeLines := make([]string, len(rawLines))
	masks := make([][]bool, len(rawLines))
	var st lexState
	for i, raw := range rawLines {
		codeLines[i], masks[i], st = codeView(raw, st)
	}

	var out []appGuardViolation
	for i, code := range codeLines {
		if strings.TrimSpace(code) == "" {
			continue
		}
		for _, hit := range matchEfficiencyRules(code, masks[i], codeLines, i) {
			if appGuardSuppressed(rawLines, codeLines, i) {
				continue
			}
			out = append(out, appGuardViolation{
				File: rel, Line: i + 1, Rule: hit.rule, Message: hit.message,
			})
		}
	}
	return out
}

type ruleHit struct {
	rule    string
	message string
}

// matchEfficiencyRules returns the rule hits on one code-view line. Every regex
// match is gated by the in-string mask: a match that BEGINS inside a string
// literal is ignored (it's data, not code). The setInterval delay can span the
// call onto following lines, so it reads a small forward window of code-view text.
func matchEfficiencyRules(code string, mask []bool, codeLines []string, idx int) []ruleHit {
	var hits []ruleHit
	focus := codeMatch(reVisibilityListener, code, mask) ||
		codeMatch(reVisibilityHandler, code, mask) ||
		codeMatch(reWindowFocusListener, code, mask) ||
		codeMatch(reWindowFocusHandler, code, mask)
	if focus {
		hits = append(hits, ruleHit{rule: "no-focus-refetch", message: appGuardFocusMsg})
	}
	if codeMatch(reSetInterval, code, mask) {
		// Scan a generous forward window so a long inline callback doesn't push the
		// delay literal out of view; the depth scanner stops at the matching ')'.
		window := strings.Join(codeLines[idx:minInt(idx+40, len(codeLines))], "\n")
		if delay, ok := setIntervalDelayLiteral(window); ok && delay < appGuardPollFloorMs {
			hits = append(hits, ruleHit{rule: "no-tight-poll", message: appGuardPollMsg})
		}
	}
	return hits
}

// codeMatch reports whether re matches code at a position that is NOT inside a
// string literal — i.e. the match is real code, not a pattern mentioned inside a
// quoted string. It checks every match so a code-context hit later in the line is
// not hidden by an earlier in-string mention.
func codeMatch(re *regexp.Regexp, code string, mask []bool) bool {
	for _, loc := range re.FindAllStringIndex(code, -1) {
		start := loc[0]
		if start >= 0 && start < len(mask) && mask[start] {
			continue // begins inside a string literal — ignore
		}
		return true
	}
	return false
}

// setIntervalDelayLiteral extracts the LAST non-empty top-level argument of the
// first setInterval(...) call in s and, when it is a plain integer literal (the
// poll period), returns it. A computed/variable delay returns ok=false so a
// legitimate dynamic cadence is never flagged. Paren/bracket/brace depth and
// string spans are tracked so a comma or paren inside the callback is not mistaken
// for the delay argument or the closing paren; a trailing comma
// (setInterval(fn, 5000,)) does not hide the literal.
func setIntervalDelayLiteral(s string) (int, bool) {
	loc := reSetInterval.FindStringIndex(s)
	if loc == nil {
		return 0, false
	}
	// Position the cursor at the '(' the regex matched (its last char).
	i := loc[1] - 1
	depth := 0
	segStart := i + 1
	var args []string
	var quote byte
	esc := false
	for ; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if c == ')' && depth == 0 {
				args = append(args, s[segStart:i])
				return lastLiteralDelay(args)
			}
		case ',':
			if depth == 1 {
				args = append(args, s[segStart:i])
				segStart = i + 1
			}
		}
	}
	return 0, false
}

// lastLiteralDelay returns the integer value of the last non-empty argument when
// it is a numeric literal — the setInterval period — tolerating a trailing comma.
func lastLiteralDelay(args []string) (int, bool) {
	for j := len(args) - 1; j >= 0; j-- {
		if strings.TrimSpace(args[j]) == "" {
			continue
		}
		return parseDelayLiteral(args[j])
	}
	return 0, false
}

// parseDelayLiteral returns the integer value of arg when it is a pure numeric
// literal (optionally with `_` digit separators), else ok=false.
func parseDelayLiteral(arg string) (int, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, false
	}
	digits := strings.ReplaceAll(arg, "_", "")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return n, true
}

// appGuardSuppressed reports whether the wuphf-allow opt-in marker appears IN A
// COMMENT on the violating line or the nearest non-blank line above it. Requiring
// a comment (not just any text) means a `wuphfAllowList` identifier or a string
// literal containing the marker cannot silence a real violation.
func appGuardSuppressed(rawLines, codeLines []string, idx int) bool {
	if markerInComment(rawLines, codeLines, idx) {
		return true
	}
	for j := idx - 1; j >= 0; j-- {
		if strings.TrimSpace(rawLines[j]) == "" {
			continue
		}
		return markerInComment(rawLines, codeLines, j)
	}
	return false
}

// markerInComment reports whether the opt-in marker is present on line idx AND
// stripped from its code view — i.e. it lives in a comment, not in code or a
// string literal (codeView keeps string contents, so a quoted marker stays in
// codeLines and is correctly NOT treated as an opt-in).
func markerInComment(rawLines, codeLines []string, idx int) bool {
	if idx < 0 || idx >= len(rawLines) {
		return false
	}
	if !strings.Contains(rawLines[idx], wuphfAllowMarker) {
		return false
	}
	if idx < len(codeLines) && strings.Contains(codeLines[idx], wuphfAllowMarker) {
		return false // appears in code/string, not a comment
	}
	return true
}

// lexState is the cross-line lexer state codeView threads: an open /* */ block
// comment and an open backtick template literal both continue onto the next line,
// so a guard pattern inside a multi-line template/comment is correctly masked.
type lexState struct {
	inBlock bool
	quote   byte
	esc     bool
}

// codeView returns line with // line comments and /* */ block comments removed,
// alongside a per-character mask marking which kept characters are INSIDE a string
// literal (content, not the delimiters). String contents are kept — the event
// name in addEventListener("visibilitychange") is a string we must still see — but
// the mask lets the rule matcher reject a pattern that lives entirely inside a
// quoted string. A "//" inside a string is not treated as a comment. The returned
// lexState carries open block-comment and open backtick-template state to the next
// line. A non-backtick quote left open at EOL is an unterminated string (illegal
// without a `\` continuation and a compile error anyway), so it is reset rather
// than masking the rest of the file.
func codeView(line string, st lexState) (string, []bool, lexState) {
	var b strings.Builder
	mask := make([]bool, 0, len(line))
	write := func(c byte, inString bool) {
		b.WriteByte(c)
		mask = append(mask, inString)
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if st.inBlock {
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				st.inBlock = false
				i++
			}
			continue
		}
		if st.quote != 0 {
			closing := !st.esc && c == st.quote
			write(c, !closing) // content is in-string; the closing delimiter is code
			switch {
			case st.esc:
				st.esc = false
			case c == '\\':
				st.esc = true
			case c == st.quote:
				st.quote = 0
			}
			continue
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			break // line comment: drop the rest
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '*' {
			st.inBlock = true
			i++
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			st.quote = c
			write(c, false) // opening delimiter is code
			continue
		}
		write(c, false)
	}
	// Only a backtick template legally carries to the next line; reset a dangling
	// '/' or " quote so one malformed line can't mask everything after it.
	if st.quote == '"' || st.quote == '\'' {
		st.quote = 0
		st.esc = false
	}
	return b.String(), mask, st
}

// appEfficiencyGuardError renders the violations into a single caller error
// (HTTP 400 upstream) the App Builder reads and fixes before republishing.
func appEfficiencyGuardError(violations []appGuardViolation) error {
	var b strings.Builder
	b.WriteString("app: publish blocked by the efficiency harness — fix these wasteful interactions and republish (they burn integration rate limits and LLM tokens):")
	for _, v := range violations {
		fmt.Fprintf(&b, "\n  • %s:%d [%s] %s", v.File, v.Line, v.Rule, v.Message)
	}
	return newCustomAppCallerError("%s", b.String())
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
