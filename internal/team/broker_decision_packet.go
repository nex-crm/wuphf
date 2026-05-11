package team

// broker_decision_packet.go owns the in-memory Decision Packet model,
// the b.mu-guarded mutators that intake / owner / reviewer goroutines
// call into, and the on-disk persistence layer at
// ~/.wuphf/tasks/<task_id>/decision_packet.json.
//
// Concurrency: every public mutator acquires b.mu before touching the
// b.decisionPackets map, so a reviewer goroutine appending a grade can
// race the intake driver appending a Spec without losing writes. This
// matches the Lane A pattern (b.mu serialises lifecycle state changes).
//
// Persistence model (matches the existing broker-state.json pattern):
//
//   1. Every TransitionLifecycle call triggers a write (synchronous, on
//      the broker goroutine).
//   2. While a task is in LifecycleStateRunning the broker also flushes
//      the packet on a 5-second debounced timer for durability across
//      restarts (the running phase produces no transitions).
//   3. Writes are atomic-rename: <id>/decision_packet.json.tmp →
//      <id>/decision_packet.json.
//   4. On write failure: log + 3-attempt retry with exponential backoff;
//      on final failure, post a channel banner to the team chat and
//      keep going. The next transition triggers a fresh retry.
//   5. On read failure (corrupt or missing JSON): regenerate from
//      in-memory state if the broker still has the packet; otherwise
//      transition the task to LifecycleStateUnknown so the operator
//      surfaces the missing state explicitly instead of seeing junk
//      data.
//
// The persistence layer is fronted by decisionPacketStore, an interface
// with one production implementation (osDecisionPacketStore) and one
// test implementation (used in broker_decision_packet_test.go to
// inject disk-full / corrupted-JSON failure modes). Production code
// never instantiates the test store; tests never instantiate the OS one
// against real ~/.wuphf paths.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// decisionPacketChannel is the channel banner target when persistence
// fails. The "general" channel is the existing fallback for system
// messages (see PostSystemMessage default).
const decisionPacketChannel = "general"

// decisionPacketWriteAttempts is the number of times a write is retried
// before the broker posts a banner and gives up for this transition.
const decisionPacketWriteAttempts = 3

// decisionPacketBackoffBase is the base delay used between retry
// attempts (multiplied by 2^attempt). Kept short because the broker
// goroutine is on the lifecycle hot path; long sleeps would freeze the
// inbox.
const decisionPacketBackoffBase = 25 * time.Millisecond

// decisionPacketRunningFlushInterval is the debounced timer used while
// a task sits in LifecycleStateRunning so progress is durable across
// restarts even without an explicit transition. Five seconds matches
// the design doc's "5-second flush" wording.
const decisionPacketRunningFlushInterval = 5 * time.Second

// ErrDecisionPacketNotFound is returned by GetDecisionPacket when no
// packet has been seeded for the task. Callers can distinguish missing
// from corrupt by inspecting the error.
var ErrDecisionPacketNotFound = errors.New("decision packet not found")

// ErrDecisionPacketCorrupt is returned by readDecisionPacketLocked when
// the on-disk JSON is unparseable. Used internally to drive the
// regenerate-or-mark-unknown branch.
var ErrDecisionPacketCorrupt = errors.New("decision packet on-disk file is corrupt")

// decisionPacketStore is the storage abstraction the broker writes
// Decision Packet JSON through. The interface deliberately models a
// per-task path layout — Read/Write take a taskID and the store knows
// the on-disk shape.
//
// Two implementations:
//
//   - osDecisionPacketStore (production): writes under
//     <runtime-home>/.wuphf/tasks/<taskID>/decision_packet.json with
//     atomic-rename semantics.
//   - injectable test stores in broker_decision_packet_test.go: simulate
//     disk-full / permission / corrupt-JSON failure modes without a
//     real filesystem.
type decisionPacketStore interface {
	// Write serialises the packet atomically. The implementation owns
	// JSON marshalling so callers cannot accidentally bypass the on-
	// disk shape contract.
	Write(taskID string, packet DecisionPacket) error
	// Read returns the on-disk packet, or ErrDecisionPacketNotFound when
	// the file does not exist, or ErrDecisionPacketCorrupt (wrapped)
	// when the JSON cannot be parsed.
	Read(taskID string) (DecisionPacket, error)
}

// osDecisionPacketStore is the production filesystem-backed store. The
// rootDir is bound at construction (typically <runtime-home>/.wuphf) so
// tests can pin a tmpdir without monkey-patching package globals.
type osDecisionPacketStore struct {
	rootDir string
}

// newOSDecisionPacketStore returns a production store rooted under the
// runtime home (env-overridable via WUPHF_RUNTIME_HOME). When the
// runtime home cannot be resolved, falls back to a process-relative
// .wuphf to mirror defaultBrokerStatePath's behaviour.
func newOSDecisionPacketStore() *osDecisionPacketStore {
	home := config.RuntimeHomeDir()
	if home == "" {
		return &osDecisionPacketStore{rootDir: ".wuphf"}
	}
	return &osDecisionPacketStore{rootDir: filepath.Join(home, ".wuphf")}
}

// taskDir returns the per-task directory ~/.wuphf/tasks/<id>/.
func (s *osDecisionPacketStore) taskDir(taskID string) string {
	return filepath.Join(s.rootDir, "tasks", strings.TrimSpace(taskID))
}

// packetPath returns the canonical on-disk path for the packet JSON.
func (s *osDecisionPacketStore) packetPath(taskID string) string {
	return filepath.Join(s.taskDir(taskID), "decision_packet.json")
}

// Write serialises the packet to <taskDir>/decision_packet.json.tmp and
// atomic-renames into place. The temp file is dropped on any error so
// the on-disk decision_packet.json is never left in a half-written
// state. mkdir-all keeps the broker resilient to a fresh ~/.wuphf
// directory created mid-run.
func (s *osDecisionPacketStore) Write(taskID string, packet DecisionPacket) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("decision packet write: empty task id")
	}
	if !IsSafeTaskID(taskID) {
		return fmt.Errorf("decision packet write: task id %q invalid", taskID)
	}
	dir := s.taskDir(taskID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("decision packet mkdir %s: %w", dir, err)
	}
	body, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return fmt.Errorf("decision packet marshal: %w", err)
	}
	finalPath := s.packetPath(taskID)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		return fmt.Errorf("decision packet write tmp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("decision packet rename %s -> %s: %w", tmpPath, finalPath, err)
	}
	return nil
}

// Read loads the on-disk packet. Returns ErrDecisionPacketNotFound when
// the file is missing, and a wrapped ErrDecisionPacketCorrupt when the
// JSON cannot be decoded.
func (s *osDecisionPacketStore) Read(taskID string) (DecisionPacket, error) {
	var packet DecisionPacket
	taskID = strings.TrimSpace(taskID)
	if !IsSafeTaskID(taskID) {
		return packet, fmt.Errorf("decision packet read: task id %q invalid", taskID)
	}
	path := s.packetPath(taskID)
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return packet, ErrDecisionPacketNotFound
		}
		return packet, fmt.Errorf("decision packet read %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &packet); err != nil {
		return packet, fmt.Errorf("%w at %s: %w", ErrDecisionPacketCorrupt, path, err)
	}
	return packet, nil
}

// decisionPacketState is the broker-side aggregate that wraps the in-
// memory packet map plus the persistence backend and a bookkeeping mutex.
// Stored on Broker as b.decisionPackets so a single test broker has
// both the live data and the store-injection seam.
type decisionPacketState struct {
	mu            sync.Mutex
	packets       map[string]*DecisionPacket
	store         decisionPacketStore
	runningTimers map[string]*time.Timer
}

// ensureDecisionPacketStateLocked lazily initialises b.decisionPackets so
// callers don't need to remember the first-touch boilerplate. Caller
// holds b.mu.
func (b *Broker) ensureDecisionPacketStateLocked() *decisionPacketState {
	if b == nil {
		return nil
	}
	if b.decisionPackets == nil {
		b.decisionPackets = &decisionPacketState{
			packets:       map[string]*DecisionPacket{},
			store:         newOSDecisionPacketStore(),
			runningTimers: map[string]*time.Timer{},
		}
	} else if b.decisionPackets.packets == nil {
		b.decisionPackets.packets = map[string]*DecisionPacket{}
	}
	if b.decisionPackets.runningTimers == nil {
		b.decisionPackets.runningTimers = map[string]*time.Timer{}
	}
	if b.decisionPackets.store == nil {
		b.decisionPackets.store = newOSDecisionPacketStore()
	}
	return b.decisionPackets
}

// SetDecisionPacketStore swaps the persistence backend on a broker. Used
// by tests to inject a mock store that simulates disk-full / corrupt
// JSON. Production code never calls this.
func (b *Broker) SetDecisionPacketStore(store decisionPacketStore) {
	if b == nil || store == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.ensureDecisionPacketStateLocked()
	state.store = store
}

// getOrInitPacketLocked returns the existing packet for taskID or
// allocates a fresh one with TaskID set. Caller holds b.mu.
func (b *Broker) getOrInitPacketLocked(taskID string) *DecisionPacket {
	state := b.ensureDecisionPacketStateLocked()
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if existing, ok := state.packets[taskID]; ok && existing != nil {
		return existing
	}
	packet := &DecisionPacket{TaskID: taskID}
	state.packets[taskID] = packet
	return packet
}

// stampLifecycleStateLocked syncs DecisionPacket.LifecycleState with the
// task's current lifecycle and bumps UpdatedAt to now. Called from every
// mutator and from TransitionLifecycle. Caller holds b.mu.
func (b *Broker) stampLifecycleStateLocked(packet *DecisionPacket) {
	if packet == nil {
		return
	}
	packet.UpdatedAt = time.Now().UTC()
	for i := range b.tasks {
		if b.tasks[i].ID == packet.TaskID {
			packet.LifecycleState = b.tasks[i].LifecycleState
			return
		}
	}
}

// SetSpec replaces the Spec on the packet for taskID. Lane B's intake
// driver calls this once per task; subsequent intake retries replace
// the prior Spec wholesale. Emits the spec.created lifecycle manifest.
func (b *Broker) SetSpec(taskID string, spec Spec) error {
	if b == nil {
		return fmt.Errorf("set spec: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("set spec: empty task id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	packet := b.getOrInitPacketLocked(taskID)
	packet.Spec = spec
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	b.emitLifecycleManifestLocked(lifecycleManifestPayload{
		Subkind:        LifecycleManifestSpecCreated,
		TaskID:         taskID,
		LifecycleState: packet.LifecycleState,
	})
	return nil
}

// AppendSessionReport replaces the SessionReport on the packet for
// taskID. The owner agent commits one session report per session;
// resumes (changes_requested → running) replace the prior report.
// Emits artifact.ready.
func (b *Broker) AppendSessionReport(taskID string, report SessionReport) error {
	if b == nil {
		return fmt.Errorf("append session report: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("append session report: empty task id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	packet := b.getOrInitPacketLocked(taskID)
	packet.SessionReport = report
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	b.emitLifecycleManifestLocked(lifecycleManifestPayload{
		Subkind:        LifecycleManifestArtifactReady,
		TaskID:         taskID,
		LifecycleState: packet.LifecycleState,
	})
	return nil
}

// AppendDiffSummary replaces the ChangedFiles list with the supplied
// slice. The owner agent recomputes the full diff at session-report
// time, so a wholesale replace matches the producer's mental model.
func (b *Broker) AppendDiffSummary(taskID string, files []DiffSummary) error {
	if b == nil {
		return fmt.Errorf("append diff summary: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("append diff summary: empty task id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	packet := b.getOrInitPacketLocked(taskID)
	cp := make([]DiffSummary, len(files))
	copy(cp, files)
	packet.ChangedFiles = cp
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	return nil
}

// AppendReviewerGrade is the multi-writer hot path. Each reviewer agent
// calls in once with their grade; the broker serialises the appends via
// b.mu. Emits review.submitted. Mirrors into Lane D's routing-side
// store so convergence runs on every grade.
func (b *Broker) AppendReviewerGrade(taskID string, grade ReviewerGrade) error {
	if b == nil {
		return fmt.Errorf("append reviewer grade: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("append reviewer grade: empty task id")
	}
	if !grade.Severity.IsCanonical() {
		return fmt.Errorf("append reviewer grade: severity %q is not canonical", grade.Severity)
	}
	if strings.TrimSpace(grade.ReviewerSlug) == "" {
		return fmt.Errorf("append reviewer grade: reviewer slug required")
	}
	if grade.SubmittedAt.IsZero() {
		grade.SubmittedAt = time.Now().UTC()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.appendReviewerGradeToPacketLocked(taskID, grade); err != nil {
		return err
	}
	// Mirror into Lane D's routing-side store so the convergence rule
	// runs on every grade. The routing helper is mutex-free under the
	// already-held b.mu and skips re-canonicalising fields the packet
	// path already validated.
	if task := b.taskByIDLocked(taskID); task != nil {
		if err := b.appendReviewerGradeRoutingLocked(taskID, grade); err != nil {
			log.Printf("broker: routing-side mirror of grade for task %q failed: %v", taskID, err)
			// Routing mirror is load-bearing: if Lane D's
			// convergence store never sees the grade, the task
			// will sit forever waiting on a grade that the packet
			// already claims happened. Surface the error so the
			// caller can retry / alert rather than silently
			// diverging.
			return fmt.Errorf("append reviewer grade: routing mirror: %w", err)
		}
	}
	return nil
}

// appendReviewerGradeToPacketLocked records `grade` on the packet's
// ReviewerGrades slice, replacing any existing entry for the same
// reviewer slug (idempotent retries do NOT duplicate). Caller must
// hold b.mu. Used by both AppendReviewerGrade (the public multi-writer
// path) and the convergence sweeper's timeout filler so the packet and
// routing stores stay in sync.
func (b *Broker) appendReviewerGradeToPacketLocked(taskID string, grade ReviewerGrade) error {
	packet := b.getOrInitPacketLocked(taskID)
	replaced := false
	for i, g := range packet.ReviewerGrades {
		if normalizeReviewerSlug(g.ReviewerSlug) == normalizeReviewerSlug(grade.ReviewerSlug) {
			packet.ReviewerGrades[i] = grade
			replaced = true
			break
		}
	}
	if !replaced {
		packet.ReviewerGrades = append(packet.ReviewerGrades, grade)
	}
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	b.emitLifecycleManifestLocked(lifecycleManifestPayload{
		Subkind:        LifecycleManifestReviewSubmitted,
		TaskID:         taskID,
		LifecycleState: packet.LifecycleState,
		ReviewerSlug:   grade.ReviewerSlug,
		Severity:       grade.Severity,
	})
	return nil
}

// SetDependencies replaces the Dependencies block on the packet. Sub-
// issue parents and BlockedOn entries are managed at the task level by
// existing broker code; this mutator just mirrors them onto the
// human-facing artifact.
func (b *Broker) SetDependencies(taskID string, deps Dependencies) error {
	if b == nil {
		return fmt.Errorf("set dependencies: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("set dependencies: empty task id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	packet := b.getOrInitPacketLocked(taskID)
	cp := Dependencies{ParentTaskID: deps.ParentTaskID}
	if len(deps.BlockedOn) > 0 {
		cp.BlockedOn = make([]string, len(deps.BlockedOn))
		copy(cp.BlockedOn, deps.BlockedOn)
	}
	packet.Dependencies = cp
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	return nil
}

// GetDecisionPacket returns a copy of the in-memory packet for taskID.
// On a cache miss, attempts to read from disk. If the on-disk file is
// corrupt, the broker logs a warning and returns
// ErrDecisionPacketNotFound — callers fall back to regenerate-or-unknown
// via OnDecisionPacketCorrupt.
func (b *Broker) GetDecisionPacket(taskID string) (DecisionPacket, error) {
	if b == nil {
		return DecisionPacket{}, fmt.Errorf("get decision packet: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return DecisionPacket{}, fmt.Errorf("get decision packet: empty task id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.ensureDecisionPacketStateLocked()
	state.mu.Lock()
	if existing, ok := state.packets[taskID]; ok && existing != nil {
		out := *existing
		state.mu.Unlock()
		return out, nil
	}
	state.mu.Unlock()
	disk, err := state.store.Read(taskID)
	if err != nil {
		if errors.Is(err, ErrDecisionPacketNotFound) {
			return DecisionPacket{}, ErrDecisionPacketNotFound
		}
		if errors.Is(err, ErrDecisionPacketCorrupt) {
			log.Printf("broker: decision packet on disk for task %q is corrupt; treating as missing: %v", taskID, err)
			return DecisionPacket{}, ErrDecisionPacketNotFound
		}
		return DecisionPacket{}, err
	}
	state.mu.Lock()
	cp := disk
	state.packets[taskID] = &cp
	state.mu.Unlock()
	return disk, nil
}

// RegenerateOrMarkUnknown handles the corrupt-on-disk recovery path.
// If broker memory still has the packet, it is rewritten to disk
// (regeneration). If memory is also empty (cold restart after crash),
// the task is transitioned to LifecycleStateUnknown so the operator
// surfaces the missing state explicitly. Build-time gate #6 (path 4).
func (b *Broker) RegenerateOrMarkUnknown(taskID string) error {
	if b == nil {
		return fmt.Errorf("regenerate decision packet: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("regenerate decision packet: empty task id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.ensureDecisionPacketStateLocked()
	state.mu.Lock()
	packet, ok := state.packets[taskID]
	state.mu.Unlock()
	if ok && packet != nil {
		// In-memory copy is authoritative; write it back to disk and
		// drop the corrupt file.
		b.persistDecisionPacketLocked(taskID, *packet)
		return nil
	}
	// Cold restart: the in-memory packet is gone too. Mark the task as
	// unknown so the operator sees an explicit recovery decision.
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			task := &b.tasks[i]
			prev := task.LifecycleState
			task.LifecycleState = LifecycleStateUnknown
			// Zero the legacy mirror fields inline so readers that
			// still branch on status / pipelineStage / reviewState /
			// blocked (rather than LifecycleState) uniformly see
			// "operator must resolve". Every other lifecycle write
			// routes through applyLifecycleStateLocked, which keeps
			// these in lockstep — without the explicit reset here a
			// half-stamped task in `unknown` would appear active to
			// legacy readers and quietly diverge from the lifecycle
			// index.
			task.pipelineStage = ""
			task.reviewState = ""
			task.status = "unknown"
			task.blocked = false
			b.indexLifecycleLocked(task.ID, prev, LifecycleStateUnknown)
			log.Printf("broker: decision packet for task %q corrupt with no in-memory copy; lifecycle moved to unknown for operator review", taskID)
			return nil
		}
	}
	return ErrDecisionPacketNotFound
}

// persistDecisionPacketLocked writes the packet to disk with retry. On
// final failure posts a channel banner. Caller holds b.mu.
//
// The retry loop runs synchronously on the broker goroutine. The total
// budget at base=25ms with attempts=3 is 25 + 50 = 75ms of sleep across
// failures, which is acceptable on the lifecycle hot path. Production
// disk-full conditions are extremely rare; retries primarily protect
// against transient EAGAIN / ENOSPC.
//
// TODO(v1.1): if disk-full latency turns out to matter in dogfood,
// move attempts 2-3 onto a background goroutine and update the
// retry-tests to wait deterministically on a sync hook.
func (b *Broker) persistDecisionPacketLocked(taskID string, packet DecisionPacket) {
	state := b.ensureDecisionPacketStateLocked()
	var lastErr error
	for attempt := 0; attempt < decisionPacketWriteAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(decisionPacketBackoffBase * time.Duration(1<<attempt))
		}
		if err := state.store.Write(taskID, packet); err != nil {
			lastErr = err
			log.Printf("broker: decision packet write attempt %d/%d for task %q: class=%s err=%v",
				attempt+1, decisionPacketWriteAttempts, taskID, classifyDecisionPacketError(err), err)
			continue
		}
		lastErr = nil
		break
	}
	if lastErr == nil {
		return
	}
	banner := fmt.Sprintf("persistence error on task %s — your changes are still in memory but not saved to disk yet, fix the underlying issue and the next transition will retry.", taskID)
	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   decisionPacketChannel,
		Kind:      "persistence_error",
		Content:   banner,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
}

// classifyDecisionPacketError returns a coarse class string used in
// logging so operators triaging a banner can grep the broker log for
// the underlying failure mode without parsing wrapped chains.
func classifyDecisionPacketError(err error) string {
	if err == nil {
		return "ok"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no space"), strings.Contains(msg, "disk full"), strings.Contains(msg, "enospc"):
		return "disk-full"
	case strings.Contains(msg, "permission denied"), strings.Contains(msg, "eacces"):
		return "permission"
	case strings.Contains(msg, "read-only"), strings.Contains(msg, "erofs"):
		return "read-only"
	default:
		return "io"
	}
}

// scheduleRunningFlushLocked arms a debounced flush timer for taskID
// while it sits in LifecycleStateRunning. If a timer already exists,
// it is reset. Caller holds b.mu.
//
// The timer fires on its own goroutine and re-acquires b.mu; the
// callback writes the current in-memory packet without emitting any
// new manifest event (this is durability, not progress).
func (b *Broker) scheduleRunningFlushLocked(taskID string) {
	state := b.ensureDecisionPacketStateLocked()
	if existing, ok := state.runningTimers[taskID]; ok && existing != nil {
		existing.Reset(decisionPacketRunningFlushInterval)
		return
	}
	timer := time.AfterFunc(decisionPacketRunningFlushInterval, func() {
		b.flushRunningPacket(taskID)
	})
	state.runningTimers[taskID] = timer
}

// cancelRunningFlushLocked stops the running-flush timer (if any) for
// the task. Called when the task transitions out of running.
func (b *Broker) cancelRunningFlushLocked(taskID string) {
	state := b.ensureDecisionPacketStateLocked()
	if timer, ok := state.runningTimers[taskID]; ok && timer != nil {
		timer.Stop()
		delete(state.runningTimers, taskID)
	}
}

// flushRunningPacket is the timer callback. Acquires b.mu, writes the
// current packet, and (if the task is still running) re-arms the timer.
func (b *Broker) flushRunningPacket(taskID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.ensureDecisionPacketStateLocked()
	state.mu.Lock()
	packet, ok := state.packets[taskID]
	state.mu.Unlock()
	if !ok || packet == nil {
		return
	}
	stillRunning := false
	for i := range b.tasks {
		if b.tasks[i].ID == taskID && b.tasks[i].LifecycleState == LifecycleStateRunning {
			stillRunning = true
			break
		}
	}
	b.persistDecisionPacketLocked(taskID, *packet)
	if stillRunning {
		// Re-arm without scheduleRunningFlushLocked's reset shortcut:
		// the timer has already fired so we replace it with a fresh
		// AfterFunc.
		state.runningTimers[taskID] = time.AfterFunc(decisionPacketRunningFlushInterval, func() {
			b.flushRunningPacket(taskID)
		})
	} else {
		delete(state.runningTimers, taskID)
	}
}

// OnReviewerConvergence is the hook Lane D's convergence rule calls
// when all assigned reviewers have graded (or the timeout fired). The
// hook emits decision.required and transitions the task into the
// decision lifecycle state. Lane D owns the rule; Lane C owns the
// transition + event.
func (b *Broker) OnReviewerConvergence(taskID string, reason string) error {
	if b == nil {
		return fmt.Errorf("on reviewer convergence: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("on reviewer convergence: empty task id")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "all reviewers graded"
	}
	if err := b.TransitionLifecycle(taskID, LifecycleStateDecision, reason); err != nil {
		return fmt.Errorf("on reviewer convergence: transition: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	packet := b.getOrInitPacketLocked(taskID)
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	b.emitLifecycleManifestLocked(lifecycleManifestPayload{
		Subkind:        LifecycleManifestDecisionRequired,
		TaskID:         taskID,
		LifecycleState: packet.LifecycleState,
		Reason:         reason,
	})
	return nil
}

// recordDecisionAction is the set of human actions Lane G's UI dispatches
// into the decision endpoint. Kept as a typed string so a typo in the
// REST handler ("megre") fails to compile.
type recordDecisionAction string

const (
	RecordDecisionApprove        recordDecisionAction = "approve"
	RecordDecisionRequestChanges recordDecisionAction = "request_changes"
	RecordDecisionBlock          recordDecisionAction = "block"
	RecordDecisionDefer          recordDecisionAction = "defer"
)

// canonicalRecordDecisionActions enumerates the four valid resolutions.
func canonicalRecordDecisionActions() []recordDecisionAction {
	return []recordDecisionAction{
		RecordDecisionApprove,
		RecordDecisionRequestChanges,
		RecordDecisionBlock,
		RecordDecisionDefer,
	}
}

// IsCanonical reports whether a is one of the four valid resolutions.
func (a recordDecisionAction) IsCanonical() bool {
	for _, want := range canonicalRecordDecisionActions() {
		if a == want {
			return true
		}
	}
	return false
}

// RecordTaskDecision is the resolution endpoint Lane G's UI calls when
// the human merges, requests changes, blocks, or defers a Decision
// Packet. Emits decision.recorded and routes the underlying state
// change through the existing lifecycle transition layer.
//
// On merge: transitions to LifecycleStateApproved and writes the wiki
// promotion article (see writeWikiPromotionLocked). The auto-merge
// evaluator is deferred to v1.1 (see TODO inline).
//
// The Decision Packet RecordTaskDecision is distinct from the
// pre-existing decision-ledger RecordDecision in ledger.go (which
// records a free-form officeDecisionRecord). This entry point is the
// human's resolution of one Decision Packet; ledger.RecordDecision is
// the broker-wide audit log.

// ErrUnknownDecisionAction is returned when rawAction does not map to
// a canonical recordDecisionAction value. Wraps the HTTP layer that
// surfaces it as 400 to distinguish from internal failures (500).
var ErrUnknownDecisionAction = errors.New("record decision: action is not canonical")

// RecordTaskDecision records a decision attributed to actorSlug. When
// actorSlug is empty the broker stamps "system" so the audit trail is
// always populated — the HTTP handler passes the authenticated
// requestActor.Slug; internal callers (timeout sweeper, tests) can
// pass "" to opt into the system fallback.
func (b *Broker) RecordTaskDecision(taskID, rawAction, actorSlug string) error {
	if b == nil {
		return fmt.Errorf("record decision: nil broker")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("record decision: empty task id")
	}
	if !IsSafeTaskID(taskID) {
		return fmt.Errorf("record decision: task id %q invalid", taskID)
	}
	action := recordDecisionAction(strings.TrimSpace(rawAction))
	// Phase 1 backwards-compat shim: pre-rename callers send "merge";
	// post-rename callers send "approve". Both must work for one
	// release. Phase 2's first commit deletes this shim.
	// TODO(phase-2-first-commit): delete this shim.
	if action == "merge" {
		log.Printf("broker: deprecation: decision action %q is deprecated; use %q (Phase 2 will drop this shim)", "merge", "approve")
		action = RecordDecisionApprove
	}
	if !action.IsCanonical() {
		return fmt.Errorf("%w: %q", ErrUnknownDecisionAction, rawAction)
	}
	actorSlug = strings.TrimSpace(actorSlug)
	if actorSlug == "" {
		actorSlug = "system"
	}
	target, reason := lifecycleStateForDecisionAction(action)
	// TODO(v1.1): auto-merge evaluator — once a safe-class definition
	// (e.g. wiki-only edits with all green checks and zero
	// critical/major grades) lands, route merge actions through the
	// evaluator before falling back to direct lifecycle transition.
	//
	// Hold b.mu across the transition + packet stamp + persist so the
	// running-flush timer / convergence sweeper cannot fire between
	// transition and stamp and overwrite the just-stamped packet with
	// a pre-decision copy (code-reviewer H3).
	// Wrap the locked section in an inner function so we can defer the
	// unlock and stay panic-safe across the five Locked helpers below.
	// A panic in any of them used to leave b.mu held forever; defer +
	// IIFE collapses both the early-return unlock site and the trailing
	// unlock into one path.
	if err := func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, err := b.transitionLifecycleLocked(taskID, target, reason); err != nil {
			return fmt.Errorf("record decision: transition: %w", err)
		}
		packet := b.getOrInitPacketLocked(taskID)
		b.stampLifecycleStateLocked(packet)
		b.persistDecisionPacketLocked(taskID, *packet)
		b.emitLifecycleManifestLocked(lifecycleManifestPayload{
			Subkind:        LifecycleManifestDecisionRecorded,
			TaskID:         taskID,
			LifecycleState: packet.LifecycleState,
			Action:         string(action),
			ActorSlug:      actorSlug,
			Reason:         reason,
		})
		if action == RecordDecisionApprove {
			b.writeWikiPromotionLocked(taskID, *packet)
			b.broadcastDecisionLocked(taskID, *packet)
		}
		return nil
	}(); err != nil {
		return err
	}
	// OnDecisionRecorded acquires b.mu itself; call it after Unlock
	// to avoid a self-deadlock through the unblock cascade.
	b.OnDecisionRecorded(taskID)
	return nil
}

// lifecycleStateForDecisionAction maps a human resolution to the target
// lifecycle state plus a transition reason string. Kept as a small
// table so the mapping is grep-able.
func lifecycleStateForDecisionAction(action recordDecisionAction) (LifecycleState, string) {
	switch action {
	case RecordDecisionApprove:
		return LifecycleStateApproved, "human merged decision"
	case RecordDecisionRequestChanges:
		return LifecycleStateChangesRequested, "human requested changes"
	case RecordDecisionBlock:
		return LifecycleStateBlockedOnPRMerge, "human blocked on dependency"
	case RecordDecisionDefer:
		// Defer keeps the task on the human's plate but stops the
		// broker's reviewer convergence countdown by parking it back
		// in review until the human re-engages.
		return LifecycleStateReview, "human deferred decision"
	}
	return LifecycleStateReview, "unknown action"
}

// wikiPromotionPath returns the team/-rooted relative path the merged
// Decision Packet is promoted to. Lives under team/decisions/ so the
// existing wiki path validator (validateArticlePath) accepts it.
// Returns the empty string when taskID fails the safe-id allowlist;
// callers should treat empty as "skip wiki promotion" rather than
// fabricate a path from untrusted input.
func wikiPromotionPath(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if !IsSafeTaskID(taskID) {
		return ""
	}
	return filepath.ToSlash(filepath.Join("team", "decisions", taskID+".md"))
}

// renderWikiPromotion produces the markdown body for a merged Decision
// Packet. The shape is grep-able sections that match the on-page rhythm
// of the Decision Packet view: title from spec.Problem, AC list,
// session report, reviewer grades, and the final action.
func renderWikiPromotion(packet DecisionPacket) string {
	var sb strings.Builder
	title := strings.TrimSpace(packet.Spec.Problem)
	if title == "" {
		title = "Decision " + packet.TaskID
	}
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("Task ID: `" + packet.TaskID + "`\n")
	if !packet.UpdatedAt.IsZero() {
		sb.WriteString("Merged at: " + packet.UpdatedAt.UTC().Format(time.RFC3339) + "\n")
	}
	sb.WriteString("\n## Spec\n\n")
	if outcome := strings.TrimSpace(packet.Spec.TargetOutcome); outcome != "" {
		sb.WriteString("**Target outcome:** " + outcome + "\n\n")
	}
	if assignment := strings.TrimSpace(packet.Spec.Assignment); assignment != "" {
		sb.WriteString("**Assignment:** " + assignment + "\n\n")
	}
	if len(packet.Spec.AcceptanceCriteria) > 0 {
		sb.WriteString("### Acceptance criteria\n\n")
		for _, ac := range packet.Spec.AcceptanceCriteria {
			marker := "[ ]"
			if ac.Done {
				marker = "[x]"
			}
			sb.WriteString("- " + marker + " " + ac.Statement + "\n")
		}
		sb.WriteString("\n")
	}
	if len(packet.Spec.Constraints) > 0 {
		sb.WriteString("### Constraints\n\n")
		for _, c := range packet.Spec.Constraints {
			sb.WriteString("- " + c + "\n")
		}
		sb.WriteString("\n")
	}
	report := packet.SessionReport
	if strings.TrimSpace(report.Highlights) != "" || len(report.TopWins) > 0 || len(report.DeadEnds) > 0 || len(report.Metadata) > 0 {
		sb.WriteString("## Session report\n\n")
		if hl := strings.TrimSpace(report.Highlights); hl != "" {
			sb.WriteString(hl + "\n\n")
		}
		if len(report.TopWins) > 0 {
			sb.WriteString("### Top wins\n\n")
			for _, w := range report.TopWins {
				if w.Delta != "" {
					sb.WriteString("- **" + w.Delta + "** — " + w.Description + "\n")
				} else {
					sb.WriteString("- " + w.Description + "\n")
				}
			}
			sb.WriteString("\n")
		}
		if len(report.DeadEnds) > 0 {
			sb.WriteString("### Dead ends\n\n")
			for _, d := range report.DeadEnds {
				sb.WriteString("- *(discard)* " + d.Tried + " — " + d.Reason + "\n")
			}
			sb.WriteString("\n")
		}
		if len(report.Metadata) > 0 {
			sb.WriteString("### Metadata\n\n")
			keys := make([]string, 0, len(report.Metadata))
			for k := range report.Metadata {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString("- `" + k + "`: " + report.Metadata[k] + "\n")
			}
			sb.WriteString("\n")
		}
	}
	if len(packet.ReviewerGrades) > 0 {
		sb.WriteString("## Reviewer grades\n\n")
		for _, g := range packet.ReviewerGrades {
			sb.WriteString("- **" + string(g.Severity) + "** by `" + g.ReviewerSlug + "`")
			if loc := renderReviewerLocation(g); loc != "" {
				sb.WriteString(" — " + loc)
			}
			sb.WriteString("\n")
			if s := strings.TrimSpace(g.Suggestion); s != "" {
				sb.WriteString("  - Suggestion: " + s + "\n")
			}
			if r := strings.TrimSpace(g.Reasoning); r != "" {
				sb.WriteString("  - Reasoning: " + r + "\n")
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("## Decision\n\nMerged via WUPHF Decision Inbox.\n")
	return sb.String()
}

func renderReviewerLocation(g ReviewerGrade) string {
	if g.FilePath == "" {
		return ""
	}
	if g.Line > 0 {
		return fmt.Sprintf("`%s:%d`", g.FilePath, g.Line)
	}
	return fmt.Sprintf("`%s`", g.FilePath)
}

// writeWikiPromotionLocked enqueues the wiki article promotion via the
// existing wiki worker. Caller holds b.mu. Failures are logged but do
// not block the merge transition itself — the on-disk
// decision_packet.json is the source of truth, and the wiki article is
// a derived view.
func (b *Broker) writeWikiPromotionLocked(taskID string, packet DecisionPacket) {
	if b == nil || b.wikiWorker == nil {
		// Tests without a wiki worker simply skip promotion. The
		// merge transition itself already landed.
		return
	}
	body := renderWikiPromotion(packet)
	relPath := wikiPromotionPath(taskID)
	if relPath == "" {
		log.Printf("broker: wiki promotion skipped for task %q (invalid id)", taskID)
		return
	}
	commitMsg := fmt.Sprintf("decision: promote %s on approval", taskID)
	ctx := b.wikiPromotionContext()
	// Capture the worker locally so the goroutine doesn't race with
	// concurrent nil-assignment on b.wikiWorker (e.g. broker shutdown).
	worker := b.wikiWorker
	// Run the wiki write off-lock so a slow git commit cannot block
	// every other broker mutator. The wiki worker has its own
	// serialisation queue, so concurrent triggers are safe.
	go func() {
		_, _, err := worker.Enqueue(ctx, "system", relPath, body, "create", commitMsg)
		if err != nil {
			log.Printf("broker: wiki promotion for task %q failed: %v", taskID, err)
		}
	}()
}

// broadcastDecisionLocked posts a system message to the task's
// originating channel announcing the merged decision. This closes the
// "merging means posting output to everyone in channel and wiki" leg
// of the multi-agent control loop — wiki promotion is the canonical
// record, this announce is the discovery hook so other agents
// subscribed to the channel know to read the wiki.
//
// Caller holds b.mu.
func (b *Broker) broadcastDecisionLocked(taskID string, packet DecisionPacket) {
	if b == nil {
		return
	}
	task := b.findTaskByIDLocked(taskID)
	channel := ""
	if task != nil {
		channel = normalizeChannelSlug(task.Channel)
	}
	if channel == "" {
		channel = "general"
	}
	headline := strings.TrimSpace(packet.Spec.Problem)
	if headline == "" && task != nil {
		headline = strings.TrimSpace(task.Title)
	}
	if headline == "" {
		headline = "decision recorded"
	}
	relPath := wikiPromotionPath(taskID)
	wikiNote := ""
	if relPath != "" {
		wikiNote = fmt.Sprintf("\nCanonical output: `wiki/%s`", relPath)
	}
	content := fmt.Sprintf(
		"🔔 %s approved: %s. Other agents — read the wiki entry, not pre-approval channel messages.%s",
		taskID, headline, wikiNote,
	)
	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   channel,
		Kind:      "decision_approved",
		Title:     fmt.Sprintf("%s approved", taskID),
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
}

// wikiPromotionContext returns the broker's lifecycle context if
// available, otherwise context.Background(). The wiki worker accepts a
// context so a broker shutdown can drain in-flight writes.
func (b *Broker) wikiPromotionContext() context.Context {
	if b == nil || b.lifecycleCtx == nil {
		return context.Background()
	}
	return b.lifecycleCtx
}
