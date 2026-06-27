import asyncio

from harness import build_agent as ba
from harness.build_agent import (
    CliBuildAgent,
    DeepAgentBuildAgent,
    StubBuildAgent,
    _extract_json,
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


def test_build_agent_prefers_headless_cli(monkeypatch):
    # Key-free path wins: a headless CLI (Claude Code / Codex) is selected first.
    monkeypatch.setattr(ba, "_cli_provider", lambda: "claude")
    assert isinstance(build_agent(), CliBuildAgent)


def test_build_agent_falls_back_to_deepagents_with_key(monkeypatch):
    # No CLI, but deepagents + a model key -> the BYOK deep agent.
    if ba.importlib.util.find_spec("deepagents") is None:
        import pytest

        pytest.skip("deepagents not installed (the `agent` extra)")
    monkeypatch.setattr(ba, "_cli_provider", lambda: None)
    monkeypatch.setattr(ba, "_model_key_available", lambda: True)
    assert isinstance(build_agent(), DeepAgentBuildAgent)


def test_build_agent_degrades_to_stub_with_no_cli_and_no_key(monkeypatch):
    # CI / no inner harness at all -> the deterministic stub keeps the harness running.
    monkeypatch.setattr(ba, "_cli_provider", lambda: None)
    monkeypatch.setattr(ba, "_model_key_available", lambda: False)
    assert isinstance(build_agent(), StubBuildAgent)


def test_extract_json_pulls_object_from_noisy_output():
    text = 'log line\nthinking...\n{"name": "X", "n": {"a": 1}}\ntrailing log'
    assert _extract_json(text) == {"name": "X", "n": {"a": 1}}


def test_cli_build_agent_parses_headless_json(monkeypatch):
    out = (
        'codex preamble...\n'
        '{"name":"Inbound routing","tool_id":"inbound-routing","narration":"n",'
        '"steps":[{"id":"t","kind":"trigger","title":"New lead","detail":"d"},'
        '{"id":"a","kind":"action","title":"Route","detail":"d","integration":"Slack","gated":true}]}'
    )

    class _Proc:
        returncode = 0
        stdout = out
        stderr = ""

    monkeypatch.setattr(ba.subprocess, "run", lambda *a, **k: _Proc())
    spec = CliBuildAgent("codex").compile("route inbound leads to slack")
    assert spec.tool_id == "inbound-routing"
    assert [s.kind for s in spec.steps] == ["trigger", "action"]
    assert spec.steps[1].gated is True


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
