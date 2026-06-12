package team

// office_eval.go — the U0.1 outcome eval harness (docs/specs/sota-uplift.md).
//
// Deterministic checks that boot a real broker and measure HARNESS quality:
// does the system deliver full specs, full thread context, task-relevant
// knowledge, and upstream outcomes to the agent that needs them, and does
// the task lifecycle hold its contract end to end. No LLM calls — the
// scripted driver below plays the agent, so the checks measure what the
// harness puts in front of a model, not what a model does with it.
//
// Each check encodes one verified gap from the SOTA gap analysis. Checks
// with a non-empty KnownGap are EXPECTED to fail until the named phase
// lands; they are the executable form of the uplift plan. The compounding
// delta (warm knowledge present in packets vs cold absent) is the moat
// metric the plan gates on.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// OfficeEvalCheck is one scored assertion inside an eval job.
type OfficeEvalCheck struct {
	Job      string `json:"job"`
	Check    string `json:"check"`
	Pass     bool   `json:"pass"`
	Detail   string `json:"detail,omitempty"`
	KnownGap string `json:"known_gap,omitempty"` // expected red until this plan phase lands
}

// OfficeEvalReport aggregates all checks from one harness run.
type OfficeEvalReport struct {
	Checks []OfficeEvalCheck `json:"checks"`
}

// Passed counts green checks.
func (r *OfficeEvalReport) Passed() int {
	n := 0
	for _, c := range r.Checks {
		if c.Pass {
			n++
		}
	}
	return n
}

// UnexpectedFailures returns red checks that are NOT marked as known gaps —
// these indicate a regression and should fail CI in strict mode.
func (r *OfficeEvalReport) UnexpectedFailures() []OfficeEvalCheck {
	var out []OfficeEvalCheck
	for _, c := range r.Checks {
		if !c.Pass && c.KnownGap == "" {
			out = append(out, c)
		}
	}
	return out
}

// KnownGapStatus returns known-gap checks with their current state, so a
// phase landing flips visibly from red to green in the report.
func (r *OfficeEvalReport) KnownGapStatus() []OfficeEvalCheck {
	var out []OfficeEvalCheck
	for _, c := range r.Checks {
		if c.KnownGap != "" {
			out = append(out, c)
		}
	}
	return out
}

func (r *OfficeEvalReport) add(job, check string, pass bool, detail, knownGap string) {
	r.Checks = append(r.Checks, OfficeEvalCheck{
		Job: job, Check: check, Pass: pass, Detail: detail, KnownGap: knownGap,
	})
}

// launcherForBrokerFixture builds a bare ceo+eng launcher bound to the
// given broker — enough for packet construction in evals and tests without
// pane/tmux state.
func launcherForBrokerFixture(b *Broker) *Launcher {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			},
		},
	}
	l.installBroker(b)
	return l
}

// officeEvalFixture is one scratch office: an in-process broker with a wiki
// worker + learning log, and a bare launcher bound to it for packet builds.
type officeEvalFixture struct {
	broker     *Broker
	launcher   *Launcher
	scratchDir string
	cleanup    func()
}

func newOfficeEvalFixture(dir string) (*officeEvalFixture, error) {
	root := filepath.Join(dir, "wiki")
	backup := filepath.Join(dir, "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		return nil, fmt.Errorf("office eval: wiki repo init: %w", err)
	}
	b := NewBrokerAt(filepath.Join(dir, "broker-state.json"))
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	b.mu.Lock()
	b.wikiWorker = worker
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "eng", Name: "Engineer"},
	}
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}},
	}
	b.mu.Unlock()
	b.ensureTeamLearningLog()

	l := launcherForBrokerFixture(b)

	return &officeEvalFixture{
		broker:     b,
		launcher:   l,
		scratchDir: dir,
		cleanup: func() {
			cancel()
			<-worker.Done()
		},
	}, nil
}

// seedWikiFile materializes a deliverable file under the fixture's wiki
// root so the B5 artifact-existence gate (knowledge-integrity) sees a real
// file — done now requires the artifact to exist, not just be named. Plain
// disk write; existence does not require a commit.
func (fx *officeEvalFixture) seedWikiFile(relPath, content string) error {
	worker := fx.broker.WikiWorker()
	if worker == nil || worker.Repo() == nil {
		return fmt.Errorf("seed wiki file: no wiki worker")
	}
	full := filepath.Join(worker.Repo().Root(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o600)
}

// RunOfficeEvals runs every eval job in its own scratch office under dir
// and returns the combined report. dir must be a writable scratch directory
// (the caller owns its lifetime; t.TempDir() or os.MkdirTemp both work).
func RunOfficeEvals(dir string) (*OfficeEvalReport, error) {
	report := &OfficeEvalReport{}
	// Force lexical-only retrieval for the whole run so a host env with a
	// real embedding key (OPENAI_API_KEY / VOYAGE_API_KEY) cannot make the
	// deterministic checks network-dependent. The hybrid-retrieval job
	// installs the deterministic stub for its own scope.
	setRetrievalEmbedding(nil, nil)
	defer resetRetrievalEmbedding()
	// Pin the office runtime home to the eval scratch dir — the same env
	// knob the live office launches with. Agent scratch dirs and DoD-check
	// working dirs (headless_workspace.go) derive from it; without the pin
	// a `go run ./cmd/office-eval` would create dirs under the developer's
	// real ~/.wuphf. Restored on return; in `go test` the package init
	// already pins this var to a leaked tempdir, so the override nests.
	priorHome, hadHome := os.LookupEnv("WUPHF_RUNTIME_HOME")
	if err := os.Setenv("WUPHF_RUNTIME_HOME", dir); err != nil {
		return nil, fmt.Errorf("office eval: pin runtime home: %w", err)
	}
	defer func() {
		if hadHome {
			_ = os.Setenv("WUPHF_RUNTIME_HOME", priorHome)
		} else {
			_ = os.Unsetenv("WUPHF_RUNTIME_HOME")
		}
	}()
	jobs := []struct {
		name string
		run  func(*officeEvalFixture, *OfficeEvalReport) error
	}{
		{"lifecycle-basic", evalJobLifecycleBasic},
		{"intake-definition", evalJobIntakeDefinition},
		{"spec-fidelity", evalJobSpecFidelity},
		{"thread-context", evalJobThreadContext},
		{"knowledge-injection", evalJobKnowledgeInjection},
		{"dependency-handoff", evalJobDependencyHandoff},
		{"turn-journal", evalJobTurnJournal},
		{"compounding-loop", evalJobCompoundingLoop},
		{"completion-hook", evalJobCompletionHook},
		{"human-sovereignty", evalJobHumanSovereignty},
		{"entity-articles", evalJobEntityArticles},
		{"playbook-compilation", evalJobPlaybookCompilation},
		{"notebook-bookends", evalJobNotebookBookends},
		{"hybrid-retrieval", evalJobHybridRetrieval},
		{"grounding", evalJobGrounding},
		{"done-integrity", evalJobDoneIntegrity},
		{"live-paths", evalJobLivePaths},
		{"workspace-isolation", evalJobWorkspaceIsolation},
		{"utterance-routing", evalJobUtteranceRouting},
		{"task-integrity", evalJobTaskIntegrity},
		{"knowledge-integrity", evalJobKnowledgeIntegrity},
		{"scheduler-truth", evalJobSchedulerTruth},
		{"human-boundary", evalJobHumanBoundary},
		{"platform-honesty", evalJobPlatformHonesty},
	}
	for i, job := range jobs {
		fx, err := newOfficeEvalFixture(filepath.Join(dir, fmt.Sprintf("job-%d", i)))
		if err != nil {
			return nil, fmt.Errorf("office eval %s: fixture: %w", job.name, err)
		}
		err = job.run(fx, report)
		fx.cleanup()
		if err != nil {
			return nil, fmt.Errorf("office eval %s: %w", job.name, err)
		}
	}
	return report, nil
}

// evalJobLifecycleBasic: a task created with an owner can be completed by
// that owner and lands in a done status with dependents' bookkeeping intact.
func evalJobLifecycleBasic(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "lifecycle-basic"
	task, _, err := fx.broker.EnsureTask("general", "Ship the welcome email", "Send the welcome email to the new signup list.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	r.add(job, "task created with owner", task.ID != "" && task.Owner == "eng", fmt.Sprintf("id=%s owner=%s state=%s", task.ID, task.Owner, task.LifecycleState), "")

	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: task.ID, Channel: "general", CreatedBy: "eng"}); err != nil {
		r.add(job, "owner can complete the task", false, err.Error(), "")
		return nil
	}
	inReview := fx.broker.TaskByID(task.ID)
	r.add(job, "owner completion routes through review", inReview != nil && strings.EqualFold(strings.TrimSpace(inReview.status), "review"),
		fmt.Sprintf("status=%q lifecycle=%s", strings.TrimSpace(inReview.status), inReview.LifecycleState), "")
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: task.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		r.add(job, "reviewer can approve the task", false, err.Error(), "")
		return nil
	}
	done := fx.broker.TaskByID(task.ID)
	r.add(job, "approved task reaches a done status", done != nil && strings.EqualFold(strings.TrimSpace(done.status), "done"),
		fmt.Sprintf("status=%q lifecycle=%s", strings.TrimSpace(done.status), done.LifecycleState), "")

	// U1.1 regression guard: a task with a required definition-of-done
	// check cannot be completed while the check fails, and the failure is
	// stamped on the task; once the check passes, completion proceeds.
	gated, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Ship the export with a passing check",
		Details: "gated work", Owner: "eng", CreatedBy: "ceo",
		VerificationKind: "command", VerificationSpec: "test -f proof.txt", VerificationRequired: true,
	})
	if err != nil {
		return err
	}
	_, completeErr := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: gated.Task.ID, Channel: "general", CreatedBy: "eng"})
	stamped := fx.broker.TaskByID(gated.Task.ID)
	r.add(job, "failing definition-of-done check blocks completion", completeErr != nil &&
		stamped != nil && stamped.VerificationResult != nil && !stamped.VerificationResult.Pass &&
		!strings.EqualFold(strings.TrimSpace(stamped.status), "done"),
		fmt.Sprintf("completeErr=%v", completeErr), "")

	// Produce the artifact the check demands, then complete again: the
	// harness — not the agent's claim — decides done.
	workDir := strings.TrimSpace(stamped.WorktreePath)
	if workDir == "" {
		workDir = fx.scratchDir
	}
	if err := os.WriteFile(filepath.Join(workDir, "proof.txt"), []byte("export shipped"), 0o644); err != nil {
		return err
	}
	if workDir == fx.scratchDir {
		// No task worktree in the fixture: pin the check's cwd by
		// rewriting the spec to an absolute path probe.
		fx.broker.mu.Lock()
		if t := fx.broker.taskByIDLocked(gated.Task.ID); t != nil && t.Verification != nil {
			t.Verification.Spec = "test -f " + filepath.Join(workDir, "proof.txt")
		}
		fx.broker.mu.Unlock()
	}
	_, completeErr = fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: gated.Task.ID, Channel: "general", CreatedBy: "eng"})
	verified := fx.broker.TaskByID(gated.Task.ID)
	r.add(job, "completion was machine-verified before done", completeErr == nil &&
		verified != nil && verified.VerificationResult != nil && verified.VerificationResult.Pass,
		fmt.Sprintf("completeErr=%v result=%+v", completeErr, verified.VerificationResult), "")
	return nil
}

// evalJobIntakeDefinition: the R4 intake contract (core-loop step 2). The CEO
// defines a created task via team_task action=define; the definition must
// persist, round-trip the teamTask wire shape, and lead the owner's execution
// packet. A non-CEO specialist must NOT be able to define — same auth class
// as the other scope-shaping actions.
func evalJobIntakeDefinition(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "intake-definition"
	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Launch the partner newsletter",
		Details: "Get the first partner newsletter out the door.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	def := &TaskDefinition{
		Goal: "Ship the first partner newsletter to the approved partner list this week",
		Deliverables: []TaskDeliverable{
			{Name: "newsletter draft", Format: "markdown in the wiki"},
			{Name: "send report", Format: "CSV"},
		},
		SuccessCriteria: []string{
			"Draft approved by the human before sending",
			"newsletter.md exists in the task worktree",
		},
		AccessNeeded: []string{"mailing-list account"},
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition:       def,
		VerificationKind: "artifact", VerificationSpec: "newsletter.md", VerificationRequired: true,
	}); err != nil {
		r.add(job, "ceo can define the task", false, err.Error(), "")
		return nil
	}
	r.add(job, "ceo can define the task", true, "", "")

	stored := fx.broker.TaskByID(created.Task.ID)
	persisted := stored != nil && stored.Definition != nil &&
		stored.Definition.Goal == def.Goal &&
		len(stored.Definition.Deliverables) == 2 &&
		len(stored.Definition.SuccessCriteria) == 2 &&
		len(stored.Definition.AccessNeeded) == 1 &&
		strings.TrimSpace(stored.Definition.DefinedAt) != "" &&
		stored.Verification != nil && stored.Verification.Spec == "newsletter.md"
	r.add(job, "definition persists with verification alongside", persisted,
		fmt.Sprintf("definition=%+v verification=%+v", stored.Definition, stored.Verification), "")

	// (a) wire round-trip: marshal the task through the teamTaskWire shadow
	// and back; the definition must survive byte-for-byte under the single
	// snake_case "definition" key.
	blob, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	var roundTripped teamTask
	if err := json.Unmarshal(blob, &roundTripped); err != nil {
		return err
	}
	rt := roundTripped.Definition
	roundTrips := strings.Contains(string(blob), `"definition"`) &&
		strings.Contains(string(blob), `"success_criteria"`) &&
		strings.Contains(string(blob), `"access_needed"`) &&
		rt != nil && rt.Goal == def.Goal &&
		len(rt.Deliverables) == 2 && rt.Deliverables[0].Format == "markdown in the wiki" &&
		len(rt.SuccessCriteria) == 2 && rt.DefinedAt == stored.Definition.DefinedAt
	r.add(job, "definition round-trips the teamTask wire", roundTrips,
		fmt.Sprintf("roundTripped=%+v", rt), "")

	// (b) the execution packet leads with the contract: goal, deliverable
	// format, success criteria, and access all reach the owner.
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *stored, "Task assigned to you.")
	carried := strings.Contains(packet, def.Goal) &&
		strings.Contains(packet, "markdown in the wiki") &&
		strings.Contains(packet, "Draft approved by the human before sending") &&
		strings.Contains(packet, "mailing-list account")
	r.add(job, "execution packet carries goal + deliverable format + success criteria", carried,
		fmt.Sprintf("packet=%d chars", len(packet)), "")

	// (c) define is CEO/human-scoped: a registered specialist (even the task
	// owner) is rejected with a forbidden steer.
	_, defineErr := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: created.Task.ID, Channel: "general", CreatedBy: "eng",
		Definition: &TaskDefinition{Goal: "specialist rewrite of the contract"},
	})
	var mutationErr *TaskMutationError
	rejected := errors.As(defineErr, &mutationErr) && mutationErr.Kind == TaskMutationForbidden
	after := fx.broker.TaskByID(created.Task.ID)
	r.add(job, "define by a non-CEO specialist is rejected", rejected &&
		after != nil && after.Definition != nil && after.Definition.Goal == def.Goal,
		fmt.Sprintf("err=%v", defineErr), "")
	return nil
}

// evalJobSpecFidelity: the execution packet carries the full task spec, not
// a 512-char stub (U0.2 regression guard).
func evalJobSpecFidelity(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "spec-fidelity"
	marker := "ACCEPTANCE-CRITERION-OMEGA: the export must round-trip JSON numbers above 2^53 as strings."
	details := strings.Repeat("Background paragraph about the data-export feature and its edge cases. ", 40) + marker // ~2.9k chars, marker at the tail
	task, _, err := fx.broker.EnsureTask("general", "Build the data export", details, "eng", "ceo", "")
	if err != nil {
		return err
	}
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(task.ID), "Task assigned to you.")
	r.add(job, "execution packet carries the full spec tail", strings.Contains(packet, marker),
		fmt.Sprintf("details=%d chars, packet=%d chars", len(details), len(packet)), "")
	return nil
}

// evalJobThreadContext: a specialist woken mid-thread sees the whole thread,
// not a 4-message keyhole (U0.2 regression guard).
func evalJobThreadContext(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "thread-context"
	root, err := fx.broker.PostMessage("you", "general", "Kickoff: we need the pricing page rebuilt. MSG-00", nil, "")
	if err != nil {
		return err
	}
	for i := 1; i <= 9; i++ {
		from := "ceo"
		if i%2 == 0 {
			from = "you"
		}
		if _, err := fx.broker.PostMessage(from, "general", fmt.Sprintf("Decision %d about the pricing rebuild. MSG-%02d", i, i), nil, root.ID); err != nil {
			return err
		}
	}
	trigger, err := fx.broker.PostMessage("you", "general", "@eng please pick this up. MSG-10", []string{"eng"}, root.ID)
	if err != nil {
		return err
	}
	packet := fx.launcher.buildMessageWorkPacket(trigger, "eng")
	missing := []string{}
	for i := 0; i <= 9; i++ {
		if !strings.Contains(packet, fmt.Sprintf("MSG-%02d", i)) {
			missing = append(missing, fmt.Sprintf("MSG-%02d", i))
		}
	}
	r.add(job, "specialist packet carries the full 10-message thread", len(missing) == 0,
		fmt.Sprintf("missing=%v", missing), "")
	return nil
}

// evalJobKnowledgeInjection: the compounding-delta probe. A warm office has
// a directly task-relevant learning on record; the work packet for that
// task should carry it. The cold control proves the probe itself does not
// false-positive.
func evalJobKnowledgeInjection(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "knowledge-injection"
	insight := "Acme renewals: always CC the CSM and lead with the usage-growth chart."
	log := fx.broker.TeamLearningLog()
	if log == nil {
		return fmt.Errorf("learning log not wired")
	}
	if _, err := log.Append(context.Background(), LearningRecord{
		Type: "operational", Key: "acme-renewal-email", Insight: insight,
		Confidence: 9, Source: "execution", Trusted: true, Scope: "team", CreatedBy: "eng", CreatedAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("seed learning: %w", err)
	}

	task, _, err := fx.broker.EnsureTask("general", "Draft the Acme renewal email", "Write the renewal email for Acme's Q3 renewal.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(task.ID), "Task assigned to you.")
	r.add(job, "warm: task-relevant learning reaches the work packet", strings.Contains(packet, insight),
		"the office knows the playbook; the packet for the exact matching task must carry it (U2.2 regression guard)", "")

	// Cold control: an unrelated task must NOT receive that learning, or
	// the warm check is measuring spray, not relevance.
	unrelated, _, err := fx.broker.EnsureTask("general", "Fix the CI flake in wiki tests", "The wiki lint test is flaky under -race.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	coldPacket := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(unrelated.ID), "Task assigned to you.")
	r.add(job, "cold control: unrelated task does not receive the learning", !strings.Contains(coldPacket, insight), "", "")
	return nil
}

// evalJobDependencyHandoff: when task B depends on task A, B's execution
// packet after A completes should carry A's outcome — dependency edges must
// move data, not just scheduling.
func evalJobDependencyHandoff(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "dependency-handoff"
	outcome := "FINDING: competitor prices at $49/seat; recommend launching at $39 with annual discount."
	a, _, err := fx.broker.EnsureTask("general", "Research competitor pricing", "Compare competitor pricing tiers.\n"+outcome, "ceo", "ceo", "")
	if err != nil {
		return err
	}
	b, _, err := fx.broker.EnsureTask("general", "Write the pricing page", "Use the research outcome to draft the page.", "eng", "ceo", "", a.ID)
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: a.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		return err
	}
	unblocked := fx.broker.TaskByID(b.ID)
	r.add(job, "dependent unblocks when upstream completes", unblocked != nil && !unblocked.blocked,
		fmt.Sprintf("blocked=%v state=%s", unblocked != nil && unblocked.blocked, unblocked.LifecycleState), "")

	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *unblocked, "Task unblocked.")
	r.add(job, "dependent's packet carries the upstream outcome", strings.Contains(packet, outcome),
		"B depends on A; A finished with a concrete finding; B's packet must contain it (U3.2 regression guard)", "")
	return nil
}

// evalJobTurnJournal: the living task brief (U2.3/U3.3) — what one turn
// tried must reach the next turn's packet, including a teammate's. Extended
// for B4: the packet build deterministically records its injected context
// manifest on the turn, and the settled turn's ledger entry carries it.
func evalJobTurnJournal(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "turn-journal"
	task, _, err := fx.broker.EnsureTask("general", "Stabilize the flaky auth test", "Find and fix the flaky auth test.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	fx.broker.AppendTaskLedgerEntry(task.ID, TaskLedgerEntry{
		Agent: "eng", Outcome: "turn timed out after 20m",
		Said:    "Reproduced the flake: auth_test.go races on the shared fixture. Next: isolate the fixture per test.",
		Actions: []string{"task_updated: noted reproduction steps"},
	})
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(task.ID), "Continue.")
	r.add(job, "next turn's packet carries the task journal",
		strings.Contains(packet, "TASK JOURNAL") && strings.Contains(packet, "isolate the fixture"),
		"turn N+1 must start from what turn N tried, not from amnesia (U2.3/U3.3 regression guard)", "")

	// B4 context transparency: seed a learning that matches this task, then
	// dispatch the owner through the real wake path. The packet build (not
	// the model) records the injected item ids on the turn; the settled
	// turn's ledger entry must carry them.
	llog := fx.broker.TeamLearningLog()
	if llog == nil {
		return fmt.Errorf("learning log not wired")
	}
	seeded, err := llog.Append(context.Background(), LearningRecord{
		Type: "operational", Key: "flaky-auth-fixture", Insight: "Flaky auth test: always isolate the shared fixture per test.",
		Confidence: 8, Source: "execution", Trusted: true, Scope: "team", CreatedBy: "eng", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("seed learning: %w", err)
	}
	// Force the task executable so sendTaskUpdate dispatches (fresh fixture
	// tasks may sit pre-Running; the gate under test is the packet→ledger
	// manifest, not lifecycle admission).
	fx.broker.mu.Lock()
	if t := fx.broker.taskByIDLocked(task.ID); t != nil {
		t.LifecycleState = LifecycleStateRunning
		t.status = "in_progress"
	}
	fx.broker.mu.Unlock()
	woke := make(chan struct{}, 1)
	stub := func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		select {
		case woke <- struct{}{}:
		default:
		}
		// Park until cancelled so the recovery paths never fire; the
		// ledger entry is recorded when the turn settles on cancel.
		<-ctx.Done()
		return ctx.Err()
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	defer headlessCodexRunTurnOverride.Store(prior)
	current := fx.broker.TaskByID(task.ID)
	fx.launcher.sendTaskUpdate(
		notificationTarget{Slug: "eng"},
		officeActionLog{Kind: "task_updated", Actor: "ceo", Channel: current.Channel, RelatedID: task.ID},
		*current,
		"Continue work.",
	)
	select {
	case <-woke:
	case <-time.After(8 * time.Second):
	}
	fx.launcher.stopHeadlessWorkers()
	var contextUsed []string
	if after := fx.broker.TaskByID(task.ID); after != nil {
		for _, entry := range after.Ledger {
			if len(entry.ContextUsed) > 0 {
				contextUsed = entry.ContextUsed
			}
		}
	}
	found := false
	for _, item := range contextUsed {
		if item == "learning:"+seeded.ID {
			found = true
		}
	}
	r.add(job, "packet build records ContextUsed on the ledger entry", found,
		fmt.Sprintf("context_used=%v want learning:%s", contextUsed, seeded.ID), "")
	return nil
}

// evalJobCompoundingLoop: the full moat loop (U4.1 + U2.2) — a verified
// outcome auto-distills into the learning store, and the NEXT similar task's
// packet carries it without any human or agent touching the knowledge layer.
func evalJobCompoundingLoop(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "compounding-loop"
	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Migrate the billing webhooks to the signed-endpoint format",
		Details: "Switch billing webhooks to signed endpoints and confirm delivery.", Owner: "eng", CreatedBy: "ceo",
		VerificationKind: "command", VerificationSpec: "exit 0", VerificationRequired: true,
	})
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: created.Task.ID, Channel: "general", CreatedBy: "eng"}); err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: created.Task.ID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		return err
	}
	// The mutation queues distillation async; run it synchronously here for
	// a deterministic eval (idempotency makes the double-run safe).
	fx.broker.distillCompletedTask(created.Task.ID)

	next, _, err := fx.broker.EnsureTask("general", "Add retry handling to the billing webhooks delivery", "Harden billing webhooks delivery with retries.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(next.ID), "Task assigned to you.")
	r.add(job, "verified outcome compounds into the next similar task's packet",
		strings.Contains(packet, "Verified outcome") && strings.Contains(packet, "billing webhooks"),
		"done(verified) → auto-learning → injected into the next matching task with zero human steps (the moat loop, U4.1+U2.2)", "")
	return nil
}

// evalJobCompletionHook: the B1 deterministic completion hook
// (task_completion_hook.go, core-loop steps 6–7.1). One task journeys
// through all four contracts: (a) a task with a Definition cannot reach
// done without a delivered artifact; (b) reaching done with the artifact
// posts the done-post to the task channel and raises the Inbox notice;
// (c) entity facts for the mentioned entities land in the team knowledge
// graph through the existing fact-log path; (d) reopen re-engages the
// owner through the same wake path a fresh assignment uses.
