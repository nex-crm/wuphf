"""Service-surface tests: /health, /run (dispatch + interrupt), /resume, fail-loud."""

from fastapi.testclient import TestClient

from orchestrator.harness import FakeHarness
from orchestrator.lifecycle import State, TurnOutcome
from orchestrator.service import create_app


def _client(outcome=TurnOutcome.SUBMITTED_FOR_REVIEW):
    return TestClient(create_app(harness_factory=lambda req: FakeHarness(outcome)))


def test_health():
    assert _client().get("/health").json()["status"] == "ok"


def test_run_dispatch_interrupts_then_resume_approves():
    c = _client(TurnOutcome.SUBMITTED_FOR_REVIEW)
    run = c.post("/run", json={"task_id": "svc1", "record": {"task_id": "svc1", "lifecycle_state": "running"}}).json()
    assert run["status"] == "interrupted"
    assert run["interrupt"]["gate_kind"] == "review"

    resumed = c.post("/resume", json={"task_id": "svc1", "thread_id": "svc1", "decision": "approve"}).json()
    assert resumed["status"] == "done"
    assert resumed["projection"]["lifecycle_state"] == State.APPROVED.value
    assert resumed["projection"]["status"] == "done"  # derived 4-tuple in the projection


def test_run_projection_shape():
    c = _client(TurnOutcome.CONTINUE)
    out = c.post("/run", json={"task_id": "svc2", "record": {"task_id": "svc2", "lifecycle_state": "running"}}).json()
    proj = out["projection"]
    assert set(proj) == {"task_id", "lifecycle_state", "pipeline_stage", "review_state", "status", "blocked"}
    assert proj["lifecycle_state"] == "running"


def test_unknown_record_fails_loud_not_dispatched():
    c = _client()
    out = c.post("/run", json={
        "task_id": "svc3",
        "record": {"task_id": "svc3", "pipeline_stage": "act", "status": "verifying", "blocked": False},
    }).json()
    assert out["status"] == "done"
    assert out["projection"]["lifecycle_state"] == State.UNKNOWN.value
    # Full projection shape even on the fail-loud path: the Go broker decodes every
    # field, so a half-populated record could zero-out legit data. status carries
    # the "unknown" signal too.
    assert set(out["projection"]) == {
        "task_id", "lifecycle_state", "pipeline_stage", "review_state", "status", "blocked"
    }
    assert out["projection"]["status"] == "unknown"


def test_gate_interrupt_projects_gate_state_not_executable():
    # Broker-facing contract: a /run that hits a review gate must project the gate
    # state (review), never the executable "running" — otherwise the broker keeps
    # re-dispatching the same task every tick.
    c = _client(TurnOutcome.SUBMITTED_FOR_REVIEW)
    out = c.post("/run", json={
        "task_id": "svc4", "record": {"task_id": "svc4", "lifecycle_state": "running"}
    }).json()
    assert out["status"] == "interrupted"
    assert out["projection"]["lifecycle_state"] == State.REVIEW.value


def test_schema_version_mismatch_rejected():
    c = _client()
    resp = c.post("/run", json={
        "schema_version": 999,
        "task_id": "svc5",
        "record": {"task_id": "svc5", "lifecycle_state": "running"},
    })
    assert resp.status_code == 400
    assert "schema_version" in resp.json()["detail"]


def test_unknown_request_field_rejected():
    # extra="forbid": a field the Go side adds but Python forgot must fail loud at
    # decode time, not be silently dropped.
    c = _client()
    resp = c.post("/run", json={
        "task_id": "svc6",
        "record": {"task_id": "svc6", "lifecycle_state": "running"},
        "bogus_new_field": "surprise",
    })
    assert resp.status_code == 422
