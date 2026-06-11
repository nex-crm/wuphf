package team

// office_eval_jobs_human_boundary.go — the Wave E "human-boundary" eval job
// (docs/specs/ten-out-of-ten.md). Two contracts at the human↔agent boundary:
//
//	(a) E5 define-time interview enforcement: a Definition that lands with
//	    placeholder markers ("[CONTACT NAME]", "NEEDS CONFIRMATION", "TBD")
//	    or unmet access needs raises ONE batched human interview for the
//	    task deterministically — v3's CEO wrote around the holes without
//	    asking (v3 [17:33:18]: "NO interview so far ... it wrote angles
//	    around the gaps instead").
//	(b) Human messages reach agents IN FULL: the 800-char poll clip and the
//	    2000-char packet thread clip no longer apply to human-authored
//	    content (v3 [18:05–18:10]: "half the message read, half dropped").
//
// FE render-boundary assertions (E1) live in vitest component tests, not
// here — this job covers the broker/packet layers only.

import (
	"fmt"
	"strings"
)

func evalJobHumanBoundary(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "human-boundary"

	// --- (a) E5: placeholder define raises the batched interview ---
	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Send the Q4 renewal outreach",
		Details: "Renewal outreach emails for the Q4 book.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	taskID := created.Task.ID
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: taskID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal: "Email [CONTACT NAME] at Acme about the Q4 renewal",
			Deliverables: []TaskDeliverable{
				{Name: "renewal email draft", Format: "markdown in the wiki"},
			},
			SuccessCriteria: []string{"ARR figure confirmed (currently NEEDS CONFIRMATION)"},
		},
	}); err != nil {
		return err
	}
	countGapInterviews := func(id string) (int, humanInterview) {
		fx.broker.mu.Lock()
		defer fx.broker.mu.Unlock()
		n := 0
		var last humanInterview
		for _, req := range fx.broker.requests {
			if requestIsHumanInterview(req) && requestIsActive(req) && strings.TrimSpace(req.IssueID) == id {
				n++
				last = cloneHumanInterview(req)
			}
		}
		return n, last
	}
	n, raised := countGapInterviews(taskID)
	r.add(job, "placeholder define raises a pending interview for the task before any dispatch",
		n == 1 && strings.Contains(raised.Question, "[CONTACT NAME]"),
		fmt.Sprintf("interviews=%d question=%q", n, raised.Question), "")

	// Idempotence: re-defining with the holes still open must not stack a
	// second interview (the dedupe key + active-interview check both guard).
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: taskID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{Goal: "Email [CONTACT NAME] at Acme about the Q4 renewal"},
	}); err != nil {
		return err
	}
	n, _ = countGapInterviews(taskID)
	r.add(job, "re-define with the same gaps does not stack a second interview", n == 1,
		fmt.Sprintf("interviews=%d", n), "")

	// Access needs alone (no placeholder text) also require the human up
	// front — the batched interview is where access gets granted.
	accessTask, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Pull the billing export",
		Details: "Monthly billing export for finance.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: accessTask.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:         "Export the month's billing data to the wiki",
			AccessNeeded: []string{"billing dashboard account"},
		},
	}); err != nil {
		return err
	}
	n, raised = countGapInterviews(accessTask.Task.ID)
	r.add(job, "define with access_needed raises the interview asking for the grant",
		n == 1 && strings.Contains(raised.Question, "billing dashboard account"),
		fmt.Sprintf("interviews=%d question=%q", n, raised.Question), "")

	// A complete definition (no placeholders, no access needs) must NOT
	// nag the human — the gate fires on genuine gaps only.
	cleanTask, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Refresh the pricing page copy",
		Details: "Update the pricing page with the new tiers.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: cleanTask.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Publish the updated pricing page copy with the three new tiers",
			Deliverables:    []TaskDeliverable{{Name: "pricing copy", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"pricing.md lists all three tiers"},
		},
	}); err != nil {
		return err
	}
	n, _ = countGapInterviews(cleanTask.Task.ID)
	r.add(job, "complete definition raises no interview", n == 0, fmt.Sprintf("interviews=%d", n), "")

	// --- (b) human content reaches the agent in full ---
	// A human redline message far past both the old 800-char poll clip and
	// the 2000-char packet thread clip; the tail marker must survive into
	// the agent's work packet verbatim.
	tail := "FINAL-REDLINE-OMEGA: sender name is Maya, send window closes Friday."
	long := strings.Repeat("Redline detail about contacts, dates, and per-account corrections. ", 40) + tail // ~2.7k chars
	if len(long) <= 2000 {
		return fmt.Errorf("human-boundary: fixture message too short to exercise the clips (%d chars)", len(long))
	}
	if _, err := fx.broker.PostMessage("you", "general", long, nil, ""); err != nil {
		return err
	}
	trigger, err := fx.broker.PostMessage("you", "general", "@eng apply my redlines above.", []string{"eng"}, "")
	if err != nil {
		return err
	}
	packet := fx.launcher.buildMessageWorkPacket(trigger, "eng")
	r.add(job, "human message far past the old clips reaches the work packet in full",
		strings.Contains(packet, tail),
		fmt.Sprintf("msg=%d chars packet=%d chars", len(long), len(packet)), "")
	return nil
}
