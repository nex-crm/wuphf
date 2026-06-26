package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestApprovalAuditRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		seed []ApprovalAuditEntry
	}{
		{
			name: "single executed_ok entry",
			seed: []ApprovalAuditEntry{
				{
					ApprovalRequestID:    "req-1",
					TaskID:               "task-abc",
					Platform:             "gmail",
					ActionID:             "send_email",
					ConnectionKey:        "gmail:najmu@nex.ai",
					RequestedAt:          "2026-05-27T13:15:00Z",
					AnsweredAt:           "2026-05-27T13:15:42Z",
					ExecutedAt:           "2026-05-27T13:16:00Z",
					Outcome:              ApprovalOutcomeExecutedOK,
					OutcomeSummary:       "Sent email to alex@nex.ai",
					OutcomeChatMessageID: "msg-101",
					Actor:                "ceo",
					Channel:              "general",
					CreatedAt:            "2026-05-27T13:16:00Z",
				},
			},
		},
		{
			name: "rejected + timed_out + executed_failed in one task",
			seed: []ApprovalAuditEntry{
				{
					ApprovalRequestID: "req-2",
					TaskID:            "task-xyz",
					Outcome:           ApprovalOutcomeRejected,
					OutcomeSummary:    "Human said no",
					CreatedAt:         "2026-05-27T14:00:00Z",
				},
				{
					ApprovalRequestID: "req-3",
					TaskID:            "task-xyz",
					Outcome:           ApprovalOutcomeTimedOut,
					CreatedAt:         "2026-05-27T14:30:00Z",
				},
				{
					ApprovalRequestID: "req-4",
					TaskID:            "task-xyz",
					Outcome:           ApprovalOutcomeExecutedFailed,
					OutcomeSummary:    "API returned 500",
					CreatedAt:         "2026-05-27T14:45:00Z",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "broker-state.json")
			b := NewBrokerAt(path)
			for _, entry := range tc.seed {
				if err := b.RecordApprovalAudit(entry); err != nil {
					t.Fatalf("record: %v", err)
				}
			}

			// In-memory read.
			if tc.seed[0].TaskID != "" {
				got := b.ListApprovalAuditByTask(tc.seed[0].TaskID)
				wantCount := 0
				for _, s := range tc.seed {
					if s.TaskID == tc.seed[0].TaskID {
						wantCount++
					}
				}
				if len(got) != wantCount {
					t.Fatalf("expected %d entries by task, got %d", wantCount, len(got))
				}
			}

			// Verify request-id filtering works on a known entry.
			byReq := b.ListApprovalAuditByRequest(tc.seed[0].ApprovalRequestID)
			if len(byReq) != 1 {
				t.Fatalf("expected 1 entry by request_id, got %d", len(byReq))
			}
			if byReq[0].OutcomeSummary != tc.seed[0].OutcomeSummary {
				t.Fatalf("summary mismatch: got %q want %q", byReq[0].OutcomeSummary, tc.seed[0].OutcomeSummary)
			}

			// Reload from disk and confirm the state round-tripped.
			b2 := NewBrokerAt(path)
			if err := b2.loadState(); err != nil {
				t.Fatalf("loadState: %v", err)
			}
			for _, want := range tc.seed {
				got := b2.ListApprovalAuditByRequest(want.ApprovalRequestID)
				if len(got) != 1 {
					t.Fatalf("after reload: expected 1 entry for %s, got %d", want.ApprovalRequestID, len(got))
				}
				if got[0].Outcome != want.Outcome {
					t.Fatalf("after reload: outcome mismatch for %s: got %q want %q",
						want.ApprovalRequestID, got[0].Outcome, want.Outcome)
				}
				if got[0].TaskID != want.TaskID {
					t.Fatalf("after reload: task_id mismatch for %s", want.ApprovalRequestID)
				}
			}
		})
	}
}

// TestApprovalAuditIdempotent confirms that re-recording the same (request_id,
// outcome) tuple does not append a duplicate. This matters because the MCP
// caller may retry POST /approval-audit on transient network errors and we
// don't want the inbox trail to show "executed_ok → executed_ok → executed_ok".
func TestApprovalAuditIdempotent(t *testing.T) {
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
	entry := ApprovalAuditEntry{
		ApprovalRequestID: "req-1",
		TaskID:            "task-1",
		Outcome:           ApprovalOutcomeExecutedOK,
		OutcomeSummary:    "first write",
	}
	if err := b.RecordApprovalAudit(entry); err != nil {
		t.Fatal(err)
	}
	entry.OutcomeSummary = "second write should be ignored"
	if err := b.RecordApprovalAudit(entry); err != nil {
		t.Fatal(err)
	}
	got := b.ListApprovalAuditByRequest("req-1")
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after duplicate write, got %d", len(got))
	}
	if got[0].OutcomeSummary != "first write" {
		t.Fatalf("expected first-write to win, got %q", got[0].OutcomeSummary)
	}
}

// TestApprovalAuditLoadMissingField confirms that a broker-state.json snapshot
// written by an older broker (no approval_audit key) loads cleanly with an
// empty slice — backwards compatibility for users upgrading their local install.
func TestApprovalAuditLoadMissingField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broker-state.json")
	// Write a minimal state file that intentionally omits approval_audit.
	legacy := map[string]any{
		"counter":  1,
		"messages": []any{},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	b := NewBrokerAt(path)
	if err := b.loadState(); err != nil {
		t.Fatalf("loadState should tolerate missing approval_audit: %v", err)
	}
	got := b.ListApprovalAuditByTask("anything")
	if len(got) != 0 {
		t.Fatalf("expected empty list from legacy state, got %d entries", len(got))
	}
}
