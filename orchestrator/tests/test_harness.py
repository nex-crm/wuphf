"""P2 harness tests. The SDK call is isolated and lazy, so everything here runs
without claude-agent-sdk installed or a model key — the policy (classification,
env resolution, prompt building, degrade-safe selection) is the unit under test."""

from __future__ import annotations

import pytest

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


def test_build_harness_degrades_to_fake_without_sdk():
    # No SDK in CI -> the service stays runnable via FakeHarness.
    h = build_harness("claude-sonnet-4-6", {})
    assert isinstance(h, FakeHarness)
