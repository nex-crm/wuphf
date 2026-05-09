package team

import "testing"

func TestInferTaskTypeTreatsAuditWorkAsResearch(t *testing.T) {
	got := inferTaskType("eng", "Audit repo and define the fastest path to a working web UI", "Working plan for the faceless YouTube business build.")
	if got != "research" {
		t.Fatalf("inferTaskType returned %q, want research", got)
	}
}

func TestTaskDefaultExecutionModeTreatsEngineeringFeatureWorkAsLocalWorktree(t *testing.T) {
	if got := taskDefaultExecutionMode("operator", "feature", "Implement the repository-backed approval engine", "Ship the code path and tests."); got != "local_worktree" {
		t.Fatalf("taskDefaultExecutionMode returned %q, want local_worktree", got)
	}
	if got := taskDefaultExecutionMode("generalist", "bugfix", "Fix the API regression in the worker", "Debug the code path and update the repo."); got != "local_worktree" {
		t.Fatalf("taskDefaultExecutionMode returned %q, want local_worktree", got)
	}
	if got := taskDefaultExecutionMode("gtm", "launch", "Launch the outbound sequence", "Coordinate the campaign and approvals."); got != "office" {
		t.Fatalf("taskDefaultExecutionMode returned %q, want office", got)
	}
}

func TestTaskRequiresRealExternalExecution(t *testing.T) {
	if !taskRequiresRealExternalExecution(&teamTask{
		Title:   "Create one new Notion proof artifact for the intake slice",
		Details: "Use the connected Notion workspace and leave evidence in channel.",
		Owner:   "builder",
		status:  "in_progress",
	}) {
		t.Fatal("expected Notion create task to require real external execution")
	}
	if taskRequiresRealExternalExecution(&teamTask{
		Title:   "Generate a local preview packet for Slack handoff",
		Details: "Preview only. Do not post externally yet.",
		Owner:   "builder",
		status:  "in_progress",
	}) {
		t.Fatal("expected preview-only task not to require real external execution")
	}
}

func TestNormalizeTaskPlanMarksLiveExternalTasks(t *testing.T) {
	task := &teamTask{
		Title:   "Post the consulting update to Slack",
		Details: "Use live external execution for the customer-facing announcement.",
		Owner:   "ceo",
	}

	normalizeTaskPlan(task)

	if task.ExecutionMode != "live_external" {
		t.Fatalf("expected live_external execution mode, got %q", task.ExecutionMode)
	}
	if task.reviewState != "pending_review" {
		t.Fatalf("expected pending_review for live external task, got %q", task.reviewState)
	}
	if !taskNeedsStructuredReview(task) {
		t.Fatal("expected live external task to require structured review")
	}
}

func TestTaskRequiresLiveExternalExecutionRecognizesCommonIntegrations(t *testing.T) {
	cases := []teamTask{
		{Title: "Slack kickoff write"},
		{Title: "Notion evidence page", Details: "Create the live external proof page."},
		{Title: "Drive handoff", Details: "Upload the deliverable to Google Drive."},
	}

	for _, tc := range cases {
		task := tc
		if !taskRequiresRealExternalExecution(&task) {
			t.Fatalf("expected %q to be treated as live external", task.Title)
		}
	}
}

func TestRejectTheaterTaskForLiveBusiness(t *testing.T) {
	rejected := &teamTask{
		Title:    "Create one new Notion proof packet for the client handoff",
		Details:  "Use live external execution and keep the review bundle in sync.",
		Owner:    "builder",
		TaskType: "launch",
	}
	if err := rejectTheaterTaskForLiveBusiness(rejected); err == nil {
		t.Fatal("expected live business theater task to be rejected")
	}

	rejectedByChannel := &teamTask{
		Channel:       "client-delivery",
		Title:         "Generate consulting review packet artifact from the updated blueprint",
		Details:       "Post the exact local artifact path for the reviewer.",
		Owner:         "builder",
		ExecutionMode: "local_worktree",
	}
	if err := rejectTheaterTaskForLiveBusiness(rejectedByChannel); err == nil {
		t.Fatal("expected theater task in client-delivery lane to be rejected")
	}

	allowed := &teamTask{
		Title:    "Create preview packet for Slack handoff",
		Details:  "Preview only. Mock testing is intended.",
		Owner:    "builder",
		TaskType: "launch",
	}
	if err := rejectTheaterTaskForLiveBusiness(allowed); err != nil {
		t.Fatalf("expected explicit mock/preview task to be allowed, got %v", err)
	}
}
