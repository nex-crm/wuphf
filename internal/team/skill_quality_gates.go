package team

// skill_quality_gates.go is the compiler's quality layer (core-loop R5).
// These gates used to live in the Stage B synthesizer provider; the
// synthesizer is gone (skills are created ONLY by playbook compilation),
// but the SkillOpt-derived mechanics survive and now guard the compile
// path itself:
//
//   - bloat gates: soft warn above the SkillOpt median, hard reject at 2×
//     the median for machine-compiled bodies;
//   - depth gate: new machine-compiled skills must carry real procedure
//     (minimum body length + enumerated steps inside a procedure section);
//   - update-first threshold: compilation prefers UPDATING an existing
//     skill over creating a new one, as long as the merged body stays
//     under the token-size threshold (spec core-loop step 7.3).
//
// The protected-invariant gate (INVARIANT-START/END) lives next to the
// enhance/rename helpers in skill_compile_writer.go.

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// skillCompactnessSoftLimitBytes is the warn threshold on compiled skill
	// bodies. SkillOpt's median final skill is ~920 tokens (~5-6KB), and
	// length-as-effort is a known LLM failure mode — bodies above this are
	// accepted but logged so bloat is visible.
	skillCompactnessSoftLimitBytes = 6 * 1024

	// skillUpdateFirstMaxBytes is the file-size (token) threshold from the
	// core-loop spec: compilation always prefers updating an existing skill
	// over creating a new one — even when that grows the skill's scope — as
	// long as the merged body stays under this cap. It doubles as the hard
	// bloat-rejection cap for new machine-compiled bodies. Set at 2× the
	// SkillOpt median (~5-6KB): above this the body is almost always
	// length-as-effort rather than load-bearing procedure.
	skillUpdateFirstMaxBytes = 2 * skillCompactnessSoftLimitBytes

	// skillNewBodyMinBytes is the depth floor for new machine-compiled skill
	// bodies. Below this the "skill" is almost always a two-line how-to that
	// should have stayed prose in the playbook article.
	skillNewBodyMinBytes = 400

	// skillNewBodyMinSteps is the minimum number of enumerated steps a new
	// machine-compiled skill body must carry inside a procedure section.
	skillNewBodyMinSteps = 3
)

// errSkillEnhanceTooLarge signals that an update-first merge would push the
// existing skill past skillUpdateFirstMaxBytes. Callers fall back to
// creating a new skill instead of growing the existing one further.
var errSkillEnhanceTooLarge = errors.New("merged skill body exceeds the update-first size threshold")

// procedureSectionHeadings names the body sections where enumerated
// procedure steps are expected to live. Lines outside these sections —
// `## Inputs`, `## Output`, `## Examples`, etc. — are NOT counted toward
// the depth gate's step minimum, otherwise a shallow `## Steps` block
// could be padded to pass via bullets in other sections.
var procedureSectionHeadings = []string{"## Steps", "## How to"}

// enforceSkillDepthGate rejects machine-compiled skill bodies that are too
// shallow (or too bloated) to be worth codifying. Applied to NEW skills the
// compiler's LLM classifier emits — explicit human-authored playbook
// frontmatter and enhance merges (bounded additions to an existing body)
// are exempt.
func enforceSkillDepthGate(body string) error {
	trimmed := strings.TrimSpace(body)
	if len(trimmed) < skillNewBodyMinBytes {
		return fmt.Errorf("body too shallow (%d < %d bytes — new skills need substantive procedure, not a snippet)",
			len(trimmed), skillNewBodyMinBytes)
	}
	if len(trimmed) > skillUpdateFirstMaxBytes {
		return fmt.Errorf("body too bloated (%d > %d bytes — skills should be compact; SkillOpt median is ~5-6KB)",
			len(trimmed), skillUpdateFirstMaxBytes)
	}
	if steps := countEnumeratedStepsInSections(body, procedureSectionHeadings); steps < skillNewBodyMinSteps {
		return fmt.Errorf("body has %d enumerated steps inside %v, need at least %d for a new skill (steps outside these sections do not count)",
			steps, procedureSectionHeadings, skillNewBodyMinSteps)
	}
	return nil
}

// countEnumeratedStepsInSections counts enumerated lines that live INSIDE
// any of the named `## ` sections. Lines in other sections (or at the top
// of the body before any heading) are not counted.
func countEnumeratedStepsInSections(body string, sections []string) int {
	targets := make(map[string]bool, len(sections))
	for _, s := range sections {
		targets[strings.TrimSpace(s)] = true
	}
	current := ""
	n := 0
	for _, line := range strings.Split(body, "\n") {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "## ") {
			current = trimmedLine
			continue
		}
		if !targets[current] {
			continue
		}
		if isEnumeratedStepLine(line) {
			n++
		}
	}
	return n
}

// isEnumeratedStepLine reports whether line looks like a single step
// entry — a leading "1." / "2." style number, a "- " bullet, or a "* "
// bullet. Tolerates leading whitespace so nested lists inside a step
// block still count.
func isEnumeratedStepLine(line string) bool {
	trim := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
		return true
	}
	// Numbered: "1." through "999." — we only need to recognise the
	// shape, not parse the number.
	if len(trim) >= 2 && trim[0] >= '0' && trim[0] <= '9' {
		rest := trim[1:]
		// Allow up to two more digits.
		for i := 0; i < 2 && len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9'; i++ {
			rest = rest[1:]
		}
		if strings.HasPrefix(rest, ". ") || strings.HasPrefix(rest, ".\t") {
			return true
		}
	}
	return false
}
