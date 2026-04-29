package team

// scheduler_workflow_test.go covers processWorkflowJob via the
// resolveWorkflowProvider seam. The seam lets us drive the
// lookup-failure / cancel / execute-failure / execute-success
// branches without spinning up a real action.Registry. Includes the
// regression for context.Canceled mid-execute — pre-fix, that path
// recorded the workflow as "failed" via UpdateSkillExecutionByWorkflowKey,
// corrupting persisted skill state on graceful shutdown.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

// stubWorkflowProvider satisfies action.Provider with no-op defaults
// for every method except Name and ExecuteWorkflow, which are wired
// into the test fixtures. Other methods panic if called so a future
// scheduler change that starts using them surfaces loudly.
type stubWorkflowProvider struct {
	name    string
	execute func(ctx context.Context, req action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error)
}

func (p *stubWorkflowProvider) Name() string     { return p.name }
func (p *stubWorkflowProvider) Configured() bool { return true }
func (p *stubWorkflowProvider) Supports(action.Capability) bool {
	return true
}
func (p *stubWorkflowProvider) ExecuteWorkflow(ctx context.Context, req action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
	if p.execute == nil {
		return action.WorkflowExecuteResult{}, errors.New("test stub: execute not configured")
	}
	return p.execute(ctx, req)
}

func (p *stubWorkflowProvider) Guide(context.Context, string) (action.GuideResult, error) {
	panic("test stub: Guide unexpectedly called")
}
func (p *stubWorkflowProvider) ListConnections(context.Context, action.ListConnectionsOptions) (action.ConnectionsResult, error) {
	panic("test stub: ListConnections unexpectedly called")
}
func (p *stubWorkflowProvider) SearchActions(context.Context, string, string, string) (action.ActionSearchResult, error) {
	panic("test stub: SearchActions unexpectedly called")
}
func (p *stubWorkflowProvider) ActionKnowledge(context.Context, string, string) (action.KnowledgeResult, error) {
	panic("test stub: ActionKnowledge unexpectedly called")
}
func (p *stubWorkflowProvider) ExecuteAction(context.Context, action.ExecuteRequest) (action.ExecuteResult, error) {
	panic("test stub: ExecuteAction unexpectedly called")
}
func (p *stubWorkflowProvider) CreateWorkflow(context.Context, action.WorkflowCreateRequest) (action.WorkflowCreateResult, error) {
	panic("test stub: CreateWorkflow unexpectedly called")
}
func (p *stubWorkflowProvider) ListWorkflowRuns(context.Context, string) (action.WorkflowRunsResult, error) {
	panic("test stub: ListWorkflowRuns unexpectedly called")
}
func (p *stubWorkflowProvider) ListRelays(context.Context, action.ListRelaysOptions) (action.RelayListResult, error) {
	panic("test stub: ListRelays unexpectedly called")
}
func (p *stubWorkflowProvider) RelayEventTypes(context.Context, string) (action.RelayEventTypesResult, error) {
	panic("test stub: RelayEventTypes unexpectedly called")
}
func (p *stubWorkflowProvider) CreateRelay(context.Context, action.RelayCreateRequest) (action.RelayResult, error) {
	panic("test stub: CreateRelay unexpectedly called")
}
func (p *stubWorkflowProvider) ActivateRelay(context.Context, action.RelayActivateRequest) (action.RelayResult, error) {
	panic("test stub: ActivateRelay unexpectedly called")
}
func (p *stubWorkflowProvider) ListRelayEvents(context.Context, action.RelayEventsOptions) (action.RelayEventsResult, error) {
	panic("test stub: ListRelayEvents unexpectedly called")
}
func (p *stubWorkflowProvider) GetRelayEvent(context.Context, string) (action.RelayEventDetail, error) {
	panic("test stub: GetRelayEvent unexpectedly called")
}

func newWorkflowJobScheduler(t *testing.T, broker schedulerBroker, resolve func(string) (action.Provider, error)) *watchdogScheduler {
	t.Helper()
	return &watchdogScheduler{
		broker:                  broker,
		clock:                   newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)),
		deliverTask:             func(officeActionLog, teamTask) {},
		resolveWorkflowProvider: resolve,
	}
}

func TestSchedulerProcessWorkflowJob_LookupFailureRecordsAndReschedules(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	resolve := func(name string) (action.Provider, error) {
		return nil, errors.New("provider missing")
	}
	s := newWorkflowJobScheduler(t, b, resolve)
	s.processWorkflowJob(schedulerJob{
		Slug:        "wf-1",
		Channel:     "general",
		WorkflowKey: "report",
		Provider:    "composio",
	})
	if len(b.deliveredActions) != 1 || b.deliveredActions[0] != "external_workflow_failed:report" {
		t.Errorf("expected one external_workflow_failed action; got %+v", b.deliveredActions)
	}
	if len(b.skillUpdates) != 1 || b.skillUpdates[0].status != "failed" {
		t.Errorf("expected one failed skill update; got %+v", b.skillUpdates)
	}
	if len(b.jobStateUpdates) != 1 || b.jobStateUpdates[0].slug != "wf-1" {
		t.Errorf("expected one jobStateUpdate for wf-1; got %+v", b.jobStateUpdates)
	}
}

func TestSchedulerProcessWorkflowJob_ExecuteSuccessRecordsAndUpdatesStatus(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	resolve := func(name string) (action.Provider, error) {
		return &stubWorkflowProvider{
			name: "composio",
			execute: func(_ context.Context, _ action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
				return action.WorkflowExecuteResult{Status: "ok"}, nil
			},
		}, nil
	}
	s := newWorkflowJobScheduler(t, b, resolve)
	s.processWorkflowJob(schedulerJob{
		Slug:        "wf-2",
		Channel:     "general",
		WorkflowKey: "report",
		Provider:    "composio",
	})
	if len(b.deliveredActions) != 1 || b.deliveredActions[0] != "external_workflow_executed:report" {
		t.Errorf("expected one external_workflow_executed action; got %+v", b.deliveredActions)
	}
	if len(b.skillUpdates) != 1 || b.skillUpdates[0].status != "ok" {
		t.Errorf("expected skill update with status=ok; got %+v", b.skillUpdates)
	}
}

func TestSchedulerProcessWorkflowJob_ExecuteErrorRecordsFailure(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	resolve := func(name string) (action.Provider, error) {
		return &stubWorkflowProvider{
			name: "composio",
			execute: func(_ context.Context, _ action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
				return action.WorkflowExecuteResult{}, errors.New("provider 500")
			},
		}, nil
	}
	s := newWorkflowJobScheduler(t, b, resolve)
	s.processWorkflowJob(schedulerJob{
		Slug:        "wf-3",
		Channel:     "general",
		WorkflowKey: "report",
		Provider:    "composio",
	})
	if len(b.deliveredActions) != 1 || b.deliveredActions[0] != "external_workflow_failed:report" {
		t.Errorf("expected one external_workflow_failed action; got %+v", b.deliveredActions)
	}
	if len(b.skillUpdates) != 1 || b.skillUpdates[0].status != "failed" {
		t.Errorf("expected skill update with status=failed; got %+v", b.skillUpdates)
	}
}

// Regression: a workflow execute returning context.Canceled (i.e. Stop
// was called mid-execute) must NOT record the workflow as failed. With
// the original cancelable-ctx fix and no Canceled special-case, this
// path would call UpdateSkillExecutionByWorkflowKey(key, "failed") on
// graceful shutdown, corrupting persisted skill state.
func TestSchedulerProcessWorkflowJob_CanceledDoesNotRecordFailure(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	resolve := func(name string) (action.Provider, error) {
		return &stubWorkflowProvider{
			name: "composio",
			execute: func(_ context.Context, _ action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
				return action.WorkflowExecuteResult{}, context.Canceled
			},
		}, nil
	}
	s := newWorkflowJobScheduler(t, b, resolve)
	s.processWorkflowJob(schedulerJob{
		Slug:        "wf-4",
		Channel:     "general",
		WorkflowKey: "report",
		Provider:    "composio",
	})
	if len(b.deliveredActions) != 0 {
		t.Errorf("cancellation must not record any action; got %+v", b.deliveredActions)
	}
	if len(b.skillUpdates) != 0 {
		t.Errorf("cancellation must not update skill state; got %+v", b.skillUpdates)
	}
	if len(b.jobStateUpdates) != 0 {
		t.Errorf("cancellation must not reschedule the job; got %+v", b.jobStateUpdates)
	}
}

// And the same for context.DeadlineExceeded — same shutdown-protection
// rationale; either error means "we never got to run the workflow."
func TestSchedulerProcessWorkflowJob_DeadlineExceededDoesNotRecordFailure(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	resolve := func(name string) (action.Provider, error) {
		return &stubWorkflowProvider{
			name: "composio",
			execute: func(_ context.Context, _ action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
				return action.WorkflowExecuteResult{}, context.DeadlineExceeded
			},
		}, nil
	}
	s := newWorkflowJobScheduler(t, b, resolve)
	s.processWorkflowJob(schedulerJob{
		Slug:        "wf-5",
		Channel:     "general",
		WorkflowKey: "report",
		Provider:    "composio",
	})
	if len(b.deliveredActions) != 0 || len(b.skillUpdates) != 0 || len(b.jobStateUpdates) != 0 {
		t.Errorf("deadline exceeded must not produce side effects; actions=%v skill=%v job=%v",
			b.deliveredActions, b.skillUpdates, b.jobStateUpdates)
	}
}
