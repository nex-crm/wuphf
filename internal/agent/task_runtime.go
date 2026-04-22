package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	taskLogRootEnv          = "WUPHF_TASK_LOG_ROOT"
	compactionTokenLimitEnv = "WUPHF_COMPACTION_TOKEN_LIMIT"
	defaultTokenLimit       = 16000
	// CEO routes work for the whole office and burns through context faster
	// than any specialist, so it gets a much larger working window before the
	// loop archives older turns into an Office Insight.
	ceoSlug         = "ceo"
	ceoTokenLimit   = 200000
	compactionRatio = 0.8
)

func defaultTaskLogRoot() string {
	if root := strings.TrimSpace(os.Getenv(taskLogRootEnv)); root != "" {
		return root
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "office", "tasks")
	}
	return filepath.Join(home, ".wuphf", "office", "tasks")
}

func nextTaskID(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "task"
	}
	return fmt.Sprintf("%s-%d", slug, time.Now().UnixMilli())
}

func compactionTokenLimit(slug string) int {
	if raw := strings.TrimSpace(os.Getenv(compactionTokenLimitEnv)); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err == nil && limit > 0 {
			return limit
		}
	}
	if slug == ceoSlug {
		return ceoTokenLimit
	}
	return defaultTokenLimit
}

func estimateSessionTokens(entries []SessionEntry) int {
	total := 0
	for _, entry := range entries {
		total += estimateTextTokens(entry.Content)
	}
	return total
}

func estimateTextTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

func shouldCompactEntries(entries []SessionEntry, slug string) bool {
	if len(entries) < 8 {
		return false
	}
	trigger := int(float64(compactionTokenLimit(slug)) * compactionRatio)
	return estimateSessionTokens(entries) >= trigger
}

func splitEntriesForCompaction(entries []SessionEntry) (prefix, archived, recent []SessionEntry) {
	pivot := 0
	for pivot < len(entries) && entries[pivot].Type == "system" {
		pivot++
	}
	prefix = append(prefix, entries[:pivot]...)
	body := entries[pivot:]
	if len(body) < 2 {
		return prefix, nil, body
	}

	mid := len(body) / 2
	archived = append(archived, body[:mid]...)
	recent = append(recent, body[mid:]...)
	return prefix, archived, recent
}

func buildOfficeInsightSummary(entries []SessionEntry) string {
	lines := make([]string, 0, 8)
	for _, entry := range entries {
		if entry.Metadata != nil {
			if officeInsight, ok := entry.Metadata["officeInsight"].(bool); ok && officeInsight {
				continue
			}
		}

		text := strings.TrimSpace(entry.Content)
		if text == "" {
			continue
		}

		lines = append(lines, fmt.Sprintf("- %s: %s", compactionLabel(entry.Type), truncateRuntimeText(text, 160)))
		if len(lines) >= 8 {
			break
		}
	}

	if len(lines) == 0 {
		return ""
	}

	return "Office Insight\n" +
		"Summarized archive of older context so the active thread can stay within the working window.\n" +
		strings.Join(lines, "\n")
}

func compactionLabel(entryType string) string {
	switch entryType {
	case "user":
		return "Request"
	case "assistant":
		return "Response"
	case "tool_call":
		return "Tool"
	case "tool_result":
		return "Tool Result"
	default:
		return "Context"
	}
}

func truncateRuntimeText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}

	runes := []rune(text)
	cut := strings.TrimSpace(string(runes[:limit]))
	return cut + "..."
}
