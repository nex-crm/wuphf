package teammcp

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/nex-crm/wuphf/internal/team"
)

func promotionHintsForNotes(entries []brokerMemoryNote) []string {
	hints := make([]string, 0, 2)
	for _, entry := range entries {
		reason := durablePrivateMemoryReason(entry)
		if reason == "" {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			title = strings.TrimSpace(entry.Key)
		}
		hints = append(hints, fmt.Sprintf("- %s (%s) looks like a durable %s. If the whole workspace should rely on it, run team_memory_promote key=%s.", title, entry.Key, reason, entry.Key))
		if len(hints) >= 2 {
			break
		}
	}
	return hints
}

func durablePrivateMemoryReason(note brokerMemoryNote) string {
	text := normalizeMemoryHintText(strings.Join([]string{note.Title, note.Content}, "\n"))
	if text == "" {
		return ""
	}
	switch {
	case containsAnyPhrase(text, "playbook", "runbook", "checklist", "standard operating procedure", "sop"):
		return "playbook"
	case containsAnyPhrase(text, "approved", "final decision", "decided", "decision", "canonical", "source of truth", "locked in"):
		return "decision"
	case containsAnyPhrase(text, "preference", "prefers", "always use", "never use"):
		return "preference"
	case containsAnyPhrase(text, "handoff", "hand off", "handover", "owner", "deadline", "eta", "ship date", "due date", "next step"):
		return "handoff"
	default:
		return ""
	}
}

func sharedMemoryRoutingHints(mySlug string, hits []team.ScopedMemoryHit, office brokerOfficeMembersResponse) []string {
	allowed := make(map[string]struct{}, len(office.Members))
	for _, member := range office.Members {
		slug := strings.TrimSpace(member.Slug)
		if slug != "" {
			allowed[slug] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	candidates := map[string]struct{}{}
	for _, hit := range hits {
		if slug := strings.TrimSpace(hit.OwnerSlug); slug != "" {
			if _, ok := allowed[slug]; ok && slug != mySlug {
				candidates[slug] = struct{}{}
			}
		}
		text := strings.TrimSpace(hit.Title + "\n" + hit.Snippet)
		for _, slug := range extractKnownMentionSlugs(text, allowed) {
			if slug != mySlug {
				candidates[slug] = struct{}{}
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	slugs := make([]string, 0, len(candidates))
	for slug := range candidates {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	mentions := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		mentions = append(mentions, "@"+slug)
	}
	return []string{
		fmt.Sprintf("- Shared memory points to %s. If you need fresher working context, ask in the office instead of guessing; private notes stay private.", strings.Join(mentions, ", ")),
	}
}

func extractKnownMentionSlugs(text string, allowed map[string]struct{}) []string {
	text = strings.TrimSpace(text)
	if text == "" || len(allowed) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		var b strings.Builder
		for j := i + 1; j < len(runes); j++ {
			r := runes[j]
			if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' {
				b.WriteRune(unicode.ToLower(r))
				continue
			}
			break
		}
		slug := strings.TrimSpace(b.String())
		if slug == "" {
			continue
		}
		if _, ok := allowed[slug]; !ok {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

func normalizeMemoryHintText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func containsAnyPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, normalizeMemoryHintText(phrase)) {
			return true
		}
	}
	return false
}
