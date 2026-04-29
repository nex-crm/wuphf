package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestNormalizeRequestOptions_StableOrderAndIDs pins that calling
// normalizeRequestOptions with empty input returns kind-specific
// defaults in their declared order, and that supplied options are
// preserved in caller order while picking up missing metadata from
// the defaults.
func TestNormalizeRequestOptions_StableOrderAndIDs(t *testing.T) {
	defaults, fallback := requestOptionDefaults("approval")

	// Empty input yields defaults plus the kind's recommendation.
	got, rec := normalizeRequestOptions("approval", "", nil)
	if rec != fallback {
		t.Errorf("recommended: want %q, got %q", fallback, rec)
	}
	if len(got) != len(defaults) {
		t.Fatalf("expected %d defaults, got %d", len(defaults), len(got))
	}
	for i := range defaults {
		if got[i].ID != defaults[i].ID {
			t.Errorf("position %d: ID drift — want %q, got %q", i, defaults[i].ID, got[i].ID)
		}
	}

	// Supplied options preserve order and pick up labels from defaults.
	out, _ := normalizeRequestOptions("approval", "approve", []interviewOption{
		{ID: "reject"},
		{ID: "approve"},
	})
	if len(out) != 2 || out[0].ID != "reject" || out[1].ID != "approve" {
		t.Errorf("expected caller order preserved, got %+v", out)
	}
	// Approval defaults declare a Description for "approve" — assert it is
	// inherited rather than left blank.
	if out[1].Description == "" {
		t.Error("expected approve description to be inherited from defaults")
	}
}

// TestEnrichRequestOptions_AddsKindDefaults pins that empty input
// returns the kind's full default option list (used when callers
// haven't customised the choice surface).
func TestEnrichRequestOptions_AddsKindDefaults(t *testing.T) {
	for _, kind := range []string{"approval", "confirm", "choice", "interview"} {
		t.Run(kind, func(t *testing.T) {
			got := enrichRequestOptions(kind, nil)
			defaults, _ := requestOptionDefaults(kind)
			if len(got) == 0 || len(got) != len(defaults) {
				t.Fatalf("kind=%s: expected %d defaults, got %d", kind, len(defaults), len(got))
			}
			for i := range defaults {
				if got[i].ID != defaults[i].ID {
					t.Errorf("kind=%s pos %d: want %q, got %q", kind, i, defaults[i].ID, got[i].ID)
				}
			}
		})
	}
}

// TestCancelActiveHumanInterviewsLocked_NoOpWhenNonePending pins that
// the cancellation path is a no-op when no active interview matches
// the channel filter, and returns 0 to signal nothing was changed.
func TestCancelActiveHumanInterviewsLocked_NoOpWhenNonePending(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	defer b.mu.Unlock()

	// No requests at all.
	if got := b.cancelActiveHumanInterviewsLocked("human", "abandoned", "general", ""); got != 0 {
		t.Errorf("empty broker: expected 0 cancellations, got %d", got)
	}

	// Add a resolved request — must not be cancelled.
	b.requests = append(b.requests, humanInterview{
		ID:      "req-resolved",
		Kind:    "interview",
		Status:  "resolved",
		Channel: "general",
	})
	if got := b.cancelActiveHumanInterviewsLocked("human", "abandoned", "general", ""); got != 0 {
		t.Errorf("only-resolved broker: expected 0 cancellations, got %d", got)
	}
	if b.requests[0].Status != "resolved" {
		t.Error("resolved request should not be flipped by cancel")
	}
}

func TestBrokerRequestsLifecycle(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request list failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode requests: %v", err)
	}
	if len(listing.Requests) != 1 || listing.Pending == nil {
		t.Fatalf("expected one pending request, got %+v", listing)
	}
	if listing.Requests[0].ReminderAt == "" || listing.Requests[0].FollowUpAt == "" || listing.Requests[0].RecheckAt == "" {
		t.Fatalf("expected reminder timestamps on request create, got %+v", listing.Requests[0])
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":          listing.Requests[0].ID,
		"choice_text": "Yes",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 answering request, got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
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
	for _, job := range queue.Scheduler {
		if job.TargetType == "request" && job.TargetID == listing.Requests[0].ID && !strings.EqualFold(job.Status, "done") {
			t.Fatalf("expected answered request scheduler jobs to complete, got %+v", job)
		}
	}

	if b.HasBlockingRequest() {
		t.Fatal("expected blocking request to clear after answer")
	}
}

// Regression: the broker rejects new messages with 409 whenever ANY blocking
// request is pending (handlePostMessage uses firstBlockingRequest across all
// channels), so GET /requests must expose a "scope=all" view. Without it, the
// web UI only sees per-channel requests and can't render a blocker that lives
// in another channel — leaving the human stuck: can't send, can't see why.
func TestBrokerGetRequestsScopeAllSeesCrossChannelBlocker(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "backend", "ceo", "CEO")
	ensureTestMemberAccess(b, "backend", "human", "Human")
	ensureTestMemberAccess(b, "general", "human", "Human")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	createBody, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "backend",
		"title":    "Deploy approval",
		"question": "Ship the backend migration?",
		"blocking": true,
		"required": true,
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create cross-channel request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating backend request, got %d", resp.StatusCode)
	}

	// Per-channel view (#general) must NOT see the #backend blocker — this is
	// the pre-fix behavior the UI was relying on and is still correct.
	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=general&viewer_slug=human", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("per-channel listing failed: %v", err)
	}
	var perChannel struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&perChannel); err != nil {
		t.Fatalf("decode per-channel response: %v", err)
	}
	resp.Body.Close()
	if len(perChannel.Requests) != 0 || perChannel.Pending != nil {
		t.Fatalf("expected #general view to hide #backend request, got %+v", perChannel)
	}

	// scope=all must include the cross-channel blocker so the overlay can show
	// what's preventing the human from chatting anywhere.
	req, _ = http.NewRequest(http.MethodGet, base+"/requests?scope=all&viewer_slug=human", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scope=all listing failed: %v", err)
	}
	var global struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&global); err != nil {
		t.Fatalf("decode scope=all response: %v", err)
	}
	resp.Body.Close()
	if len(global.Requests) != 1 {
		t.Fatalf("expected 1 blocker across channels, got %d: %+v", len(global.Requests), global.Requests)
	}
	if global.Pending == nil || global.Pending.Channel != "backend" {
		t.Fatalf("expected pending blocker from #backend, got %+v", global.Pending)
	}
}

func TestBrokerCancelBlockingApprovalUnblocksMessages(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Ship it?",
		"blocking": true,
		"required": true,
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create approval failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating approval, got %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created request: %v", err)
	}
	if !b.HasBlockingRequest() {
		t.Fatal("approval should block before it is canceled")
	}

	messageBody, _ := json.Marshal(map[string]any{
		"from":    "you",
		"channel": "general",
		"content": "This should still be blocked.",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(messageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message before cancel failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected approval to block message before cancel, got %d", resp.StatusCode)
	}

	cancelBody, _ := json.Marshal(map[string]any{
		"action": "cancel",
		"id":     created.Request.ID,
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(cancelBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cancel approval failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 canceling approval, got %d: %s", resp.StatusCode, raw)
	}
	if b.HasBlockingRequest() {
		t.Fatal("canceled approval should not block")
	}

	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(messageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message after cancel failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected message after canceled approval to succeed, got %d", resp.StatusCode)
	}
}

func TestBrokerHumanInterviewDoesNotBlockAndCancelsOnHumanMessage(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"kind":     "interview",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Human interview",
		"question": "Which customer segment should we prioritize?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-thread-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create interview failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating interview, got %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created interview: %v", err)
	}
	if created.Request.Blocking || created.Request.Required {
		t.Fatalf("human interviews must be non-blocking, got %+v", created.Request)
	}
	if b.HasBlockingRequest() {
		t.Fatal("human interview should not count as a blocking request")
	}

	createFollowUpBody, _ := json.Marshal(map[string]any{
		"kind":     "interview",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Follow-up interview",
		"question": "Which launch channel should we test next?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-thread-2",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createFollowUpBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create follow-up interview failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200 creating follow-up interview, got %d: %s", resp.StatusCode, raw)
	}
	var createdFollowUp struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createdFollowUp); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created follow-up interview: %v", err)
	}
	resp.Body.Close()

	invalidMessageBody, _ := json.Marshal(map[string]any{
		"from":    "",
		"channel": "general",
		"content": "This send should fail validation.",
		"tagged":  []string{"unknown-agent"},
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(invalidMessageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post invalid message after interview failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid message to fail before canceling interview, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/interview/answer?id="+created.Request.ID, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get interview answer after invalid send failed: %v", err)
	}
	var pendingAnswer struct {
		Answered *interviewAnswer `json:"answered"`
		Status   string           `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pendingAnswer); err != nil {
		t.Fatalf("decode pending interview answer: %v", err)
	}
	resp.Body.Close()
	if pendingAnswer.Answered != nil || pendingAnswer.Status != "pending" {
		t.Fatalf("expected invalid send to leave interview pending, got %+v", pendingAnswer)
	}

	messageBody, _ := json.Marshal(map[string]any{
		"from":     "you",
		"channel":  "general",
		"content":  "Let's keep moving in this thread.",
		"reply_to": "msg-thread-1",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(messageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message after interview failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected message send after interview to succeed, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?scope=all&viewer_slug=human&include_resolved=true", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list requests failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Requests []humanInterview `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode requests: %v", err)
	}
	if len(listing.Requests) != 2 {
		t.Fatalf("expected two interviews, got %+v", listing.Requests)
	}
	byID := map[string]humanInterview{}
	for _, listed := range listing.Requests {
		byID[listed.ID] = listed
	}
	if byID[created.Request.ID].Status != "canceled" {
		t.Fatalf("expected replied-to interview to be canceled after human message, got %+v", byID[created.Request.ID])
	}
	if byID[createdFollowUp.Request.ID].Status != "pending" {
		t.Fatalf("expected queued follow-up interview to remain pending, got %+v", byID[createdFollowUp.Request.ID])
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/interview", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get active interview failed: %v", err)
	}
	var activeInterview struct {
		Pending *humanInterview `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&activeInterview); err != nil {
		resp.Body.Close()
		t.Fatalf("decode active interview: %v", err)
	}
	resp.Body.Close()
	if activeInterview.Pending == nil || activeInterview.Pending.ID != createdFollowUp.Request.ID {
		t.Fatalf("expected active interview to switch to follow-up %q, got %+v", createdFollowUp.Request.ID, activeInterview.Pending)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/interview/answer?id="+created.Request.ID, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get interview answer failed: %v", err)
	}
	defer resp.Body.Close()
	var answer struct {
		Answered *interviewAnswer `json:"answered"`
		Status   string           `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode interview answer: %v", err)
	}
	if answer.Answered != nil || answer.Status != "canceled" {
		t.Fatalf("expected canceled interview answer state, got %+v", answer)
	}
}

func TestBrokerRequestAnswerUnblocksDependentTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createRequestBody, _ := json.Marshal(map[string]any{
		"action":   "create",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approve the launch packet",
		"question": "Should we proceed with the external launch?",
		"kind":     "approval",
		"blocking": true,
		"required": true,
		"reply_to": "msg-approval-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createRequestBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request create response: %v", err)
	}
	reqID := created.Request.ID
	if reqID == "" {
		t.Fatal("expected request id")
	}

	createTaskBody, _ := json.Marshal(map[string]any{
		"action":     "create",
		"channel":    "general",
		"title":      "Ship the launch packet after approval",
		"details":    "Continue once the approval request is answered.",
		"created_by": "ceo",
		"owner":      "builder",
		"depends_on": []string{reqID},
		"task_type":  "launch",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(createTaskBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating task, got %d: %s", resp.StatusCode, raw)
	}
	var taskResult struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&taskResult); err != nil {
		t.Fatalf("decode task create response: %v", err)
	}
	if !taskResult.Task.Blocked {
		t.Fatalf("expected task to start blocked on request dependency, got %+v", taskResult.Task)
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":        reqID,
		"choice_id": "approve",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("answer request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 answering request, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get tasks failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	var updated *teamTask
	for i := range listing.Tasks {
		if listing.Tasks[i].ID == taskResult.Task.ID {
			updated = &listing.Tasks[i]
			break
		}
	}
	if updated == nil {
		t.Fatalf("expected to find task %s after answer", taskResult.Task.ID)
	}
	if updated.Blocked {
		t.Fatalf("expected task to be unblocked after request answer, got %+v", updated)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("expected task to resume in_progress after answer, got %+v", updated)
	}
}

func TestBrokerDecisionRequestsDefaultToBlocking(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}

	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !created.Request.Blocking || !created.Request.Required {
		t.Fatalf("expected approval to default to blocking+required, got %+v", created.Request)
	}
	if got := created.Request.RecommendedID; got != "approve" {
		t.Fatalf("expected approval recommended_id to default to approve, got %q", got)
	}
	if len(created.Request.Options) != 5 {
		t.Fatalf("expected enriched approval options, got %+v", created.Request.Options)
	}
	var approveWithNote *interviewOption
	for i := range created.Request.Options {
		if created.Request.Options[i].ID == "approve_with_note" {
			approveWithNote = &created.Request.Options[i]
			break
		}
	}
	if approveWithNote == nil || !approveWithNote.RequiresText || strings.TrimSpace(approveWithNote.TextHint) == "" {
		t.Fatalf("expected approve_with_note to require text, got %+v", approveWithNote)
	}
}

func TestBrokerRequestAnswerRequiresCustomTextWhenOptionNeedsIt(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()

	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":        created.Request.ID,
		"choice_id": "approve_with_note",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for missing custom text, got %d: %s", resp.StatusCode, raw)
	}
}

func TestRequestAnswerUnblocksReferencedTask(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	now := "2026-01-01T00:00:00Z"
	b.channels = append(b.channels, teamChannel{Slug: "client-loop", Name: "Client Loop"})
	b.requests = append(b.requests, humanInterview{
		ID:        "request-11",
		Kind:      "input",
		Status:    "pending",
		From:      "builder",
		Channel:   "client-loop",
		Question:  "What exact client name should I use for the Google Drive workspace folder?",
		Blocking:  true,
		Required:  true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	b.tasks = append(b.tasks, teamTask{
		ID:        "task-3",
		Channel:   "client-loop",
		Title:     "Create live client workspace in Google Drive",
		Details:   "Blocked on request-11: exact client name for the workspace folder.",
		Owner:     "builder",
		Status:    "blocked",
		Blocked:   true,
		CreatedBy: "operator",
		CreatedAt: now,
		UpdatedAt: now,
	})
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	answerBody, _ := json.Marshal(map[string]any{
		"id":          "request-11",
		"custom_text": "Meridian Growth Studio",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if got := b.tasks[0]; got.Blocked {
		t.Fatalf("expected task to unblock after request answer, got %+v", got)
	} else {
		if got.Status != "in_progress" {
			t.Fatalf("expected task status to move to in_progress, got %+v", got)
		}
		if !strings.Contains(got.Details, "Meridian Growth Studio") {
			t.Fatalf("expected task details to include human answer, got %q", got.Details)
		}
	}
	var found bool
	for _, action := range b.actions {
		if action.Kind == "task_unblocked" && action.RelatedID == "task-3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected task_unblocked action after answering request")
	}
}
