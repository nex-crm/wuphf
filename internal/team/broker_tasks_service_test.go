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
	if b.tasks[0].Status != "done" {
		t.Fatalf("expected broker state to be updated, got %+v", b.tasks[0])
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
