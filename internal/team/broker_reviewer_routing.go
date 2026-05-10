package team

// broker_reviewer_routing.go is Lane D's reviewer-routing layer. It owns
// four responsibilities:
//
//  1. Snapshotting a task's "what changed" signals (file diff, wiki
//     paths touched, manifest tool calls observed, spec tags) into
//     ReviewerRoutingSignals so routing decisions are deterministic and
//     unit-testable without spinning up a worktree.
//
//  2. Computing the intersection between a task's signals and every
//     officeMember's Watching set, returning the auto-assigned agent
//     slug list (ResolveReviewers / AssignReviewers).
//
//  3. Driving the review → decision convergence rule: a task transitions
//     when every assigned reviewer has emitted a graded review.submitted,
//     OR the per-task timeout elapses, OR a reviewer's headless session
//     terminates without a grade. evaluateConvergenceLocked is called on
//     every grade append and on a 30-second background sweep.
//
//  4. Filling missing reviewer slots with a SeveritySkipped grade on
//     timeout/exit so the Decision Packet always carries N grades for N
//     assigned reviewers, never a half-open hole that blocks merge.
//
// Reviewer Concern #1 resolution (process-watch hook for crash detection):
// The design doc lists scheduleTaskLifecycleLocked and the launcher_loops
// goroutines as candidates. Neither fires reliably as a typed "agent X
// exited without grading task Y" callback in the current broker — they
// are pane-status / scheduler-driven loops that operate on tasks, not on
// per-agent session lifetimes.
//
// What DOES fire reliably is the headless manifest event (emitHeadlessManifest
// in headless_event.go). It is emitted unconditionally at the end of every
// headless turn — both on cmd.Wait() success (Status=idle) and on cmd.Wait()
// error (Status=error). For Lane D's purposes, "reviewer process exited
// without submitting" is functionally identical to "timeout elapsed without
// submitting" because the design treats them with the same fill behaviour
// and the same 10-minute deadline.
//
// Implementation choice: the convergence sweeper observes manifest-terminal
// events (ToolNames extraction code already walks the same lines) and
// records the most recent terminal status per (reviewerSlug, taskID). When
// the sweeper sees a reviewer whose terminal status is "idle" or "error"
// AND no grade has landed AND the per-reviewer minimum-wait window has
// elapsed (default: full timeout), it fills the slot with the
// "reviewer process exited" reasoning instead of "reviewer timed out". This
// preserves the design's user-visible distinction without coupling Lane D
// to per-agent-session lifetimes the broker does not currently expose.
//
// All locked helpers require b.mu held by the caller; the public wrappers
// acquire b.mu themselves.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// reviewerConvergenceManifestTerminalStatuses is the set of HeadlessEvent
// manifest Status values that mean "this reviewer's most recent turn has
// ended cleanly." Used by the sweeper to differentiate timed-out from
// process-exited reviewers when filling skipped slots.
var reviewerConvergenceManifestTerminalStatuses = map[string]struct{}{
	"idle":  {},
	"error": {},
}

// ResolveReviewers returns the set of agent slugs whose Watching set
// intersects with the task's current signals. Order is stable
// (lexicographic) so callers can assert on the slice in tests.
//
// Tunnel-invited humans are not auto-assigned by this function — they
// are appended manually via the CLI (`wuphf task review --invite <slug>`)
// and stored on teamTask.Reviewers alongside the agent slugs.
func (b *Broker) ResolveReviewers(taskID string) ([]string, error) {
	if b == nil {
		return nil, fmt.Errorf("resolve reviewers: nil broker")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.resolveReviewersLocked(taskID)
}

func (b *Broker) resolveReviewersLocked(taskID string) ([]string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("resolve reviewers: task id required")
	}
	task := b.taskByIDLocked(taskID)
	if task == nil {
		return nil, fmt.Errorf("resolve reviewers: task %q not found", taskID)
	}
	signals := b.extractRoutingSignalsLocked(task)
	matched := make([]string, 0, len(b.members))
	seen := make(map[string]struct{}, len(b.members))
	for i := range b.members {
		member := &b.members[i]
		slug := strings.TrimSpace(member.Slug)
		if slug == "" {
			continue
		}
		if member.Watching.IsEmpty() {
			continue
		}
		if !watchingMatchesSignals(member.Watching, signals) {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		matched = append(matched, slug)
	}
	sort.Strings(matched)
	return matched, nil
}

// AssignReviewers stamps the resolved (or manually overridden) reviewer
// slug list onto the task and stamps ReviewStartedAt with the broker's
// clock. Idempotent: re-calling with the same slugs is a no-op for the
// reviewer list but always re-stamps the start time so a re-entered
// review window has a fresh deadline.
//
// The caller is expected to be the lifecycle transition layer's
// running → review hook. Lane D's broker_reviewer_routing.go does not
// register that hook itself; Lane A's transition layer is the canonical
// invocation point. For tests, AssignReviewers can be called directly.
func (b *Broker) AssignReviewers(taskID string, slugs []string) error {
	if b == nil {
		return fmt.Errorf("assign reviewers: nil broker")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.assignReviewersLocked(taskID, slugs)
}

func (b *Broker) assignReviewersLocked(taskID string, slugs []string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("assign reviewers: task id required")
	}
	task := b.taskByIDLocked(taskID)
	if task == nil {
		return fmt.Errorf("assign reviewers: task %q not found", taskID)
	}
	deduped := make([]string, 0, len(slugs))
	seen := make(map[string]struct{}, len(slugs))
	for _, raw := range slugs {
		slug := strings.TrimSpace(raw)
		if slug == "" {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		deduped = append(deduped, slug)
	}
	sort.Strings(deduped)
	task.Reviewers = deduped
	task.ReviewStartedAt = b.reviewerNow().UTC().Format(time.RFC3339)
	return nil
}

// AppendReviewerGrade is the Lane D-side stub for Lane C's Decision
// Packet mutator. Lane C will replace this implementation when it
// merges; the contract preserved here is the signature
// (taskID string, grade ReviewerGrade) error and the post-condition
// "the grade has been recorded against the task and convergence has
// been re-evaluated."
//
// While Lane C is parallel-in-flight, this stub stores grades in a
// transient in-memory slice on the broker keyed by task ID. The slice
// is intentionally NOT persisted to disk (Lane C owns persistence) so
// a broker restart drops the grades — acceptable for v1 build because
// the test suite seeds grades directly within the same process.
//
// Convergence is re-evaluated under b.mu after the grade lands; this
// matches the design doc's "trigger every grade-append" contract.
// appendReviewerGradeRoutingLocked records grade in the routing-side
// convergence index (b.reviewerGradesByTask) and re-runs the convergence
// rule. Lane C's AppendReviewerGrade is the public entrypoint and must
// call this helper after the packet write so the routing path observes
// the same grade. This avoids a second public method colliding on the
// canonical name.
func (b *Broker) appendReviewerGradeRoutingLocked(taskID string, grade ReviewerGrade) error {
	return b.appendReviewerGradeLocked(taskID, grade)
}

func (b *Broker) appendReviewerGradeLocked(taskID string, grade ReviewerGrade) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("append reviewer grade: task id required")
	}
	if strings.TrimSpace(grade.ReviewerSlug) == "" {
		return fmt.Errorf("append reviewer grade: reviewer slug required")
	}
	task := b.taskByIDLocked(taskID)
	if task == nil {
		return fmt.Errorf("append reviewer grade: task %q not found", taskID)
	}
	if grade.SubmittedAt.IsZero() {
		grade.SubmittedAt = b.reviewerNow().UTC()
	}
	if b.reviewerGradesByTask == nil {
		b.reviewerGradesByTask = make(map[string][]ReviewerGrade)
	}
	existing := b.reviewerGradesByTask[taskID]
	// Idempotency on (taskID, ReviewerSlug): a second grade from the
	// same reviewer overwrites the first slot rather than appending,
	// so the convergence rule sees the latest grade. This matches the
	// design's "review.submitted with grade present" semantics.
	replaced := false
	for i := range existing {
		if existing[i].ReviewerSlug == grade.ReviewerSlug {
			existing[i] = grade
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, grade)
	}
	b.reviewerGradesByTask[taskID] = existing
	if err := b.evaluateConvergenceLocked(taskID); err != nil {
		log.Printf("broker: reviewer convergence eval after grade append for task %q failed: %v", taskID, err)
	}
	return nil
}

// ReviewerGrades returns a copy of the grades recorded against a task.
// Used by tests; production consumers go through Lane C's Decision
// Packet read path.
func (b *Broker) ReviewerGrades(taskID string) []ReviewerGrade {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	src := b.reviewerGradesByTask[strings.TrimSpace(taskID)]
	out := make([]ReviewerGrade, len(src))
	copy(out, src)
	return out
}

// gcReviewerGradesByTaskLocked drops the routing-side mirror entry for
// a merged task. The Decision Packet retains the canonical grade list
// — this index is used only by evaluateConvergenceLocked, which never
// runs after a task transitions out of LifecycleStateReview.
//
// Caller must hold b.mu. Idempotent: a missing entry is a no-op.
func (b *Broker) gcReviewerGradesByTaskLocked(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || b.reviewerGradesByTask == nil {
		return
	}
	delete(b.reviewerGradesByTask, taskID)
}

// evaluateConvergenceLocked runs the convergence rule for a single task.
// Three exit paths:
//
//   - All assigned reviewers have a grade → transition to decision.
//   - Timeout elapsed AND at least one reviewer is missing → fill missing
//     slots with SeveritySkipped, transition to decision.
//   - Otherwise → no-op.
//
// Caller must hold b.mu. Idempotent: a task already in decision (or
// post-decision) is skipped without error.
func (b *Broker) evaluateConvergenceLocked(taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	task := b.taskByIDLocked(taskID)
	if task == nil {
		return fmt.Errorf("convergence: task %q not found", taskID)
	}
	if task.LifecycleState != LifecycleStateReview {
		return nil
	}
	reviewers := task.Reviewers
	if len(reviewers) == 0 {
		// No assigned reviewers means nothing to wait for; transition
		// immediately. The Decision Packet will surface as "no review
		// required" — the human still gates the merge.
		_, err := b.transitionLifecycleLocked(taskID, LifecycleStateDecision, "convergence: no reviewers assigned")
		return err
	}

	grades := b.reviewerGradesByTask[taskID]
	graded := make(map[string]bool, len(grades))
	for _, g := range grades {
		graded[g.ReviewerSlug] = true
	}

	missing := make([]string, 0, len(reviewers))
	for _, slug := range reviewers {
		if !graded[slug] {
			missing = append(missing, slug)
		}
	}

	if len(missing) == 0 {
		// All graded. Fire the transition exactly once; the lifecycle
		// transition layer rejects duplicate transitions because the
		// state will already be decision after this call.
		_, err := b.transitionLifecycleLocked(taskID, LifecycleStateDecision, "convergence: all reviewers graded")
		return err
	}

	// Some reviewers are still pending. Check timeout.
	deadline := b.reviewerDeadlineLocked(task)
	if deadline.IsZero() {
		// No start-time recorded — defensive. Treat as not-yet-elapsed.
		return nil
	}
	if !b.reviewerNow().After(deadline) {
		return nil
	}

	// Timeout elapsed. Fill each missing slot. Differentiate "process
	// exited" from "timed out" by checking the most-recent terminal
	// manifest status on the reviewer's task-scoped agent stream.
	terminalStatuses := b.observedTerminalStatusByReviewerLocked(taskID, missing)
	now := b.reviewerNow().UTC()
	packet := b.getOrInitPacketLocked(taskID)
	for _, slug := range missing {
		reasoning := "reviewer timed out"
		if status, ok := terminalStatuses[slug]; ok {
			if _, terminal := reviewerConvergenceManifestTerminalStatuses[status]; terminal {
				reasoning = "reviewer process exited"
			}
		}
		filler := ReviewerGrade{
			ReviewerSlug: slug,
			Severity:     SeveritySkipped,
			Reasoning:    reasoning,
			SubmittedAt:  now,
		}
		// Mirror the filler to BOTH stores so consumers — Lane G's
		// Decision Packet view (read-side) and Lane D's convergence
		// rule (the rule itself) — observe the same set of grades.
		// The pre-integration code only wrote the routing mirror,
		// which left packet.ReviewerGrades short of a slot whenever a
		// timeout fired and made the UI look like the reviewer had
		// silently disappeared. Both writes happen under the
		// already-held b.mu so they land atomically.
		b.reviewerGradesByTask[taskID] = append(b.reviewerGradesByTask[taskID], filler)
		packet.ReviewerGrades = append(packet.ReviewerGrades, filler)
		b.postReviewTimeoutChannelMessageLocked(task, slug, reasoning)
	}
	b.persistDecisionPacketLocked(taskID, *packet)

	_, err := b.transitionLifecycleLocked(taskID, LifecycleStateDecision, "convergence: timeout")
	return err
}

// EvaluateConvergence is the public wrapper around
// evaluateConvergenceLocked. Acquires b.mu. Used by tests and by the
// background sweeper goroutine.
func (b *Broker) EvaluateConvergence(taskID string) error {
	if b == nil {
		return fmt.Errorf("evaluate convergence: nil broker")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.evaluateConvergenceLocked(taskID)
}

// SweepReviewConvergence iterates every task currently in the review
// bucket of the lifecycle index and re-evaluates convergence. Cheap
// because the lifecycle index is O(1) and only review-state tasks are
// scanned (not the full task list). Called by the 30-second background
// goroutine and exposed for direct test invocation.
func (b *Broker) SweepReviewConvergence() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepReviewConvergenceLocked()
}

func (b *Broker) sweepReviewConvergenceLocked() {
	bucket := b.lifecycleIndex[LifecycleStateReview]
	for _, taskID := range bucket {
		if err := b.evaluateConvergenceLocked(taskID); err != nil {
			log.Printf("broker: review-convergence sweep for task %q failed: %v", taskID, err)
		}
	}
}

// StartReviewConvergenceSweeper launches the 30-second background
// goroutine that re-evaluates convergence for every review-state task.
// Returns a stop function the caller invokes on broker shutdown. Tests
// avoid this entry point and drive convergence by calling
// EvaluateConvergence / SweepReviewConvergence directly with their own
// fake clock.
func (b *Broker) StartReviewConvergenceSweeper(ctx context.Context) func() {
	if b == nil {
		return func() {}
	}
	ticker := time.NewTicker(time.Duration(reviewConvergenceTickInterval) * time.Second)
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ticker.Stop()
			close(done)
		})
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				b.SweepReviewConvergence()
			}
		}
	}()
	return stop
}

// reviewerDeadlineLocked computes the convergence deadline for a task
// from ReviewStartedAt + max(ReviewTimeoutSeconds, default). Returns
// the zero Time when ReviewStartedAt cannot be parsed, which the
// caller treats as "do not time out yet."
func (b *Broker) reviewerDeadlineLocked(task *teamTask) time.Time {
	if task == nil {
		return time.Time{}
	}
	startedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(task.ReviewStartedAt))
	if err != nil {
		return time.Time{}
	}
	timeoutSeconds := task.ReviewTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = reviewConvergenceDefaultTimeoutSeconds
	}
	return startedAt.Add(time.Duration(timeoutSeconds) * time.Second)
}

// reviewerNow returns the broker's clock. Tests inject b.nowFn to
// advance synthetic time without sleeping; production callers go
// through time.Now.
func (b *Broker) reviewerNow() time.Time {
	if b == nil {
		return time.Now()
	}
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

// observedTerminalStatusByReviewerLocked walks the most-recent manifest
// HeadlessEvent recorded for each reviewer on the task's agent stream
// and returns the latest Status (idle/error) per reviewer. Reviewers
// without any manifest events on this task are absent from the map.
//
// Cheap because each reviewer's stream is bounded by
// agentStreamTaskHistoryLimit. We do not parse non-manifest events
// (status/text/tool_use/tool_result) — only manifest, which carries
// the terminal Status.
func (b *Broker) observedTerminalStatusByReviewerLocked(taskID string, slugs []string) map[string]string {
	out := make(map[string]string, len(slugs))
	if b.agentStreams == nil {
		return out
	}
	for _, slug := range slugs {
		stream, ok := b.agentStreams[slug]
		if !ok {
			continue
		}
		// Walk in reverse so the FIRST manifest we hit is the most
		// recent. recentTaskLocked returns chronological order, so
		// iterate from the end.
		stream.mu.Lock()
		lines := stream.taskLines[taskID]
		stream.mu.Unlock()
		for i := len(lines) - 1; i >= 0; i-- {
			var ev HeadlessEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(lines[i])), &ev); err != nil {
				continue
			}
			if ev.Type != HeadlessEventTypeManifest {
				continue
			}
			out[slug] = ev.Status
			break
		}
	}
	return out
}

// extractRoutingSignalsLocked builds a ReviewerRoutingSignals snapshot
// for the task. Three sources:
//
//   - Files / WikiPaths: `git diff --name-only` between the worktree
//     and the parent branch. WikiPaths is the subset of Files that
//     glob-match the wiki article path convention. Empty when the
//     worktree path is unset (legacy task) or the diff exec fails;
//     errors are logged, not propagated, because routing on best-effort
//     signals is preferable to gating the entire convergence path.
//
//   - ToolNames: union of HeadlessEvent.ToolCalls from manifest events
//     across every agent stream that has lines for this task.
//
//   - TaskTags: verbatim teamTask.Tags.
func (b *Broker) extractRoutingSignalsLocked(task *teamTask) ReviewerRoutingSignals {
	if task == nil {
		return ReviewerRoutingSignals{}
	}
	signals := ReviewerRoutingSignals{
		TaskTags: append([]string(nil), task.Tags...),
	}

	files := b.taskWorktreeDiffLocked(task)
	signals.Files = files
	signals.WikiPaths = filterWikiPaths(files)

	signals.ToolNames = b.taskManifestToolNamesLocked(task.ID)

	return signals
}

// taskWorktreeDiffLocked runs `git diff --name-only` against the task's
// worktree and parent branch. Returns nil on any error; logging is
// best-effort only.
func (b *Broker) taskWorktreeDiffLocked(task *teamTask) []string {
	worktreePath := strings.TrimSpace(task.WorktreePath)
	if worktreePath == "" {
		return nil
	}
	parent := strings.TrimSpace(task.WorktreeBranch)
	if parent == "" {
		parent = "HEAD"
	}
	out, err := runGitOutput(worktreePath, "diff", "--name-only", parent)
	if err != nil {
		log.Printf("broker: reviewer routing: diff failed for task %q (worktree=%q parent=%q): %v",
			task.ID, worktreePath, parent, err)
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// taskManifestToolNamesLocked returns the union of distinct
// HeadlessEvent.ToolCalls.ToolName values observed across every agent
// stream's task-scoped buffer for taskID. Order is lexicographic.
func (b *Broker) taskManifestToolNamesLocked(taskID string) []string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	if b.agentStreams == nil {
		return nil
	}
	seen := make(map[string]struct{})
	for _, stream := range b.agentStreams {
		stream.mu.Lock()
		lines := stream.taskLines[taskID]
		// Copy to drop the lock fast.
		buf := make([]string, len(lines))
		copy(buf, lines)
		stream.mu.Unlock()
		for _, line := range buf {
			var ev HeadlessEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &ev); err != nil {
				continue
			}
			if ev.Type != HeadlessEventTypeManifest {
				continue
			}
			for _, call := range ev.ToolCalls {
				name := strings.TrimSpace(call.ToolName)
				if name == "" {
					continue
				}
				seen[name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// filterWikiPaths returns the subset of paths that look like wiki
// article paths. The convention is "wiki/" or "team/" prefixes (the
// repo's wiki worker stores articles under either); this matches the
// PR #729 manifest extractor's wiki-recognition heuristic.
func filterWikiPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		clean := strings.TrimPrefix(strings.TrimSpace(p), "./")
		if clean == "" {
			continue
		}
		if strings.HasPrefix(clean, "wiki/") || strings.HasPrefix(clean, "team/") {
			out = append(out, clean)
		}
	}
	return out
}

// watchingMatchesSignals returns true when ANY non-empty Watching
// category has at least one entry that matches the task's signals.
// Empty categories are skipped (an empty Files list does not auto-match
// every diff). At least one Watching category must be non-empty for
// any match to be reported — caller filters out IsEmpty agents earlier.
func watchingMatchesSignals(w Watching, s ReviewerRoutingSignals) bool {
	if len(w.Files) > 0 && anyGlobMatches(w.Files, s.Files) {
		return true
	}
	if len(w.WikiPaths) > 0 && anyGlobMatches(w.WikiPaths, s.WikiPaths) {
		return true
	}
	if len(w.ToolNames) > 0 && anyExactMatches(w.ToolNames, s.ToolNames) {
		return true
	}
	if len(w.TaskTags) > 0 && anyExactMatches(w.TaskTags, s.TaskTags) {
		return true
	}
	return false
}

// anyGlobMatches reports whether any pattern in patterns matches any
// candidate in candidates under filepath.Match semantics. Invalid
// glob patterns are logged and skipped — the routing layer must not
// fail an entire convergence because one agent's Watching set carries a
// malformed entry.
func anyGlobMatches(patterns, candidates []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			ok, err := filepath.Match(pattern, candidate)
			if err != nil {
				log.Printf("broker: reviewer routing: invalid glob %q: %v", pattern, err)
				break
			}
			if ok {
				return true
			}
		}
	}
	return false
}

// anyExactMatches reports whether any value in want is present in have
// (string-equal, trimmed). Used for ToolNames and TaskTags where glob
// semantics would over-match short canonical identifiers.
func anyExactMatches(want, have []string) bool {
	if len(want) == 0 || len(have) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(have))
	for _, v := range have {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = struct{}{}
		}
	}
	for _, v := range want {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := set[v]; ok {
			return true
		}
	}
	return false
}

// taskByIDLocked returns the *teamTask pointer for the given ID, or nil
// if not found. Caller must hold b.mu. Lookups are O(N) over b.tasks
// because the broker does not currently maintain a primary-key index;
// the lifecycle index is per-state, not per-id. This matches existing
// patterns in broker_tasks_*.go.
func (b *Broker) taskByIDLocked(taskID string) *teamTask {
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			return &b.tasks[i]
		}
	}
	return nil
}

// postReviewTimeoutChannelMessageLocked posts the agent.review.timeout
// banner to the team channel. Caller must hold b.mu. The message is
// kind=agent.review.timeout so the frontend can render a distinct
// banner without sniffing message text.
func (b *Broker) postReviewTimeoutChannelMessageLocked(task *teamTask, reviewerSlug, reasoning string) {
	if task == nil {
		return
	}
	channel := strings.TrimSpace(task.Channel)
	if channel == "" {
		channel = "general"
	}
	now := b.reviewerNow().UTC().Format(time.RFC3339)
	msg := channelMessage{
		ID:        fmt.Sprintf("review-timeout-%s-%s-%d", task.ID, reviewerSlug, b.reviewerNow().UnixNano()),
		From:      "system",
		Channel:   channel,
		Kind:      "agent.review.timeout",
		Title:     fmt.Sprintf("Reviewer %s did not grade task %s", reviewerSlug, task.ID),
		Content:   fmt.Sprintf("Reviewer %q on task %q: %s — slot filled with skipped placeholder.", reviewerSlug, task.ID, reasoning),
		Timestamp: now,
	}
	b.appendMessageLocked(msg)
}
