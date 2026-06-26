"""P2 harness tests. The SDK call is isolated and lazy, so everything here runs
without claude-agent-sdk installed or a model key — the policy (classification,
env resolution, prompt building, degrade-safe selection) is the unit under test."""

from __future__ import annotations

import importlib.util

import pytest

# The degrade-path tests assert SDK-ABSENT behavior; they run in CI (SDK not
# installed) and skip locally once claude-agent-sdk is installed for live runs.
_SDK_PRESENT = importlib.util.find_spec("claude_agent_sdk") is not None
_sdk_absent_only = pytest.mark.skipif(_SDK_PRESENT, reason="claude-agent-sdk installed; SDK-absent path is exercised in CI")

from orchestrator.harness import (
    ClaudeAgentHarness,
    FakeHarness,
    ToolCall,
    TurnTranscript,
    _permission_mode_for,
    _prompt_for,
    build_harness,
    classify_outcome,
)
from orchestrator.lifecycle import State, TurnOutcome
from orchestrator.wire import McpServer

# The MCP-namespaced name Claude Code presents for the teammcp tool.
TEAM_TASK = "mcp__wuphf-office__team_task"


def tc(action: str, name: str = TEAM_TASK) -> ToolCall:
    return ToolCall(name=name, action=action)


@pytest.mark.parametrize(
    "state, calls, expected",
    [
        # Explicit terminal actions win, regardless of state.
        (State.RUNNING, [tc("submit_for_review")], TurnOutcome.SUBMITTED_FOR_REVIEW),
        (State.RUNNING, [tc("complete")], TurnOutcome.COMPLETED),
        (State.RUNNING, [tc("done")], TurnOutcome.COMPLETED),
        (State.RUNNING, [tc("block")], TurnOutcome.BLOCKED),
        # Priority: complete beats a same-turn submit.
        (State.RUNNING, [tc("submit_for_review"), tc("complete")], TurnOutcome.COMPLETED),
        # A turn that created child tasks is a decomposition.
        (State.RUNNING, [tc("create"), tc("create")], TurnOutcome.DECOMPOSED),
        # Decompose ranks below an explicit terminal action...
        (State.RUNNING, [tc("create"), tc("complete")], TurnOutcome.COMPLETED),
        (State.RUNNING, [tc("create"), tc("block")], TurnOutcome.BLOCKED),
        # ...but above PLAN_READY: a planning turn that decomposed is DECOMPOSED.
        (State.PLANNING, [tc("create")], TurnOutcome.DECOMPOSED),
        # A planning turn with no terminal action produced a plan to approve.
        (State.PLANNING, [], TurnOutcome.PLAN_READY),
        (State.PLANNING, [tc("comment")], TurnOutcome.PLAN_READY),
        # Planning still yields to an explicit submit/complete if one occurs.
        (State.PLANNING, [tc("complete")], TurnOutcome.COMPLETED),
        # A work turn that did stuff but didn't finish needs another turn.
        (State.RUNNING, [tc("comment")], TurnOutcome.CONTINUE),
        (State.CHANGES_REQUESTED, [], TurnOutcome.CONTINUE),
    ],
)
def test_classify_outcome(state, calls, expected):
    assert classify_outcome(state, TurnTranscript(tool_calls=calls)) == expected


def test_team_task_actions_filters_other_tools_and_dedups():
    t = TurnTranscript(
        tool_calls=[
            tc("submit_for_review"),
            tc("submit_for_review"),  # dup
            ToolCall(name="mcp__wuphf-office__wiki_write", action="ignored"),  # not team_task
            ToolCall(name="Bash", action=""),  # no action
        ]
    )
    assert t.team_task_actions() == {"submit_for_review"}


def test_classify_ignores_non_team_task_action():
    # A different tool that happens to carry action="complete" must NOT terminate.
    t = TurnTranscript(tool_calls=[ToolCall(name="mcp__wuphf-office__wiki_write", action="complete")])
    assert classify_outcome(State.RUNNING, t) == TurnOutcome.CONTINUE


def test_decomposed_children_extracts_specs_in_order():
    t = TurnTranscript(tool_calls=[
        ToolCall(name=TEAM_TASK, action="create", args={"title": "scaffold", "parent_issue_id": "G"}),
        ToolCall(name=TEAM_TASK, action="create", args={"title": "tests", "depends_on": ["c1", " "]}),
        ToolCall(name=TEAM_TASK, action="comment", args={"body": "noise"}),  # not a create
        ToolCall(name="Bash", action="", args={"title": "ignored"}),         # not team_task
    ])
    specs = t.decomposed_children()
    assert [s.title for s in specs] == ["scaffold", "tests"]
    assert specs[0].depends_on == ()
    assert specs[1].depends_on == ("c1",)  # blank dep is dropped


def test_decomposed_children_empty_when_no_create():
    t = TurnTranscript(tool_calls=[tc("submit_for_review"), tc("comment")])
    assert t.decomposed_children() == []


def test_decompose_turn_transitions_goal_to_running():
    # End-to-end of the decompose policy: a turn that created children classifies
    # DECOMPOSED and (non-gated) leaves the goal RUNNING so the broker coordinates
    # the children on the next tick.
    from orchestrator.lifecycle import apply_turn_outcome, gate_for_outcome

    t = TurnTranscript(tool_calls=[ToolCall(name=TEAM_TASK, action="create", args={"title": "x"})])
    outcome = classify_outcome(State.RUNNING, t)
    assert outcome is TurnOutcome.DECOMPOSED
    assert gate_for_outcome(State.RUNNING, outcome) is None  # not human-gated
    assert apply_turn_outcome(State.RUNNING, outcome) is State.RUNNING


def test_allowed_mcp_tools_grants_each_wired_server():
    # The harness must allow its wired MCP tools, or the agent's team_task calls
    # are permission-denied (transcript shows the call, but the broker never sees
    # the side effect). Server-prefix grant covers every tool the server exposes.
    h = ClaudeAgentHarness(
        model="m",
        mcp={
            "wuphf-office": McpServer(command="wuphf", args=["mcp-team"]),
            "extra": McpServer(command="x", args=[]),
        },
    )
    assert sorted(h._allowed_mcp_tools()) == ["mcp__extra", "mcp__wuphf-office"]


def test_allowed_mcp_tools_empty_without_servers():
    assert ClaudeAgentHarness(model="m", mcp={})._allowed_mcp_tools() == []


def test_mcp_servers_config_resolves_env_names_to_values():
    h = ClaudeAgentHarness(
        model="claude-sonnet-4-6",
        mcp={"wuphf-office": McpServer(command="wuphf", args=["mcp-team"], env_passthrough=["WUPHF_BROKER_TOKEN", "MISSING_VAR"])},
        env={"WUPHF_BROKER_TOKEN": "secret-value", "UNRELATED": "x"},
    )
    cfg = h._mcp_servers_config()
    office = cfg["wuphf-office"]
    assert office == {
        "type": "stdio",
        "command": "wuphf",
        "args": ["mcp-team"],
        # Name present in env -> value; name absent -> "" (never dropped, never leaked).
        "env": {"WUPHF_BROKER_TOKEN": "secret-value", "MISSING_VAR": ""},
    }
    # An env var the agent wasn't granted must not appear.
    assert "UNRELATED" not in office["env"]


@_sdk_absent_only
def test_run_turn_raises_clear_error_without_sdk():
    # claude-agent-sdk is not installed in CI: run_turn must fail loud with guidance.
    h = ClaudeAgentHarness(model="m", mcp={})
    with pytest.raises(RuntimeError, match="claude-agent-sdk"):
        h.run_turn({"lifecycle_state": "running", "messages": []})


def test_prompt_for_joins_messages():
    prompt = _prompt_for({"messages": [{"role": "user", "content": "do X"}, {"role": "user", "content": "then Y"}]})
    assert "do X" in prompt and "then Y" in prompt


def test_prompt_for_falls_back_when_empty():
    prompt = _prompt_for({"messages": []})
    assert "submit_for_review" in prompt and "complete" in prompt


def test_permission_mode():
    assert _permission_mode_for(State.PLANNING) == "plan"
    assert _permission_mode_for(State.RUNNING) == "acceptEdits"
    assert _permission_mode_for(State.CHANGES_REQUESTED) == "acceptEdits"


@_sdk_absent_only
def test_build_harness_degrades_to_fake_without_sdk():
    # No SDK in CI -> the service stays runnable via FakeHarness.
    h = build_harness("claude-sonnet-4-6", {})
    assert isinstance(h, FakeHarness)


@_sdk_absent_only
def test_build_harness_warns_when_degrading(caplog):
    # The degrade must be VISIBLE in operator logs — a silent FakeHarness in prod
    # means tasks transition state with no real agent work behind them.
    import logging

    with caplog.at_level(logging.WARNING, logger="orchestrator.harness"):
        build_harness("claude-sonnet-4-6", {})
    assert any("FakeHarness" in r.message for r in caplog.records)


@pytest.mark.skipif(not _SDK_PRESENT, reason="needs claude-agent-sdk installed")
def test_build_harness_uses_real_harness_when_sdk_present():
    # The complement: with the SDK installed, build_harness wires the real agent.
    h = build_harness("claude-sonnet-4-6", {})
    assert isinstance(h, ClaudeAgentHarness)
