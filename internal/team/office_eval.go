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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/embedding"
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
func evalJobCompletionHook(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "completion-hook"
	// Wire the entity fact log + cross-entity graph onto the fixture broker.
	// Production wiring rides ensureEntitySynthesizer; the eval skips the
	// LLM synthesizer — the hook writes facts directly.
	fx.broker.mu.Lock()
	worker := fx.broker.wikiWorker
	fx.broker.factLog = NewFactLog(worker)
	fx.broker.entityGraph = NewEntityGraph(worker)
	factLog := fx.broker.factLog
	fx.broker.mu.Unlock()

	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Close the Acme Corp renewal",
		Details: "Coordinate with @eng on the renewal brief for Acme Corp.",
		Owner:   "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	taskID := created.Task.ID
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: taskID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Secure a 12-month renewal from Acme Corp at current seat count",
			Deliverables:    []TaskDeliverable{{Name: "renewal brief", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"Renewal brief published to the wiki"},
		},
	}); err != nil {
		return err
	}

	// (a) Whichever action would land the task in done must be blocked while
	// no artifact is on record. complete may route through review first
	// depending on the task's review template, so drive the path generically.
	finish := func(artifactPath string) error {
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "complete", ID: taskID, Channel: "general", CreatedBy: "eng",
			ArtifactPath: artifactPath,
		}); err != nil {
			return err
		}
		if cur := fx.broker.TaskByID(taskID); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.status), "done") {
			_, err := fx.broker.MutateTask(TaskPostRequest{
				Action: "approve", ID: taskID, Channel: "general", CreatedBy: "ceo",
				ArtifactPath: artifactPath,
			})
			return err
		}
		return nil
	}
	gateErr := finish("")
	var mutationErr *TaskMutationError
	notDone := fx.broker.TaskByID(taskID) != nil && !strings.EqualFold(strings.TrimSpace(fx.broker.TaskByID(taskID).status), "done")
	r.add(job, "defined task cannot reach done without an artifact",
		errors.As(gateErr, &mutationErr) && mutationErr.Kind == TaskMutationArtifactRequired &&
			strings.Contains(mutationErr.Message, "artifact_path") && notDone,
		fmt.Sprintf("err=%v", gateErr), "")

	// (b) Same finishing action with artifact_path lands done, posts the
	// done-post to the task channel, and raises the non-blocking Inbox notice.
	const artifact = "team/playbooks/acme-renewal.md"
	if err := finish(artifact); err != nil {
		r.add(job, "defined task reaches done once artifact_path is passed", false, err.Error(), "")
		return nil
	}
	done := fx.broker.TaskByID(taskID)
	r.add(job, "defined task reaches done once artifact_path is passed",
		done != nil && strings.EqualFold(strings.TrimSpace(done.status), "done") && done.Artifact == artifact,
		fmt.Sprintf("status=%q artifact=%q", strings.TrimSpace(done.status), done.Artifact), "")

	fx.broker.mu.Lock()
	donePost := ""
	for _, msg := range fx.broker.messages {
		if msg.Kind == taskDeliveredMessageKind && msg.SourceTaskID == taskID {
			donePost = msg.Content
		}
	}
	noticeFound := false
	for _, req := range fx.broker.requests {
		if req.Kind == "notice" && strings.TrimSpace(req.IssueID) == taskID && requestIsActive(req) && !req.Blocking {
			noticeFound = true
		}
	}
	fx.broker.mu.Unlock()
	r.add(job, "done-post lands in the task channel with summary + artifact link",
		strings.Contains(donePost, "delivered:") && strings.Contains(donePost, "Renewal brief published to the wiki") && strings.Contains(donePost, artifact),
		fmt.Sprintf("post=%q", donePost), "")
	r.add(job, "done raises a non-blocking Inbox notice", noticeFound, "", "")

	// (c) Entity facts: the task mentions two entities (@eng, "Acme Corp").
	// The distillation goroutine records them via the existing fact-log
	// path. MutateTask already queued it; run it synchronously too
	// (idempotent + single-flight) and poll for the async wiki commits.
	fx.broker.distillCompletedTask(taskID)
	waitForFacts := func(kind EntityKind, slug string) []Fact {
		deadline := time.Now().Add(10 * time.Second)
		for {
			facts, _ := factLog.List(kind, slug)
			if len(facts) > 0 || time.Now().After(deadline) {
				return facts
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	companyFacts := waitForFacts(EntityKindCompanies, "acme-corp")
	peopleFacts := waitForFacts(EntityKindPeople, "eng")
	factsCarryTask := len(companyFacts) > 0 && len(peopleFacts) > 0 &&
		strings.Contains(companyFacts[0].Text, taskID) && strings.Contains(companyFacts[0].Text, artifact) &&
		strings.Contains(peopleFacts[0].Text, taskID)
	r.add(job, "entity facts for both mentioned entities reach the team KG",
		factsCarryTask,
		fmt.Sprintf("companies/acme-corp=%d people/eng=%d", len(companyFacts), len(peopleFacts)), "")

	// (d) Reopen re-engages the owner: the task lands back in an executable
	// lifecycle state (the exact gate sendTaskUpdate dispatches on) and the
	// owner's headless turn is enqueued through the same wake path a fresh
	// assignment uses. The run-turn override captures the dispatched turn.
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "reopen", ID: taskID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		r.add(job, "reopen re-enqueues the owner's headless turn", false, err.Error(), "")
		return nil
	}
	reopened := fx.broker.TaskByID(taskID)
	executable := reopened != nil && isExecutableTeamTaskStatus(reopened.LifecycleState) &&
		strings.EqualFold(strings.TrimSpace(reopened.status), "in_progress")
	woke := make(chan string, 1)
	stub := func(_ *Launcher, _ context.Context, slug, notification string, _ ...string) error {
		select {
		case woke <- slug + "\n" + notification:
		default:
		}
		return nil
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	defer headlessCodexRunTurnOverride.Store(prior)
	fx.launcher.sendTaskUpdate(
		notificationTarget{Slug: "eng"},
		officeActionLog{Kind: "task_updated", Actor: "ceo", Channel: reopened.Channel, RelatedID: taskID},
		*reopened,
		"Task reopened — resume work.",
	)
	dispatched := ""
	select {
	case dispatched = <-woke:
	case <-time.After(8 * time.Second):
	}
	fx.launcher.stopHeadlessWorkers()
	r.add(job, "reopen re-enqueues the owner's headless turn",
		executable && strings.HasPrefix(dispatched, "eng\n") && strings.Contains(dispatched, taskID),
		fmt.Sprintf("lifecycle=%s status=%q dispatched=%d chars", reopened.LifecycleState, strings.TrimSpace(reopened.status), len(dispatched)), "")
	return nil
}

// evalJobHumanSovereignty: the human's "no" is sovereign and audible
// (core-loop grader fix family #1; ICP-eval v2 observations [00:55],
// [01:04], [01:06]). One task journeys through the three mechanisms:
//
//	(a) feedback-in-packet — the human's request-changes TEXT renders
//	    verbatim in the owner's next execution packet AND in the wake
//	    notification content, not as a bare "changes requested" flag
//	    ([00:55]: "The feedback isn't visible in the packet").
//	(b) open objection hard-blocks terminal transitions — while the
//	    human's objection stands, approve/complete by ANY agent (the
//	    lead included, on both the team_task path and the
//	    /tasks/{id}/decision path) is refused with an error naming the
//	    objection; a HUMAN approve clears it and lands the task
//	    ([01:04]: "CEO self-approves over a human rejection").
//	(c) reopen symmetric with close — the lead reopens the closed task
//	    and the owner is re-enqueued through the same wake path a fresh
//	    assignment uses ([01:06]: the CEO could close over an objection
//	    in one message but believed it could not reopen).
func evalJobHumanSovereignty(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "human-sovereignty"
	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Draft the renewal one-pager",
		Details: "Draft the renewal one-pager for the Q4 account review.",
		Owner:   "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	taskID := created.Task.ID
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "submit_for_review", ID: taskID, Channel: "general", CreatedBy: "eng",
		Details: "First draft attached.",
	}); err != nil {
		return err
	}

	// (a) Human bounces the work with concrete feedback.
	const feedback = "Use Dana as the champion, not a fabricated contact, and rebuild the Corti sequence escalation-first."
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "request_changes", ID: taskID, Channel: "general",
		Details: feedback, CreatedBy: "human",
	}); err != nil {
		return err
	}
	bounced := fx.broker.TaskByID(taskID)
	if bounced == nil {
		return fmt.Errorf("human-sovereignty: task %s vanished after request_changes", taskID)
	}
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng",
		officeActionLog{Kind: "task_updated", Actor: "human"}, *bounced, "Revise per feedback.")
	r.add(job, "request-changes feedback renders verbatim in the owner's execution packet",
		strings.Contains(packet, "CHANGES REQUESTED by @human") && strings.Contains(packet, feedback),
		fmt.Sprintf("packet=%d chars", len(packet)), "")
	wake := fx.launcher.notifyCtx().TaskNotificationContent(
		officeActionLog{Kind: "task_updated", Actor: "human"}, *bounced)
	r.add(job, "request-changes feedback renders in the wake notification content",
		strings.Contains(wake, "CHANGES REQUESTED by @human") && strings.Contains(wake, feedback),
		fmt.Sprintf("wake=%d chars", len(wake)), "")

	// (b) Every agent path to a terminal transition is refused while the
	// human's objection is open — the error names the objection.
	_, ceoApproveErr := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: taskID, Channel: "general", CreatedBy: "ceo"})
	var mutationErr *TaskMutationError
	ceoBlocked := errors.As(ceoApproveErr, &mutationErr) && mutationErr.Kind == TaskMutationForbidden &&
		strings.Contains(mutationErr.Message, "@human") && strings.Contains(mutationErr.Message, "Dana")
	r.add(job, "lead approve is blocked while the human objection is open", ceoBlocked,
		fmt.Sprintf("err=%v", ceoApproveErr), "")
	_, ownerCompleteErr := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: taskID, Channel: "general", CreatedBy: "eng"})
	ownerBlocked := errors.As(ownerCompleteErr, &mutationErr) && mutationErr.Kind == TaskMutationForbidden
	r.add(job, "owner complete is blocked while the human objection is open", ownerBlocked,
		fmt.Sprintf("err=%v", ownerCompleteErr), "")
	decisionErr := fx.broker.RecordTaskDecision(taskID, "approve", "ceo")
	notDone := fx.broker.TaskByID(taskID) != nil && !strings.EqualFold(strings.TrimSpace(fx.broker.TaskByID(taskID).status), "done")
	r.add(job, "decision-endpoint agent approve is blocked while the human objection is open",
		errors.Is(decisionErr, ErrHumanObjectionOpen) && notDone,
		fmt.Sprintf("err=%v", decisionErr), "")

	// A HUMAN approve clears the objection and lands the task.
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: taskID, Channel: "general", CreatedBy: "human"}); err != nil {
		r.add(job, "human approve clears the objection and lands the task", false, err.Error(), "")
		return nil
	}
	done := fx.broker.TaskByID(taskID)
	r.add(job, "human approve clears the objection and lands the task",
		done != nil && strings.EqualFold(strings.TrimSpace(done.status), "done") &&
			done.HumanObjection == nil && done.ChangesRequested == nil,
		fmt.Sprintf("status=%q objection=%v changes=%v", strings.TrimSpace(done.status), done.HumanObjection, done.ChangesRequested), "")

	// (c) The lead reopens the closed task; the owner is re-enqueued
	// through the same wake path a fresh assignment uses (B1 seam).
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "reopen", ID: taskID, Channel: "general", CreatedBy: "ceo"}); err != nil {
		r.add(job, "lead reopen re-engages the owner", false, err.Error(), "")
		return nil
	}
	reopened := fx.broker.TaskByID(taskID)
	executable := reopened != nil && isExecutableTeamTaskStatus(reopened.LifecycleState) &&
		strings.EqualFold(strings.TrimSpace(reopened.status), "in_progress")
	woke := make(chan string, 1)
	stub := func(_ *Launcher, _ context.Context, slug, notification string, _ ...string) error {
		select {
		case woke <- slug + "\n" + notification:
		default:
		}
		return nil
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	defer headlessCodexRunTurnOverride.Store(prior)
	fx.launcher.sendTaskUpdate(
		notificationTarget{Slug: "eng"},
		officeActionLog{Kind: "task_updated", Actor: "ceo", Channel: reopened.Channel, RelatedID: taskID},
		*reopened,
		"Task reopened — resume work.",
	)
	dispatched := ""
	select {
	case dispatched = <-woke:
	case <-time.After(8 * time.Second):
	}
	fx.launcher.stopHeadlessWorkers()
	r.add(job, "lead reopen re-engages the owner", executable &&
		strings.HasPrefix(dispatched, "eng\n") && strings.Contains(dispatched, taskID),
		fmt.Sprintf("lifecycle=%s status=%q dispatched=%d chars", reopened.LifecycleState, strings.TrimSpace(reopened.status), len(dispatched)), "")
	return nil
}

// evalJobEntityArticles: the B2 entity wiki articles (entity_article.go,
// core-loop step 7.2). A done task whose facts touch two entities must
// deterministically produce one article per entity at the stable brief
// path, each [[wikilinking]] the other (backlinks via the entity graph),
// each citing its claims with footnotes that reference the source task and
// artifact. A second done task touching the same entity UPDATES the same
// file — no duplicate article, the new fact appended. No LLM anywhere: the
// skeleton is pure template assembly.
func evalJobEntityArticles(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "entity-articles"
	fx.broker.mu.Lock()
	worker := fx.broker.wikiWorker
	fx.broker.factLog = NewFactLog(worker)
	fx.broker.entityGraph = NewEntityGraph(worker)
	fx.broker.mu.Unlock()
	root := worker.Repo().Root()

	// runTask drives one defined task to done (complete → approve when the
	// review template demands it) and runs the distillation trigger
	// synchronously — idempotent + single-flight against the goroutine
	// MutateTask already queued.
	runTask := func(title, details, goal, deliverable, artifact string) (string, error) {
		created, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "create", Channel: "general", Title: title,
			Details: details, Owner: "eng", CreatedBy: "ceo",
		})
		if err != nil {
			return "", err
		}
		id := created.Task.ID
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "define", ID: id, Channel: "general", CreatedBy: "ceo",
			Definition: &TaskDefinition{
				Goal:            goal,
				Deliverables:    []TaskDeliverable{{Name: deliverable, Format: "markdown in the wiki"}},
				SuccessCriteria: []string{deliverable + " published to the wiki"},
			},
		}); err != nil {
			return "", err
		}
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "complete", ID: id, Channel: "general", CreatedBy: "eng", ArtifactPath: artifact,
		}); err != nil {
			return "", err
		}
		if cur := fx.broker.TaskByID(id); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.status), "done") {
			if _, err := fx.broker.MutateTask(TaskPostRequest{
				Action: "approve", ID: id, Channel: "general", CreatedBy: "ceo", ArtifactPath: artifact,
			}); err != nil {
				return "", err
			}
		}
		fx.broker.distillCompletedTask(id)
		return id, nil
	}

	// waitForArticle polls the stable article path until it contains every
	// needle (the async distill goroutine may own the single-flight slot).
	waitForArticle := func(relPath string, needles ...string) string {
		deadline := time.Now().Add(10 * time.Second)
		for {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
			if err == nil {
				body := string(content)
				ok := true
				for _, n := range needles {
					if !strings.Contains(body, n) {
						ok = false
						break
					}
				}
				if ok {
					return body
				}
			}
			if time.Now().After(deadline) {
				if err != nil {
					return ""
				}
				return string(content)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	const (
		companyPath = "team/companies/acme-corp.md"
		peoplePath  = "team/people/eng.md"
		artifact1   = "team/playbooks/acme-renewal.md"
		artifact2   = "team/playbooks/acme-expansion.md"
	)
	task1, err := runTask(
		"Close the Acme Corp renewal",
		"Coordinate with @eng on the renewal brief for Acme Corp.",
		"Secure a 12-month renewal from Acme Corp at current seat count",
		"renewal brief", artifact1,
	)
	if err != nil {
		return err
	}

	// (a) Both entity articles exist at stable wiki paths.
	companyArticle := waitForArticle(companyPath, task1)
	peopleArticle := waitForArticle(peoplePath, task1)
	r.add(job, "done task writes one article per touched entity at the stable path",
		companyArticle != "" && peopleArticle != "",
		fmt.Sprintf("%s=%dB %s=%dB", companyPath, len(companyArticle), peoplePath, len(peopleArticle)), "")

	// (b) Each article wikilinks the other — backlinks render on BOTH sides
	// from the entity graph's edges.
	r.add(job, "articles wikilink each other in both directions",
		strings.Contains(companyArticle, "[[people/eng]]") && strings.Contains(peopleArticle, "[[companies/acme-corp]]"),
		"", "")

	// (c) Claims carry footnote citations referencing the source task +
	// artifact: a [^n] marker in the body and a footnote definition that
	// names the task and links the artifact.
	cited := strings.Contains(companyArticle, "[^1]") &&
		strings.Contains(companyArticle, "## References") &&
		strings.Contains(companyArticle, "Task "+task1) &&
		strings.Contains(companyArticle, "["+artifact1+"]("+artifact1+")")
	r.add(job, "every fact-sourced claim carries a footnote citation to its task/artifact", cited, "", "")

	// (d) A second done task touching the same entities UPDATES the same
	// article file — no duplicate — and appends the new fact.
	task2, err := runTask(
		"Draft the Acme Corp expansion proposal",
		"Work with @eng on the expansion sizing for Acme Corp.",
		"Draft the Acme Corp expansion proposal for the spring renewal",
		"expansion proposal", artifact2,
	)
	if err != nil {
		return err
	}
	updated := waitForArticle(companyPath, task2)
	entries, _ := os.ReadDir(filepath.Join(root, "team", "companies"))
	companyFiles := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "acme-corp") && strings.HasSuffix(e.Name(), ".md") {
			companyFiles++
		}
	}
	r.add(job, "regeneration updates the same article in place — no duplicate, new fact appended",
		strings.Contains(updated, task1) && strings.Contains(updated, task2) &&
			companyFiles == 1 && strings.Count(updated, "\n# ") == 1 &&
			strings.Contains(updated, "[^2]"),
		fmt.Sprintf("files=%d len=%dB", companyFiles, len(updated)), "")
	return nil
}

// evalPlaybookFastPathProvider is the deterministic scanner provider for the
// playbook-compilation eval: it promotes ONLY articles carrying explicit
// Anthropic skill frontmatter (the same fast path defaultLLMProvider has)
// and classifies everything else as not-a-skill. No LLM call ever happens,
// so the eval is hermetic.
type evalPlaybookFastPathProvider struct{}

func (evalPlaybookFastPathProvider) AskIsSkill(_ context.Context, _, articleContent, _ string) (bool, SkillFrontmatter, string, string, error) {
	if fm, body, parseErr := ParseSkillMarkdown([]byte(articleContent)); parseErr == nil {
		return true, fm, body, "", nil
	}
	// No explicit skill frontmatter → not a skill. The parse error is
	// intentionally not propagated: it is the classification signal, the
	// same contract as defaultLLMProvider's fast path.
	return false, SkillFrontmatter{}, "", "", nil
}

// evalJobPlaybookCompilation: the B3 loop (playbook_draft.go +
// policy_compile.go, core-loop steps 7.2/7.3/8/11). (a) A verified-done
// defined task with two success criteria and a distilled learning produces
// a draft playbook article at the stable team/playbooks path with task +
// artifact citations; (b) a second similar task UPDATES the same playbook
// (worked example appended, no duplicate file); (c) a playbook with a
// "## Rules" section, run through the compile funnel, yields a skill AND
// atomic policies carrying the skill's agent assignment; (d) duplicate rule
// text does not mint a second policy; (e) an agent's system prompt carries
// its assigned policies and NOT one assigned exclusively to another agent —
// and carries its assigned compiled skill (the step-8 always-loaded check).
func evalJobPlaybookCompilation(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "playbook-compilation"
	fx.broker.mu.Lock()
	worker := fx.broker.wikiWorker
	fx.broker.mu.Unlock()
	root := worker.Repo().Root()

	// runVerifiedTask drives one defined task with a passing machine check
	// to done and runs the distillation trigger synchronously (idempotent +
	// single-flight against the goroutine MutateTask already queued).
	runVerifiedTask := func(title, goal, artifact string, criteria []string) (string, error) {
		created, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "create", Channel: "general", Title: title,
			Details: "Repeatable office workflow.", Owner: "eng", CreatedBy: "ceo",
			VerificationKind: "command", VerificationSpec: "exit 0", VerificationRequired: true,
		})
		if err != nil {
			return "", err
		}
		id := created.Task.ID
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "define", ID: id, Channel: "general", CreatedBy: "ceo",
			Definition: &TaskDefinition{
				Goal:            goal,
				Deliverables:    []TaskDeliverable{{Name: "investor update", Format: "markdown in the wiki"}},
				SuccessCriteria: criteria,
			},
		}); err != nil {
			return "", err
		}
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "complete", ID: id, Channel: "general", CreatedBy: "eng", ArtifactPath: artifact,
		}); err != nil {
			return "", err
		}
		if cur := fx.broker.TaskByID(id); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.status), "done") {
			if _, err := fx.broker.MutateTask(TaskPostRequest{
				Action: "approve", ID: id, Channel: "general", CreatedBy: "ceo", ArtifactPath: artifact,
			}); err != nil {
				return "", err
			}
		}
		fx.broker.distillCompletedTask(id)
		return id, nil
	}

	waitForFile := func(relPath string, needles ...string) string {
		deadline := time.Now().Add(10 * time.Second)
		for {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
			if err == nil {
				body := string(content)
				ok := true
				for _, n := range needles {
					if !strings.Contains(body, n) {
						ok = false
						break
					}
				}
				if ok {
					return body
				}
			}
			if time.Now().After(deadline) {
				if err != nil {
					return ""
				}
				return string(content)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// (a) Verified-done defined task with 2 criteria → draft playbook at the
	// stable path, frontmatter draft: true, citations to task + artifact.
	const (
		playbookRel = "team/playbooks/send-the-weekly-investor-update.md"
		artifact1   = "team/reports/investor-update-week-23.md"
		artifact2   = "team/reports/investor-update-week-24.md"
	)
	task1, err := runVerifiedTask(
		"Send the weekly investor update",
		"Get the weekly investor update out to the approved list",
		artifact1,
		[]string{"Update published to the wiki", "Email sent to the investor list"},
	)
	if err != nil {
		return err
	}
	draft := waitForFile(playbookRel, task1)
	r.add(job, "verified-done defined task drafts a playbook at the stable path",
		strings.Contains(draft, "draft: true") && strings.Contains(draft, playbookDraftMarker) &&
			strings.Contains(draft, "## Checklist") && strings.Contains(draft, "Email sent to the investor list"),
		fmt.Sprintf("%s=%dB", playbookRel, len(draft)), "")
	r.add(job, "draft playbook cites the source task and artifact",
		strings.Contains(draft, "Task "+task1) && strings.Contains(draft, "["+artifact1+"]("+artifact1+")") &&
			strings.Contains(draft, "[^1]"),
		"", "")

	// (b) A second similar task UPDATES the same playbook — worked example
	// appended, no duplicate file.
	task2, err := runVerifiedTask(
		"Send the weekly investor update for week 24",
		"Get the week-24 investor update out to the approved list",
		artifact2,
		[]string{"Update published to the wiki", "Email sent to the investor list"},
	)
	if err != nil {
		return err
	}
	updated := waitForFile(playbookRel, task2)
	entries, _ := os.ReadDir(filepath.Join(root, "team", "playbooks"))
	playbookFiles := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "send-the-weekly-investor-update") && strings.HasSuffix(e.Name(), ".md") {
			playbookFiles++
		}
	}
	r.add(job, "second similar task appends a worked example — no duplicate playbook",
		playbookFiles == 1 && strings.Contains(updated, task1) && strings.Contains(updated, task2) &&
			strings.Contains(updated, "[^2]"),
		fmt.Sprintf("files=%d len=%dB", playbookFiles, len(updated)), "")

	// (c) A playbook with a ## Rules section through the compile funnel
	// yields a skill AND atomic policies with the skill's agent assignment.
	// The fixture scanner promotes explicit-frontmatter articles only — no
	// LLM — mirroring defaultLLMProvider's fast path.
	fx.broker.SetSkillScanner(NewSkillScanner(fx.broker, evalPlaybookFastPathProvider{}, 10))
	const ruleOne = "Always CC the CSM on renewal emails"
	const ruleTwo = "Never send pricing before the demo call"
	playbookWithRules := `---
name: qualify-inbound-leads
description: Qualify inbound leads before routing them to sales.
---
# Qualify inbound leads

## Steps

1. Check the lead against the ICP notes in the wiki.
2. Score the lead and record the score.
3. Route qualified leads to the AE channel.

## Rules

- ` + ruleOne + `
- ` + ruleTwo + `
`
	if _, _, err := worker.Enqueue(context.Background(), ArchivistAuthor,
		"team/playbooks/qualify-inbound-leads.md", playbookWithRules, "replace",
		"fixture: playbook with rules"); err != nil {
		return err
	}
	if _, err := fx.broker.compileWikiSkills(context.Background(), "team/playbooks", false, "manual"); err != nil {
		return fmt.Errorf("compile pass: %w", err)
	}
	skills := fx.broker.ListActiveSkillSummaries()
	var compiledSkill *SkillSummary
	for i := range skills {
		if skills[i].Slug == "qualify-inbound-leads" {
			compiledSkill = &skills[i]
			break
		}
	}
	r.add(job, "compile funnel yields the playbook's skill, roster-assigned",
		compiledSkill != nil && len(compiledSkill.OwnerAgents) == 2,
		fmt.Sprintf("skills=%d", len(skills)), "")

	findPolicy := func(rule string) *officePolicy {
		for _, p := range fx.broker.ListPolicies() {
			if normalizePolicyRuleText(p.Rule) == normalizePolicyRuleText(rule) {
				out := p
				return &out
			}
		}
		return nil
	}
	p1, p2 := findPolicy(ruleOne), findPolicy(ruleTwo)
	r.add(job, "compile funnel yields atomic policies with the skill's agent assignment",
		p1 != nil && p2 != nil &&
			len(p1.Agents) == 2 && policyAppliesToAgent(*p1, "eng") && policyAppliesToAgent(*p1, "ceo"),
		fmt.Sprintf("policies=%d", len(fx.broker.ListPolicies())), "")

	// (d) Duplicate rule text (different casing, second playbook) does not
	// mint a second policy.
	dupPlaybook := `---
name: renewal-email-outreach
description: Send renewal outreach emails to existing customers.
---
# Renewal email outreach

## Steps

1. Pull the renewal list.
2. Draft the email from the template.
3. Send and log the outcome.

## Rules

- ALWAYS CC the CSM on renewal emails
`
	if _, _, err := worker.Enqueue(context.Background(), ArchivistAuthor,
		"team/playbooks/renewal-email-outreach.md", dupPlaybook, "replace",
		"fixture: playbook with duplicate rule"); err != nil {
		return err
	}
	if _, err := fx.broker.compileWikiSkills(context.Background(), "team/playbooks", false, "manual"); err != nil {
		return fmt.Errorf("second compile pass: %w", err)
	}
	dupCount := 0
	for _, p := range fx.broker.ListPolicies() {
		if normalizePolicyRuleText(p.Rule) == normalizePolicyRuleText(ruleOne) {
			dupCount++
		}
	}
	r.add(job, "duplicate rule text does not create a second policy", dupCount == 1,
		fmt.Sprintf("matching policies=%d", dupCount), "")

	// (e) Always-loaded (core-loop step 8): an agent's prompt carries its
	// assigned policies + skills and NOT a policy assigned exclusively to
	// another agent.
	const engOnlyRule = "Run the deploy checklist before every release"
	const ceoOnlyRule = "Review specialist hiring proposals within one day"
	if _, err := fx.broker.RecordPolicyScoped("human_directed", engOnlyRule, []string{"eng"}); err != nil {
		return err
	}
	if _, err := fx.broker.RecordPolicyScoped("human_directed", ceoOnlyRule, []string{"ceo"}); err != nil {
		return err
	}
	engPrompt := fx.launcher.buildPrompt("eng")
	r.add(job, "agent prompt carries its assigned policy and not another agent's",
		strings.Contains(engPrompt, engOnlyRule) && !strings.Contains(engPrompt, ceoOnlyRule) &&
			strings.Contains(engPrompt, ruleOne),
		fmt.Sprintf("prompt=%d chars", len(engPrompt)), "")
	r.add(job, "agent prompt carries its assigned compiled skill (always loaded)",
		strings.Contains(engPrompt, "qualify-inbound-leads"),
		"", "")
	return nil
}

// evalJobNotebookBookends: the B4 deterministic notebook bookends
// (task_notebook_bookends.go, core-loop step 5). (a) The FIRST headless
// turn enqueue for a (agent, defined-task) pair creates the agent's
// pre-task research note at agents/{slug}/notebook/{task-id}.md with the
// Definition and an empty Research section; (b) a single team_wiki_search
// retrieval with the agent's reader identity spans wiki + that agent's OWN
// notebook (permissioned: another reader does not see it); (c) verified
// done appends the post-task section with the artifact link.
func evalJobNotebookBookends(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "notebook-bookends"
	worker := fx.broker.WikiWorker()
	if worker == nil {
		return fmt.Errorf("wiki worker not wired")
	}
	root := worker.Repo().Root()

	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Publish the onboarding teardown brief",
		Details: "Write up the onboarding teardown findings.", Owner: "eng", CreatedBy: "ceo",
		VerificationKind: "command", VerificationSpec: "exit 0", VerificationRequired: true,
	})
	if err != nil {
		return err
	}
	id := created.Task.ID
	const goal = "Ship the onboarding teardown brief to the wiki with one actionable fix list"
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: id, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            goal,
			Deliverables:    []TaskDeliverable{{Name: "teardown brief", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"Brief published to the wiki"},
		},
	}); err != nil {
		return err
	}

	// (a) First headless enqueue for (eng, task) queues the pre-task note.
	// The parked stub keeps the turn pending so no recovery path fires.
	stub := func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		<-ctx.Done()
		return ctx.Err()
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	defer headlessCodexRunTurnOverride.Store(prior)
	fx.launcher.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{
		Prompt:  "Work packet:\n- Task: #" + id,
		Channel: "general",
		TaskID:  id,
	})
	notePath := filepath.Join(root, "agents", "eng", "notebook", id+".md")
	waitForNote := func(needles ...string) string {
		deadline := time.Now().Add(10 * time.Second)
		for {
			content, err := os.ReadFile(notePath)
			if err == nil {
				body := string(content)
				ok := true
				for _, n := range needles {
					if !strings.Contains(body, n) {
						ok = false
						break
					}
				}
				if ok {
					return body
				}
			}
			if time.Now().After(deadline) {
				return string(content)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	pre := waitForNote(goal, "## Definition", "## Research")
	fx.launcher.stopHeadlessWorkers()
	r.add(job, "task start creates the pre-task note with the Definition",
		strings.Contains(pre, goal) && strings.Contains(pre, "## Definition") &&
			strings.Contains(pre, "## Research") && strings.Contains(pre, "## Retrieved context"),
		fmt.Sprintf("note=%dB at agents/eng/notebook/%s.md", len(pre), id), "")

	// (b) One retrieval call spans wiki + the agent's OWN notebook: the
	// /wiki/search surface behind team_wiki_search merges the reader's own
	// shelf, and only theirs.
	searchHits := func(reader string) string {
		target := "/wiki/search?pattern=" + url.QueryEscape("onboarding teardown brief")
		if reader != "" {
			target += "&reader=" + url.QueryEscape(reader)
		}
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		fx.broker.handleWikiSearch(rec, req)
		return rec.Body.String()
	}
	ownBody := searchHits("eng")
	otherBody := searchHits("ceo")
	r.add(job, "wiki search with the agent's reader identity spans its own notebook",
		strings.Contains(ownBody, "agents/eng/notebook/"+id+".md") &&
			!strings.Contains(otherBody, "agents/eng/notebook/"),
		fmt.Sprintf("own=%dB other=%dB", len(ownBody), len(otherBody)), "")

	// (c) Verified done appends the post-task section with the artifact link.
	const artifact = "team/briefs/onboarding-teardown.md"
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "complete", ID: id, Channel: "general", CreatedBy: "eng", ArtifactPath: artifact,
	}); err != nil {
		return err
	}
	if cur := fx.broker.TaskByID(id); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.status), "done") {
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "approve", ID: id, Channel: "general", CreatedBy: "ceo", ArtifactPath: artifact,
		}); err != nil {
			return err
		}
	}
	fx.broker.distillCompletedTask(id)
	post := waitForNote("## Post-task", artifact)
	r.add(job, "verified done appends the post-task section with the artifact link",
		strings.Contains(post, "## Post-task") && strings.Contains(post, "["+artifact+"]("+artifact+")") &&
			strings.Contains(post, "Learnings distilled"),
		fmt.Sprintf("note=%dB", len(post)), "")
	return nil
}

// evalJobHybridRetrieval: the B4 hybrid retrieval spine
// (hybrid_retrieval.go behind the U2 relevantLearnings seam). With no
// embedding provider the lexical behavior is unchanged; with the
// deterministic stub configured, a learning whose wording shares NO
// lexical tokens with the query but is dense-adjacent per the stub's
// vectors ranks in the top-k, lexical hits survive the fusion, and record
// texts are embedded once through the content-hash cache.
func evalJobHybridRetrieval(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "hybrid-retrieval"
	llog := fx.broker.TeamLearningLog()
	if llog == nil {
		return fmt.Errorf("learning log not wired")
	}
	ctx := context.Background()
	// The adjacent learning shares ONLY sub-3-char tokens ("q3", "ai",
	// "ux") with the query — invisible to the lexical tokenizer (length
	// floor 3), visible to the stub's dense vectors (length floor 2).
	adjacent, err := llog.Append(ctx, LearningRecord{
		Type: "operational", Key: "q3-ai-ux", Insight: "q3 ai ux: cap nav depth at two levels",
		Confidence: 8, Source: "execution", Trusted: true, Scope: "team", CreatedBy: "eng", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	lexical, err := llog.Append(ctx, LearningRecord{
		Type: "operational", Key: "acme-renewal-email", Insight: "Acme renewals: always CC the CSM and lead with the usage-growth chart.",
		Confidence: 9, Source: "execution", Trusted: true, Scope: "team", CreatedBy: "eng", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	if _, err := llog.Append(ctx, LearningRecord{
		Type: "operational", Key: "webhook-signing", Insight: "Billing webhooks: rotate signing keys quarterly.",
		Confidence: 7, Source: "execution", Trusted: true, Scope: "team", CreatedBy: "eng", CreatedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	const denseQuery = "Q3 AI UX pass"
	const lexicalQuery = "Draft the Acme renewal email for the Q3 renewals"

	contains := func(results []LearningSearchResult, id string) bool {
		for _, rec := range results {
			if rec.ID == id {
				return true
			}
		}
		return false
	}

	// (a) No provider configured → exact lexical behavior: the dense-only
	// adjacent learning is NOT retrievable, the token-overlap one is.
	// RunOfficeEvals already forces lexical-only for the run.
	coldDense := relevantLearnings(llog, denseQuery, 5)
	coldLexical := relevantLearnings(llog, lexicalQuery, 5)
	r.add(job, "no provider: lexical behavior unchanged (dense-only record absent, lexical hit present)",
		!contains(coldDense, adjacent.ID) && contains(coldLexical, lexical.ID),
		fmt.Sprintf("dense-query results=%d lexical-query results=%d", len(coldDense), len(coldLexical)), "")

	// (b) Stub provider + scratch content-hash cache configured.
	cache := embedding.NewCache(filepath.Join(fx.scratchDir, "embeddings.jsonl"))
	setRetrievalEmbedding(embedding.NewStubProvider(), cache)
	// Restore the run-wide lexical forcing when this job ends.
	defer setRetrievalEmbedding(nil, nil)

	warm := relevantLearnings(llog, denseQuery, 5)
	r.add(job, "stub provider: zero-token-overlap semantically-adjacent learning ranks in top-k",
		contains(warm, adjacent.ID),
		fmt.Sprintf("results=%d want id=%s", len(warm), adjacent.ID), "")
	stillLexical := relevantLearnings(llog, lexicalQuery, 5)
	r.add(job, "stub provider: lexical hits survive RRF fusion",
		contains(stillLexical, lexical.ID), "", "")

	// (c) Record texts embed once through the content-hash cache: the
	// second identical retrieval adds no new cache rows.
	entriesAfterFirst := cache.Stats().Entries
	_ = relevantLearnings(llog, denseQuery, 5)
	entriesAfterSecond := cache.Stats().Entries
	r.add(job, "embeddings are cached by content hash (no re-embedding unchanged texts)",
		entriesAfterFirst > 0 && entriesAfterSecond == entriesAfterFirst,
		fmt.Sprintf("entries first=%d second=%d", entriesAfterFirst, entriesAfterSecond), "")

	// (d) End to end: the work packet for the dense-only task carries the
	// adjacent insight when the provider is configured.
	task, _, err := fx.broker.EnsureTask("general", denseQuery, "", "eng", "ceo", "")
	if err != nil {
		return err
	}
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(task.ID), "Task assigned to you.")
	r.add(job, "hybrid: dense-adjacent learning reaches the work packet",
		strings.Contains(packet, "cap nav depth at two levels"),
		fmt.Sprintf("packet=%d chars", len(packet)), "")
	return nil
}

// evalJobGrounding: ground execution in retrieval, forbid fabrication
// (core-loop grader fix family #2; ICP-eval v2 [00:00], [00:10], [00:47],
// [00:50]). Four contracts:
//
//	(a) full-fidelity titles — a long money-bearing title renders its full
//	    amount in the owner's packet and wake content (the live $61k
//	    account was briefed as $6k off a display-clipped title);
//	(b) mandatory task-start retrieval — a task whose details mention an
//	    entity with an existing wiki article gets a RETRIEVED CONTEXT
//	    block listing that article (title + path), and the wiki hit rides
//	    the context manifest; a task with no wiki hits carries the
//	    explicit "(searched the wiki for: … — no hits)" line so a false
//	    "no data" claim is impossible to make honestly;
//	(c) stop-order backstop — a human message posted into a running
//	    task's channel leads the owner's next packet ("HUMAN POSTED WHILE
//	    YOU WORKED"); a leading "stop" blocks complete until a packet
//	    build consumed the note, after which complete succeeds. Non-halt
//	    notes ride the packet without blocking.
//	(d) the human_note_pending wire shape is additive and round-trips.
func evalJobGrounding(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "grounding"

	// (a) Long money-bearing title: the amount sits past the old 120-char
	// display clip; the owner's packet and wake content must carry it.
	moneyTitle := "Renew the Corti Labs contract before the Q4 board review and make sure the two unresolved support escalations are handled escalation-first in every touch ($61,000 ARR at risk)"
	moneyTask, _, err := fx.broker.EnsureTask("general", moneyTitle, "", "eng", "ceo", "")
	if err != nil {
		return err
	}
	moneyPacket := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(moneyTask.ID), "Task assigned to you.")
	r.add(job, "owner packet carries the full money-bearing title", strings.Contains(moneyPacket, "$61,000"),
		fmt.Sprintf("title=%d chars packet=%d chars", len(moneyTitle), len(moneyPacket)), "")
	moneyWake := fx.launcher.notifyCtx().TaskNotificationContent(officeActionLog{Kind: "task_updated", Actor: "ceo"}, *fx.broker.TaskByID(moneyTask.ID))
	r.add(job, "wake notification carries the full money-bearing title", strings.Contains(moneyWake, "$61,000"), "", "")

	// (b) Mandatory retrieval: an approved wiki article about the entity
	// must surface in the packet's RETRIEVED CONTEXT block by title + path.
	worker := fx.broker.WikiWorker()
	if worker == nil {
		return fmt.Errorf("wiki worker not wired")
	}
	const acmePath = "team/accounts/acme-corp.md"
	acmeArticle := "# Acme Corp — renewal brief\n\nRED risk. Owner on record: Dana Whitfield. $48k ARR, renewal Q3.\n"
	if _, _, err := worker.Enqueue(context.Background(), ArchivistAuthor, acmePath, acmeArticle, "replace", "fixture: acme account brief"); err != nil {
		return err
	}
	acmeTask, _, err := fx.broker.EnsureTask("general", "Prepare the Acme Corp QBR one-pager",
		"Build the QBR one-pager for Acme Corp using our account briefs and the renewal playbook.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	acmePacket, acmeContext := fx.launcher.notifyCtx().BuildTaskExecutionPacketWithContext("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(acmeTask.ID), "Task assigned to you.")
	carriesHit := strings.Contains(acmePacket, "RETRIEVED CONTEXT") &&
		strings.Contains(acmePacket, "wiki:"+acmePath) &&
		strings.Contains(acmePacket, "Acme Corp — renewal brief")
	r.add(job, "packet's RETRIEVED CONTEXT lists the existing entity article (title + path)", carriesHit,
		fmt.Sprintf("packet=%d chars", len(acmePacket)), "")
	hitOnManifest := false
	for _, item := range acmeContext {
		if item == "wiki:"+acmePath {
			hitOnManifest = true
		}
	}
	r.add(job, "wiki hit rides the context_used manifest", hitOnManifest, fmt.Sprintf("context_used=%v", acmeContext), "")

	noHitTask, _, err := fx.broker.EnsureTask("general", "Tune the quokka zephyr cadence experiment",
		"Calibrate the quokka zephyr cadence rig.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	noHitPacket := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(noHitTask.ID), "Task assigned to you.")
	r.add(job, "no wiki hits → packet carries the explicit searched-no-hits line",
		strings.Contains(noHitPacket, "(searched the wiki for:") && strings.Contains(noHitPacket, "no hits"),
		fmt.Sprintf("packet=%d chars", len(noHitPacket)), "")

	// (c) Stop-order backstop. Force the task running, post a human "stop"
	// into its channel, and walk the gate: complete blocked → packet leads
	// with the note (consuming it) → complete succeeds.
	stopTask, _, err := fx.broker.EnsureTask("general", "Draft the renewal outreach sequence", "Draft the outreach sequence for the renewal book.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	// EnsureTask mints a per-task channel for business-objective work; the
	// human's stop order lands in THAT channel, like the live run.
	stopChannel := normalizeChannelSlug(stopTask.Channel)
	if stopChannel == "" {
		stopChannel = "general"
	}
	fx.broker.mu.Lock()
	if t := fx.broker.taskByIDLocked(stopTask.ID); t != nil {
		t.LifecycleState = LifecycleStateRunning
		t.status = "in_progress"
	}
	fx.broker.mu.Unlock()
	const stopOrder = "Stop — do not build a placeholder. Read team/accounts/acme-corp.md first; Dana Whitfield is the owner on record."
	if _, err := fx.broker.PostMessage("you", stopChannel, stopOrder, nil, ""); err != nil {
		return err
	}
	marked := fx.broker.TaskByID(stopTask.ID)
	r.add(job, "human message in a running task's channel arms human_note_pending with halt",
		marked != nil && marked.HumanNotePending != nil && marked.HumanNotePending.Halt &&
			strings.Contains(marked.HumanNotePending.Body, "Dana Whitfield"),
		fmt.Sprintf("note=%+v", marked.HumanNotePending), "")

	_, blockedErr := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: stopTask.ID, Channel: "general", CreatedBy: "eng"})
	var mutationErr *TaskMutationError
	r.add(job, "leading stop blocks agent complete until a packet consumed the note",
		errors.As(blockedErr, &mutationErr) && mutationErr.Kind == TaskMutationForbidden &&
			strings.Contains(mutationErr.Message, "stop order"),
		fmt.Sprintf("err=%v", blockedErr), "")

	stopPacket := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, *fx.broker.TaskByID(stopTask.ID), "Continue.")
	r.add(job, "owner's next packet leads with HUMAN POSTED WHILE YOU WORKED",
		strings.HasPrefix(stopPacket, "HUMAN POSTED WHILE YOU WORKED") && strings.Contains(stopPacket, stopOrder),
		fmt.Sprintf("packet head=%q", truncate(stopPacket, 120)), "")
	consumed := fx.broker.TaskByID(stopTask.ID)
	r.add(job, "packet build consumes the note", consumed != nil && consumed.HumanNotePending == nil, "", "")
	_, completeErr := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: stopTask.ID, Channel: "general", CreatedBy: "eng"})
	r.add(job, "complete succeeds once a packet consumed the note", completeErr == nil, fmt.Sprintf("err=%v", completeErr), "")

	// Non-halt note: rides the next packet's top but never blocks.
	fyiTask, _, err := fx.broker.EnsureTask("general", "Assemble the Brightline expansion brief", "Assemble the expansion brief for Brightline.", "eng", "ceo", "")
	if err != nil {
		return err
	}
	fyiChannel := normalizeChannelSlug(fyiTask.Channel)
	if fyiChannel == "" {
		fyiChannel = "general"
	}
	fx.broker.mu.Lock()
	if t := fx.broker.taskByIDLocked(fyiTask.ID); t != nil {
		t.LifecycleState = LifecycleStateRunning
		t.status = "in_progress"
	}
	fx.broker.mu.Unlock()
	if _, err := fx.broker.PostMessage("you", fyiChannel, "FYI the Brightline seat count changed to 240 this morning.", nil, ""); err != nil {
		return err
	}
	_, fyiCompleteErr := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: fyiTask.ID, Channel: "general", CreatedBy: "eng"})
	r.add(job, "non-halt human note never blocks complete", fyiCompleteErr == nil, fmt.Sprintf("err=%v", fyiCompleteErr), "")

	// (d) Wire round-trip: human_note_pending survives the teamTaskWire
	// shadow under its additive snake_case key.
	wireTask := teamTask{ID: "task-wire", Title: "wire probe", HumanNotePending: &TaskHumanNote{From: "human", Body: "stop the line", At: "2026-06-10T00:00:00Z", Halt: true}}
	blob, err := json.Marshal(wireTask)
	if err != nil {
		return err
	}
	var roundTripped teamTask
	if err := json.Unmarshal(blob, &roundTripped); err != nil {
		return err
	}
	rt := roundTripped.HumanNotePending
	r.add(job, "human_note_pending round-trips the teamTask wire",
		strings.Contains(string(blob), `"human_note_pending"`) && rt != nil && rt.Halt && rt.Body == "stop the line" && rt.From == "human",
		fmt.Sprintf("blob=%s", truncate(string(blob), 200)), "")
	return nil
}
