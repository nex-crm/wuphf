package team

// slack_task_reporting_test.go covers Slice 1: task-id hyperlinking on
// outbound, the subtask-assignment ping (the Hermes bug), de-duped lifecycle
// reporting into the task thread, and wiki-article links.

import (
	"context"
	"strings"
	"testing"
)

func seedReportTask(b *Broker, task teamTask) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, task)
}

func setReportTaskState(b *Broker, id string, state LifecycleState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].LifecycleState = state
		}
	}
}

// --- Item 1: task-id links ---

func TestFormatOutboundHyperlinksTaskIDs(t *testing.T) {
	tr, b := newTestSlackTransport(t, "C0123", newFakeSlackAPI())
	b.SetWebURL("http://127.0.0.1:7905")

	out, ok := tr.FormatOutbound(channelMessage{
		From:    "ceo",
		Channel: "slack-general",
		Content: "Done with OFFICE-12, blocked on OFFICE-3.",
	})
	if !ok {
		t.Fatal("expected outbound to format")
	}
	if !strings.Contains(out.Text, "<http://127.0.0.1:7905/tasks/OFFICE-12|OFFICE-12>") {
		t.Errorf("OFFICE-12 not hyperlinked: %q", out.Text)
	}
	if !strings.Contains(out.Text, "<http://127.0.0.1:7905/tasks/OFFICE-3|OFFICE-3>") {
		t.Errorf("OFFICE-3 not hyperlinked: %q", out.Text)
	}
	// The full numeric run must be captured: OFFICE-12 must not link as OFFICE-1.
	if strings.Contains(out.Text, "/tasks/OFFICE-1|OFFICE-1>") {
		t.Errorf("OFFICE-12 mis-linked as OFFICE-1: %q", out.Text)
	}
}

func TestRenderTaskLinksBoundariesAndNoWebURL(t *testing.T) {
	tr, b := newTestSlackTransport(t, "C0123", newFakeSlackAPI())

	// No WebURL → never link (a bare id beats a dead link).
	if got := tr.renderTaskLinks("see OFFICE-7"); got != "see OFFICE-7" {
		t.Fatalf("without WebURL, text must pass through: %q", got)
	}

	b.SetWebURL("http://host")
	// Glued to a longer leading word → not a task id.
	if got := tr.renderTaskLinks("XOFFICE-7"); strings.Contains(got, "/tasks/") {
		t.Errorf("XOFFICE-7 should not be linked: %q", got)
	}
	// Real id is linked.
	if got := tr.renderTaskLinks("OFFICE-7 done"); !strings.Contains(got, "<http://host/tasks/OFFICE-7|OFFICE-7>") {
		t.Errorf("OFFICE-7 should be linked: %q", got)
	}
}

func TestIDPrefixAccessor(t *testing.T) {
	b := newTestBroker(t)
	// Default fallback when unset.
	if got := b.IDPrefix(); got != defaultIDPrefix {
		t.Fatalf("IDPrefix() default = %q, want %q", got, defaultIDPrefix)
	}
	b.mu.Lock()
	b.idPrefix = "NEX"
	b.mu.Unlock()
	if got := b.IDPrefix(); got != "NEX" {
		t.Fatalf("IDPrefix() = %q, want NEX", got)
	}
}

// --- Item 2: subtask assignment ping (the Hermes fix) ---

func TestReportSubtaskAssignedPingsRegisteredAgent(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://host")

	// A registered foreign agent (Hermes) with a real Slack user id.
	if _, err := b.RegisterSlackAgent("hermes", "Hermes", "U999"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}

	parent := teamTask{ID: "OFFICE-1", Channel: "slack-general", Title: "Parent", Owner: "ceo", LifecycleState: LifecycleStateRunning}
	sub := teamTask{ID: "OFFICE-2", Channel: "slack-general", Title: "Draft the brief", Owner: "hermes", ParentIssueID: "OFFICE-1", LifecycleState: LifecycleStateRunning}
	seedReportTask(b, parent)
	seedReportTask(b, sub)

	r := &slackTaskReporter{t: tr, lastState: map[string]string{}, seenSubtasks: map[string]bool{}}
	r.reconcile(context.Background())

	posts := api.snapshotPosts()
	var subtaskLine string
	for _, p := range posts {
		if strings.Contains(p.Text, "Subtask") {
			subtaskLine = p.Text
			if p.ThreadTS == "" {
				t.Errorf("subtask line must be threaded under the parent: %+v", p)
			}
		}
	}
	if subtaskLine == "" {
		t.Fatalf("expected a subtask report line, got posts=%+v", posts)
	}
	// The assignee is pinged with a REAL <@U…> mention — the core bug fix.
	if !strings.Contains(subtaskLine, "<@U999>") {
		t.Errorf("subtask line must ping the registered agent: %q", subtaskLine)
	}
	if !strings.Contains(subtaskLine, "<http://host/tasks/OFFICE-2|OFFICE-2>") {
		t.Errorf("subtask line must link the subtask id: %q", subtaskLine)
	}

	// De-dupe: a second pass announces nothing new.
	before := len(api.snapshotPosts())
	r.reconcile(context.Background())
	if got := len(api.snapshotPosts()); got != before {
		t.Fatalf("subtask must be announced once: posts went %d -> %d", before, got)
	}
}

func TestReportSubtaskUnassignedWaitsForOwner(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	parent := teamTask{ID: "OFFICE-1", Channel: "slack-general", Title: "Parent", Owner: "ceo", LifecycleState: LifecycleStateRunning}
	sub := teamTask{ID: "OFFICE-2", Channel: "slack-general", Title: "Unowned", ParentIssueID: "OFFICE-1", LifecycleState: LifecycleStateRunning}
	seedReportTask(b, parent)
	seedReportTask(b, sub)

	r := &slackTaskReporter{t: tr, lastState: map[string]string{}, seenSubtasks: map[string]bool{}}
	r.reconcile(context.Background())
	for _, p := range api.snapshotPosts() {
		if strings.Contains(p.Text, "Subtask") {
			t.Fatalf("an unowned subtask must not be announced yet: %q", p.Text)
		}
	}
	if r.seenSubtasks["OFFICE-2"] {
		t.Fatal("unowned subtask must stay unseen so a later owner triggers the ping")
	}

	// Assign an owner → now it pings.
	setReportTaskOwner(b, "OFFICE-2", "ceo")
	r.reconcile(context.Background())
	found := false
	for _, p := range api.snapshotPosts() {
		if strings.Contains(p.Text, "Subtask") {
			found = true
		}
	}
	if !found {
		t.Fatal("subtask should be announced once an owner is assigned")
	}
}

func setReportTaskOwner(b *Broker, id, owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].Owner = owner
		}
	}
}

// --- Item 3: lifecycle reporting ---

func TestReportLifecycleTransitionsAndDeDupes(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://host")
	seedReportTask(b, teamTask{ID: "OFFICE-5", Channel: "slack-general", Title: "Ship it", Owner: "ceo", LifecycleState: LifecycleStateRunning})

	r := &slackTaskReporter{t: tr, lastState: map[string]string{}, seenSubtasks: map[string]bool{}}
	// First sighting (running) is a transition from "" → posts.
	r.reconcile(context.Background())
	if countLifecycleLines(api, "running") != 1 {
		t.Fatalf("expected one running line, posts=%+v", api.snapshotPosts())
	}
	// Same state → no new post.
	r.reconcile(context.Background())
	if countLifecycleLines(api, "running") != 1 {
		t.Fatalf("identical state must not re-post: posts=%+v", api.snapshotPosts())
	}
	// Transition to review → one new post.
	setReportTaskState(b, "OFFICE-5", LifecycleStateReview)
	r.reconcile(context.Background())
	if countLifecycleLines(api, "review") != 1 {
		t.Fatalf("expected one review line, posts=%+v", api.snapshotPosts())
	}
	// The lifecycle line links the task id.
	for _, p := range api.snapshotPosts() {
		if strings.Contains(p.Text, "review") && !strings.Contains(p.Text, "<http://host/tasks/OFFICE-5|OFFICE-5>") {
			t.Errorf("lifecycle line must link the task id: %q", p.Text)
		}
	}
}

func countLifecycleLines(api *fakeSlackAPI, state string) int {
	n := 0
	for _, p := range api.snapshotPosts() {
		if strings.Contains(p.Text, "is now") && strings.Contains(p.Text, state) {
			n++
		}
	}
	return n
}

// prime() must suppress replaying existing state on (re)start.
func TestReporterPrimeSuppressesReplay(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	seedReportTask(b, teamTask{ID: "OFFICE-9", Channel: "slack-general", Title: "Existing", Owner: "hermes", ParentIssueID: "OFFICE-1", LifecycleState: LifecycleStateRunning})
	seedReportTask(b, teamTask{ID: "OFFICE-1", Channel: "slack-general", Title: "Parent", Owner: "ceo", LifecycleState: LifecycleStateRunning})

	r := &slackTaskReporter{t: tr, lastState: map[string]string{}, seenSubtasks: map[string]bool{}}
	r.prime()
	r.reconcile(context.Background())
	if posts := api.snapshotPosts(); len(posts) != 0 {
		t.Fatalf("primed reporter must not replay existing tasks: %+v", posts)
	}
}

// --- Item 4: wiki link reporting ---

func TestReportWikiLinksIntoActiveTaskThread(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://host")
	// An active task owned by the wiki author so the article has a home thread.
	seedReportTask(b, teamTask{ID: "OFFICE-3", Channel: "slack-general", Title: "Research", Owner: "scout", LifecycleState: LifecycleStateRunning})
	// Ensure the thread root exists (the reporter would otherwise create it).
	if ts := tr.ensureTaskThreadRoot(context.Background(), "OFFICE-3"); ts == "" {
		t.Fatal("expected a thread root for the active task")
	}

	r := &slackTaskReporter{t: tr, lastState: map[string]string{}, seenSubtasks: map[string]bool{}}
	r.reportWiki(context.Background(), wikiWriteEvent{Path: "team/pricing-model.md", AuthorSlug: "scout"})

	var line string
	for _, p := range api.snapshotPosts() {
		if strings.Contains(p.Text, "wiki article") {
			line = p.Text
			if p.ThreadTS == "" {
				t.Errorf("wiki line must be threaded: %+v", p)
			}
		}
	}
	if line == "" {
		t.Fatalf("expected a wiki report line, posts=%+v", api.snapshotPosts())
	}
	if !strings.Contains(line, "<http://host/wiki/team/pricing-model.md|pricing model>") {
		t.Errorf("wiki line must link the article: %q", line)
	}
}

func TestReportWikiNoActiveThreadIsSilent(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://host")

	r := &slackTaskReporter{t: tr, lastState: map[string]string{}, seenSubtasks: map[string]bool{}}
	r.reportWiki(context.Background(), wikiWriteEvent{Path: "team/orphan.md", AuthorSlug: "nobody"})
	if posts := api.snapshotPosts(); len(posts) != 0 {
		t.Fatalf("a wiki write with no active author thread must be silent: %+v", posts)
	}
}

func TestWikiTitleFromPath(t *testing.T) {
	cases := map[string]string{
		"team/pricing-model.md":      "pricing model",
		"deep/nested/foo_bar.md":     "foo bar",
		"no-ext":                     "no ext",
		"team/.reviews/x.facts.json": "x.facts",
	}
	for in, want := range cases {
		if got := wikiTitleFromPath(in); got != want {
			t.Errorf("wikiTitleFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}
