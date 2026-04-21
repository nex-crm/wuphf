package team

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a mutable clock used to drive the state machine deterministically.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestReviewLog(t *testing.T, resolver ReviewerResolver, clock func() time.Time) *ReviewLog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reviews.jsonl")
	rl, err := NewReviewLog(path, resolver, clock)
	if err != nil {
		t.Fatalf("new review log: %v", err)
	}
	return rl
}

func baseSubmit() SubmitPromotionRequest {
	return SubmitPromotionRequest{
		SourceSlug: "pm",
		SourcePath: "agents/pm/notebook/2026-04-20-retro.md",
		TargetPath: "team/playbooks/q2-launch.md",
		Rationale:  "canonical playbook",
	}
}

func TestSubmitPromotion_HappyPath(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC))
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, clock.Now)

	p, err := rl.SubmitPromotion(baseSubmit())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if p.State != PromotionPending {
		t.Fatalf("state=%s, want pending", p.State)
	}
	if p.ReviewerSlug != "ceo" {
		t.Fatalf("reviewer=%s, want ceo", p.ReviewerSlug)
	}
	if p.HumanOnly {
		t.Fatal("human-only should be false for ceo reviewer")
	}
	wantExpires := clock.Now().Add(PromotionIdleExpiry)
	if !p.ExpiresAt.Equal(wantExpires) {
		t.Fatalf("expires_at=%s, want %s", p.ExpiresAt, wantExpires)
	}
	if len(p.StateHistory) != 1 || p.StateHistory[0].NewState != PromotionPending {
		t.Fatalf("state history unexpected: %+v", p.StateHistory)
	}
}

func TestSubmitPromotion_HumanOnlyResolverFlag(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "human-only" }, time.Now)
	p, err := rl.SubmitPromotion(baseSubmit())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !p.HumanOnly {
		t.Fatal("expected human-only=true")
	}
}

func TestSubmitPromotion_ReviewerOverride(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	req := baseSubmit()
	req.ReviewerOverride = "compliance"
	p, err := rl.SubmitPromotion(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if p.ReviewerSlug != "compliance" {
		t.Fatalf("reviewer=%s, want compliance", p.ReviewerSlug)
	}
}

func TestSubmitPromotion_InvalidPaths(t *testing.T) {
	rl := newTestReviewLog(t, nil, time.Now)
	cases := []struct {
		name string
		req  SubmitPromotionRequest
	}{
		{"empty source slug", SubmitPromotionRequest{TargetPath: "team/x.md"}},
		{"source not under agent", SubmitPromotionRequest{SourceSlug: "pm", SourcePath: "agents/other/notebook/x.md", TargetPath: "team/x.md"}},
		{"target not under team", SubmitPromotionRequest{SourceSlug: "pm", SourcePath: "agents/pm/notebook/x.md", TargetPath: "wrong/x.md"}},
		{"target no .md", SubmitPromotionRequest{SourceSlug: "pm", SourcePath: "agents/pm/notebook/x.md", TargetPath: "team/x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := rl.SubmitPromotion(tc.req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestApprove_HappyAndAutoAdvanceFromPending(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	updated, _, err := rl.Approve(p.ID, "ceo", "LGTM", "abc1234")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if updated.State != PromotionApproved {
		t.Fatalf("state=%s, want approved", updated.State)
	}
	if updated.CommitSHA != "abc1234" {
		t.Fatalf("commit_sha=%q, want abc1234", updated.CommitSHA)
	}
	// Both pending→in-review (auto) and in-review→approved are recorded.
	if len(updated.StateHistory) < 3 {
		t.Fatalf("expected ≥3 history entries (submit + auto pickup + approve), got %d", len(updated.StateHistory))
	}
}

func TestApprove_HumanOnlyBlocksAgent(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "human-only" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	if _, _, err := rl.Approve(p.ID, "ceo", "try it", ""); !errors.Is(err, ErrHumanOnlyReviewRequired) {
		t.Fatalf("expected ErrHumanOnlyReviewRequired, got %v", err)
	}
	// Human bypass (empty actor slug) must still succeed.
	updated, _, err := rl.Approve(p.ID, "", "human approve", "deadbeef")
	if err != nil {
		t.Fatalf("human approve: %v", err)
	}
	if updated.State != PromotionApproved {
		t.Fatalf("state=%s", updated.State)
	}
}

func TestApprove_WrongReviewerRejected(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())
	if _, _, err := rl.Approve(p.ID, "pm", "", ""); !errors.Is(err, ErrWrongReviewer) {
		t.Fatalf("expected ErrWrongReviewer, got %v", err)
	}
}

func TestApprove_AlreadyApprovedIsConflict(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())
	if _, _, err := rl.Approve(p.ID, "ceo", "", "sha1"); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if _, _, err := rl.Approve(p.ID, "ceo", "", "sha2"); !errors.Is(err, ErrPromotionAlreadyApproved) {
		t.Fatalf("expected ErrPromotionAlreadyApproved, got %v", err)
	}
}

// TestCanApprove_BlocksWrongReviewerBeforeStateMutation locks the TOCTOU fix
// where reviewApprove used to call Repo.ApplyPromotion before validating the
// actor was the assigned reviewer, letting any authenticated agent force a
// wiki commit with a bogus actor_slug.
func TestCanApprove_BlocksWrongReviewerBeforeStateMutation(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	// Wrong slug: must return ErrWrongReviewer without mutating state.
	if err := rl.CanApprove(p.ID, "pm"); !errors.Is(err, ErrWrongReviewer) {
		t.Fatalf("wrong slug: expected ErrWrongReviewer, got %v", err)
	}
	got, getErr := rl.Get(p.ID)
	if getErr != nil {
		t.Fatalf("Get after CanApprove: %v", getErr)
	}
	if got.State == PromotionApproved {
		t.Fatal("CanApprove must NOT mutate state — promotion is now approved")
	}
	// Correct slug: nil.
	if err := rl.CanApprove(p.ID, "ceo"); err != nil {
		t.Fatalf("correct slug: expected nil, got %v", err)
	}
	// Empty slug (human): nil.
	if err := rl.CanApprove(p.ID, ""); err != nil {
		t.Fatalf("human slug: expected nil, got %v", err)
	}
	// Unknown ID: ErrPromotionNotFound.
	if err := rl.CanApprove("does-not-exist", "ceo"); !errors.Is(err, ErrPromotionNotFound) {
		t.Fatalf("unknown id: expected ErrPromotionNotFound, got %v", err)
	}
}

func TestCanApprove_HumanOnlyBlocksAgent(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "human-only" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())
	if err := rl.CanApprove(p.ID, "ceo"); !errors.Is(err, ErrHumanOnlyReviewRequired) {
		t.Fatalf("agent on human-only: expected ErrHumanOnlyReviewRequired, got %v", err)
	}
	if err := rl.CanApprove(p.ID, ""); err != nil {
		t.Fatalf("human on human-only: expected nil, got %v", err)
	}
}

func TestCanApprove_AlreadyApprovedIsConflict(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())
	if _, _, err := rl.Approve(p.ID, "ceo", "", "sha1"); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if err := rl.CanApprove(p.ID, "ceo"); !errors.Is(err, ErrPromotionAlreadyApproved) {
		t.Fatalf("expected ErrPromotionAlreadyApproved, got %v", err)
	}
}

func TestRequestChangesAndResubmitLoop(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	if _, _, err := rl.AdvanceToInReview(p.ID, "ceo"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	updated, _, err := rl.RequestChanges(p.ID, "ceo", "please clarify the bit about X")
	if err != nil {
		t.Fatalf("request changes: %v", err)
	}
	if updated.State != PromotionChangesRequested {
		t.Fatalf("state=%s, want changes-requested", updated.State)
	}

	// Non-author cannot resubmit.
	if _, _, err := rl.Resubmit(p.ID, "ceo"); !errors.Is(err, ErrWrongAuthor) {
		t.Fatalf("expected ErrWrongAuthor, got %v", err)
	}

	updated, _, err = rl.Resubmit(p.ID, "pm")
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if updated.State != PromotionInReview {
		t.Fatalf("state=%s, want in-review", updated.State)
	}
}

func TestReject_AuthorWithdrawal(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	// Non-author cannot reject.
	if _, _, err := rl.Reject(p.ID, "ceo"); !errors.Is(err, ErrWrongAuthor) {
		t.Fatalf("expected ErrWrongAuthor, got %v", err)
	}
	updated, _, err := rl.Reject(p.ID, "pm")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if updated.State != PromotionRejected {
		t.Fatalf("state=%s, want rejected", updated.State)
	}
}

func TestIllegalTransitions(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	// approved → approved is illegal (caught by ErrPromotionAlreadyApproved).
	rl.Approve(p.ID, "ceo", "", "sha")
	// approved → changes-requested should fail.
	if _, _, err := rl.RequestChanges(p.ID, "ceo", "nope"); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
	// approved → rejected (terminal) should fail.
	if _, _, err := rl.Reject(p.ID, "pm"); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestTickExpiry_IdleExpires(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC))
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, clock.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	// Fast forward 15 days (past the 14d expiry).
	clock.advance(15 * 24 * time.Hour)
	transitions := rl.TickExpiry(clock.Now())
	if len(transitions) != 1 {
		t.Fatalf("transitions=%d, want 1", len(transitions))
	}
	if transitions[0].NewState != PromotionExpired {
		t.Fatalf("new_state=%s, want expired", transitions[0].NewState)
	}
	got, _ := rl.Get(p.ID)
	if got.State != PromotionExpired {
		t.Fatalf("state=%s, want expired", got.State)
	}
}

func TestTickExpiry_AutoArchive(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC))
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, clock.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())
	if _, _, err := rl.Approve(p.ID, "ceo", "", "sha"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	clock.advance(8 * 24 * time.Hour)
	transitions := rl.TickExpiry(clock.Now())
	if len(transitions) != 1 || transitions[0].NewState != PromotionArchived {
		t.Fatalf("want single archived transition, got %+v", transitions)
	}
}

func TestList_ScopeFiltering(t *testing.T) {
	rl := newTestReviewLog(t, func(target string) string {
		if strings.Contains(target, "playbooks") {
			return "compliance"
		}
		return "ceo"
	}, time.Now)
	p1, _ := rl.SubmitPromotion(SubmitPromotionRequest{
		SourceSlug: "pm", SourcePath: "agents/pm/notebook/a.md", TargetPath: "team/playbooks/a.md",
	})
	rl.SubmitPromotion(SubmitPromotionRequest{
		SourceSlug: "pm", SourcePath: "agents/pm/notebook/b.md", TargetPath: "team/people/b.md",
	})

	allList := rl.List("all")
	if len(allList) != 2 {
		t.Fatalf("all=%d", len(allList))
	}
	compList := rl.List("compliance")
	if len(compList) != 1 || compList[0].ID != p1.ID {
		t.Fatalf("compliance scope mismatch: %+v", compList)
	}

	// Reject p1 → it stays in List("all") because rejected is not archived.
	rl.Reject(p1.ID, "pm")
	if got := rl.List("all"); len(got) != 2 {
		t.Fatalf("rejected still visible: want 2, got %d", len(got))
	}
}

func TestAddComment_BumpExpiryAndPersist(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC))
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, clock.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	clock.advance(7 * 24 * time.Hour)
	updated, c, err := rl.AddComment(p.ID, "ceo", "thoughts?")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if c.Body != "thoughts?" {
		t.Fatalf("body=%q", c.Body)
	}
	if len(updated.Comments) != 1 {
		t.Fatalf("comments=%d", len(updated.Comments))
	}
	wantExpiry := clock.Now().Add(PromotionIdleExpiry)
	if !updated.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expiry not bumped: %s vs %s", updated.ExpiresAt, wantExpiry)
	}
}

func TestConcurrentSubmit_JSONLIntegrity(t *testing.T) {
	const workers = 50
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)

	var wg sync.WaitGroup
	var ok atomic.Uint64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := SubmitPromotionRequest{
				SourceSlug: "pm",
				SourcePath: fmt.Sprintf("agents/pm/notebook/draft-%d.md", i),
				TargetPath: fmt.Sprintf("team/playbooks/p-%d.md", i),
				Rationale:  "bulk",
			}
			if _, err := rl.SubmitPromotion(req); err == nil {
				ok.Add(1)
			} else {
				t.Errorf("submit %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if ok.Load() != workers {
		t.Fatalf("expected %d successful submits, got %d", workers, ok.Load())
	}
	// Verify JSONL has header + workers state records.
	lineCount := countJSONLLines(t, rl.Path())
	if lineCount < workers+1 {
		t.Fatalf("expected ≥%d lines in log, got %d", workers+1, lineCount)
	}
	// Reload and verify replay gives the same count.
	rl2, err := NewReviewLog(rl.Path(), nil, time.Now)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(rl2.List("all")); got != workers {
		t.Fatalf("reloaded count=%d, want %d", got, workers)
	}
}

func TestConcurrentApprove_SecondGetsAlreadyApproved(t *testing.T) {
	rl := newTestReviewLog(t, func(string) string { return "ceo" }, time.Now)
	p, _ := rl.SubmitPromotion(baseSubmit())

	var wg sync.WaitGroup
	var successes atomic.Uint64
	var alreadyApproved atomic.Uint64
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := rl.Approve(p.ID, "ceo", "", "sha"); err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrPromotionAlreadyApproved) {
				alreadyApproved.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 successful approve, got %d", successes.Load())
	}
	if alreadyApproved.Load() != 4 {
		t.Fatalf("expected 4 already-approved errors, got %d", alreadyApproved.Load())
	}
}

func TestReplay_MalformedLinesSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviews.jsonl")
	// Write a header, a garbage line, and a valid state record.
	hdr, _ := json.Marshal(headerRecord{Type: logRecordHeader, SchemaVersion: 1, CreatedAt: time.Now()})
	p := Promotion{
		ID: "rvw-123-0001", State: PromotionPending,
		SourceSlug: "pm", SourcePath: "agents/pm/notebook/x.md",
		TargetPath: "team/x.md", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Comments: []Comment{}, StateHistory: []StateTransition{{
			PromotionID: "rvw-123-0001", NewState: PromotionPending, Timestamp: time.Now(),
		}},
	}
	validState, _ := json.Marshal(stateRecord{
		Type: logRecordState, Promotion: p,
		Transition: StateTransition{PromotionID: p.ID, NewState: PromotionPending, Timestamp: time.Now()},
	})
	lines := strings.Join([]string{
		string(hdr),
		"not json at all",
		`{"type":"state","promotion":{}}`, // missing ID
		string(validState),
		`{"type":"unknown","foo":1}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	rl, err := NewReviewLog(path, nil, time.Now)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	all := rl.List("all")
	if len(all) != 1 {
		t.Fatalf("expected 1 valid promotion to survive replay, got %d", len(all))
	}
	if all[0].ID != "rvw-123-0001" {
		t.Fatalf("unexpected ID %q", all[0].ID)
	}
}

func TestGet_UnknownIDReturnsErrNotFound(t *testing.T) {
	rl := newTestReviewLog(t, nil, time.Now)
	if _, err := rl.Get("rvw-doesnt-exist"); !errors.Is(err, ErrPromotionNotFound) {
		t.Fatalf("want ErrPromotionNotFound, got %v", err)
	}
}

func countJSONLLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		n++
	}
	return n
}
