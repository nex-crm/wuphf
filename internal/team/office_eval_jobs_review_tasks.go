package team

// office_eval_jobs_review_tasks.go — the `review-change-tasks` eval job
// (founder directive: review in place + "Request changes via AI").
// Both flows are driven at the HTTP layer, on the exact wires the FE uses:
//
//	(a) A human "Request changes" on a notebook review must BOTH record
//	    the review state with the rationale AND create a task owned by
//	    the notebook's author agent — title "Update notebook item: <title>",
//	    details = the human's comment verbatim + the notebook path + the
//	    resubmit instruction. One POST /review/{id}/request-changes is the
//	    whole gesture; the broker composes the task.
//	(b) The wiki "Request changes via AI" payload (POST /task-plan,
//	    created_by=human, assignee=librarian) must land a librarian-owned
//	    task carrying the article path + the instruction verbatim.
//
// Creation is the authorization on this base (approve & start retired):
// both flows assert the created task lands RUNNING (legacy status
// "in_progress") with the right owner + details.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

func evalJobReviewChangeTasks(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "review-change-tasks"

	fx.broker.SetReviewerResolver(func(string) string { return "ceo" })
	fx.broker.ensureReviewLog()

	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", fx.broker.requireAuth(fx.broker.handleNotebookWrite))
	mux.HandleFunc("/notebook/promote", fx.broker.requireAuth(fx.broker.handleNotebookPromote))
	mux.HandleFunc("/review/", fx.broker.requireAuth(fx.broker.handleReviewSubpath))
	mux.HandleFunc("/task-plan", fx.broker.requireAuth(fx.broker.handleTaskPlan))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	// ── (a) request-changes on a notebook review composes the owner task ──
	const notebookPath = "agents/eng/notebook/onboarding-runbook.md"
	if status, body, err := client.postJSON("/notebook/write", map[string]any{
		"slug": "eng", "path": notebookPath,
		"content": "# Onboarding runbook\n\nDraft steps.\n", "mode": "create", "commit_message": "seed",
	}); err != nil || status != http.StatusOK {
		return fmt.Errorf("notebook write: status=%d body=%s err=%w", status, body, err)
	}
	status, body, err := client.postJSON("/notebook/promote", map[string]any{
		"my_slug": "eng", "source_path": notebookPath,
		"target_wiki_path": "team/playbooks/onboarding-runbook.md",
		"rationale":        "Ready for review.",
	})
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("promote: status=%d body=%s err=%w", status, body, err)
	}
	var promoted struct {
		PromotionID string `json:"promotion_id"`
	}
	if err := json.Unmarshal([]byte(body), &promoted); err != nil || promoted.PromotionID == "" {
		return fmt.Errorf("promote decode: body=%s err=%w", body, err)
	}

	// Human reviewer asks for changes — actor_slug "" is the web UI's
	// human wire shape; the rationale is the typed comment.
	const reviewComment = "Fold the access-request steps into one checklist — split steps got people lost."
	status, body, err = client.postJSON("/review/"+promoted.PromotionID+"/request-changes", map[string]any{
		"actor_slug": "", "rationale": reviewComment,
	})
	if err != nil {
		return err
	}
	r.add(job, "human request-changes on a notebook review returns 200",
		status == http.StatusOK, fmt.Sprintf("status=%d body=%s", status, truncateForDetail(body)), "")

	stateRecorded := false
	rationaleRecorded := false
	if rl := fx.broker.ReviewLog(); rl != nil {
		if p, gerr := rl.Get(promoted.PromotionID); gerr == nil {
			stateRecorded = p.State == PromotionChangesRequested
			for _, tr := range p.StateHistory {
				if tr.NewState == PromotionChangesRequested && tr.Rationale == reviewComment {
					rationaleRecorded = true
				}
			}
		}
	}
	r.add(job, "review state lands changes-requested with the rationale recorded",
		stateRecorded && rationaleRecorded,
		fmt.Sprintf("state=%v rationale=%v", stateRecorded, rationaleRecorded), "")

	ownerTask := findEvalTaskByTitlePrefix(fx.broker, "Update notebook item:")
	taskOK := ownerTask != nil &&
		ownerTask.Owner == "eng" &&
		ownerTask.Title == "Update notebook item: Onboarding runbook" &&
		strings.Contains(ownerTask.Details, reviewComment) &&
		strings.Contains(ownerTask.Details, notebookPath) &&
		strings.Contains(ownerTask.Details, "resubmit it for review") &&
		evalTaskStatusIsRunning(ownerTask)
	r.add(job, "request-changes creates a task owned by the notebook author with the comment + path in details",
		taskOK, describeEvalTask(ownerTask), "")

	// ── (b) wiki "Request changes via AI" payload → librarian task ─────────
	const articlePath = "team/accounts/acme-corp.md"
	const instruction = "Add the new renewal terms from the June call and refresh the pricing table."
	status, body, err = client.postJSON("/task-plan", map[string]any{
		"channel": "general", "created_by": "human",
		"tasks": []map[string]any{{
			"title":    "Update wiki article: Acme Corp",
			"assignee": LibrarianSlug,
			"details": instruction +
				"\n\nWiki article: " + articlePath +
				"\nAlso update related items (linked articles, the index) that this change affects.",
		}},
	})
	if err != nil {
		return err
	}
	r.add(job, "wiki AI-change payload returns 200 on the task-plan wire",
		status == http.StatusOK, fmt.Sprintf("status=%d body=%s", status, truncateForDetail(body)), "")

	wikiTask := findEvalTaskByTitlePrefix(fx.broker, "Update wiki article:")
	wikiOK := wikiTask != nil &&
		wikiTask.Owner == LibrarianSlug &&
		strings.Contains(wikiTask.Details, instruction) &&
		strings.Contains(wikiTask.Details, articlePath) &&
		strings.Contains(wikiTask.Details, "related items") &&
		evalTaskStatusIsRunning(wikiTask)
	r.add(job, "wiki AI-change task is owned by the librarian and carries the article path + instruction",
		wikiOK, describeEvalTask(wikiTask), "")

	return nil
}

func findEvalTaskByTitlePrefix(b *Broker, prefix string) *teamTask {
	for _, candidate := range b.AllTasks() {
		if strings.HasPrefix(candidate.Title, prefix) {
			cp := candidate
			return &cp
		}
	}
	return nil
}

// evalTaskStatusIsRunning asserts the immediate-start contract: a create
// with a real owner lands running (legacy status "in_progress") — creation
// is the authorization, no approve gate.
func evalTaskStatusIsRunning(t *teamTask) bool {
	return t != nil && strings.TrimSpace(t.status) == "in_progress"
}

func describeEvalTask(t *teamTask) string {
	if t == nil {
		return "task not found"
	}
	return fmt.Sprintf("id=%s owner=%s status=%s title=%q details=%q",
		t.ID, t.Owner, strings.TrimSpace(t.status), t.Title, truncateForDetail(t.Details))
}

func truncateForDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:197] + "..."
	}
	return s
}
