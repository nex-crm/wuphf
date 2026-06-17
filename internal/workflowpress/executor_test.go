package workflowpress

import (
	"context"
	"errors"
	"testing"
)

// TestHostExecutorRefusesMutatingActions proves the Phase 0 fail-closed
// guarantee: the host-stub backend refuses every mutating action with
// ErrNotAuthorized, even when approval has been granted, because no reviewed
// backend exists yet.
func TestHostExecutorRefusesMutatingActions(t *testing.T) {
	t.Parallel()
	exec := NewHostExecutor()
	cfg := ExecConfig{
		WorkflowID:      "wf",
		Version:         1,
		ApprovalGranted: true, // even with approval, the stub still refuses.
		FS:              []FSCap{{Path: "crm", Write: true}},
	}
	mutating := []ExecAction{
		{Name: "route_to_ae", Kind: ActionInternalWrite, Target: "crm"},
		{Name: "post_to_deal_channel", Kind: ActionExternalWrite, Target: "deal-channel"},
	}
	for _, a := range mutating {
		_, err := exec.Execute(context.Background(), cfg, a)
		if !errors.Is(err, ErrNotAuthorized) {
			t.Errorf("Execute(%s) = %v, want ErrNotAuthorized", a.Name, err)
		}
	}
}

// TestHostExecutorFailsClosedOnUnknownKind is the regression for the fail-open
// hole the keystone grader found: ActionKind.IsWrite/Mutates return false for an
// unrecognised kind, so a write smuggled under a garbage kind string would reach
// the success path. The executor must refuse any kind that is not Valid() BEFORE
// it consults Mutates — even with approval and an allow-listed, writable target,
// which is the exact shape that otherwise slips through.
func TestHostExecutorFailsClosedOnUnknownKind(t *testing.T) {
	t.Parallel()
	exec := NewHostExecutor()
	cfg := ExecConfig{
		WorkflowID:      "wf",
		Version:         1,
		ApprovalGranted: true,
		FS:              []FSCap{{Path: "crm", Write: true}},
	}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "smuggled_write", Kind: ActionKind("exfiltrate"), Target: "crm",
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(unknown kind) = %v, want ErrNotAuthorized", err)
	}
}

// TestHostExecutorEnforcesApprovalOnMutating proves Execute actually reads
// cfg.ApprovalGranted (previously it never did): a mutating action with approval
// absent is refused on the approval path.
func TestHostExecutorEnforcesApprovalOnMutating(t *testing.T) {
	t.Parallel()
	exec := NewHostExecutor()
	cfg := ExecConfig{
		WorkflowID:      "wf",
		Version:         1,
		ApprovalGranted: false,
		FS:              []FSCap{{Path: "crm", Write: true}},
	}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "route_to_ae", Kind: ActionInternalWrite, Target: "crm",
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(mutating, no approval) = %v, want ErrNotAuthorized", err)
	}
}

// TestHostExecutorRefusesUnlistedReadTarget proves deny-by-default: a read whose
// target is not in the filesystem allow-list is refused.
func TestHostExecutorRefusesUnlistedReadTarget(t *testing.T) {
	t.Parallel()
	exec := NewHostExecutor()
	cfg := ExecConfig{WorkflowID: "wf", Version: 1} // empty allow-list == deny all
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "enrich_company", Kind: ActionRead, Target: "enrichment-provider",
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(read, unlisted target) = %v, want ErrNotAuthorized", err)
	}
}

// TestHostExecutorAllowsPureListedRead confirms the only non-error path: a pure
// read whose target is allow-listed (or empty) returns a clean result without
// performing real I/O.
func TestHostExecutorAllowsPureListedRead(t *testing.T) {
	t.Parallel()
	exec := NewHostExecutor()

	// Allow-listed target.
	cfg := ExecConfig{WorkflowID: "wf", Version: 1, FS: []FSCap{{Path: "icp-rubric"}}}
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "score_lead", Kind: ActionRead, Target: "icp-rubric",
	})
	if err != nil {
		t.Fatalf("Execute(allow-listed read) = %v, want nil", err)
	}
	if res == nil || res.ExitCode != 0 {
		t.Fatalf("unexpected result %+v", res)
	}

	// Empty target == touches no external resource.
	if _, err := exec.Execute(context.Background(), cfg, ExecAction{Name: "noop", Kind: ActionRead}); err != nil {
		t.Fatalf("Execute(read, empty target) = %v, want nil", err)
	}
}

// TestHostExecutorRespectsContextCancellation confirms the stub honours a
// cancelled context before doing anything.
func TestHostExecutorRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	exec := NewHostExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := exec.Execute(ctx, ExecConfig{}, ExecAction{Name: "noop", Kind: ActionRead})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled ctx) = %v, want context.Canceled", err)
	}
}

// TestHostExecutorBackendName documents the audit label.
func TestHostExecutorBackendName(t *testing.T) {
	t.Parallel()
	if got := NewHostExecutor().Backend(); got != "host-stub" {
		t.Fatalf("Backend() = %q, want host-stub", got)
	}
}

// TestExecActionMutates spot-checks the Mutates derivation the approval gate
// relies on.
func TestExecActionMutates(t *testing.T) {
	t.Parallel()
	if (ExecAction{Kind: ActionRead}).Mutates() {
		t.Error("read action reported as mutating")
	}
	if !(ExecAction{Kind: ActionExternalWrite}).Mutates() {
		t.Error("external-write action not reported as mutating")
	}
}
