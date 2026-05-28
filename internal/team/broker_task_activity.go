package team

// broker_task_activity.go owns the per-Issue Activity feed exposed at
// GET /tasks/{id}/activity. The feed is a flat chronological list of
// everything that happened to this Issue: lifecycle transitions, comments,
// owner changes, approvals, sub-issue creation, and human_interview
// requests (with the resolved decision, or a flag for "open — answer in
// inbox"). The FE renders it under the Activity tab on the Issue detail
// page, modeled on Paperclip's activity log.

import (
	"net/http"
	"sort"
	"strings"
)

// IssueActivityEventKind is the source bucket. The FE renders a different
// icon + verb per kind; new kinds added later must extend the FE switch.
type IssueActivityEventKind string

const (
	IssueActivityKindLifecycle  IssueActivityEventKind = "lifecycle"
	IssueActivityKindComment    IssueActivityEventKind = "comment"
	IssueActivityKindAction     IssueActivityEventKind = "action"
	IssueActivityKindRequest    IssueActivityEventKind = "request"
	IssueActivityKindSubIssue   IssueActivityEventKind = "sub_issue"
)

// IssueActivityRequestStatus mirrors the human_interview lifecycle so
// the FE can render "answered: yes" vs "open — answer in Inbox" links.
type IssueActivityRequestStatus string

const (
	IssueRequestStatusOpen     IssueActivityRequestStatus = "open"
	IssueRequestStatusAnswered IssueActivityRequestStatus = "answered"
	IssueRequestStatusCanceled IssueActivityRequestStatus = "canceled"
)

// IssueActivityEvent is one row in the per-Issue Activity feed.
type IssueActivityEvent struct {
	ID        string                 `json:"id"`
	Kind      IssueActivityEventKind `json:"kind"`
	Timestamp string                 `json:"timestamp"`
	Actor     string                 `json:"actor,omitempty"`
	Summary   string                 `json:"summary,omitempty"`
	Detail    string                 `json:"detail,omitempty"`
	// Kind-specific fields. Only populated when the kind matches.
	Lifecycle *IssueActivityLifecycle `json:"lifecycle,omitempty"`
	Request   *IssueActivityRequest   `json:"request,omitempty"`
	SubIssue  *IssueActivitySubIssue  `json:"sub_issue,omitempty"`
}

type IssueActivityLifecycle struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type IssueActivityRequest struct {
	RequestID    string                     `json:"request_id"`
	Status       IssueActivityRequestStatus `json:"status"`
	Question     string                     `json:"question,omitempty"`
	ChoiceID     string                     `json:"choice_id,omitempty"`
	ChoiceText   string                     `json:"choice_text,omitempty"`
	CustomText   string                     `json:"custom_text,omitempty"`
	AnsweredAt   string                     `json:"answered_at,omitempty"`
	Blocking     bool                       `json:"blocking,omitempty"`
}

type IssueActivitySubIssue struct {
	SubIssueID string `json:"sub_issue_id"`
	Title      string `json:"title,omitempty"`
}

// IssueActivityResponse is the wire shape for GET /tasks/{id}/activity.
type IssueActivityResponse struct {
	TaskID string               `json:"task_id"`
	Events []IssueActivityEvent `json:"events"`
}

// handleTaskActivity serves GET /tasks/{id}/activity. Merges officeActionLog
// entries (lifecycle + workflow actions), comments from the channelMessage
// stream, requests filtered by IssueID, and sub-issues filtered by
// ParentIssueID. Sorted oldest → newest so the FE can render top-down.
func (b *Broker) handleTaskActivity(w http.ResponseWriter, r *http.Request, actor requestActor, taskID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	task := b.findTaskByIDLocked(taskID)
	if task == nil {
		b.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	reviewers := append([]string(nil), task.Reviewers...)
	events := b.collectIssueActivityLocked(taskID)
	b.mu.Unlock()

	if !taskAccessAllowed(actor, reviewers) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})

	writeJSON(w, http.StatusOK, IssueActivityResponse{
		TaskID: taskID,
		Events: events,
	})
}

// collectIssueActivityLocked builds the merged event list. Caller MUST
// hold b.mu.
func (b *Broker) collectIssueActivityLocked(taskID string) []IssueActivityEvent {
	out := make([]IssueActivityEvent, 0, 32)

	// 1. officeActionLog entries where RelatedID matches the task.
	//    Lifecycle transitions appear here as kind="lifecycle_changed" /
	//    "lifecycle_*"; status mutations as task_created/task_updated/etc.
	for _, a := range b.actions {
		if strings.TrimSpace(a.RelatedID) != taskID {
			continue
		}
		kind := IssueActivityKindAction
		var lifecycle *IssueActivityLifecycle
		if strings.HasPrefix(a.Kind, "lifecycle_") || a.Kind == "task_created" {
			kind = IssueActivityKindLifecycle
			// Try to parse "from X to Y" from summary; otherwise leave the
			// transition fields blank and let the summary line carry the
			// detail. Cheap heuristic; the broker doesn't currently
			// structure transitions on the action.
			lifecycle = parseLifecycleFromSummary(a.Summary)
		}
		out = append(out, IssueActivityEvent{
			ID:        a.ID,
			Kind:      kind,
			Timestamp: a.CreatedAt,
			Actor:     a.Actor,
			Summary:   a.Summary,
			Lifecycle: lifecycle,
		})
	}

	// 2. Comment messages tied to the task. The comment posting path
	//    stamps messages with related_id=taskID via appendActionLocked —
	//    we already include those above. But it's also helpful to surface
	//    the raw comment body. Walk messages and pick up kind="task_comment".
	for _, m := range b.messages {
		if strings.TrimSpace(m.SourceTaskID) != taskID {
			continue
		}
		if m.Kind != "task_comment" && m.Kind != "issue_comment" {
			continue
		}
		out = append(out, IssueActivityEvent{
			ID:        "msg-" + m.ID,
			Kind:      IssueActivityKindComment,
			Timestamp: m.Timestamp,
			Actor:     m.From,
			Summary:   firstActivityLine(m.Content, 140),
			Detail:    m.Content,
		})
	}

	// 3. Human interviews linked to this Issue.
	for _, req := range b.requests {
		if strings.TrimSpace(req.IssueID) != taskID {
			continue
		}
		status := IssueRequestStatusOpen
		switch strings.ToLower(strings.TrimSpace(req.Status)) {
		case "answered", "resolved":
			status = IssueRequestStatusAnswered
		case "canceled", "cancelled":
			status = IssueRequestStatusCanceled
		}
		ev := IssueActivityEvent{
			ID:        "req-" + req.ID,
			Kind:      IssueActivityKindRequest,
			Timestamp: req.CreatedAt,
			Actor:     req.From,
			Summary:   firstActivityLine(req.Question, 140),
			Request: &IssueActivityRequest{
				RequestID: req.ID,
				Status:    status,
				Question:  req.Question,
				Blocking:  req.Blocking,
			},
		}
		if req.Answered != nil {
			ev.Request.ChoiceID = req.Answered.ChoiceID
			ev.Request.ChoiceText = req.Answered.ChoiceText
			ev.Request.CustomText = req.Answered.CustomText
			ev.Request.AnsweredAt = req.Answered.AnsweredAt
			if strings.TrimSpace(req.Answered.AnsweredAt) != "" {
				ev.Timestamp = req.Answered.AnsweredAt
			}
		}
		out = append(out, ev)
	}

	// 4. Sub-issue creations. Pick up any task with ParentIssueID==this.
	for _, t := range b.tasks {
		if strings.TrimSpace(t.ParentIssueID) != taskID {
			continue
		}
		out = append(out, IssueActivityEvent{
			ID:        "subissue-" + t.ID,
			Kind:      IssueActivityKindSubIssue,
			Timestamp: t.CreatedAt,
			Actor:     t.CreatedBy,
			Summary:   "Sub-issue created: " + t.Title,
			SubIssue: &IssueActivitySubIssue{
				SubIssueID: t.ID,
				Title:      t.Title,
			},
		})
	}

	return out
}

// parseLifecycleFromSummary is a best-effort extractor for "drafting → running"
// style transitions inside an officeActionLog summary. Returns nil when no
// arrow is present — caller falls back to the summary line alone.
func parseLifecycleFromSummary(summary string) *IssueActivityLifecycle {
	// The broker writes lifecycle transitions in a few forms; we only
	// pull structured from-to when the literal "→" arrow appears (the
	// canonical lifecycle_card format from postIssueLifecycleCardLocked).
	const arrow = "→"
	if !strings.Contains(summary, arrow) {
		return nil
	}
	parts := strings.SplitN(summary, arrow, 2)
	if len(parts) != 2 {
		return nil
	}
	return &IssueActivityLifecycle{
		From: strings.TrimSpace(stripBracketed(parts[0])),
		To:   strings.TrimSpace(stripBracketed(parts[1])),
	}
}

// stripBracketed trims trailing "[reason]" or wrapping quote chars.
func stripBracketed(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "["); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.Trim(s, `"'`)
}

// firstActivityLine clips to the first line + max characters for compact
// summary rendering. Multi-line content is preserved in `detail` for
// expanders. Distinct from the package's `firstLine` (which is a
// no-arg helper in promotion_commit.go).
func firstActivityLine(s string, maxLen int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if maxLen > 0 && len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}
