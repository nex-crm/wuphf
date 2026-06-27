import pytest
from pydantic import ValidationError

from harness.wire import SCHEMA_VERSION, BuildRequest, WorkflowSpec, WorkflowStep


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
