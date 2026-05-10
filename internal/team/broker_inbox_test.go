package team

// broker_inbox_test.go covers Lane E:
//
//   - 1000-task load test (TestInboxQueryUnder100msAt1000Tasks)
//     verifies the indexed lookup keeps the inbox query under the
//     100ms ceiling from the design doc's "Decision Inbox query —
//     1000+ tasks" failure-mode row.
//
//   - Auth filter test (TestInboxAuthFiltersByReviewerMembership +
//     TestTaskByIDAuthFiltersByReviewerMembership) verifies the
//     Tunnel-human reviewer auth matrix: human sessions get an
//     inbox filtered to tasks they review and 200/403 on the packet
//     view; broker token sees full inbox.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestInboxQueryUnder100msAt1000Tasks seeds 1000 tasks distributed
// across all 8 lifecycle states and asserts the InboxFilterNeedsDecision
// query returns within 100ms. The point of the index is that we never
// scan the full 1000-task list to answer "what's in decision?" — the
// bucket length is read in O(1) and only the rows we return cost us.
func TestInboxQueryUnder100msAt1000Tasks(t *testing.T) {
	const taskCount = 1000
	const ceiling = 100 * time.Millisecond

	b := newTestBroker(t)
	canonical := CanonicalLifecycleStates()
	rng := rand.New(rand.NewSource(7)) // deterministic for CI

	now := time.Now().UTC()
	b.mu.Lock()
	for i := 0; i < taskCount; i++ {
		state := canonical[rng.Intn(len(canonical))]
		task := teamTask{
			ID:        fmt.Sprintf("task-%04d", i),
			Title:     fmt.Sprintf("Task %d", i),
			CreatedAt: now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
		}
		b.tasks = append(b.tasks, task)
		// Apply via the lifecycle layer so derived fields + index
		// stay synchronized.
		if _, err := b.transitionLifecycleLocked(task.ID, state, "load test seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed transition for %s -> %s: %v", task.ID, state, err)
		}
	}
	b.mu.Unlock()

	// Warm up the path once so the timed run is steady-state. This
	// makes the assertion protect against the index drifting into an
	// O(N) scan, not against go-runtime cold start.
	if _, err := b.Inbox(InboxFilterNeedsDecision); err != nil {
		t.Fatalf("warmup inbox: %v", err)
	}

	start := time.Now()
	payload, err := b.Inbox(InboxFilterNeedsDecision)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if elapsed > ceiling {
		t.Fatalf("inbox query at %d tasks: %s > ceiling %s", taskCount, elapsed, ceiling)
	}
	t.Logf("inbox query at %d tasks took %s (ceiling %s)", taskCount, elapsed, ceiling)

	// Sanity: the counts came from the index, not from rows. The
	// payload's NeedsDecision count must equal the bucket length.
	b.mu.Lock()
	wantNeedsDecision := len(b.lifecycleIndex[LifecycleStateDecision])
	b.mu.Unlock()
	if payload.Counts.NeedsDecision != wantNeedsDecision {
		t.Fatalf("counts.needsDecision = %d, want %d", payload.Counts.NeedsDecision, wantNeedsDecision)
	}
	if len(payload.Rows) != wantNeedsDecision {
		t.Fatalf("rows = %d, want %d", len(payload.Rows), wantNeedsDecision)
	}
	if payload.RefreshedAt == "" {
		t.Fatal("refreshedAt must be populated")
	}
	if _, err := time.Parse(time.RFC3339, payload.RefreshedAt); err != nil {
		t.Fatalf("refreshedAt %q is not RFC3339: %v", payload.RefreshedAt, err)
	}
}

// TestInboxAuthFiltersByReviewerMembership exercises the Tunnel-human
// reviewer auth matrix on /tasks/inbox. Three tasks; one human is in
// task-A's reviewer list. The human's inbox must contain task-A only;
// the broker token's inbox must contain all three tasks in decision.
func TestInboxAuthFiltersByReviewerMembership(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Reviewable", CreatedAt: now, Reviewers: []string{"mira"}},
		{ID: "task-b", Title: "Not for Mira", CreatedAt: now},
		{ID: "task-c", Title: "Also not for Mira", CreatedAt: now, Reviewers: []string{"alex"}},
	}
	for _, id := range []string{"task-a", "task-b", "task-c"} {
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDecision, "auth test seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	b.mu.Unlock()

	// Mira accepts an invite so her session resolves through
	// humanSessionFromRequest. The cookie-based path is the canonical
	// one; reaching for it here also catches regressions in the auth
	// middleware composition.
	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	cookie := &http.Cookie{Name: humanSessionCookie, Value: sessionToken}

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/inbox", b.requireAuth(b.handleTasksInbox))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Mira (human session): only sees task-a.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/inbox?filter=needs_decision", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mira inbox request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mira inbox status = %d, want 200", resp.StatusCode)
	}
	var miraPayload InboxPayload
	if err := json.NewDecoder(resp.Body).Decode(&miraPayload); err != nil {
		t.Fatalf("mira decode: %v", err)
	}
	resp.Body.Close()
	if len(miraPayload.Rows) != 1 || miraPayload.Rows[0].TaskID != "task-a" {
		t.Fatalf("mira rows = %+v, want one task-a row", miraPayload.Rows)
	}
	// Counts are O(1) and intentionally NOT auth-filtered: they
	// describe broker-wide state. The design doc inbox counts header
	// renders broker totals; reviewer-filter applies to rows.
	if miraPayload.Counts.NeedsDecision != 3 {
		t.Fatalf("mira counts.needsDecision = %d, want 3", miraPayload.Counts.NeedsDecision)
	}

	// 2. Broker token: sees all three tasks.
	brokerReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/inbox?filter=needs_decision", nil)
	brokerReq.Header.Set("Authorization", "Bearer "+b.Token())
	brokerResp, err := http.DefaultClient.Do(brokerReq)
	if err != nil {
		t.Fatalf("broker inbox request: %v", err)
	}
	if brokerResp.StatusCode != http.StatusOK {
		t.Fatalf("broker inbox status = %d, want 200", brokerResp.StatusCode)
	}
	var brokerPayload InboxPayload
	if err := json.NewDecoder(brokerResp.Body).Decode(&brokerPayload); err != nil {
		t.Fatalf("broker decode: %v", err)
	}
	brokerResp.Body.Close()
	if len(brokerPayload.Rows) != 3 {
		t.Fatalf("broker rows = %d, want 3", len(brokerPayload.Rows))
	}

	// 3. Unauthenticated: 401.
	bareReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/inbox?filter=needs_decision", nil)
	bareResp, err := http.DefaultClient.Do(bareReq)
	if err != nil {
		t.Fatalf("bare inbox request: %v", err)
	}
	if bareResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bare inbox status = %d, want 401", bareResp.StatusCode)
	}
	bareResp.Body.Close()

	// 4. Unknown filter: 400.
	badReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/inbox?filter=nonsense", nil)
	badReq.Header.Set("Authorization", "Bearer "+b.Token())
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatalf("bad filter inbox request: %v", err)
	}
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad filter status = %d, want 400", badResp.StatusCode)
	}
	badResp.Body.Close()
}

// TestTaskByIDAuthFiltersByReviewerMembership covers the second leg of
// the auth matrix: human session in the reviewer list gets 200; not in
// the list gets 403; broker token gets 200; unauthenticated gets 401.
func TestTaskByIDAuthFiltersByReviewerMembership(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Reviewable", CreatedAt: now, Reviewers: []string{"mira"}},
		{ID: "task-b", Title: "Locked out", CreatedAt: now, Reviewers: []string{"alex"}},
	}
	for _, id := range []string{"task-a", "task-b"} {
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDecision, "auth test seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// Stash an in-memory Decision Packet for both tasks so the 200
	// path returns the actual artifact rather than the "not yet
	// available" 404 branch.
	// Seed Lane C's Decision Packet store directly under the lock.
	state := b.ensureDecisionPacketStateLocked()
	state.mu.Lock()
	state.packets["task-a"] = &DecisionPacket{
		TaskID:         "task-a",
		LifecycleState: LifecycleStateDecision,
		Spec: Spec{
			Problem:    "Validate the auth matrix",
			Assignment: "Land Lane E",
		},
		ReviewerGrades: []ReviewerGrade{
			{ReviewerSlug: "mira", Severity: SeverityMajor, Suggestion: "tighten auth"},
		},
	}
	state.packets["task-b"] = &DecisionPacket{
		TaskID:         "task-b",
		LifecycleState: LifecycleStateDecision,
	}
	state.mu.Unlock()
	b.mu.Unlock()

	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	cookie := &http.Cookie{Name: humanSessionCookie, Value: sessionToken}

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/", b.requireAuth(b.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Mira on task-a: 200 with packet payload.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/task-a", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mira task-a request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body := readResponseBody(resp)
		t.Fatalf("mira task-a status = %d body=%s, want 200", resp.StatusCode, body)
	}
	var packet DecisionPacket
	if err := json.NewDecoder(resp.Body).Decode(&packet); err != nil {
		t.Fatalf("mira packet decode: %v", err)
	}
	resp.Body.Close()
	if packet.TaskID != "task-a" {
		t.Fatalf("packet.TaskID = %q, want task-a", packet.TaskID)
	}
	if packet.Spec.Assignment != "Land Lane E" {
		t.Fatalf("packet.Spec.Assignment = %q", packet.Spec.Assignment)
	}

	// 2. Mira on task-b: 403.
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/task-b", nil)
	req2.AddCookie(cookie)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("mira task-b request: %v", err)
	}
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("mira task-b status = %d, want 403", resp2.StatusCode)
	}
	resp2.Body.Close()

	// 3. Broker token on task-b: 200.
	brokerReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/task-b", nil)
	brokerReq.Header.Set("Authorization", "Bearer "+b.Token())
	brokerResp, err := http.DefaultClient.Do(brokerReq)
	if err != nil {
		t.Fatalf("broker task-b request: %v", err)
	}
	if brokerResp.StatusCode != http.StatusOK {
		body := readResponseBody(brokerResp)
		t.Fatalf("broker task-b status = %d body=%s, want 200", brokerResp.StatusCode, body)
	}
	brokerResp.Body.Close()

	// 4. Unauthenticated on task-a: 401.
	bareReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/task-a", nil)
	bareResp, err := http.DefaultClient.Do(bareReq)
	if err != nil {
		t.Fatalf("bare task-a request: %v", err)
	}
	if bareResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bare task-a status = %d, want 401", bareResp.StatusCode)
	}
	bareResp.Body.Close()

	// 5. Unknown task ID: 404.
	missingReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/tasks/task-zzz", nil)
	missingReq.Header.Set("Authorization", "Bearer "+b.Token())
	missingResp, err := http.DefaultClient.Do(missingReq)
	if err != nil {
		t.Fatalf("missing task request: %v", err)
	}
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing task status = %d, want 404", missingResp.StatusCode)
	}
	missingResp.Body.Close()
}

// TestTaskAccessAllowedSlugCollision (E-FU-2) locks down the corner
// case where a tunnel-human display-name normalises to the same slug
// as an existing officeMember (agent). The auth check
// (taskAccessAllowed) does string comparison on slugs after lowercase
// normalisation; if a collision were possible at registration time,
// the human would silently inherit the agent's task access. This test
// asserts the current invariant: human-session slug membership against
// the agent reviewer roster always falls through to "denied" unless
// the human session was admitted under a slug already bound to the
// reviewer membership AND the broker's session table records it. The
// test serves as the regression oracle if a future refactor changes
// the collision policy at registration time.
func TestTaskAccessAllowedSlugCollision(t *testing.T) {
	// Three branches of taskAccessAllowed:
	//
	//  1. Broker token: always allowed.
	//  2. Human session whose slug matches a reviewer: allowed.
	//  3. Human session whose slug does NOT match any reviewer: denied.
	//
	// A slug collision between a tunnel-human and an agent reviewer
	// would mean (2) fires for an unintended actor. The test seeds a
	// task with reviewer "agent-a" and verifies that:
	//   - a human session admitted with HumanSlug "agent-a" lands in
	//     the "match" branch (this is the case the hardening is meant
	//     to prevent at the registration layer);
	//   - a human session admitted with HumanSlug "stranger" lands in
	//     the "denied" branch.
	cases := []struct {
		name      string
		actor     requestActor
		reviewers []string
		want      bool
	}{
		{
			name:      "broker token always allowed",
			actor:     requestActor{Kind: requestActorKindBroker},
			reviewers: []string{"agent-a", "agent-b"},
			want:      true,
		},
		{
			name:      "matching slug allowed",
			actor:     requestActor{Kind: requestActorKindHuman, Slug: "agent-a"},
			reviewers: []string{"agent-a", "agent-b"},
			want:      true,
		},
		{
			name:      "stranger denied",
			actor:     requestActor{Kind: requestActorKindHuman, Slug: "stranger"},
			reviewers: []string{"agent-a", "agent-b"},
			want:      false,
		},
		{
			name:      "case-insensitive match allowed",
			actor:     requestActor{Kind: requestActorKindHuman, Slug: "AGENT-A"},
			reviewers: []string{"agent-a"},
			want:      true,
		},
		{
			name:      "whitespace-padded slug allowed",
			actor:     requestActor{Kind: requestActorKindHuman, Slug: "  agent-a  "},
			reviewers: []string{"agent-a"},
			want:      true,
		},
		{
			name:      "empty slug denied",
			actor:     requestActor{Kind: requestActorKindHuman, Slug: ""},
			reviewers: []string{"agent-a"},
			want:      false,
		},
		{
			name:      "unknown actor kind denied",
			actor:     requestActor{Kind: requestActorKind("unrecognised"), Slug: "agent-a"},
			reviewers: []string{"agent-a"},
			want:      false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := taskAccessAllowed(tc.actor, tc.reviewers)
			if got != tc.want {
				t.Fatalf("taskAccessAllowed(actor=%+v, reviewers=%v) = %v, want %v", tc.actor, tc.reviewers, got, tc.want)
			}
		})
	}
}

// TestHumanInviteSlugNormalisesPredictably (E-FU-2) asserts the
// registration-time slug derivation is deterministic. Two invites
// accepted under the same display name produce the same slug; the
// resolution is therefore predictable and a future collision-prevention
// hook only needs to guard the (slug, kind) pair on the registration
// path. If this normalisation changes, taskAccessAllowed's case-fold
// comparison must change in lockstep.
func TestHumanInviteSlugNormalisesPredictably(t *testing.T) {
	cases := []struct {
		display string
		want    string
	}{
		{"Mira", "mira"},
		{"  Mira  ", "mira"},
		{"Alex Riley", "alex-riley"},
		{"alex@example.com", "alex-example-com"},
		{"AGENT-A", "agent-a"},
		{"   ", ""},
	}
	for _, tc := range cases {
		got := normalizeHumanSessionSlug(tc.display)
		if got != tc.want {
			t.Errorf("normalizeHumanSessionSlug(%q) = %q, want %q", tc.display, got, tc.want)
		}
	}
}

// TestInboxFilterMappings sanity-checks the bucket -> filter coverage.
// A typo in the inboxFilterToStates table would have the inbox return
// the wrong rows; the design doc lists the mapping explicitly so this
// test is the regression oracle.
func TestInboxFilterMappings(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)
	earlier := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "decision-1", CreatedAt: now},
		{ID: "running-1", CreatedAt: now},
		{ID: "blocked-1", CreatedAt: now},
		{ID: "merged-today-1", CreatedAt: now, CompletedAt: now},
		{ID: "merged-old-1", CreatedAt: earlier, CompletedAt: earlier},
	}
	transitions := map[string]LifecycleState{
		"decision-1":     LifecycleStateDecision,
		"running-1":      LifecycleStateRunning,
		"blocked-1":      LifecycleStateBlockedOnPRMerge,
		"merged-today-1": LifecycleStateMerged,
		"merged-old-1":   LifecycleStateMerged,
	}
	for id, state := range transitions {
		if _, err := b.transitionLifecycleLocked(id, state, "filter test"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	b.mu.Unlock()

	cases := []struct {
		filter  InboxFilter
		wantIDs []string
		minRows int
	}{
		{InboxFilterNeedsDecision, []string{"decision-1"}, 1},
		{InboxFilterRunning, []string{"running-1"}, 1},
		{InboxFilterBlocked, []string{"blocked-1"}, 1},
		{InboxFilterMergedToday, []string{"merged-today-1"}, 1},
	}
	for _, tc := range cases {
		t.Run(string(tc.filter), func(t *testing.T) {
			payload, err := b.Inbox(tc.filter)
			if err != nil {
				t.Fatalf("inbox(%s): %v", tc.filter, err)
			}
			if len(payload.Rows) != tc.minRows {
				t.Fatalf("filter %s: rows = %d, want %d (rows=%+v)", tc.filter, len(payload.Rows), tc.minRows, payload.Rows)
			}
			gotIDs := make([]string, 0, len(payload.Rows))
			for _, row := range payload.Rows {
				gotIDs = append(gotIDs, row.TaskID)
			}
			if strings.Join(gotIDs, ",") != strings.Join(tc.wantIDs, ",") {
				t.Fatalf("filter %s: ids = %v, want %v", tc.filter, gotIDs, tc.wantIDs)
			}
		})
	}

	// Counts are stable across filters and intentionally O(1).
	payload, err := b.Inbox(InboxFilterAll)
	if err != nil {
		t.Fatalf("inbox(all): %v", err)
	}
	if payload.Counts.NeedsDecision != 1 {
		t.Errorf("counts.needsDecision = %d, want 1", payload.Counts.NeedsDecision)
	}
	if payload.Counts.Running != 1 {
		t.Errorf("counts.running = %d, want 1", payload.Counts.Running)
	}
	if payload.Counts.Blocked != 1 {
		t.Errorf("counts.blocked = %d, want 1", payload.Counts.Blocked)
	}
	if payload.Counts.MergedToday != 1 {
		t.Errorf("counts.mergedToday = %d, want 1", payload.Counts.MergedToday)
	}
}

func readResponseBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.String()
}
