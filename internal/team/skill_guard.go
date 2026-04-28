package team

// skill_guard.go implements the safety scanner that gates compiled skills before
// they are written to the wiki. Ported from Hermes' tools/skills_guard.py with
// WUPHF-specific trust ladder semantics:
//
//	  builtin / trusted: allow safe + caution + dangerous (logged only)
//	  community (Stage A wiki):  allow safe + caution-with-warning, REJECT dangerous
//	  agent_created (Stage B):  allow safe ONLY, REJECT caution + dangerous
//
// ScanSkill emits a verdict and a list of findings. The trust-ladder gate is
// applied by the caller (writeSkillProposalLocked), so the same scan can be
// re-used for runtime audits without forcing rejection semantics here.

import (
	"fmt"
	"regexp"
	"strings"
)

// GuardVerdict captures the highest-severity finding from a scan.
type GuardVerdict string

const (
	// VerdictSafe means no findings were detected.
	VerdictSafe GuardVerdict = "safe"
	// VerdictCaution means at least one cautionary pattern matched.
	VerdictCaution GuardVerdict = "caution"
	// VerdictDangerous means at least one dangerous pattern matched.
	VerdictDangerous GuardVerdict = "dangerous"
)

// GuardTrustLevel marks how much we trust the source of a skill body. The
// caller maps this onto the scan verdict to decide allow / reject.
type GuardTrustLevel string

const (
	// TrustBuiltin is reserved for skills shipped with the binary.
	TrustBuiltin GuardTrustLevel = "builtin"
	// TrustTrusted is for explicitly vetted skills (workspace admin authored).
	TrustTrusted GuardTrustLevel = "trusted"
	// TrustCommunity is the Stage A wiki source — humans wrote the article,
	// the LLM merely classified it.
	TrustCommunity GuardTrustLevel = "community"
	// TrustAgentCreated is the Stage B+ LLM-synth path. Treated stricter than
	// Hermes' policy because WUPHF agents can synthesize at scale.
	TrustAgentCreated GuardTrustLevel = "agent_created"
)

// GuardScanResult bundles the verdict, findings, trust level, and a short
// human-readable summary suitable for stamping into frontmatter or surfacing
// in the UI.
type GuardScanResult struct {
	Verdict    GuardVerdict
	TrustLevel GuardTrustLevel
	Findings   []string
	Summary    string
}

// Compiled regex patterns. Module-level so the cost is paid once.
var (
	// Dangerous body patterns.
	// Matches JS-style call syntax AND shell-style invocations such as
	// `eval "$(...)"`, `eval '...'`, `eval $(...)` , `eval ` + "`" + `...` + "`" + `. The
	// regex anchors on the keyword followed by an opening paren or any
	// quoting / command-substitution metacharacter, which catches every
	// dangerous shape seen in the corpus while still rejecting prose like
	// "evaluate the result".
	guardEvalRe = regexp.MustCompile("\\beval\\s*[(\"'$`]")
	// curl|sh / wget|sh: match a pipe into any common POSIX shell, not just
	// the literal token "sh". Adds coverage for real-world  patterns piping
	// directly into bash/zsh that previously slipped past the guard.
	guardCurlShRe = regexp.MustCompile(`\bcurl\s+[^\n]*\|\s*(?:sh|bash|zsh|ksh|dash|fish)\b`)
	guardWgetShRe = regexp.MustCompile(`\bwget\s+[^\n]*\|\s*(?:sh|bash|zsh|ksh|dash|fish)\b`)
	guardRmRfRe   = regexp.MustCompile(`\brm\s+-rf\s+/[^\s]*`)
	guardExecRe   = regexp.MustCompile(`\bexec\s*\(`)

	// Caution body patterns.
	guardSetupURLRe  = regexp.MustCompile(`(?i)(setup|install)[:\s]`)
	guardURLRe       = regexp.MustCompile(`https?://[^\s]+`)
	guardShellMetaRe = regexp.MustCompile(`[;|&$` + "`" + `]`)
	guardCodeFenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_-]*)\\n(.*?)```")
	guardSlugCheckRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// ScanSkill applies all guard rules to fm + body and returns a verdict and
// findings list. The caller decides allow / reject based on the trust ladder.
//
// Findings are accumulated in severity order: frontmatter integrity issues
// first (always dangerous), then body dangerous patterns, then caution
// patterns. Verdict is the highest severity seen.
func ScanSkill(fm SkillFrontmatter, body string, trust GuardTrustLevel) GuardScanResult {
	res := GuardScanResult{
		Verdict:    VerdictSafe,
		TrustLevel: trust,
		Findings:   nil,
	}

	// Frontmatter integrity (HARD REJECT).
	if strings.TrimSpace(fm.Name) == "" {
		res.Findings = append(res.Findings, "frontmatter: name is required")
		res.Verdict = VerdictDangerous
	}
	if strings.TrimSpace(fm.Description) == "" {
		res.Findings = append(res.Findings, "frontmatter: description is required")
		res.Verdict = VerdictDangerous
	}

	// Slug regex (caution).
	if name := strings.TrimSpace(fm.Name); name != "" && !guardSlugCheckRe.MatchString(name) {
		res.Findings = append(res.Findings, "frontmatter: name not a valid slug")
		if res.Verdict != VerdictDangerous {
			res.Verdict = VerdictCaution
		}
	}

	// Body content (HARD REJECT).
	if guardEvalRe.MatchString(body) {
		res.Findings = append(res.Findings, "body: eval() pattern detected")
		res.Verdict = VerdictDangerous
	}
	if guardCurlShRe.MatchString(body) {
		res.Findings = append(res.Findings, "body: curl|sh pattern detected")
		res.Verdict = VerdictDangerous
	}
	if guardWgetShRe.MatchString(body) {
		res.Findings = append(res.Findings, "body: wget|sh pattern detected")
		res.Verdict = VerdictDangerous
	}
	if guardRmRfRe.MatchString(body) {
		res.Findings = append(res.Findings, "body: rm -rf <abs path> detected")
		res.Verdict = VerdictDangerous
	}
	if guardExecRe.MatchString(body) {
		res.Findings = append(res.Findings, "body: exec() pattern detected")
		res.Verdict = VerdictDangerous
	}

	// Body content (caution): shell metacharacters in non-bash code blocks.
	for _, m := range guardCodeFenceRe.FindAllStringSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		lang := strings.TrimSpace(strings.ToLower(m[1]))
		blockBody := m[2]
		if lang == "" || lang == "bash" || lang == "sh" || lang == "shell" {
			continue
		}
		if guardShellMetaRe.MatchString(blockBody) {
			res.Findings = append(res.Findings, "body: shell metas in non-bash code block")
			if res.Verdict != VerdictDangerous {
				res.Verdict = VerdictCaution
			}
			break
		}
	}

	// External URL near Setup:/Install: heuristic (caution).
	if hasExternalURLNearSetup(body) {
		res.Findings = append(res.Findings, "body: external URL near Setup/Install block")
		if res.Verdict != VerdictDangerous {
			res.Verdict = VerdictCaution
		}
	}

	res.Summary = summarizeGuard(res.Verdict, res.Findings)
	return res
}

// hasExternalURLNearSetup returns true if a line containing "Setup:" or
// "Install:" (case-insensitive) is followed within the next two lines by an
// http(s) URL.
func hasExternalURLNearSetup(body string) bool {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if !guardSetupURLRe.MatchString(ln) {
			continue
		}
		end := i + 3
		if end > len(lines) {
			end = len(lines)
		}
		for j := i; j < end; j++ {
			if guardURLRe.MatchString(lines[j]) {
				return true
			}
		}
	}
	return false
}

// summarizeGuard produces a short human-readable string for the SafetyScan
// frontmatter field and tooltips.
func summarizeGuard(verdict GuardVerdict, findings []string) string {
	if len(findings) == 0 {
		return string(verdict) + " (no findings)"
	}
	tags := make([]string, 0, len(findings))
	for _, f := range findings {
		idx := strings.Index(f, ":")
		if idx > 0 {
			tags = append(tags, strings.TrimSpace(f[idx+1:]))
		} else {
			tags = append(tags, f)
		}
	}
	return fmt.Sprintf("%s: %d %s (%s)",
		verdict,
		len(findings),
		pluralize("finding", len(findings)),
		strings.Join(tags, ", "),
	)
}

func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
