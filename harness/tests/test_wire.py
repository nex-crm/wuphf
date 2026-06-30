import pytest
from pydantic import ValidationError

from harness.wire import (
    SCHEMA_VERSION,
    ApiCall,
    BuildRequest,
    WorkflowSpec,
    WorkflowStep,
)


def test_build_request_defaults_schema_version():
    assert BuildRequest(message="hi").schema_version == SCHEMA_VERSION


def test_extra_fields_rejected():
    with pytest.raises(ValidationError):
        BuildRequest(message="hi", bogus="x")


def test_workflow_spec_round_trips():
    spec = WorkflowSpec(name="n", tool_id="t", steps=[
        WorkflowStep(id="a", kind="trigger", title="t", detail="d"),
    ])
    assert WorkflowSpec.model_validate(spec.model_dump()) == spec


def test_workflow_step_with_api_round_trips():
    # A replay-capable spec carries ApiCall on a step. extra="forbid" must NOT 422
    # the `api` field (the FE/agent contract has it; the Python models lacked it).
    step = WorkflowStep(
        id="a",
        kind="action",
        title="Post to Slack",
        detail="d",
        integration="Slack",
        gated=True,
        api=ApiCall(
            method="POST",
            url="https://slack.com/api/chat.postMessage",
            headers={"Content-Type": "application/json"},
            body={"channel": "#ops"},
            auth_ref="slack-bot-token",
        ),
    )
    spec = WorkflowSpec(name="n", tool_id="t", steps=[step])
    rebuilt = WorkflowSpec.model_validate(spec.model_dump())
    assert rebuilt == spec
    assert rebuilt.steps[0].api is not None
    assert rebuilt.steps[0].api.auth_ref == "slack-bot-token"
