package team

// broker_decision_packet_test.go covers Success Criteria gate #6
// (Decision Packet persistence behavior, four paths) plus a smoke test
// for the wiki promotion on merged.
//
// The four required paths:
//
//   1. Concurrent writes — three goroutines append reviewer grades to
//      the same task; b.mu serialises so all three land.
//   2. Atomic-rename happy path — write produces the canonical file on
//      disk and the body decodes back to an equal in-memory packet.
//   3. Disk-full retry — a mock store returns an error N times before
//      succeeding; the broker retries and never loses memory state. The
//      always-fail variant asserts the channel banner posts.
//   4. Corrupt JSON read — half-written file on disk is detected on
//      read; in-memory packet drives regeneration; cold-restart with
//      no memory falls through to LifecycleStateUnknown.
//
// Plus TestDecisionPacketWikiPromotionOnMerged — a populated packet
// transitioning into merged calls RecordTaskDecision and the wiki
// promotion path emits an article matching the expected markdown shape.
//
// Filesystem mock pattern: the package-private decisionPacketStore
// interface is overridden via b.SetDecisionPacketStore(). This is the
// standard "interface seam in production code, fake in tests" pattern
// used elsewhere in the team package.

import (
	"context"
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

// fakeDecisionPacketStore is the test double for decisionPacketStore.
// It records every Write call (kept for goroutine-race assertions),
// optionally rejects N writes before succeeding, and serves Read from
// a configurable corpus.
type fakeDecisionPacketStore struct {
	mu             sync.Mutex
	writes         []DecisionPacket
	failNextWrites int   // counter; if >0, fail the next Write and decrement
	failAlways     bool  // never returns success on Write
	failErr        error // error returned on a failed Write
	corpus         map[string]DecisionPacket
	corpusErr      map[string]error // override the Read result for a task
}

func newFakeDecisionPacketStore() *fakeDecisionPacketStore {
	return &fakeDecisionPacketStore{
		corpus:    map[string]DecisionPacket{},
		corpusErr: map[string]error{},
	}
}

func (s *fakeDecisionPacketStore) Write(taskID string, packet DecisionPacket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAlways {
		err := s.failErr
		if err == nil {
			err = errors.New("fake: disk full")
		}
		return err
	}
	if s.failNextWrites > 0 {
		s.failNextWrites--
		err := s.failErr
		if err == nil {
			err = errors.New("fake: disk full")
		}
		return err
	}
	s.writes = append(s.writes, packet)
	s.corpus[taskID] = packet
	return nil
}

func (s *fakeDecisionPacketStore) Read(taskID string) (DecisionPacket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.corpusErr[taskID]; ok {
		return DecisionPacket{}, err
	}
	if packet, ok := s.corpus[taskID]; ok {
		return packet, nil
	}
	return DecisionPacket{}, ErrDecisionPacketNotFound
}

func (s *fakeDecisionPacketStore) writesCopy() []DecisionPacket {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DecisionPacket, len(s.writes))
	copy(out, s.writes)
	return out
}

// resetWrites clears the recorded write log so a test setup phase that
// performs incidental persistence (e.g. AssignReviewers) does not skew a
// later write-count assertion.
func (s *fakeDecisionPacketStore) resetWrites() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = nil
}

// seedTaskInState installs a task into b.tasks at the given lifecycle
// state. Mirrors the small fixture used by Lane A's tests so we don't
// drag in the full intake driver.
func seedTaskInState(t *testing.T, b *Broker, taskID string, state LifecycleState) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	row, ok := derivedFieldsFor(state)
	if !ok {
		t.Fatalf("seed task: %q is not canonical", state)
	}
	b.tasks = append(b.tasks, teamTask{
		ID:             taskID,
		Title:          "fixture " + taskID,
		LifecycleState: state,
	})
	task := &b.tasks[len(b.tasks)-1]
	task.status = row.Status
	task.reviewState = row.ReviewState
	task.pipelineStage = row.PipelineStage
	task.blocked = row.Blocked
	b.indexLifecycleLocked(task.ID, "", state)
}

// TestDecisionPacketConcurrentReviewerGrades asserts build-time gate
// #6, path 1: three goroutines simultaneously append reviewer grades
// and all three land in the packet via b.mu serialisation.
func TestDecisionPacketConcurrentReviewerGrades(t *testing.T) {
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	b.SetDecisionPacketStore(store)
	taskID := "task-concurrent"
	seedTaskInState(t, b, taskID, LifecycleStateReview)

	reviewers := []struct {
		slug     string
		severity Severity
	}{
		{"reviewer-a", SeverityCritical},
		{"reviewer-b", SeverityMajor},
		{"reviewer-c", SeverityMinor},
	}

	var wg sync.WaitGroup
	for _, r := range reviewers {
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			grade := ReviewerGrade{
				ReviewerSlug: r.slug,
				Severity:     r.severity,
				Suggestion:   "fix " + r.slug,
				Reasoning:    "because",
			}
			if err := b.AppendReviewerGrade(taskID, grade); err != nil {
				t.Errorf("AppendReviewerGrade(%s): %v", r.slug, err)
			}
		}()
	}
	wg.Wait()

	packet, err := b.GetDecisionPacket(taskID)
	if err != nil {
		t.Fatalf("GetDecisionPacket: %v", err)
	}
	if got, want := len(packet.ReviewerGrades), len(reviewers); got != want {
		t.Fatalf("reviewer grades count: got %d, want %d (lost write under b.mu)", got, want)
	}
	seen := map[string]Severity{}
	for _, g := range packet.ReviewerGrades {
		seen[g.ReviewerSlug] = g.Severity
	}
	for _, r := range reviewers {
		if got, ok := seen[r.slug]; !ok {
			t.Errorf("reviewer %q missing from packet", r.slug)
		} else if got != r.severity {
			t.Errorf("reviewer %q severity: got %s, want %s", r.slug, got, r.severity)
		}
	}
}

// TestDecisionPacketAtomicRenameHappyPath asserts build-time gate #6,
// path 2: writing a packet against the production OS store produces
// the canonical decision_packet.json file with bytes that decode back
// to an equal in-memory packet, and no leftover .tmp file.
func TestDecisionPacketAtomicRenameHappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	b := newTestBroker(t)
	taskID := "task-rename"
	seedTaskInState(t, b, taskID, LifecycleStateRunning)

	if err := b.SetSpec(taskID, Spec{
		Problem:    "rename test",
		Assignment: "verify atomic rename",
		AcceptanceCriteria: []ACItem{
			{Statement: "file lands on disk"},
			{Statement: "tmp file is cleaned up"},
		},
	}); err != nil {
		t.Fatalf("SetSpec: %v", err)
	}

	expected := filepath.Join(dir, ".wuphf", "tasks", taskID, "decision_packet.json")
	body, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read on-disk packet: %v", err)
	}
	if !strings.Contains(string(body), `"taskId": "`+taskID+`"`) {
		t.Fatalf("on-disk packet missing taskId: %s", string(body))
	}
	if !strings.Contains(string(body), `"problem": "rename test"`) {
		t.Fatalf("on-disk packet missing spec.problem: %s", string(body))
	}

	tmp := expected + ".tmp"
	if _, err := os.Stat(tmp); err == nil {
		t.Fatalf("expected tmp file %q to be cleaned up after rename", tmp)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat tmp: %v", err)
	}

	disk, err := b.GetDecisionPacket(taskID)
	if err != nil {
		t.Fatalf("GetDecisionPacket after read: %v", err)
	}
	if disk.TaskID != taskID || disk.Spec.Problem != "rename test" {
		t.Fatalf("on-disk decode mismatch: %+v", disk)
	}
}

// TestDecisionPacketRetryThenBanner asserts build-time gate #6, path 3:
// disk-full simulated; the broker retries 3 times, then posts a
// channel banner once the retry budget is exhausted, and the in-memory
// packet survives.
func TestDecisionPacketRetryThenBanner(t *testing.T) {
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	store.failAlways = true
	store.failErr = errors.New("write tmp: no space left on device (ENOSPC)")
	b.SetDecisionPacketStore(store)
	taskID := "task-disk-full"
	seedTaskInState(t, b, taskID, LifecycleStateReview)

	if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
		ReviewerSlug: "reviewer-x",
		Severity:     SeverityMajor,
		Reasoning:    "diff is large",
	}); err != nil {
		t.Fatalf("AppendReviewerGrade: %v", err)
	}

	// Memory copy MUST survive even though every disk write failed.
	packet, err := b.GetDecisionPacket(taskID)
	if err != nil {
		t.Fatalf("GetDecisionPacket after disk-full: %v", err)
	}
	if len(packet.ReviewerGrades) != 1 {
		t.Fatalf("memory packet lost grades: %+v", packet)
	}

	bannerFound := false
	b.mu.Lock()
	for _, msg := range b.messages {
		if msg.Kind == "persistence_error" && strings.Contains(msg.Content, taskID) {
			bannerFound = true
			break
		}
	}
	b.mu.Unlock()
	if !bannerFound {
		t.Fatalf("expected channel banner with kind=persistence_error mentioning %q after retry exhaustion", taskID)
	}

	// Once the underlying store recovers, the next mutator should
	// successfully persist the accumulated state.
	store.mu.Lock()
	store.failAlways = false
	store.failNextWrites = 0
	store.mu.Unlock()
	if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
		ReviewerSlug: "reviewer-y",
		Severity:     SeverityMinor,
		Reasoning:    "follow-up",
	}); err != nil {
		t.Fatalf("AppendReviewerGrade after recovery: %v", err)
	}
	writes := store.writesCopy()
	if len(writes) == 0 {
		t.Fatalf("expected at least one successful write after store recovery; got 0")
	}
	last := writes[len(writes)-1]
	if len(last.ReviewerGrades) != 2 {
		t.Fatalf("recovered write should carry both grades; got %d", len(last.ReviewerGrades))
	}
}

// TestDecisionPacketRetrySucceedsBeforeBudget asserts the retry budget
// behaviour: a transient failure that recovers within the 3-attempt
// budget posts no banner and lands the write.
func TestDecisionPacketRetrySucceedsBeforeBudget(t *testing.T) {
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	store.failNextWrites = 2 // succeed on attempt 3
	store.failErr = errors.New("write tmp: temporarily unavailable (EAGAIN)")
	b.SetDecisionPacketStore(store)
	taskID := "task-transient"
	seedTaskInState(t, b, taskID, LifecycleStateReview)
	// Pre-assign two reviewers so convergence has something to wait on
	// after the first grade lands; otherwise the Lane D mirror would
	// immediately transition to decision and add a second persisted
	// write that this retry-budget assertion is not measuring.
	if err := b.AssignReviewers(taskID, []string{"reviewer-flap", "reviewer-still-pending"}); err != nil {
		t.Fatalf("AssignReviewers: %v", err)
	}
	store.resetWrites()

	if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
		ReviewerSlug: "reviewer-flap",
		Severity:     SeverityNitpick,
	}); err != nil {
		t.Fatalf("AppendReviewerGrade: %v", err)
	}

	if writes := store.writesCopy(); len(writes) != 1 {
		t.Fatalf("expected exactly one successful write after 2 transient failures; got %d", len(writes))
	}

	b.mu.Lock()
	for _, msg := range b.messages {
		if msg.Kind == "persistence_error" {
			t.Errorf("did not expect a persistence_error banner when retry succeeds within budget; got %q", msg.Content)
		}
	}
	b.mu.Unlock()
}

// TestDecisionPacketCorruptReadRegeneratesFromMemory asserts build-time
// gate #6, path 4 (warm regeneration branch): a corrupt on-disk file
// surfaces as not-found via GetDecisionPacket, then RegenerateOrMarkUnknown
// rewrites the file from in-memory state.
func TestDecisionPacketCorruptReadRegeneratesFromMemory(t *testing.T) {
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	b.SetDecisionPacketStore(store)
	taskID := "task-corrupt"
	seedTaskInState(t, b, taskID, LifecycleStateRunning)

	if err := b.SetSpec(taskID, Spec{
		Problem:            "corrupt-read recovery",
		Assignment:         "regenerate from memory",
		AcceptanceCriteria: []ACItem{{Statement: "regen succeeds"}},
	}); err != nil {
		t.Fatalf("SetSpec: %v", err)
	}

	// Now simulate corruption on the next read.
	store.mu.Lock()
	store.corpusErr[taskID] = fmt.Errorf("%w: simulated half-write", ErrDecisionPacketCorrupt)
	store.mu.Unlock()

	// Drop the in-memory cached packet to force the corrupt-read code
	// path inside GetDecisionPacket.
	b.mu.Lock()
	state := b.ensureDecisionPacketStateLocked()
	state.mu.Lock()
	originalPacket := *state.packets[taskID]
	delete(state.packets, taskID)
	state.mu.Unlock()
	b.mu.Unlock()

	_, err := b.GetDecisionPacket(taskID)
	if !errors.Is(err, ErrDecisionPacketNotFound) {
		t.Fatalf("corrupt read should surface as ErrDecisionPacketNotFound; got %v", err)
	}

	// Restore the in-memory packet and run regeneration. This mirrors
	// the warm-restart branch where the broker memory still has the
	// last-known packet.
	b.mu.Lock()
	state.mu.Lock()
	cp := originalPacket
	state.packets[taskID] = &cp
	state.mu.Unlock()
	b.mu.Unlock()

	store.mu.Lock()
	delete(store.corpusErr, taskID)
	store.mu.Unlock()

	if err := b.RegenerateOrMarkUnknown(taskID); err != nil {
		t.Fatalf("RegenerateOrMarkUnknown: %v", err)
	}

	regenerated, err := b.GetDecisionPacket(taskID)
	if err != nil {
		t.Fatalf("GetDecisionPacket after regen: %v", err)
	}
	if regenerated.Spec.Problem != "corrupt-read recovery" {
		t.Fatalf("regen lost spec.problem: %+v", regenerated)
	}
}

// TestDecisionPacketCorruptReadColdRestartUnknown asserts build-time
// gate #6, path 4 (cold-restart branch): when both the on-disk file is
// corrupt and the in-memory packet is gone (broker process restarted
// without a successful prior persist), the task transitions into
// LifecycleStateUnknown so the operator surfaces the missing state.
func TestDecisionPacketCorruptReadColdRestartUnknown(t *testing.T) {
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	b.SetDecisionPacketStore(store)
	taskID := "task-cold"
	seedTaskInState(t, b, taskID, LifecycleStateReview)

	store.mu.Lock()
	store.corpusErr[taskID] = fmt.Errorf("%w: byte 17 is not valid JSON", ErrDecisionPacketCorrupt)
	store.mu.Unlock()

	if err := b.RegenerateOrMarkUnknown(taskID); err != nil {
		t.Fatalf("RegenerateOrMarkUnknown: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	var found *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			found = &b.tasks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("task %q missing after cold-restart regen", taskID)
	}
	if found.LifecycleState != LifecycleStateUnknown {
		t.Fatalf("expected cold-restart corrupt to transition to LifecycleStateUnknown; got %q", found.LifecycleState)
	}
}

// TestDecisionPacketWikiPromotionOnMerged is the smoke test for the
// merge → wiki article path. It uses an in-memory wikiTestPublisher
// so we don't need a real git repo on disk.
func TestDecisionPacketWikiPromotionOnMerged(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	b := newTestBroker(t)
	taskID := "task-merge"
	seedTaskInState(t, b, taskID, LifecycleStateDecision)

	spec := Spec{
		Problem:       "Reviewer convergence misses skipped reviewers",
		Assignment:    "patch convergence rule + add coverage",
		TargetOutcome: "Decision Packet renders skipped reviewers as warning banner",
		AcceptanceCriteria: []ACItem{
			{Statement: "skipped reviewer surfaces in Decision Packet view", Done: true},
			{Statement: "convergence test green"},
		},
	}
	if err := b.SetSpec(taskID, spec); err != nil {
		t.Fatalf("SetSpec: %v", err)
	}
	if err := b.AppendSessionReport(taskID, SessionReport{
		Highlights: "Patched convergence rule and added 2 unit tests.",
		TopWins:    []Win{{Delta: "+78 LOC", Description: "test coverage"}},
		DeadEnds:   []DeadEnd{{Tried: "central reviewer-state machine", Reason: "too coupled to lane D"}},
		Metadata:   map[string]string{"agent": "owner-1"},
	}); err != nil {
		t.Fatalf("AppendSessionReport: %v", err)
	}
	if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
		ReviewerSlug: "reviewer-a",
		Severity:     SeverityMinor,
		Suggestion:   "rename helper for clarity",
		Reasoning:    "current name overloads existing 'reviewer-state'",
	}); err != nil {
		t.Fatalf("AppendReviewerGrade: %v", err)
	}

	// Set up a tiny wiki worker pointed at a tempdir-rooted repo so
	// the promotion can land. Stand up the real Repo + WikiWorker and
	// wire it onto the broker.
	wikiRoot := filepath.Join(dir, "wiki-repo")
	wikiBackup := filepath.Join(dir, "wiki-backup")
	repo := NewRepoAt(wikiRoot, wikiBackup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	// Order matters: stop the worker BEFORE cancelling its start
	// context so an in-flight git commit isn't killed mid-stream. Stop
	// closes the request channel; the drain loop exits cleanly after
	// processing whatever was already in flight. Then cancel.
	t.Cleanup(func() {
		worker.Stop()
		<-worker.Done()
		cancel()
	})
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	if err := b.RecordTaskDecision(taskID, string(RecordDecisionMerge), "test-human"); err != nil {
		t.Fatalf("RecordTaskDecision: %v", err)
	}

	// Wiki write is enqueued in a background goroutine spawned by
	// writeWikiPromotionLocked. Poll for the file to appear — once it
	// exists the goroutine's Enqueue call has completed and it's safe
	// to stop the worker without racing on the channel.
	relPath := wikiPromotionPath(taskID)
	expectedPath := filepath.Join(wikiRoot, relPath)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(expectedPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Cancel the context to signal the worker to drain, then wait for
	// completion. This avoids the Stop/Enqueue channel race because
	// cancel signals the worker's drain loop to exit after processing
	// in-flight requests, while Stop closes the request channel which
	// can race with a concurrent Enqueue.
	cancel()
	<-worker.Done()
	body, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("wiki promotion article %q never landed: %v", expectedPath, err)
	}
	bodyStr := string(body)
	for _, want := range []string{
		"# Reviewer convergence misses skipped reviewers",
		"Task ID: `" + taskID + "`",
		"## Spec",
		"## Session report",
		"## Reviewer grades",
		"reviewer-a",
		"## Decision",
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("wiki promotion missing expected fragment %q\nbody:\n%s", want, bodyStr)
		}
	}
	// Lifecycle state must have moved to merged.
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID != taskID {
			continue
		}
		if b.tasks[i].LifecycleState != LifecycleStateMerged {
			t.Errorf("task %q lifecycle: got %s, want merged", taskID, b.tasks[i].LifecycleState)
		}
		break
	}
}

// TestDecisionPacketEventEmissionShape verifies that the five
// lifecycle manifest events register on the existing
// HeadlessEventTypeManifest channel rather than a parallel pipeline.
// Events carry the typed subkind discriminator in Detail.
func TestDecisionPacketEventEmissionShape(t *testing.T) {
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	b.SetDecisionPacketStore(store)
	taskID := "task-events"
	seedTaskInState(t, b, taskID, LifecycleStateIntake)

	if err := b.SetSpec(taskID, Spec{
		Problem:            "event shape",
		Assignment:         "verify subkind discriminator",
		AcceptanceCriteria: []ACItem{{Statement: "ok"}},
	}); err != nil {
		t.Fatalf("SetSpec: %v", err)
	}
	if err := b.TransitionLifecycle(taskID, LifecycleStateRunning, "owner started"); err != nil {
		t.Fatalf("TransitionLifecycle: %v", err)
	}
	if err := b.AppendSessionReport(taskID, SessionReport{Highlights: "done"}); err != nil {
		t.Fatalf("AppendSessionReport: %v", err)
	}
	if err := b.TransitionLifecycle(taskID, LifecycleStateReview, "ready for review"); err != nil {
		t.Fatalf("TransitionLifecycle review: %v", err)
	}
	if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
		ReviewerSlug: "r1",
		Severity:     SeverityMinor,
	}); err != nil {
		t.Fatalf("AppendReviewerGrade: %v", err)
	}
	if err := b.OnReviewerConvergence(taskID, "all graded"); err != nil {
		t.Fatalf("OnReviewerConvergence: %v", err)
	}

	b.mu.Lock()
	stream := b.lifecycleManifestStreamLocked(taskID)
	b.mu.Unlock()
	if stream == nil {
		t.Fatalf("expected lifecycle manifest stream to be allocated by emission")
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	required := map[string]bool{
		string(LifecycleManifestSpecCreated):      false,
		string(LifecycleManifestArtifactReady):    false,
		string(LifecycleManifestReviewSubmitted):  false,
		string(LifecycleManifestDecisionRequired): false,
	}
	for _, line := range stream.lines {
		for k := range required {
			if strings.Contains(line.Text, k) {
				required[k] = true
			}
		}
	}
	for k, ok := range required {
		if !ok {
			t.Errorf("expected lifecycle manifest event with subkind %q", k)
		}
	}
}

// TestDecisionPacketSerialisationGuardsConcurrent runs many concurrent
// mutators across multiple tasks to exercise the b.mu serialisation
// under -race. Lighter than path 1's strict assertion — this is the
// stress test.
func TestDecisionPacketSerialisationGuardsConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short")
	}
	b := newTestBroker(t)
	store := newFakeDecisionPacketStore()
	b.SetDecisionPacketStore(store)

	const tasks = 4
	const writers = 6
	var counter atomic.Uint64

	for i := 0; i < tasks; i++ {
		taskID := fmt.Sprintf("task-stress-%d", i)
		seedTaskInState(t, b, taskID, LifecycleStateReview)
	}

	var wg sync.WaitGroup
	for i := 0; i < tasks; i++ {
		taskID := fmt.Sprintf("task-stress-%d", i)
		for w := 0; w < writers; w++ {
			w := w
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := b.AppendReviewerGrade(taskID, ReviewerGrade{
					ReviewerSlug: fmt.Sprintf("reviewer-%d", w),
					Severity:     SeverityMinor,
					Reasoning:    "stress",
				})
				if err == nil {
					counter.Add(1)
				}
			}()
		}
	}
	wg.Wait()
	if got := counter.Load(); got != uint64(tasks*writers) {
		t.Fatalf("expected %d successful appends; got %d", tasks*writers, got)
	}
	for i := 0; i < tasks; i++ {
		taskID := fmt.Sprintf("task-stress-%d", i)
		packet, err := b.GetDecisionPacket(taskID)
		if err != nil {
			t.Fatalf("GetDecisionPacket(%s): %v", taskID, err)
		}
		if got, want := len(packet.ReviewerGrades), writers; got != want {
			t.Errorf("task %s: grades count: got %d, want %d", taskID, got, want)
		}
	}
}
