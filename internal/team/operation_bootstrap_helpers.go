package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/operations"
)

func normalizeOperationIntegrationKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(value)
}

func operationSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "&", " and ")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func operationFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func operationRenderTemplateString(value string, replacements map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(replacements) == 0 {
		return value
	}
	args := make([]string, 0, len(replacements)*2)
	for key, replacement := range replacements {
		args = append(args, "{{"+key+"}}", replacement)
	}
	return strings.NewReplacer(args...).Replace(value)
}

func cloneOperationMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func operationAutomationApprovalSummary(rules []operations.ApprovalRule) string {
	if len(rules) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		if desc := strings.TrimSpace(rule.Description); desc != "" {
			parts = append(parts, desc)
			continue
		}
		if trigger := strings.TrimSpace(rule.Trigger); trigger != "" {
			parts = append(parts, trigger)
		}
	}
	return strings.Join(parts, "; ")
}
