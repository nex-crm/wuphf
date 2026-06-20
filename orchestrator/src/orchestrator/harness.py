"""Inner-harness interface — the seam that keeps Claude Code/Codex as the real
execution engine. A graph dispatch node calls run_turn(); the harness runs ONE
agent turn (the great inner harness, untouched) and reports a structured outcome.

  - Harness        : the protocol.
  - FakeHarness    : scripted outcomes for tests + key-free local runs.
  - ClaudeAgentHarness : drives Claude Code via the Claude Agent SDK (lazy import;
                     only used when claude-agent-sdk is installed + a key is set).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from .lifecycle import TurnOutcome
from .runstate import TaskRun


@dataclass
class TurnResult:
    outcome: TurnOutcome
    text: str = ""


class Harness(Protocol):
    def run_turn(self, task: TaskRun) -> TurnResult:  # pragma: no cover - protocol
        ...


class FakeHarness:
    """Returns a scripted outcome. Used by tests and key-free local runs so the
    whole orchestration loop is exercisable without a model."""

    def __init__(self, outcome: TurnOutcome = TurnOutcome.SUBMITTED_FOR_REVIEW, text: str = "fake turn"):
        self._outcome = outcome
        self._text = text

    def run_turn(self, task: TaskRun) -> TurnResult:
        return TurnResult(outcome=self._outcome, text=self._text)


class ClaudeAgentHarness:
    """Drives Claude Code for one turn via the Claude Agent SDK, wired to teammcp.

    Lazy-imports claude-agent-sdk so the package installs and tests run without it.
    The teammcp MCP server + broker token are passed exactly as the existing CLI
    providers do (see internal/provider/claude.go) so tool calls authenticate to
    the broker. Outcome classification (submitted_for_review / plan_ready / ...)
    is parsed from the run result — P1 stub returns COMPLETED; P2 wires real
    classification."""

    def __init__(self, *, model: str, mcp_config: dict, broker_env: dict):
        self._model = model
        self._mcp_config = mcp_config
        self._broker_env = broker_env

    def run_turn(self, task: TaskRun) -> TurnResult:  # pragma: no cover - needs key
        try:
            import claude_agent_sdk  # noqa: F401  (lazy: optional dependency)
        except ImportError as e:
            raise RuntimeError(
                "ClaudeAgentHarness requires the 'claude' extra (claude-agent-sdk)"
            ) from e
        # P2: build a ClaudeAgentOptions with the teammcp MCP server + broker env,
        # run one turn over task['messages'] under task['system_prompt'], stream
        # results, and classify the outcome. Intentionally not implemented in P1.
        raise NotImplementedError("ClaudeAgentHarness.run_turn lands in P2")
