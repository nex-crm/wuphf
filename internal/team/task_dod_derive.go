package team

// task_dod_derive.go — deterministic DoD→verification derivation at intake
// (done-integrity fix family; ICP-eval v2 [01:22]/[01:36]: Sam's verbatim
// "a file landing/index.html exists … Don't tell me it's done unless that
// check passes" was never encoded as machine verification, and the CEO
// claimed done in 40 seconds with no check).
//
// When a human states an explicitly machine-checkable definition of done in
// the task text, the broker encodes it as a required TaskVerification at the
// moment the task is created/defined — the harness, not the model's
// diligence, carries the human's check. The pattern set is deliberately
// SMALL and conservative: false negatives are fine (the CEO's intake
// contract still mandates passing verification_kind/spec on define), false
// positives are not, so only unambiguous file-exists / file-contains
// phrasing and backtick-quoted commands that follow an explicit DoD cue
// ever fire.
//
// Trust model: derivation runs only on task-creation details and on
// definition success criteria — both are CEO/human-authored surfaces
// (checkTaskActionAuthLocked restricts create/define for specialists), the
// same trust class task_verification.go documents for hand-written specs.

import (
	"fmt"
	"regexp"
	"strings"
)

// dodCuePattern matches the explicit "this is my definition of done"
// phrasings. Free-form details only derive a check when one of these cues
// is present; definition success criteria are check statements by
// construction and skip the cue requirement.
var dodCuePattern = regexp.MustCompile(`(?i)\bdefinition of done\b|\bdod\b|\bdon'?t tell me (?:it'?s|this is) done unless\b|\bdo not tell me (?:it'?s|this is) done unless\b`)

// dodFileContainsPattern matches "file <path> exists and contains <quoted
// text>". The contained text must be quoted (double, single, or backtick) —
// unquoted prose after "contains" is too ambiguous to turn into a grep.
var dodFileContainsPattern = regexp.MustCompile("(?i)\\bfile\\s+`?([A-Za-z0-9._/-]+\\.[A-Za-z0-9]+)`?\\s+exists\\s+and\\s+contains\\s+(?:the\\s+text\\s+)?(?:\"([^\"\n]+)\"|'([^'\n]+)'|`([^`\n]+)`)")

// dodFileExistsPattern matches the bare "file <path> exists" check. The
// path charset is restricted to shell-safe characters (no spaces, no
// quotes) and must carry an extension so prose like "the file system
// exists" can never match.
var dodFileExistsPattern = regexp.MustCompile("(?i)\\bfile\\s+`?([A-Za-z0-9._/-]+\\.[A-Za-z0-9]+)`?\\s+exists\\b")

// dodBacktickCommandPattern matches a backtick-quoted shell command that
// follows a DoD cue within a short window. Only consulted when no file
// pattern matched (a backticked file PATH inside the file phrasing must
// not be mistaken for a command).
var dodBacktickCommandPattern = regexp.MustCompile("(?is)(?:definition of done|\\bdod\\b|don'?t tell me (?:it'?s|this is) done unless|do not tell me (?:it'?s|this is) done unless)[^`]{0,160}`([^`\n]{2,200})`")

// dodSafeContainsLiteral reports whether the quoted text can be embedded in
// a single-quoted grep argument without escaping surprises.
func dodSafeContainsLiteral(s string) bool {
	return s != "" && !strings.ContainsAny(s, "'\\")
}

// deriveDoDVerificationFromText inspects free text for an explicit,
// machine-checkable definition of done and returns the derived required
// command check, or nil when nothing unambiguous is found. requireCue gates
// derivation on a DoD cue being present (used for free-form details);
// success criteria pass requireCue=false.
func deriveDoDVerificationFromText(text string, requireCue bool) *TaskVerification {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if requireCue && !dodCuePattern.MatchString(text) {
		return nil
	}
	if m := dodFileContainsPattern.FindStringSubmatch(text); m != nil {
		path := m[1]
		contains := m[2]
		if contains == "" {
			contains = m[3]
		}
		if contains == "" {
			contains = m[4]
		}
		if dodSafeContainsLiteral(contains) {
			return &TaskVerification{
				Kind:     taskVerificationKindCommand,
				Spec:     fmt.Sprintf("test -f '%s' && grep -qF -e '%s' '%s'", path, contains, path),
				Required: true,
			}
		}
		// Unsafe contained text: fall through to the exists-only check —
		// a narrower gate beats no gate, and beats a broken grep.
	}
	if m := dodFileExistsPattern.FindStringSubmatch(text); m != nil {
		return &TaskVerification{
			Kind:     taskVerificationKindCommand,
			Spec:     fmt.Sprintf("test -f '%s'", m[1]),
			Required: true,
		}
	}
	// Backtick-quoted command directly following a DoD cue. Require a space
	// so a backticked file path (`landing/index.html`) is never adopted as
	// a "command".
	if requireCue {
		if m := dodBacktickCommandPattern.FindStringSubmatch(text); m != nil {
			cmd := strings.TrimSpace(m[1])
			if strings.ContainsRune(cmd, ' ') {
				return &TaskVerification{Kind: taskVerificationKindCommand, Spec: cmd, Required: true}
			}
		}
	}
	return nil
}

// deriveTaskVerificationFromDetails is the task-creation hook: derive a
// required check from the create call's details when the human stated an
// explicit DoD there. Nil when nothing unambiguous is present.
func deriveTaskVerificationFromDetails(details string) *TaskVerification {
	return deriveDoDVerificationFromText(details, true)
}

// deriveTaskVerificationFromDefinition is the define-action hook: derive a
// required check from the structured success criteria. Criteria are check
// statements by construction, so the file patterns fire without a DoD cue;
// backtick commands still require one (and so never fire here).
func deriveTaskVerificationFromDefinition(def *TaskDefinition) *TaskVerification {
	if def == nil {
		return nil
	}
	for _, c := range def.SuccessCriteria {
		if v := deriveDoDVerificationFromText(c, false); v != nil {
			return v
		}
	}
	return nil
}
