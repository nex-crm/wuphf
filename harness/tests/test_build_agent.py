import asyncio

from harness import build_agent as ba
from harness.build_agent import (
    DeepAgentBuildAgent,
    StubBuildAgent,
    _spec_from_capture,
    build_agent,
)


def test_subject_detection_and_tool_mapping():
    a = StubBuildAgent()
    assert a.compile("route inbound demo requests").tool_id == "inbound-routing"
    assert a.compile("triage support escalations").tool_id == "support-escalations"
    assert a.compile("approve expense over policy").tool_id == "expense-exceptions"


def test_action_step_is_gated_external_mutation():
    spec = StubBuildAgent().compile("route inbound leads to slack")
    action = next(s for s in spec.steps if s.kind == "action")
    assert action.gated is True and action.integration  # CQ1: external mutation gates


def test_clarify_asks_threshold_when_absent_then_channel():
    a = StubBuildAgent()
    # No amount -> ask for the threshold.
    assert a.compile("route inbound leads").clarify.field == "threshold"
    # Amount present but no channel -> ask where to route.
    assert a.compile("route inbound leads over $5k").clarify.field == "channel"
    # Amount + channel -> no question needed.
    assert a.compile("route inbound leads over $5k to slack").clarify is None


def test_stream_yields_one_step_each_then_the_spec():
    async def collect():
        return [ev async for ev in StubBuildAgent().stream("route inbound demo requests")]

    evs = asyncio.run(collect())
    assert [e["type"] for e in evs][-1] == "spec"
    assert sum(1 for e in evs if e["type"] == "step") == len(evs[-1]["spec"]["steps"])


def test_build_agent_degrades_to_stub_without_model_key(monkeypatch):
    # No api_key provider (Claude Code CLI auth doesn't count for LangChain) -> stub,
    # whether or not deepagents is installed.
    monkeypatch.setattr(ba, "_model_key_available", lambda: False)
    assert isinstance(build_agent(), StubBuildAgent)


def test_build_agent_uses_deep_agent_when_installed_and_keyed(monkeypatch):
    if ba.importlib.util.find_spec("deepagents") is None:
        import pytest

        pytest.skip("deepagents not installed (the `agent` extra)")
    monkeypatch.setattr(ba, "_model_key_available", lambda: True)
    agent = build_agent()
    assert isinstance(agent, DeepAgentBuildAgent)  # constructed; no live model call


def test_spec_from_capture_builds_validated_spec():
    spec = _spec_from_capture(
        {
            "name": "Inbound routing",
            "tool_id": "inbound-routing",
            "steps": [
                {"id": "t", "kind": "trigger", "title": "New lead", "detail": "d"},
                {"id": "a", "kind": "action", "title": "Route", "detail": "d", "integration": "Slack", "gated": True},
            ],
            "narration": "done",
            "clarify": {"field": "channel", "prompt": "where?", "step_id": "a"},
        },
        fallback_tool_id=None,
    )
    assert spec.tool_id == "inbound-routing"
    assert spec.steps[1].gated is True
    assert spec.clarify.field == "channel"
