package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// TestHasUnresolvedDepsLocked_TerminalStatusesResolve pins the contract:
// any terminal status — done, completed, canceled, cancelled — counts
// as a resolved dependency. Active statuses (in_progress, blocked) and
// missing IDs are unresolved. This mirrors requestIsResolvedLocked for
// humanInterview deps so a parent's cancellation no longer permanently
// orphans every dependent task.
func TestHasUnresolvedDepsLocked_TerminalStatusesResolve(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	defer b.mu.Unlock()

	b.tasks = []teamTask{
		{ID: "dep-done", Status: "done"},
		{ID: "dep-completed", Status: "completed"},
		{ID: "dep-canceled", Status: "canceled"},
		{ID: "dep-cancelled", Status: "cancelled"},
		{ID: "dep-in-progress", Status: "in_progress"},
		{ID: "dep-blocked", Status: "blocked"},
	}

	cases := []struct {
		name string
		dep  string
		want bool
	}{
		{"resolved when done", "dep-done", false},
		{"resolved when completed", "dep-completed", false},
		{"resolved when canceled", "dep-canceled", false},
		{"resolved when cancelled", "dep-cancelled", false},
		{"unresolved when in_progress", "dep-in-progress", true},
		{"unresolved when blocked", "dep-blocked", true},
		{"unresolved when missing", "ghost-id", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &teamTask{DependsOn: []string{tc.dep}}
			if got := b.hasUnresolvedDepsLocked(task); got != tc.want {
				t.Errorf("dep=%q: want %v, got %v", tc.dep, tc.want, got)
			}
		})
	}

	// Mixed: any unresolved among many returns true.
	mixed := &teamTask{DependsOn: []string{"dep-done", "dep-in-progress"}}
	if !b.hasUnresolvedDepsLocked(mixed) {
		t.Error("mixed deps with one in_progress: expected unresolved=true")
	}

	// Mixed terminals only: all resolved.
	terminals := &teamTask{DependsOn: []string{"dep-done", "dep-cancelled", "dep-completed"}}
	if b.hasUnresolvedDepsLocked(terminals) {
		t.Error("all-terminal deps: expected unresolved=false")
	}
}

// TestReconcileOrphanedBlockedTasksLocked_UnblocksCancelledParent
// pins the persisted-state migration that fixes orphaned dependents
// from before the cancelled-dep fix landed. A task that was Blocked=
// true while its parent was cancelled now resumes on broker reload —
// previously it stayed blocked forever because the old
// unblockDependentsLocked only fired on Status="done". The migration
// is idempotent: running a second time has no effect.
func TestReconcileOrphanedBlockedTasksLocked_UnblocksCancelledParent(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "parent-canceled", Status: "canceled", Channel: "general"},
		{ID: "parent-done", Status: "done", Channel: "general"},
		{ID: "parent-active", Status: "in_progress", Channel: "general"},
		{
			ID: "orphan-1", Status: "blocked", Blocked: true, Channel: "general",
			DependsOn: []string{"parent-canceled"},
		},
		{
			ID: "stuck", Status: "blocked", Blocked: true, Channel: "general",
			DependsOn: []string{"parent-active"},
		},
		{
			ID: "orphan-multi", Status: "blocked", Blocked: true, Channel: "general",
			DependsOn: []string{"parent-canceled", "parent-done"},
		},
	}

	b.reconcileOrphanedBlockedTasksLocked()

	byID := map[string]*teamTask{}
	for i := range b.tasks {
		byID[b.tasks[i].ID] = &b.tasks[i]
	}

	if got := byID["orphan-1"]; got.Blocked || got.Status != "in_progress" {
		t.Errorf("orphan-1 should be unblocked, got Blocked=%v Status=%q", got.Blocked, got.Status)
	}
	if got := byID["orphan-multi"]; got.Blocked || got.Status != "in_progress" {
		t.Errorf("orphan-multi should be unblocked, got Blocked=%v Status=%q", got.Blocked, got.Status)
	}
	if got := byID["stuck"]; !got.Blocked || got.Status != "blocked" {
		t.Errorf("stuck (parent still active) must remain blocked, got Blocked=%v Status=%q", got.Blocked, got.Status)
	}

	// Idempotent: running again should be a no-op for previously
	// reconciled tasks.
	beforeSecond := make(map[string]string, len(b.tasks))
	for i := range b.tasks {
		beforeSecond[b.tasks[i].ID] = b.tasks[i].UpdatedAt
	}
	b.reconcileOrphanedBlockedTasksLocked()
	for i := range b.tasks {
		if b.tasks[i].UpdatedAt != beforeSecond[b.tasks[i].ID] {
			t.Errorf("task %s UpdatedAt changed on second reconcile run", b.tasks[i].ID)
		}
	}
	b.mu.Unlock()
}

// TestTaskReuseMatch_HasScopedIdentity pins the precondition for
// scoped-identity dedupe in findReusableTaskLocked: a match counts as
// scoped only when SourceSignalID or SourceDecisionID is non-empty.
// Empty strings (including whitespace) must NOT register as scoped.
func TestTaskReuseMatch_HasScopedIdentity(t *testing.T) {
	cases := []struct {
		name string
		m    taskReuseMatch
		want bool
	}{
		{"empty", taskReuseMatch{}, false},
		{"whitespace signal", taskReuseMatch{SourceSignalID: "   "}, false},
		{"signal set", taskReuseMatch{SourceSignalID: "sig-1"}, true},
		{"decision set", taskReuseMatch{SourceDecisionID: "dec-1"}, true},
		{"both set", taskReuseMatch{SourceSignalID: "sig-1", SourceDecisionID: "dec-1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.hasScopedIdentity(); got != tc.want {
				t.Errorf("hasScopedIdentity(%+v): want %v, got %v", tc.m, tc.want, got)
			}
		})
	}
}

func TestTaskAndRequestViewsRejectNonMembers(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "deals",
		"name":        "deals",
		"description": "Deal strategy and pipeline work.",
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create channel failed: %v", err)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks?channel=deals&viewer_slug=fe", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get tasks as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member task access, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=deals&viewer_slug=fe", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get requests as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member request access, got %d", resp.StatusCode)
	}
}

func TestBrokerTaskLifecycle(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Frontend only",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})
	if created.Status != "in_progress" || created.Owner != "fe" {
		t.Fatalf("unexpected created task: %+v", created)
	}
	if created.FollowUpAt == "" || created.ReminderAt == "" || created.RecheckAt == "" {
		t.Fatalf("expected follow-up timestamps on task create, got %+v", created)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Actions   []officeActionLog `json:"actions"`
		Scheduler []schedulerJob    `json:"scheduler"`
		Due       []schedulerJob    `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if len(queue.Scheduler) == 0 {
		t.Fatalf("expected queue to expose scheduler state, got %+v", queue)
	}

	completed := post(map[string]any{
		"action": "complete",
		"id":     created.ID,
	})
	if completed.Status != "done" {
		t.Fatalf("expected done task, got %+v", completed)
	}
	if completed.FollowUpAt != "" || completed.ReminderAt != "" || completed.RecheckAt != "" {
		t.Fatalf("expected completion to clear follow-up timestamps, got %+v", completed)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tasks get failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode tasks list: %v", err)
	}
	if len(listing.Tasks) != 0 {
		t.Fatalf("expected done task to be hidden by default, got %+v", listing.Tasks)
	}
}

func TestBrokerTaskReassignNotifies(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Ship reassign flow",
		"created_by": "human",
		"owner":      "engineering",
	})
	if created.Owner != "engineering" {
		t.Fatalf("expected initial owner engineering, got %+v", created)
	}

	before := len(b.Messages())

	// Reassign engineering → ops.
	updated := post(map[string]any{
		"action":     "reassign",
		"id":         created.ID,
		"owner":      "ops",
		"created_by": "human",
	})
	if updated.Owner != "ops" {
		t.Fatalf("expected owner=ops after reassign, got %q", updated.Owner)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("expected status=in_progress after reassign, got %q", updated.Status)
	}

	msgs := b.Messages()[before:]
	if len(msgs) != 3 {
		for i, m := range msgs {
			t.Logf("msg[%d] channel=%s from=%s content=%q", i, m.Channel, m.From, m.Content)
		}
		t.Fatalf("expected 3 reassign messages (channel + new + prev), got %d", len(msgs))
	}

	taskChannel := normalizeChannelSlug(updated.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	newDM := channelDirectSlug("human", "ops")
	prevDM := channelDirectSlug("human", "engineering")

	seen := map[string]channelMessage{}
	for _, m := range msgs {
		seen[m.Channel] = m
		if m.Kind != "task_reassigned" {
			t.Fatalf("expected kind=task_reassigned, got %q", m.Kind)
		}
		if m.From != "human" {
			t.Fatalf("expected from=human, got %q", m.From)
		}
	}
	chMsg, ok := seen[taskChannel]
	if !ok {
		t.Fatalf("expected channel message in %q; saw %v", taskChannel, keys(seen))
	}
	if !containsAll(chMsg.Tagged, []string{"ceo", "ops", "engineering"}) {
		t.Fatalf("expected channel message tagged ceo+ops+engineering, got %v", chMsg.Tagged)
	}
	if !strings.Contains(chMsg.Content, "@engineering") || !strings.Contains(chMsg.Content, "@ops") {
		t.Fatalf("expected channel content to name both owners, got %q", chMsg.Content)
	}
	if _, ok := seen[newDM]; !ok {
		t.Fatalf("expected DM to new owner in %q; saw %v", newDM, keys(seen))
	}
	if _, ok := seen[prevDM]; !ok {
		t.Fatalf("expected DM to prev owner in %q; saw %v", prevDM, keys(seen))
	}

	// Re-posting with the same owner should be a no-op on notifications.
	before2 := len(b.Messages())
	post(map[string]any{
		"action":     "reassign",
		"id":         created.ID,
		"owner":      "ops",
		"created_by": "human",
	})
	after2 := b.Messages()[before2:]
	for _, m := range after2 {
		if m.Kind == "task_reassigned" {
			t.Fatalf("expected no new task_reassigned messages for same-owner reassign, got %+v", m)
		}
	}
}

func TestBrokerTaskCancelNotifies(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Pilot the new onboarding deck",
		"created_by": "human",
		"owner":      "design",
	})
	before := len(b.Messages())

	canceled := post(map[string]any{
		"action":     "cancel",
		"id":         created.ID,
		"created_by": "human",
	})
	if canceled.Status != "canceled" {
		t.Fatalf("expected status=canceled, got %q", canceled.Status)
	}
	if canceled.FollowUpAt != "" || canceled.ReminderAt != "" || canceled.RecheckAt != "" {
		t.Fatalf("expected cleared follow-up timestamps on cancel, got %+v", canceled)
	}

	all := b.Messages()[before:]
	msgs := make([]channelMessage, 0, len(all))
	for _, m := range all {
		if m.Kind == "task_canceled" {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) != 2 {
		for i, m := range all {
			t.Logf("all[%d] channel=%s kind=%s content=%q", i, m.Channel, m.Kind, m.Content)
		}
		t.Fatalf("expected 2 task_canceled messages (channel + owner DM), got %d", len(msgs))
	}
	taskChannel := normalizeChannelSlug(canceled.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	ownerDM := channelDirectSlug("human", "design")
	found := map[string]bool{}
	for _, m := range msgs {
		found[m.Channel] = true
	}
	if !found[taskChannel] {
		t.Fatalf("missing channel cancel message in %q", taskChannel)
	}
	if !found[ownerDM] {
		t.Fatalf("missing owner DM cancel message in %q", ownerDM)
	}
}

func channelDirectSlug(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "__" + b
}

func keys(m map[string]channelMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsAll(got, want []string) bool {
	set := make(map[string]struct{}, len(got))
	for _, g := range got {
		set[g] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

func TestBrokerOfficeFeatureTaskForGTMCompletesWithoutReviewAndUnblocksDependents(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	thesis := post(map[string]any{
		"action":         "create",
		"title":          "Define the YouTube business thesis",
		"details":        "Pick the niche and monetization ladder.",
		"created_by":     "ceo",
		"owner":          "gtm",
		"thread_id":      "msg-1",
		"task_type":      "feature",
		"execution_mode": "office",
	})
	if thesis.ReviewState != "not_required" {
		t.Fatalf("expected GTM office feature task to skip review, got %+v", thesis)
	}

	launch := post(map[string]any{
		"action":         "create",
		"title":          "Create the launch package",
		"details":        "Build the 30-video slate.",
		"created_by":     "ceo",
		"owner":          "gtm",
		"thread_id":      "msg-1",
		"task_type":      "launch",
		"execution_mode": "office",
		"depends_on":     []string{thesis.ID},
	})
	if !launch.Blocked {
		t.Fatalf("expected dependent launch task to start blocked, got %+v", launch)
	}

	completed := post(map[string]any{
		"action": "complete",
		"id":     thesis.ID,
	})
	if completed.Status != "done" || completed.ReviewState != "not_required" {
		t.Fatalf("expected thesis task to complete directly without review, got %+v", completed)
	}

	var unblocked teamTask
	for _, task := range b.AllTasks() {
		if task.ID == launch.ID {
			unblocked = task
			break
		}
	}
	if unblocked.ID == "" {
		t.Fatalf("expected to find dependent task %s", launch.ID)
	}
	if unblocked.Blocked {
		t.Fatalf("expected dependent task to unblock after thesis completion, got %+v", unblocked)
	}
}

func TestBrokerTaskCreateReusesExistingOpenTask(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Initial FE pass",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})
	second := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Updated details",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})

	if first.ID != second.ID {
		t.Fatalf("expected task reuse, got %s and %s", first.ID, second.ID)
	}
	if second.Details != "Updated details" {
		t.Fatalf("expected task details to update, got %+v", second)
	}
	if got := len(b.ChannelTasks("general")); got != 1 {
		t.Fatalf("expected one open task after reuse, got %d", got)
	}
}

func TestBrokerEnsurePlannedTaskKeepsScopedDuplicateTitlesDistinct(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)

	first, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Publish faceless AI ops episode",
		Details:          "Episode 1 pipeline task",
		Owner:            "eng",
		CreatedBy:        "ceo",
		TaskType:         "feature",
		PipelineID:       "youtube-factory",
		SourceDecisionID: "decision-episode-1",
	})
	if err != nil || reused {
		t.Fatalf("first ensure planned task: %v reused=%v", err, reused)
	}

	second, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Publish faceless AI ops episode",
		Details:          "Episode 2 pipeline task",
		Owner:            "eng",
		CreatedBy:        "ceo",
		TaskType:         "feature",
		PipelineID:       "youtube-factory",
		SourceDecisionID: "decision-episode-2",
	})
	if err != nil || reused {
		t.Fatalf("second ensure planned task: %v reused=%v", err, reused)
	}
	if first.ID == second.ID {
		t.Fatalf("expected distinct tasks for duplicate scoped titles, got %s", first.ID)
	}
	if got := len(b.ChannelTasks("general")); got != 2 {
		t.Fatalf("expected two planned tasks after duplicate scoped titles, got %d", got)
	}

	retry, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Publish faceless AI ops episode",
		Details:          "Episode 2 retry",
		Owner:            "eng",
		CreatedBy:        "ceo",
		TaskType:         "feature",
		PipelineID:       "youtube-factory",
		SourceDecisionID: "decision-episode-2",
	})
	if err != nil || !reused {
		t.Fatalf("retry ensure planned task: %v reused=%v", err, reused)
	}
	if retry.ID != second.ID {
		t.Fatalf("expected scoped retry to reuse second task, got %s want %s", retry.ID, second.ID)
	}
}

func TestBrokerTaskCreateKeepsDistinctTasksInSameThread(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":     "create",
		"title":      "Build the operating system",
		"details":    "Engineering lane",
		"created_by": "ceo",
		"owner":      "eng",
		"thread_id":  "msg-1",
	})
	second := post(map[string]any{
		"action":     "create",
		"title":      "Lock the channel thesis",
		"details":    "GTM lane",
		"created_by": "ceo",
		"owner":      "gtm",
		"thread_id":  "msg-1",
	})

	if first.ID == second.ID {
		t.Fatalf("expected distinct tasks in the same thread, got reused task id %q", first.ID)
	}
	if got := len(b.ChannelTasks("general")); got != 2 {
		t.Fatalf("expected two open tasks after distinct creates, got %d", got)
	}
}

func TestBrokerTaskPlanAssignsWorktreeForLocalWorktreeTask(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "operator",
		"tasks": []map[string]any{
			{
				"title":          "Build intake dry-run review bundle",
				"details":        "Produce the first dry-run consulting artifact bundle.",
				"assignee":       "builder",
				"task_type":      "feature",
				"execution_mode": "local_worktree",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task plan request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task plan response: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected one task, got %+v", result.Tasks)
	}
	if result.Tasks[0].ExecutionMode != "local_worktree" {
		t.Fatalf("expected local_worktree task, got %+v", result.Tasks[0])
	}
	if result.Tasks[0].WorktreePath == "" || result.Tasks[0].WorktreeBranch == "" {
		t.Fatalf("expected task plan to assign worktree metadata, got %+v", result.Tasks[0])
	}
}

func TestBrokerTaskCreateAddsAssignedOwnerToChannelMembers(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "youtube-factory", "operator", "Operator")
	if existing := b.findMemberLocked("builder"); existing == nil {
		member := officeMember{Slug: "builder", Name: "Builder"}
		applyOfficeMemberDefaults(&member)
		b.members = append(b.members, member)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "create",
		"channel":    "youtube-factory",
		"title":      "Restore remotion dependency path",
		"details":    "Unblock the real render lane.",
		"created_by": "operator",
		"owner":      "builder",
		"task_type":  "feature",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked("youtube-factory")
	if ch == nil {
		t.Fatal("expected youtube-factory channel to exist")
	}
	if !containsString(ch.Members, "builder") {
		t.Fatalf("expected assigned owner to be added to channel members, got %v", ch.Members)
	}
	if containsString(ch.Disabled, "builder") {
		t.Fatalf("expected assigned owner to be enabled in channel, got disabled=%v", ch.Disabled)
	}
}

func TestBrokerResumeTaskUnblocksAndSchedulesOwnerLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Retry kickoff send",
		Details:       "429 RESOURCE_EXHAUSTED. Retry after 2026-04-15T22:00:29.610Z.",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "live_external",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	if _, changed, err := b.BlockTask(task.ID, "operator", "Provider cooldown"); err != nil || !changed {
		t.Fatalf("block task: %v changed=%v", err, changed)
	}

	resumed, changed, err := b.ResumeTask(task.ID, "watchdog", "Retry window passed")
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	if !changed {
		t.Fatalf("expected resume to change task state, got %+v", resumed)
	}
	if resumed.Blocked || resumed.Status != "in_progress" {
		t.Fatalf("expected resumed task to be active, got %+v", resumed)
	}
	if resumed.FollowUpAt == "" {
		t.Fatalf("expected resumed task to have follow-up lifecycle timestamps, got %+v", resumed)
	}
}

func TestBrokerResumeTaskQueuesBehindExistingExclusiveOwnerLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	active, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Send kickoff email",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "live_external",
	})
	if err != nil || reused {
		t.Fatalf("ensure active task: %v reused=%v", err, reused)
	}
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Send second kickoff email",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "live_external",
		DependsOn:     []string{active.ID},
	})
	if err != nil || reused {
		t.Fatalf("ensure queued task: %v reused=%v", err, reused)
	}
	if !task.Blocked {
		t.Fatalf("expected second task to start blocked behind active lane, got %+v", task)
	}
	if _, changed, err := b.BlockTask(task.ID, "operator", "provider cooldown"); err != nil || !changed {
		t.Fatalf("block task: %v changed=%v", err, changed)
	}

	resumed, changed, err := b.ResumeTask(task.ID, "watchdog", "Retry window passed")
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	if !changed {
		t.Fatalf("expected resume to change task state, got %+v", resumed)
	}
	if resumed.Status != "open" || !resumed.Blocked {
		t.Fatalf("expected resumed task to stay queued behind active lane, got %+v", resumed)
	}
	if !containsString(resumed.DependsOn, active.ID) {
		t.Fatalf("expected resumed task to remain dependent on active lane, got %+v", resumed)
	}
}

func TestBrokerUnblockDependentsQueuesExclusiveOwnerLanes(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "youtube-factory", "ceo", "CEO")
	ensureTestMemberAccess(b, "youtube-factory", "executor", "Executor")

	now := time.Now().UTC().Format(time.RFC3339)
	b.tasks = []teamTask{
		{
			ID:            "task-setup",
			Channel:       "youtube-factory",
			Title:         "Finish prerequisite slice",
			Owner:         "executor",
			Status:        "done",
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "local_worktree",
			ReviewState:   "approved",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "task-32",
			Channel:       "youtube-factory",
			Title:         "First dependent lane",
			Owner:         "executor",
			Status:        "blocked",
			Blocked:       true,
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "live_external",
			DependsOn:     []string{"task-setup"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "task-34",
			Channel:       "youtube-factory",
			Title:         "Second dependent lane",
			Owner:         "executor",
			Status:        "blocked",
			Blocked:       true,
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "live_external",
			DependsOn:     []string{"task-setup"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "task-80",
			Channel:       "youtube-factory",
			Title:         "Third dependent lane",
			Owner:         "executor",
			Status:        "blocked",
			Blocked:       true,
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "live_external",
			DependsOn:     []string{"task-setup"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	b.mu.Lock()
	b.unblockDependentsLocked("task-setup")
	got := append([]teamTask(nil), b.tasks...)
	b.mu.Unlock()

	if got[1].Status != "in_progress" || got[1].Blocked {
		t.Fatalf("expected first dependent to become active, got %+v", got[1])
	}
	for _, task := range got[2:] {
		if task.Status != "open" || !task.Blocked {
			t.Fatalf("expected later dependent to stay queued, got %+v", task)
		}
		if !containsString(task.DependsOn, "task-32") {
			t.Fatalf("expected later dependent to queue behind task-32, got %+v", task)
		}
	}
}

func TestBrokerTaskPlanRejectsTheaterTaskInLiveDeliveryLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-delivery", "operator", "Operator")
	ensureTestMemberAccess(b, "client-delivery", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "client-delivery",
		"created_by": "operator",
		"tasks": []map[string]any{
			{
				"title":          "Generate consulting review packet artifact from the updated blueprint",
				"details":        "Post the exact local artifact path for the reviewer.",
				"assignee":       "builder",
				"task_type":      "feature",
				"execution_mode": "local_worktree",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task plan request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}
}

func TestBrokerTaskCreateRejectsLiveBusinessTheater(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":         "create",
		"channel":        "general",
		"title":          "Create one new Notion proof packet for the client handoff",
		"details":        "Use live external execution and keep the review bundle in sync.",
		"created_by":     "operator",
		"owner":          "builder",
		"task_type":      "launch",
		"execution_mode": "live_external",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected theater rejection, got status %d: %s", resp.StatusCode, raw)
	}
}

func TestBrokerTaskCompleteRejectsLiveBusinessTheater(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:            "task-1",
		Channel:       "general",
		Title:         "Create one new Notion proof packet for the client handoff",
		Details:       "Use live external execution and keep the review bundle in sync.",
		Owner:         "builder",
		Status:        "in_progress",
		CreatedBy:     "operator",
		TaskType:      "launch",
		ExecutionMode: "live_external",
		CreatedAt:     "2026-04-15T00:00:00Z",
		UpdatedAt:     "2026-04-15T00:00:00Z",
	}}
	b.counter = 1
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         "task-1",
		"created_by": "builder",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task complete request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected theater rejection on completion, got status %d: %s", resp.StatusCode, raw)
	}
}

func TestBrokerStoresLedgerAndReviewLifecycle(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	signals, err := b.RecordSignals([]officeSignal{{
		ID:         "nex-1",
		Source:     "nex_insights",
		Kind:       "risk",
		Title:      "Nex insight",
		Content:    "Signup conversion is slipping.",
		Channel:    "general",
		Owner:      "fe",
		Confidence: "high",
		Urgency:    "high",
	}})
	if err != nil || len(signals) != 1 {
		t.Fatalf("record signals: %v %v", err, signals)
	}
	decision, err := b.RecordDecision("create_task", "general", "Open a frontend follow-up.", "High-signal conversion risk.", "fe", []string{signals[0].ID}, false, false)
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Build signup conversion fix",
		Details:          "Own the CTA and onboarding flow.",
		Owner:            "fe",
		CreatedBy:        "ceo",
		ThreadID:         "msg-1",
		TaskType:         "feature",
		SourceSignalID:   signals[0].ID,
		SourceDecisionID: decision.ID,
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	if task.PipelineStage != "implement" || task.ExecutionMode != "local_worktree" || task.SourceDecisionID != decision.ID {
		t.Fatalf("expected structured task metadata, got %+v", task)
	}
	if task.WorktreePath == "" || task.WorktreeBranch == "" {
		t.Fatalf("expected planned task worktree metadata, got %+v", task)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "you",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode completed task: %v", err)
	}
	if result.Task.Status != "review" || result.Task.ReviewState != "ready_for_review" {
		t.Fatalf("expected review-ready task, got %+v", result.Task)
	}

	if _, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", task.ID, "fe", "Task is waiting for movement."); err != nil {
		t.Fatalf("create watchdog: %v", err)
	}
	if len(b.Decisions()) != 1 || len(b.Signals()) != 1 || len(b.Watchdogs()) != 1 {
		t.Fatalf("expected ledger state, got signals=%d decisions=%d watchdogs=%d", len(b.Signals()), len(b.Decisions()), len(b.Watchdogs()))
	}
}

func TestBrokerReleaseTaskCleansWorktree(t *testing.T) {
	var cleanedPath, cleanedBranch string
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error {
		cleanedPath = path
		cleanedBranch = branch
		return nil
	})
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Build signup conversion fix",
		Owner:     "fe",
		CreatedBy: "ceo",
		TaskType:  "feature",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "release",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("release task: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode released task: %v", err)
	}
	if cleanedPath == "" || cleanedBranch == "" {
		t.Fatalf("expected cleanup to run, got path=%q branch=%q", cleanedPath, cleanedBranch)
	}
	if result.Task.WorktreePath != "" || result.Task.WorktreeBranch != "" {
		t.Fatalf("expected released task worktree metadata to clear, got %+v", result.Task)
	}
}

func TestBrokerApproveRetainsLocalWorktree(t *testing.T) {
	cleanupCalls := 0
	worktreeRoot := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		path := filepath.Join(worktreeRoot, "wuphf-task-"+taskID)
		initUsableGitWorktree(t, path)
		return path, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error {
		cleanupCalls++
		return nil
	})
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Build signup conversion fix",
		Owner:     "fe",
		CreatedBy: "ceo",
		TaskType:  "feature",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	completeBody, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "fe",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(completeBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	resp.Body.Close()

	approveBody, _ := json.Marshal(map[string]any{
		"action":     "approve",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(approveBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve task: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode approved task: %v", err)
	}
	if result.Task.Status != "done" || result.Task.ReviewState != "approved" {
		t.Fatalf("expected approved task to be done/approved, got %+v", result.Task)
	}
	if result.Task.WorktreePath == "" || result.Task.WorktreeBranch == "" {
		t.Fatalf("expected approved task to retain worktree metadata, got %+v", result.Task)
	}
	if cleanupCalls != 0 {
		t.Fatalf("expected approved task to retain worktree without cleanup, got %d cleanup calls", cleanupCalls)
	}
}

func ensureTestMemberAccess(b *Broker, channel, slug, name string) {
	if b == nil {
		return
	}
	slug = normalizeChannelSlug(slug)
	if slug == "" {
		return
	}
	if existing := b.findMemberLocked(slug); existing == nil {
		member := officeMember{Slug: slug, Name: name}
		applyOfficeMemberDefaults(&member)
		b.members = append(b.members, member)
	}
	for i := range b.channels {
		if normalizeChannelSlug(b.channels[i].Slug) != normalizeChannelSlug(channel) {
			continue
		}
		if !containsString(b.channels[i].Members, slug) {
			b.channels[i].Members = append(b.channels[i].Members, slug)
		}
		return
	}
	b.channels = append(b.channels, teamChannel{
		Slug:    normalizeChannelSlug(channel),
		Name:    normalizeChannelSlug(channel),
		Members: []string{slug},
	})
}

func TestBrokerHandlePostTaskRejectsFalseReadOnlyBlockForWritableWorktree(t *testing.T) {
	worktreeDir := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	setVerifyTaskWorktreeWritableForTest(t, func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return nil
	})
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the first runnable generator slice",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "block",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
		"details":    "This turn is running in a read-only filesystem sandbox. Need a writable workspace.",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post block task: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 rejecting bogus workspace block, got %d: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "assigned local worktree is writable") {
		t.Fatalf("expected writable-worktree guidance, got %s", raw)
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active after rejected block, got %+v", updated)
	}
	if strings.Contains(strings.ToLower(updated.Details), "read-only") {
		t.Fatalf("expected false read-only detail to stay out of task state, got %+v", updated)
	}
}

func TestBrokerHandlePostTaskCapabilityGapCreatesSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Post client launch update to Slack",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "follow_up",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	detail := "Unable to continue: missing Slack integration tool path for posting the client update."
	body, _ := json.Marshal(map[string]any{
		"action":     "block",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
		"details":    detail,
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post block task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 blocking task, got %d: %s", resp.StatusCode, raw)
	}

	var blocked teamTask
	var healing teamTask
	for _, candidate := range b.AllTasks() {
		switch candidate.ID {
		case task.ID:
			blocked = candidate
		default:
			if candidate.Title == "Self-heal @eng on "+task.ID {
				healing = candidate
			}
		}
	}
	if !blocked.Blocked || blocked.Status != "blocked" {
		t.Fatalf("expected original task blocked, got %+v", blocked)
	}
	if healing.ID == "" {
		t.Fatalf("expected capability-gap self-healing task, got %+v", b.AllTasks())
	}
	if healing.Owner != "ceo" || healing.TaskType != "incident" || healing.ExecutionMode != "office" {
		t.Fatalf("expected office incident owned by ceo, got %+v", healing)
	}
	if !strings.Contains(healing.Details, "capability_gap") ||
		!strings.Contains(healing.Details, detail) ||
		!strings.Contains(healing.Details, "Repair the missing capability first") {
		t.Fatalf("expected capability repair loop details, got %q", healing.Details)
	}
}

func TestBrokerHandlePostTaskNonCapabilityBlockDoesNotCreateSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Wait for customer approval",
		Owner:     "eng",
		CreatedBy: "ceo",
		TaskType:  "follow_up",
	})
	if err != nil {
		t.Fatalf("ensure planned task: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action":     "block",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
		"details":    "Waiting on customer approval before sending the update.",
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post block task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 blocking task, got %d: %s", resp.StatusCode, raw)
	}

	for _, candidate := range b.AllTasks() {
		if isSelfHealingTaskTitle(candidate.Title) {
			t.Fatalf("did not expect self-healing task for non-capability blocker, got %+v", candidate)
		}
	}
}

func TestBrokerHandlePostTaskResumeUnblocksAfterCapabilityRepair(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Send the launch update",
		Owner:     "eng",
		CreatedBy: "ceo",
		TaskType:  "follow_up",
	})
	if err != nil {
		t.Fatalf("ensure planned task: %v", err)
	}
	if _, changed, err := b.BlockTask(task.ID, "eng", "Unable to continue: missing Slack integration."); err != nil || !changed {
		t.Fatalf("block task: changed=%v err=%v", changed, err)
	}

	body, _ := json.Marshal(map[string]any{
		"action":     "resume",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
		"details":    "Capability repaired: Slack integration is available; retry the original update.",
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post resume task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 resuming task, got %d: %s", resp.StatusCode, raw)
	}

	var resumed teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			resumed = candidate
			break
		}
	}
	if resumed.Blocked || resumed.Status != "in_progress" {
		t.Fatalf("expected original task resumed after repair, got %+v", resumed)
	}
	if !strings.Contains(resumed.Details, "missing Slack integration") ||
		!strings.Contains(resumed.Details, "Capability repaired") {
		t.Fatalf("expected resume detail appended, got %q", resumed.Details)
	}
}

func TestBrokerBlockTaskRejectsFalseReadOnlyBlockForWritableWorktree(t *testing.T) {
	worktreeDir := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	setVerifyTaskWorktreeWritableForTest(t, func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return nil
	})
	b := newTestBroker(t)
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the first runnable generator slice",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	got, changed, err := b.BlockTask(task.ID, "eng", "Need writable workspace because the filesystem sandbox is read-only.")
	if err == nil {
		t.Fatal("expected false read-only block to be rejected")
	}
	if changed {
		t.Fatalf("expected no task state change on rejected block, got %+v", got)
	}
	if !strings.Contains(err.Error(), "assigned local worktree is writable") {
		t.Fatalf("expected writable-worktree guidance, got %v", err)
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active after rejected block, got %+v", updated)
	}
	if strings.Contains(strings.ToLower(updated.Details), "read-only") {
		t.Fatalf("expected false read-only detail to stay out of task state, got %+v", updated)
	}
}

func TestBrokerEnsurePlannedTaskQueuesConcurrentExclusiveOwnerWork(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "executor", "Executor")

	first, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the homepage MVP",
		Details:       "Ship the first runnable site slice.",
		Owner:         "executor",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure first task: %v reused=%v", err, reused)
	}
	second, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Define the upload path",
		Details:       "Wire the next implementation slice after the homepage.",
		Owner:         "executor",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure second task: %v reused=%v", err, reused)
	}

	if first.Status != "in_progress" || first.Blocked {
		t.Fatalf("expected first task to stay active, got %+v", first)
	}
	if second.Status != "open" || !second.Blocked {
		t.Fatalf("expected second task to queue behind the first, got %+v", second)
	}
	if !containsString(second.DependsOn, first.ID) {
		t.Fatalf("expected second task to depend on first %s, got %+v", first.ID, second.DependsOn)
	}
	if !strings.Contains(second.Details, "Queued behind "+first.ID) {
		t.Fatalf("expected queue note in details, got %+v", second)
	}
}

func TestBrokerTaskPlanRoutesLiveBusinessTasksIntoRecentExecutionChannel(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	b.channels = append(b.channels, teamChannel{
		Slug:      "client-loop",
		Name:      "client-loop",
		Members:   []string{"ceo", "builder"},
		CreatedBy: "ceo",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "ceo",
		"tasks": []map[string]any{
			{
				"title":          "Create the client-facing operating brief",
				"assignee":       "builder",
				"details":        "Move the live client delivery forward in the workspace and leave the customer-ready brief in the execution lane.",
				"task_type":      "launch",
				"execution_mode": "office",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post task plan: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task plan response: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected one task, got %+v", result.Tasks)
	}
	if result.Tasks[0].Channel != "client-loop" {
		t.Fatalf("expected task to route into client-loop, got %+v", result.Tasks[0])
	}
}

func TestBrokerTaskPlanReusesExistingActiveLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	for i := range b.channels {
		if normalizeChannelSlug(b.channels[i].Slug) == "client-loop" {
			b.channels[i].CreatedBy = "operator"
			b.channels[i].CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	existing, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Create live client workspace in Google Drive",
		Details:       "First pass.",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure initial task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "operator",
		"tasks": []map[string]any{
			{
				"title":          "Create live client workspace in Google Drive",
				"assignee":       "builder",
				"details":        "Updated live-work details.",
				"task_type":      "follow_up",
				"execution_mode": "office",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post task plan: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task plan response: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected one task in response, got %+v", result.Tasks)
	}
	if result.Tasks[0].ID != existing.ID {
		t.Fatalf("expected task plan to reuse %s, got %+v", existing.ID, result.Tasks[0])
	}
	if got := len(b.AllTasks()); got != 1 {
		t.Fatalf("expected one durable task after reuse, got %d", got)
	}
	if result.Tasks[0].Channel != "client-loop" {
		t.Fatalf("expected reused task to stay in client-loop, got %+v", result.Tasks[0])
	}
	if result.Tasks[0].Details != "Updated live-work details." {
		t.Fatalf("expected details to update, got %+v", result.Tasks[0])
	}
}

func TestBrokerBlockTaskAllowsReadOnlyBlockWhenWriteProbeFails(t *testing.T) {
	worktreeDir := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	setVerifyTaskWorktreeWritableForTest(t, func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return fmt.Errorf("permission denied")
	})
	b := newTestBroker(t)
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the first runnable generator slice",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	got, changed, err := b.BlockTask(task.ID, "eng", "Need writable workspace because the filesystem sandbox is read-only.")
	if err != nil {
		t.Fatalf("expected real write failure blocker to pass through, got %v", err)
	}
	if !changed {
		t.Fatalf("expected task state change on real blocker, got %+v", got)
	}
	if got.Status != "blocked" || !got.Blocked {
		t.Fatalf("expected blocked task result, got %+v", got)
	}
	if !strings.Contains(got.Details, "read-only") {
		t.Fatalf("expected block reason to persist, got %+v", got)
	}
}

func TestBrokerCompleteClosesReviewTaskAndUnblocksDependents(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	architecture, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Audit the repo and design the automation architecture",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "research",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure architecture task: %v reused=%v", err, reused)
	}
	build, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the v0 automated content factory",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
		DependsOn:     []string{architecture.ID},
	})
	if err != nil || reused {
		t.Fatalf("ensure build task: %v reused=%v", err, reused)
	}
	if !build.Blocked {
		t.Fatalf("expected dependent task to start blocked, got %+v", build)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	reviewReady := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         architecture.ID,
		"created_by": "eng",
	})
	if reviewReady.Status != "review" || reviewReady.ReviewState != "ready_for_review" {
		t.Fatalf("expected first complete to move task into review, got %+v", reviewReady)
	}

	closed := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         architecture.ID,
		"created_by": "ceo",
	})
	if closed.Status != "done" || closed.ReviewState != "approved" {
		t.Fatalf("expected second complete to close review task, got %+v", closed)
	}

	var unblocked teamTask
	for _, task := range b.AllTasks() {
		if task.ID == build.ID {
			unblocked = task
			break
		}
	}
	if unblocked.ID == "" {
		t.Fatalf("expected to find dependent task %s", build.ID)
	}
	if unblocked.Blocked || unblocked.Status != "in_progress" {
		t.Fatalf("expected dependent task to unblock after review close, got %+v", unblocked)
	}
}

func TestBrokerCreateTaskReusesCompletedDependencyWorktree(t *testing.T) {
	var prepareCalls []string
	worktreeRoot := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		prepareCalls = append(prepareCalls, taskID)
		if len(prepareCalls) > 1 {
			return "", "", fmt.Errorf("unexpected prepareTaskWorktree call for %s", taskID)
		}
		path := filepath.Join(worktreeRoot, "wuphf-task-"+taskID)
		initUsableGitWorktree(t, path)
		return path, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":         "create",
		"title":          "Ship the dry-run approval packet generator",
		"details":        "Initial consulting delivery slice",
		"created_by":     "operator",
		"owner":          "builder",
		"thread_id":      "msg-1",
		"execution_mode": "local_worktree",
		"task_type":      "feature",
	})
	if first.WorktreePath == "" || first.WorktreeBranch == "" {
		t.Fatalf("expected first task worktree metadata, got %+v", first)
	}

	reviewReady := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         first.ID,
		"created_by": "builder",
	})
	if reviewReady.Status != "review" || reviewReady.ReviewState != "ready_for_review" {
		t.Fatalf("expected first complete to move task into review, got %+v", reviewReady)
	}

	approved := post(map[string]any{
		"action":     "approve",
		"channel":    "general",
		"id":         first.ID,
		"created_by": "operator",
	})
	if approved.Status != "done" || approved.ReviewState != "approved" {
		t.Fatalf("expected approve to close task, got %+v", approved)
	}

	second := post(map[string]any{
		"action":         "create",
		"title":          "Render the approval packet into a reviewable dry-run bundle",
		"details":        "Reuse the existing generator worktree",
		"created_by":     "operator",
		"owner":          "builder",
		"thread_id":      "msg-2",
		"execution_mode": "local_worktree",
		"task_type":      "feature",
		"depends_on":     []string{first.ID},
	})
	if second.WorktreePath != first.WorktreePath || second.WorktreeBranch != first.WorktreeBranch {
		t.Fatalf("expected dependent task to reuse worktree %s/%s, got %+v", first.WorktreePath, first.WorktreeBranch, second)
	}
	if got := len(prepareCalls); got != 1 {
		t.Fatalf("expected one worktree prepare call, got %d (%v)", got, prepareCalls)
	}
}

func TestBrokerSyncTaskWorktreeReplacesStaleAssignedPath(t *testing.T) {
	stalePath := t.TempDir()
	freshPath := filepath.Join(t.TempDir(), "fresh-worktree")
	var cleaned []string
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return freshPath, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error {
		cleaned = append(cleaned, path+"|"+branch)
		return nil
	})
	b := newTestBroker(t)
	task := &teamTask{
		ID:             "task-80",
		Title:          "Fix onboarding",
		Owner:          "executor",
		Status:         "in_progress",
		ExecutionMode:  "local_worktree",
		WorktreePath:   stalePath,
		WorktreeBranch: "wuphf-stale-task-80",
	}
	if err := b.syncTaskWorktreeLocked(task); err != nil {
		t.Fatalf("syncTaskWorktreeLocked: %v", err)
	}
	if task.WorktreePath != freshPath || task.WorktreeBranch != "wuphf-task-80" {
		t.Fatalf("expected stale worktree to be replaced, got %+v", task)
	}
	if len(cleaned) != 1 || !strings.Contains(cleaned[0], stalePath) {
		t.Fatalf("expected stale worktree cleanup before reprovision, got %v", cleaned)
	}
}

func TestBrokerNormalizeLoadedStateRepairsStaleAssignedWorktree(t *testing.T) {
	stalePath := t.TempDir()
	freshPath := filepath.Join(t.TempDir(), "fresh-worktree")
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return freshPath, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	now := time.Now().UTC().Format(time.RFC3339)
	b := newTestBroker(t)
	b.tasks = []teamTask{{
		ID:             "task-80",
		Channel:        "youtube-factory",
		Title:          "Fix onboarding",
		Owner:          "executor",
		Status:         "in_progress",
		ExecutionMode:  "local_worktree",
		WorktreePath:   stalePath,
		WorktreeBranch: "wuphf-stale-task-80",
		CreatedAt:      now,
		UpdatedAt:      now,
	}}

	b.mu.Lock()
	b.normalizeLoadedStateLocked()
	got := b.tasks[0]
	b.mu.Unlock()

	if got.WorktreePath != freshPath || got.WorktreeBranch != "wuphf-task-80" {
		t.Fatalf("expected normalize to refresh stale worktree, got %+v", got)
	}
}

func TestBrokerUpdatesTaskByIDAcrossChannels(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{
			Slug: "general",
			Name: "general",
		},
		{
			Slug: "planning",
			Name: "planning",
		},
	}
	handler := b.requireAuth(b.handleTasks)
	post := func(payload map[string]any) teamTask {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler(rec, req)
		resp := rec.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"channel":    "planning",
		"title":      "Inventory capabilities and approvals",
		"owner":      "planner",
		"created_by": "human",
	})
	if created.Channel != "planning" {
		t.Fatalf("expected planning task, got %+v", created)
	}

	completed := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         created.ID,
		"created_by": "human",
	})
	if completed.ID != created.ID {
		t.Fatalf("expected to update %s, got %+v", created.ID, completed)
	}
	if completed.Channel != "planning" {
		t.Fatalf("expected task channel to remain planning, got %+v", completed)
	}
	if completed.Status != "done" && completed.Status != "review" {
		t.Fatalf("expected task to move forward, got %+v", completed)
	}
}

func TestBrokerCompleteAlreadyDoneTaskStaysApproved(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Ship publish-pack output",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	reviewReady := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
	})
	if reviewReady.Status != "review" || reviewReady.ReviewState != "ready_for_review" {
		t.Fatalf("expected first complete to move task into review, got %+v", reviewReady)
	}

	approved := post(map[string]any{
		"action":     "approve",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	if approved.Status != "done" || approved.ReviewState != "approved" {
		t.Fatalf("expected approve to close task, got %+v", approved)
	}

	repeatedComplete := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	if repeatedComplete.Status != "done" || repeatedComplete.ReviewState != "approved" {
		t.Fatalf("expected repeated complete to stay done/approved, got %+v", repeatedComplete)
	}
}

func TestResolveTaskIntervalsRespectMinimumFloor(t *testing.T) {
	t.Setenv("WUPHF_TASK_FOLLOWUP_MINUTES", "1")
	t.Setenv("WUPHF_TASK_REMINDER_MINUTES", "1")
	t.Setenv("WUPHF_TASK_RECHECK_MINUTES", "1")

	if got := config.ResolveTaskFollowUpInterval(); got != 2 {
		t.Fatalf("expected follow-up interval floor of 2, got %d", got)
	}
	if got := config.ResolveTaskReminderInterval(); got != 2 {
		t.Fatalf("expected reminder interval floor of 2, got %d", got)
	}
	if got := config.ResolveTaskRecheckInterval(); got != 2 {
		t.Fatalf("expected recheck interval floor of 2, got %d", got)
	}
}

func TestInFlightTasksReturnsOnlyNonTerminalOwned(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Active task", Owner: "fe", Status: "in_progress"},
		{ID: "t2", Title: "Done task", Owner: "fe", Status: "done"},
		{ID: "t3", Title: "No owner", Owner: "", Status: "in_progress"},
		{ID: "t4", Title: "Canceled task", Owner: "be", Status: "canceled"},
		{ID: "t5", Title: "Cancelled task", Owner: "be", Status: "cancelled"},
		{ID: "t6", Title: "Pending with owner", Owner: "pm", Status: "pending"},
		{ID: "t7", Title: "Open with owner", Owner: "ceo", Status: "open"},
	}
	b.mu.Unlock()

	got := b.InFlightTasks()

	// Only tasks with owner AND non-terminal status should be returned.
	// "done", "canceled", "cancelled" are terminal. No-owner tasks excluded.
	if len(got) != 3 {
		t.Fatalf("expected 3 in-flight tasks, got %d: %+v", len(got), got)
	}
	ids := make(map[string]bool)
	for _, task := range got {
		ids[task.ID] = true
	}
	if !ids["t1"] {
		t.Error("expected t1 (in_progress+owner) to be included")
	}
	if !ids["t6"] {
		t.Error("expected t6 (pending+owner) to be included")
	}
	if !ids["t7"] {
		t.Error("expected t7 (open+owner) to be included")
	}
	if ids["t2"] {
		t.Error("expected t2 (done) to be excluded")
	}
	if ids["t3"] {
		t.Error("expected t3 (no owner) to be excluded")
	}
	if ids["t4"] {
		t.Error("expected t4 (canceled) to be excluded")
	}
	if ids["t5"] {
		t.Error("expected t5 (cancelled) to be excluded")
	}
}

func TestInFlightTasksExcludesCompletedStatus(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Active task", Owner: "fe", Status: "in_progress"},
		{ID: "t2", Title: "Completed task", Owner: "fe", Status: "completed"},
	}
	b.mu.Unlock()

	got := b.InFlightTasks()

	// "completed" is a terminal status — should be excluded just like "done".
	if len(got) != 1 {
		t.Fatalf("expected 1 in-flight task, got %d: %+v", len(got), got)
	}
	if got[0].ID != "t1" {
		t.Errorf("expected t1 (in_progress), got %q", got[0].ID)
	}
	for _, task := range got {
		if task.Status == "completed" {
			t.Errorf("completed task %q should not appear in InFlightTasks()", task.ID)
		}
	}
}

func TestBrokerMemoryWorkflowCompletionGateAndOverride(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	postTask := func(payload map[string]any) (*http.Response, []byte) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/tasks", b.Addr()), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post task: %v", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, raw
	}

	createResp, raw := postTask(map[string]any{
		"action":     "create",
		"title":      "Research prior context for onboarding",
		"created_by": "ceo",
		"owner":      "ceo",
		"task_type":  "research",
	})
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createResp.StatusCode, raw)
	}
	var created struct {
		Task teamTask `json:"task"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Task.MemoryWorkflow == nil || !created.Task.MemoryWorkflow.Required {
		t.Fatalf("expected required workflow on create, got %+v", created.Task.MemoryWorkflow)
	}

	completeResp, raw := postTask(map[string]any{
		"action":     "complete",
		"id":         created.Task.ID,
		"created_by": "ceo",
	})
	if completeResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected completion conflict, got status=%d body=%s", completeResp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "memory workflow incomplete") {
		t.Fatalf("expected actionable memory workflow error, got %s", raw)
	}

	overrideResp, raw := postTask(map[string]any{
		"action":                          "complete",
		"id":                              created.Task.ID,
		"created_by":                      "ceo",
		"memory_workflow_override":        true,
		"memory_workflow_override_reason": "Human reviewed and accepted missing memory evidence.",
	})
	if overrideResp.StatusCode != http.StatusOK {
		t.Fatalf("override status=%d body=%s", overrideResp.StatusCode, raw)
	}
	var overridden struct {
		Task teamTask `json:"task"`
	}
	if err := json.Unmarshal(raw, &overridden); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	if overridden.Task.Status != "done" {
		t.Fatalf("expected done after override, got %+v", overridden.Task)
	}
	wf := overridden.Task.MemoryWorkflow
	if wf == nil || wf.Status != MemoryWorkflowStatusOverridden || wf.Override == nil {
		t.Fatalf("expected recorded override, got %+v", wf)
	}
	if wf.Override.Actor != "ceo" || wf.Override.Reason == "" || wf.Override.Timestamp == "" {
		t.Fatalf("override metadata missing: %+v", wf.Override)
	}
}

func TestBrokerTaskPlanInitializesMemoryWorkflow(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "ceo",
		"tasks": []map[string]any{
			{
				"title":     "Map support process memory",
				"assignee":  "ceo",
				"task_type": "process-research",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/task-plan", b.Addr()), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task plan request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task plan status=%d body=%s", resp.StatusCode, raw)
	}
	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode task plan: %v", err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].MemoryWorkflow == nil || !result.Tasks[0].MemoryWorkflow.Required {
		t.Fatalf("expected required workflow from task_plan, got %+v", result.Tasks)
	}
}

func TestBrokerReusedTaskPreservesAndRecomputesMemoryWorkflow(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	postTask := func(payload map[string]any) teamTask {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/tasks", b.Addr()), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post task: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("post task status=%d body=%s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decode task: %v", err)
		}
		return result.Task
	}

	created := postTask(map[string]any{
		"action":     "create",
		"title":      "Research prior context for onboarding",
		"created_by": "ceo",
		"owner":      "ceo",
		"task_type":  "research",
		"thread_id":  "thread-1",
	})
	if _, found, err := b.RecordTaskMemoryCapture(created.ID, "ceo", MemoryWorkflowArtifact{Backend: "markdown", Source: "notebook", Path: "agents/ceo/notebook/onboarding.md"}); err != nil || !found {
		t.Fatalf("record capture found=%v err=%v", found, err)
	}

	reused := postTask(map[string]any{
		"action":     "create",
		"title":      "Research prior context for onboarding",
		"created_by": "ceo",
		"owner":      "ceo",
		"task_type":  "feature",
		"thread_id":  "thread-1",
	})
	if reused.ID != created.ID {
		t.Fatalf("expected task reuse, got new task %s vs %s", reused.ID, created.ID)
	}
	if reused.TaskType != "feature" {
		t.Fatalf("expected reused task type recomputed to feature, got %+v", reused)
	}
	if reused.MemoryWorkflow == nil {
		t.Fatalf("expected existing workflow preserved")
	}
	if reused.MemoryWorkflow.Required {
		t.Fatalf("expected reused feature task not to require workflow, got %+v", reused.MemoryWorkflow)
	}
	if len(reused.MemoryWorkflow.Captures) != 1 {
		t.Fatalf("expected prior captures preserved, got %+v", reused.MemoryWorkflow)
	}
}

func TestBrokerTaskMemoryWorkflowAcceptsContextToolEventShape(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	createBody, _ := json.Marshal(map[string]any{
		"action":     "create",
		"title":      "Research prior context for renewal process",
		"created_by": "ceo",
		"owner":      "ceo",
		"task_type":  "research",
	})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/tasks", b.Addr()), bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		Task teamTask `json:"task"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	workflowBody, _ := json.Marshal(map[string]any{
		"event":   "lookup",
		"task_id": created.Task.ID,
		"actor":   "pm",
		"query":   "renewal process",
		"citations": []map[string]any{
			{
				"corpus":   "shared",
				"backend":  "gbrain",
				"source":   "compiled_truth",
				"slug":     "process/passport-renewal",
				"page_id":  42,
				"chunk_id": 7,
				"title":    "Passport renewal process",
				"snippet":  "Use the updated renewal checklist.",
			},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/tasks/memory-workflow", b.Addr()), bytes.NewReader(workflowBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("record workflow: %v", err)
	}
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workflow status=%d body=%s", resp.StatusCode, raw)
	}
	var updated struct {
		Task teamTask `json:"task"`
	}
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	wf := updated.Task.MemoryWorkflow
	if wf == nil || wf.Lookup.Status != MemoryWorkflowStepStatusSatisfied || len(wf.Citations) != 1 {
		t.Fatalf("expected lookup workflow update, got %+v", wf)
	}
	citation := wf.Citations[0]
	if citation.PageID != "42" || citation.ChunkID != "7" || citation.SourceID != "process/passport-renewal" {
		t.Fatalf("expected normalized gbrain citation ids, got %+v", citation)
	}
}
