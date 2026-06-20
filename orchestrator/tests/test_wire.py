"""Wire-contract golden tests — the Go <-> Python shapes the broker encodes/decodes."""

from orchestrator.wire import DispatchRequest, McpServer, ResumeRequest, StepResult


def test_dispatch_request_golden():
    req = DispatchRequest(
        task_id="task-7",
        record={"task_id": "task-7", "lifecycle_state": "running", "status": "in_progress"},
        model="anthropic:claude-sonnet-4-6",
        system_prompt="You are the engineer.",
        mcp={"teammcp": McpServer(
            command="wuphf", args=["mcp-team"],
            env_passthrough=["WUPHF_BROKER_TOKEN", "WUPHF_BROKER_ADDR"],
        )},
    )
    d = req.model_dump()
    assert d["schema_version"] == 1
    assert d["mcp"]["teammcp"]["command"] == "wuphf"
    # secrets are NEVER values — only env var names cross the wire
    assert d["mcp"]["teammcp"]["env_passthrough"] == ["WUPHF_BROKER_TOKEN", "WUPHF_BROKER_ADDR"]
    assert DispatchRequest.model_validate(d) == req


def test_resume_request_rejects_unknown_decision():
    import pytest
    from pydantic import ValidationError

    ResumeRequest(task_id="t", thread_id="t", decision="approve")  # ok
    with pytest.raises(ValidationError):
        ResumeRequest(task_id="t", thread_id="t", decision="yolo")


def test_step_result_shape():
    sr = StepResult(status="interrupted", thread_id="t",
                    projection={"task_id": "t", "lifecycle_state": "review"},
                    interrupt={"gate_kind": "review"})
    assert sr.model_dump()["status"] == "interrupted"
