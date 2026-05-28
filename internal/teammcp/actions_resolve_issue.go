package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// resolveActionIssue figures out which Issue an external-action call belongs
// to. Order of preference: (1) caller-supplied IssueID that actually exists,
// (2) most-recently-updated open Issue in the channel, (3) auto-create a
// drafting Issue so the human gate still applies. Returns (issueID,
// lifecycleState, error). The lifecycle state lets the caller enforce gates
// without a follow-up GET.
func resolveActionIssue(ctx context.Context, slug, channel string, args TeamActionExecuteArgs) (string, string, error) {
	if id := strings.TrimSpace(args.IssueID); id != "" {
		var resp struct {
			Task *struct {
				ID             string `json:"id"`
				LifecycleState string `json:"lifecycle_state"`
			} `json:"task,omitempty"`
			LifecycleState string `json:"lifecycleState"`
		}
		if err := brokerGetJSON(ctx, "/tasks/"+url.PathEscape(id), &resp); err == nil &&
			resp.Task != nil && strings.TrimSpace(resp.Task.ID) != "" {
			state := strings.TrimSpace(resp.Task.LifecycleState)
			if state == "" {
				state = strings.TrimSpace(resp.LifecycleState)
			}
			return id, state, nil
		}
	}

	if ch := strings.TrimSpace(channel); ch != "" {
		var list struct {
			Tasks []struct {
				ID         string `json:"id"`
				Status     string `json:"status"`
				Lifecycle  string `json:"lifecycle_state"`
				UpdatedAt  string `json:"updated_at"`
				CreatedAt  string `json:"created_at"`
				ParentID   string `json:"parent_issue_id,omitempty"`
				TaskTypeID string `json:"task_type,omitempty"`
			} `json:"tasks"`
		}
		path := "/tasks?viewer_slug=" + url.QueryEscape(slug) + "&channel=" + url.QueryEscape(ch)
		if err := brokerGetJSON(ctx, path, &list); err == nil && len(list.Tasks) > 0 {
			var best string
			var bestState string
			var bestTS string
			for _, t := range list.Tasks {
				// Only auto-attach to top-level Issue records — a recently
				// updated child task (feature/research/sub-issue) must not
				// short-circuit the intended top-level `drafting` gate that
				// applies to the parent Issue.
				if tt := strings.ToLower(strings.TrimSpace(t.TaskTypeID)); tt != "" && tt != "issue" {
					continue
				}
				if strings.TrimSpace(t.ParentID) != "" {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(t.Status)) {
				case "done", "approved", "rejected", "cancelled", "canceled":
					continue
				}
				switch strings.ToLower(strings.TrimSpace(t.Lifecycle)) {
				case "approved", "rejected":
					continue
				}
				ts := t.UpdatedAt
				if ts == "" {
					ts = t.CreatedAt
				}
				if ts > bestTS {
					bestTS = ts
					best = t.ID
					bestState = strings.TrimSpace(t.Lifecycle)
				}
			}
			if best != "" {
				return best, bestState, nil
			}
		}
	}

	verb := actionVerbLabel(args.Platform, args.ActionID)
	platformLabel := platformDisplay(args.Platform)
	title := titleCaser.String(verb) + " via " + platformLabel
	details := strings.TrimSpace(args.Summary)
	if details == "" {
		details = "Auto-created by the broker to scope an external action the agent kicked off without an explicit Issue. " +
			"Action: " + strings.TrimSpace(args.ActionID) + " via " + platformLabel + "."
	}
	createBody := map[string]any{
		"channel":    strings.TrimSpace(channel),
		"created_by": slug,
		"tasks": []map[string]any{
			{
				"title":          title,
				"assignee":       slug,
				"details":        details,
				"task_type":      "issue",
				"execution_mode": "office",
			},
		},
	}
	var created struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := brokerPostJSON(ctx, "/task-plan", createBody, &created); err != nil {
		return "", "", fmt.Errorf("auto-create issue: %w", err)
	}
	if len(created.Tasks) == 0 || strings.TrimSpace(created.Tasks[0].ID) == "" {
		return "", "", fmt.Errorf("auto-create issue returned no id")
	}
	return strings.TrimSpace(created.Tasks[0].ID), "drafting", nil
}

// looksLikeOpaqueID returns true for argument values that look like an
// internal id rather than human-readable text (long alphanumeric tokens,
// double-colon prefixes, long consonant runs). Used by the action argument
// sanitiser to decide whether to pass the value through or redact it.
func looksLikeOpaqueID(s string) bool {
	if strings.Contains(s, "::") {
		return true
	}
	for _, tok := range strings.FieldsFunc(s, actionIDSeparator) {
		if len(tok) >= 12 && hasLetterAndDigit(tok) {
			return true
		}
		if consonantRun(tok) >= 5 {
			return true
		}
	}
	return false
}

func hasLetterAndDigit(s string) bool {
	var hasL, hasD bool
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasL = true
		case r >= '0' && r <= '9':
			hasD = true
		}
		if hasL && hasD {
			return true
		}
	}
	return false
}

func consonantRun(s string) int {
	maxRun, run := 0, 0
	for _, r := range s {
		isVowel := r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u' ||
			r == 'A' || r == 'E' || r == 'I' || r == 'O' || r == 'U' || r == 'y' || r == 'Y'
		isConsonant := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if isConsonant && !isVowel {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	return maxRun
}
