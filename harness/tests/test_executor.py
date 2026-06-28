from harness.build_agent import StubBuildAgent
from harness.executor import run_workflow
from harness.wire import WorkflowSpec, WorkflowStep


def _spec() -> WorkflowSpec:
    return StubBuildAgent().compile("route inbound leads over $5k to slack")


def test_run_halts_at_gated_step_for_approval():
    res = run_workflow(_spec())
    assert res.status == "needs_approval"
    assert res.pending_approval and res.pending_approval.step_id == "p-action"
    assert res.steps[-1].status == "awaiting_approval"


def test_run_completes_once_gated_step_is_approved():
    res = run_workflow(_spec(), approved={"p-action"})
    assert res.status == "done"
    assert all(s.status == "ok" for s in res.steps)


def test_ungated_workflow_runs_to_completion():
    spec = WorkflowSpec(name="n", tool_id="t", steps=[
        WorkflowStep(id="a", kind="trigger", title="t", detail="d"),
        WorkflowStep(id="b", kind="ai", title="t", detail="d"),
    ])
    assert run_workflow(spec).status == "done"
