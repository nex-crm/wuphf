package team

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ApprovalAuditEntry is a persisted correlation record connecting a human's
// approval decision on an action_request to the resulting execution and the
// chat outcome message that surfaced it. Bug B Layer 2 — the audit trail
// the inbox UI uses to render "I approved this at 13:15 → @ceo executed it
// at 13:16 → outcome was X" underneath an answered request.
//
// One entry is written per terminal disposition (executed_ok,
// executed_failed, rejected, timed_out, cancelled). The entry is keyed by
// approval_request_id so multiple retries of the same approval would
// produce multiple rows (each is the truth of one ExecuteAction attempt).
type ApprovalAuditEntry struct {
	ApprovalRequestID    string `json:"approval_request_id"`
	TaskID               string `json:"task_id,omitempty"`
	Platform             string `json:"platform,omitempty"`
	ActionID             string `json:"action_id,omitempty"`
	ConnectionKey        string `json:"connection_key,omitempty"`
	RequestedAt          string `json:"requested_at,omitempty"`
	AnsweredAt           string `json:"answered_at,omitempty"`
	ExecutedAt           string `json:"executed_at,omitempty"`
	Outcome              string `json:"outcome,omitempty"`
	OutcomeSummary       string `json:"outcome_summary,omitempty"`
	OutcomeChatMessageID string `json:"outcome_chat_message_id,omitempty"`
	Actor                string `json:"actor,omitempty"`
	Channel              string `json:"channel,omitempty"`
	CreatedAt            string `json:"created_at"`
}

// Valid outcome values for ApprovalAuditEntry.Outcome.
const (
	ApprovalOutcomeExecutedOK     = "executed_ok"
	ApprovalOutcomeExecutedFailed = "executed_failed"
	ApprovalOutcomeRejected       = "rejected"
	ApprovalOutcomeTimedOut       = "timed_out"
	ApprovalOutcomeCancelled      = "cancelled"
)

// isValidApprovalOutcome guards RecordApprovalAudit and the POST handler so
// only canonical enum values land in the persisted audit slice. Anything
// else would pollute consumers that group by Outcome (the inbox detail
// pane renders the trail by outcome label).
func isValidApprovalOutcome(outcome string) bool {
	switch outcome {
	case ApprovalOutcomeExecutedOK,
		ApprovalOutcomeExecutedFailed,
		ApprovalOutcomeRejected,
		ApprovalOutcomeTimedOut,
		ApprovalOutcomeCancelled:
		return true
	default:
		return false
	}
}

// RecordApprovalAudit appends an audit entry to the broker state. Idempotent
// on (approval_request_id, outcome) — if a duplicate POST lands (e.g. the
// MCP caller retried after a transient network error), the existing entry
// is preserved rather than appended a second time. Save errors are returned
// so the caller can log them; in-memory state is updated regardless.
func (b *Broker) RecordApprovalAudit(entry ApprovalAuditEntry) error {
	entry = sanitizeApprovalAuditEntry(entry)
	if strings.TrimSpace(entry.ApprovalRequestID) == "" {
		// Defensive: an empty key would silently merge every entry into one
		// row, which is worse than dropping the record entirely.
		return nil
	}
	if !isValidApprovalOutcome(entry.Outcome) {
		// Drop rather than persist an unknown outcome — keeps the audit
		// slice constrained to the documented enum so downstream readers
		// (inbox detail pane, exports) don't have to handle stray values.
		return nil
	}
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b.mu.Lock()
	for _, existing := range b.approvalAudit {
		if existing.ApprovalRequestID == entry.ApprovalRequestID &&
			existing.Outcome == entry.Outcome {
			b.mu.Unlock()
			return nil
		}
	}
	b.approvalAudit = append(b.approvalAudit, entry)
	err := b.saveLocked()
	b.mu.Unlock()
	return err
}

// ListApprovalAuditByTask returns a copy of every audit entry tagged with
// the given task id. Empty task id returns an empty slice (matches "give
// me the audit trail for the task I'm rendering" semantics).
func (b *Broker) ListApprovalAuditByTask(taskID string) []ApprovalAuditEntry {
	taskID = strings.TrimSpace(taskID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if taskID == "" {
		return []ApprovalAuditEntry{}
	}
	out := make([]ApprovalAuditEntry, 0, len(b.approvalAudit))
	for _, entry := range b.approvalAudit {
		if entry.TaskID == taskID {
			out = append(out, entry)
		}
	}
	return out
}

// ListApprovalAuditByRequest returns a copy of every audit entry tagged
// with the given approval_request_id. The inbox detail pane uses this to
// render the trail underneath an answered request row.
func (b *Broker) ListApprovalAuditByRequest(requestID string) []ApprovalAuditEntry {
	requestID = strings.TrimSpace(requestID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if requestID == "" {
		return []ApprovalAuditEntry{}
	}
	out := make([]ApprovalAuditEntry, 0, len(b.approvalAudit))
	for _, entry := range b.approvalAudit {
		if entry.ApprovalRequestID == requestID {
			out = append(out, entry)
		}
	}
	return out
}

func sanitizeApprovalAuditEntry(entry ApprovalAuditEntry) ApprovalAuditEntry {
	entry.ApprovalRequestID = strings.TrimSpace(entry.ApprovalRequestID)
	entry.TaskID = strings.TrimSpace(entry.TaskID)
	entry.Platform = strings.TrimSpace(entry.Platform)
	entry.ActionID = strings.TrimSpace(entry.ActionID)
	entry.ConnectionKey = strings.TrimSpace(entry.ConnectionKey)
	entry.RequestedAt = strings.TrimSpace(entry.RequestedAt)
	entry.AnsweredAt = strings.TrimSpace(entry.AnsweredAt)
	entry.ExecutedAt = strings.TrimSpace(entry.ExecutedAt)
	entry.Outcome = strings.TrimSpace(entry.Outcome)
	entry.OutcomeSummary = strings.TrimSpace(entry.OutcomeSummary)
	entry.OutcomeChatMessageID = strings.TrimSpace(entry.OutcomeChatMessageID)
	entry.Actor = strings.TrimSpace(entry.Actor)
	entry.Channel = strings.TrimSpace(entry.Channel)
	entry.CreatedAt = strings.TrimSpace(entry.CreatedAt)
	return entry
}

// handleApprovalAudit serves both POST (record an entry) and GET
// (list entries, filtered by task_id or request_id query string).
func (b *Broker) handleApprovalAudit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var entry ApprovalAuditEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(entry.ApprovalRequestID) == "" {
			http.Error(w, "approval_request_id required", http.StatusBadRequest)
			return
		}
		if !isValidApprovalOutcome(strings.TrimSpace(entry.Outcome)) {
			http.Error(w, "outcome must be one of executed_ok|executed_failed|rejected|timed_out|cancelled", http.StatusBadRequest)
			return
		}
		if err := b.RecordApprovalAudit(entry); err != nil {
			http.Error(w, "failed to persist approval audit", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case http.MethodGet:
		taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
		requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
		var entries []ApprovalAuditEntry
		switch {
		case requestID != "":
			entries = b.ListApprovalAuditByRequest(requestID)
		case taskID != "":
			entries = b.ListApprovalAuditByTask(taskID)
		default:
			// No filter: return everything. The inbox surface could query
			// this on first render to seed its cache; keeping it cheap by
			// returning a defensive copy under the lock.
			b.mu.Lock()
			entries = make([]ApprovalAuditEntry, len(b.approvalAudit))
			copy(entries, b.approvalAudit)
			b.mu.Unlock()
		}
		if entries == nil {
			entries = []ApprovalAuditEntry{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
