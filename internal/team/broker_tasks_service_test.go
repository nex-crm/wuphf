package team

import (
	"errors"
	"testing"
)

func TestListTasksFiltersByChannelStatusOwnerAndDone(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
		{Slug: "planning", Name: "planning", Members: []string{"pm"}},
	}
	b.tasks = []teamTask{
		{ID: "general-alice-open", Channel: "general", Title: "General alice open", Owner: "alice", Status: "open"},
		{ID: "general-alice-done", Channel: "general", Title: "General alice done", Owner: "alice", Status: "done"},
		{ID: "general-bob-open", Channel: "general", Title: "General bob open", Owner: "bob", Status: "open"},
		{ID: "general-unowned-open", Channel: "general", Title: "General unowned open", Status: "open"},
		{ID: "planning-alice-open", Channel: "planning", Title: "Planning alice open", Owner: "alice", Status: "open"},
	}

	got, err := b.ListTasks(TaskListRequest{
		Channel:    "general",
		ViewerSlug: "pm",
		MySlug:     "alice",
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	assertTaskIDs(t, got.Tasks, []string{"general-alice-open", "general-unowned-open"})

	got, err = b.ListTasks(TaskListRequest{
		Channel:     "general",
		ViewerSlug:  "pm",
		MySlug:      "alice",
		IncludeDone: true,
	})
	if err != nil {
		t.Fatalf("ListTasks include done: %v", err)
	}
	assertTaskIDs(t, got.Tasks, []string{"general-alice-open", "general-alice-done", "general-unowned-open"})

	got, err = b.ListTasks(TaskListRequest{
		Channel:      "general",
		ViewerSlug:   "pm",
		StatusFilter: "done",
	})
	if err != nil {
		t.Fatalf("ListTasks status done: %v", err)
	}
	assertTaskIDs(t, got.Tasks, []string{"general-alice-done"})
}

func TestListTasksRejectsSingleChannelNonMember(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "private", Name: "private", Members: []string{"ceo"}},
	}
	b.tasks = []teamTask{
		{ID: "private-task", Channel: "private", Title: "Private", Status: "open"},
	}

	_, err := b.ListTasks(TaskListRequest{Channel: "private", ViewerSlug: "pm"})
	if !errors.Is(err, errTaskChannelAccessDenied) {
		t.Fatalf("expected errTaskChannelAccessDenied, got %v", err)
	}
}

func TestListTasksAllChannelsStillChecksViewerAccess(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
		{Slug: "private", Name: "private", Members: []string{"ceo"}},
	}
	b.tasks = []teamTask{
		{ID: "general-task", Channel: "general", Title: "General", Status: "open"},
		{ID: "private-task", Channel: "private", Title: "Private", Status: "open"},
	}

	got, err := b.ListTasks(TaskListRequest{AllChannels: true, ViewerSlug: "pm"})
	if err != nil {
		t.Fatalf("ListTasks all channels: %v", err)
	}
	if got.Channel != "general" {
		t.Fatalf("channel: want general, got %q", got.Channel)
	}
	assertTaskIDs(t, got.Tasks, []string{"general-task"})
}

func TestAckTaskMarksTaskForOwner(t *testing.T) {
	b := newTestBroker(t)
	b.tasks = []teamTask{
		{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "in_progress"},
	}

	got, err := b.AckTask(TaskAckRequest{ID: "task-1", Channel: "general", Slug: "alice"})
	if err != nil {
		t.Fatalf("AckTask: %v", err)
	}
	if got.Task.AckedAt == "" {
		t.Fatal("expected ack timestamp")
	}
	if got.Task.UpdatedAt == "" {
		t.Fatal("expected updated timestamp")
	}
	if b.tasks[0].AckedAt == "" {
		t.Fatal("expected broker state to be updated")
	}
}

func TestAckTaskRejectsInvalidOwnerAndMissingTask(t *testing.T) {
	b := newTestBroker(t)
	b.tasks = []teamTask{
		{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "in_progress"},
	}

	_, err := b.AckTask(TaskAckRequest{ID: "task-1", Channel: "general", Slug: "bob"})
	if !errors.Is(err, errTaskAckOwnerOnly) {
		t.Fatalf("expected errTaskAckOwnerOnly, got %v", err)
	}

	_, err = b.AckTask(TaskAckRequest{ID: "missing", Channel: "general", Slug: "alice"})
	if !errors.Is(err, errTaskNotFound) {
		t.Fatalf("expected errTaskNotFound, got %v", err)
	}

	_, err = b.AckTask(TaskAckRequest{Channel: "general", Slug: "alice"})
	if !errors.Is(err, errTaskAckInvalid) {
		t.Fatalf("expected errTaskAckInvalid, got %v", err)
	}
}

func TestMutateTaskCreatesAndCompletesTask(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
	}

	created, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Write the plan",
		Owner:     "alice",
		CreatedBy: "pm",
	})
	if err != nil {
		t.Fatalf("MutateTask create: %v", err)
	}
	if created.Task.ID == "" {
		t.Fatal("expected task id")
	}
	if created.Task.Status != "in_progress" {
		t.Fatalf("created status: want in_progress, got %q", created.Task.Status)
	}
	if len(b.tasks) != 1 || b.tasks[0].ID != created.Task.ID {
		t.Fatalf("expected broker state to include created task, got %+v", b.tasks)
	}

	updated, err := b.MutateTask(TaskPostRequest{
		Action:    "complete",
		ID:        created.Task.ID,
		Channel:   "general",
		CreatedBy: "pm",
	})
	if err != nil {
		t.Fatalf("MutateTask complete: %v", err)
	}
	if updated.Task.Status != "done" {
		t.Fatalf("updated status: want done, got %q", updated.Task.Status)
	}
	if updated.Task.CompletedAt == "" {
		t.Fatal("expected completion timestamp")
	}
	if b.tasks[0].Status != "done" {
		t.Fatalf("expected broker state to be updated, got %+v", b.tasks[0])
	}
}

func TestMutateTaskReusesExistingTask(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
	}
	b.tasks = []teamTask{
		{ID: "task-1", Channel: "general", Title: "Write the plan", Owner: "alice", Status: "open"},
	}

	got, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     " Write the plan ",
		Details:   " Updated details ",
		Owner:     "alice",
		CreatedBy: "pm",
	})
	if err != nil {
		t.Fatalf("MutateTask create reuse: %v", err)
	}
	if got.Task.ID != "task-1" {
		t.Fatalf("expected reusable task id task-1, got %q", got.Task.ID)
	}
	if len(b.tasks) != 1 {
		t.Fatalf("expected reusable task without appending, got %+v", b.tasks)
	}
	if b.tasks[0].Details != "Updated details" {
		t.Fatalf("expected reused task details to update, got %q", b.tasks[0].Details)
	}
}

func TestMutateTaskTrimsDependenciesAndBlocksUnresolved(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
	}
	b.tasks = []teamTask{
		{ID: "parent-done", Channel: "general", Title: "Parent", Status: "done"},
	}

	got, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Blocked child",
		Owner:     "alice",
		CreatedBy: "pm",
		DependsOn: []string{" parent-done ", " ", " missing-parent "},
	})
	if err != nil {
		t.Fatalf("MutateTask create with dependencies: %v", err)
	}
	if got.Task.Status != "open" || !got.Task.Blocked {
		t.Fatalf("expected unresolved dependency to block open task, got status=%q blocked=%v", got.Task.Status, got.Task.Blocked)
	}
	if len(got.Task.DependsOn) != 2 {
		t.Fatalf("expected empty dependency entries to be removed, got %+v", got.Task.DependsOn)
	}
	assertTaskIDs(t, []teamTask{{ID: got.Task.DependsOn[0]}, {ID: got.Task.DependsOn[1]}}, []string{"parent-done", "missing-parent"})
}

func TestMutateTaskAppliesStateActions(t *testing.T) {
	cases := []struct {
		name        string
		task        teamTask
		req         TaskPostRequest
		wantStatus  string
		wantOwner   string
		wantBlocked bool
	}{
		{
			name:       "claim",
			task:       teamTask{ID: "task-1", Channel: "general", Title: "Task", Status: "open"},
			req:        TaskPostRequest{Action: "claim", Owner: "alice"},
			wantStatus: "in_progress",
			wantOwner:  "alice",
		},
		{
			name:       "reassign",
			task:       teamTask{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "in_progress"},
			req:        TaskPostRequest{Action: "reassign", Owner: "bob"},
			wantStatus: "in_progress",
			wantOwner:  "bob",
		},
		{
			name:        "block",
			task:        teamTask{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "in_progress"},
			req:         TaskPostRequest{Action: "block", Details: "waiting on input"},
			wantStatus:  "blocked",
			wantOwner:   "alice",
			wantBlocked: true,
		},
		{
			name:       "resume",
			task:       teamTask{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "blocked", Blocked: true},
			req:        TaskPostRequest{Action: "resume", Details: "ready again"},
			wantStatus: "in_progress",
			wantOwner:  "alice",
		},
		{
			name:       "release",
			task:       teamTask{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "in_progress"},
			req:        TaskPostRequest{Action: "release"},
			wantStatus: "open",
		},
		{
			name:       "cancel",
			task:       teamTask{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "in_progress", Blocked: true},
			req:        TaskPostRequest{Action: "cancel"},
			wantStatus: "canceled",
			wantOwner:  "alice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBroker(t)
			b.channels = []teamChannel{
				{Slug: "general", Name: "general", Members: []string{"pm"}},
			}
			b.tasks = []teamTask{tc.task}
			tc.req.ID = "task-1"
			tc.req.Channel = "general"
			tc.req.CreatedBy = "pm"

			got, err := b.MutateTask(tc.req)
			if err != nil {
				t.Fatalf("MutateTask %s: %v", tc.req.Action, err)
			}
			if got.Task.Status != tc.wantStatus {
				t.Fatalf("status: want %q, got %q", tc.wantStatus, got.Task.Status)
			}
			if got.Task.Owner != tc.wantOwner {
				t.Fatalf("owner: want %q, got %q", tc.wantOwner, got.Task.Owner)
			}
			if got.Task.Blocked != tc.wantBlocked {
				t.Fatalf("blocked: want %v, got %v", tc.wantBlocked, got.Task.Blocked)
			}
		})
	}
}

func TestMutateTaskReturnsTypedErrors(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"pm"}},
		{Slug: "private", Name: "private", Members: []string{"ceo"}},
	}
	b.tasks = []teamTask{
		{ID: "task-1", Channel: "general", Title: "Task", Owner: "alice", Status: "open"},
	}

	cases := []struct {
		name string
		req  TaskPostRequest
		kind TaskMutationErrorKind
	}{
		{
			name: "missing create title",
			req:  TaskPostRequest{Action: "create", Channel: "general", CreatedBy: "pm"},
			kind: TaskMutationInvalid,
		},
		{
			name: "missing create actor",
			req:  TaskPostRequest{Action: "create", Channel: "general", Title: "Task"},
			kind: TaskMutationInvalid,
		},
		{
			name: "channel access denied",
			req:  TaskPostRequest{Action: "create", Channel: "private", Title: "Secret", CreatedBy: "pm"},
			kind: TaskMutationForbidden,
		},
		{
			name: "missing task",
			req:  TaskPostRequest{Action: "claim", ID: "missing", Channel: "general", Owner: "alice", CreatedBy: "pm"},
			kind: TaskMutationNotFound,
		},
		{
			name: "unknown action",
			req:  TaskPostRequest{Action: "bogus", ID: "task-1", Channel: "general", CreatedBy: "pm"},
			kind: TaskMutationInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := b.MutateTask(tc.req)
			var mutationErr *TaskMutationError
			if !errors.As(err, &mutationErr) {
				t.Fatalf("expected TaskMutationError, got %v", err)
			}
			if mutationErr.Kind != tc.kind {
				t.Fatalf("kind: want %q, got %q", tc.kind, mutationErr.Kind)
			}
		})
	}
}

func TestMutateTaskReconcilesReviewStateAfterFieldPatches(t *testing.T) {
	cases := []struct {
		name       string
		task       teamTask
		req        TaskPostRequest
		wantStatus string
		wantReview string
	}{
		{
			name: "structured review overrides invalid body state",
			task: teamTask{
				ID:            "task-1",
				Channel:       "general",
				Title:         "Task",
				Owner:         "alice",
				Status:        "in_progress",
				ExecutionMode: "local_worktree",
				ReviewState:   "pending_review",
			},
			req:        TaskPostRequest{Action: "review", ReviewState: "not_required"},
			wantStatus: "review",
			wantReview: "ready_for_review",
		},
		{
			name: "office task ignores stale structured review state",
			task: teamTask{
				ID:          "task-1",
				Channel:     "general",
				Title:       "Task",
				Owner:       "alice",
				Status:      "in_progress",
				TaskType:    "follow_up",
				ReviewState: "ready_for_review",
			},
			req:        TaskPostRequest{Action: "resume", ReviewState: "approved"},
			wantStatus: "in_progress",
			wantReview: "not_required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBroker(t)
			b.channels = []teamChannel{
				{Slug: "general", Name: "general", Members: []string{"pm"}},
			}
			b.tasks = []teamTask{tc.task}
			tc.req.ID = "task-1"
			tc.req.Channel = "general"
			tc.req.CreatedBy = "pm"

			got, err := b.MutateTask(tc.req)
			if err != nil {
				t.Fatalf("MutateTask %s: %v", tc.req.Action, err)
			}
			if got.Task.Status != tc.wantStatus {
				t.Fatalf("status: want %q, got %q", tc.wantStatus, got.Task.Status)
			}
			if got.Task.ReviewState != tc.wantReview {
				t.Fatalf("review state: want %q, got %q", tc.wantReview, got.Task.ReviewState)
			}
		})
	}
}

func assertTaskIDs(t *testing.T, tasks []teamTask, want []string) {
	t.Helper()
	got := make([]string, 0, len(tasks))
	for _, task := range tasks {
		got = append(got, task.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("task ids: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("task ids: want %v, got %v", want, got)
		}
	}
}
