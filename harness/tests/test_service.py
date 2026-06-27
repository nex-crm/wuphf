import json

from fastapi.testclient import TestClient

from harness.service import create_app
from harness.wire import WorkflowSpec


def _client():
    return TestClient(create_app())


def test_health_and_providers():
    c = _client()
    assert c.get("/health").json()["status"] == "ok"
    payload = c.get("/providers").json()
    assert "providers" in payload and isinstance(payload["providers"], list)


def _sse(client, body):
    events = []
    with client.stream("POST", "/build/stream", json=body) as r:
        assert r.status_code == 200
        name = None
        for line in r.iter_lines():
            if line.startswith("event:"):
                name = line.split(":", 1)[1].strip()
            elif line.startswith("data:"):
                events.append((name, json.loads(line.split(":", 1)[1].strip())))
    return events


def test_build_stream_emits_steps_then_spec():
    evs = _sse(_client(), {"message": "route inbound demo requests to slack over $5k"})
    names = [n for n, _ in evs]
    assert names[0] == "start"
    assert "step" in names
    assert names[-1] == "spec"
    spec = evs[-1][1]["spec"]
    assert spec["tool_id"] == "inbound-routing" and len(spec["steps"]) >= 4


def test_run_executes_a_spec_and_halts_on_gate():
    c = _client()
    spec = WorkflowSpec(name="n", tool_id="inbound-routing", steps=[
        {"id": "p-action", "kind": "action", "title": "Route", "detail": "d", "integration": "Slack", "gated": True},
    ]).model_dump()
    out = c.post("/run", json={"spec": spec, "input": {}}).json()
    assert out["status"] == "needs_approval"
    out2 = c.post("/run", json={"spec": spec, "input": {"approved": ["p-action"]}}).json()
    assert out2["status"] == "done"


def test_schema_version_mismatch_rejected():
    c = _client()
    assert c.post("/build/stream", json={"schema_version": 99, "message": "x"}).status_code == 400
