package orchestration

import (
	"fmt"
	"regexp"
	"strings"
)

// Delegation represents a sub-task extracted from Team-Lead output.
type Delegation struct {
	AgentSlug string
	Task      string // The sentence context around the @mention
}

// Delegator parses Team-Lead responses and extracts specialist delegations.
type Delegator struct {
	maxConcurrent int
	mentionRe     *regexp.Regexp
}

// NewDelegator creates a delegator with the given concurrency limit.
func NewDelegator(maxConcurrent int) *Delegator {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	return &Delegator{
		maxConcurrent: maxConcurrent,
		mentionRe:     regexp.MustCompile(`@([a-z][a-z0-9-]*)`),
	}
}

// ExtractDelegations parses the Team-Lead response for @agent-slug mentions
// and extracts the surrounding sentence as the task description.
// Only mentions matching knownSlugs are returned.
func (d *Delegator) ExtractDelegations(response string, knownSlugs []string) []Delegation {
	known := make(map[string]bool, len(knownSlugs))
	for _, s := range knownSlugs {
		known[s] = true
	}

	matches := d.mentionRe.FindAllStringSubmatchIndex(response, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var delegations []Delegation

	for _, match := range matches {
		slug := response[match[2]:match[3]]
		if !known[slug] || seen[slug] {
			continue
		}
		seen[slug] = true

		sentence := extractSentence(response, match[0])
		task := strings.TrimSpace(sentence)

		delegations = append(delegations, Delegation{
			AgentSlug: slug,
			Task:      task,
		})
	}

	return delegations
}

// ApplyLimit splits delegations into immediate (up to maxConcurrent) and queued.
func (d *Delegator) ApplyLimit(delegations []Delegation) (immediate, queued []Delegation) {
	if len(delegations) <= d.maxConcurrent {
		return delegations, nil
	}
	return delegations[:d.maxConcurrent], delegations[d.maxConcurrent:]
}

// FormatSteerMessage formats a delegation as a steer message for the specialist.
func FormatSteerMessage(d Delegation) string {
	return fmt.Sprintf("[TEAM-LEAD DELEGATION] %s", d.Task)
}

// extractSentence finds the sentence containing the position pos.
// Sentences are delimited by periods, newlines, or string boundaries.
func extractSentence(text string, pos int) string {
	start := 0
	for i := pos - 1; i >= 0; i-- {
		if text[i] == '.' || text[i] == '\n' {
			start = i + 1
			break
		}
	}

	end := len(text)
	for i := pos; i < len(text); i++ {
		if text[i] == '.' || text[i] == '\n' {
			end = i + 1
			break
		}
	}

	return text[start:end]
}
