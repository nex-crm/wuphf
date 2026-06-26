"""Inner-harness interface — the seam that keeps Claude Code/Codex as the real
execution engine. A graph dispatch node calls run_turn(); the harness runs ONE
agent turn (the great inner harness, untouched) and reports a structured outcome.

  - Harness            : the protocol.
  - FakeHarness        : scripted outcomes for tests + key-free local runs.
  - ClaudeAgentHarness : drives Claude Code via the Claude Agent SDK (P1b-iv). The SDK
                         call is lazy + isolated; the turn-outcome CLASSIFIER is a
                         pure function (classify_outcome) keyed on the agent's real
                         teammcp tool calls, so the policy is fully unit-tested
                         without a model key or the SDK installed.

Outcome classification keys on the `team_task` MCP tool's `action` — the same
vocabulary the broker already uses (submit_for_review / complete / block / …).
Claude Code namespaces MCP tools as `mcp__<server>__team_task`, so the classifier
matches the tool-name suffix.
"""

from __future__ import annotations

import asyncio
import logging
import os
from dataclasses import dataclass, field
from typing import Protocol

from .lifecycle import State, TurnOutcome
from .runstate import TaskRun
from .wire import McpServer

_log = logging.getLogger(__name__)


@dataclass
class TurnResult:
    outcome: TurnOutcome
    text: str = ""


class Harness(Protocol):
    def run_turn(self, task: TaskRun) -> TurnResult:  # pragma: no cover - protocol
        ...


# --------------------------------------------------------------------------- #
# Turn transcript + outcome classification (pure — the heart of P1b-iv).
# --------------------------------------------------------------------------- #


@dataclass(frozen=True)
class ToolCall:
    """One tool invocation observed during a turn. `action` is the `team_task`
    action argument when present (empty for other tools/args); `args` is the raw
    tool input (used to read a decompose call's title/depends_on)."""

    name: str
    action: str = ""
    args: dict = field(default_factory=dict)


@dataclass(frozen=True)
class ChildSpec:
    """A child task a decompose turn asked the broker to create. The broker is the
    one that actually creates it (via the team_task MCP call); this is the
    orchestrator's structured view of the decomposition for classification +
    observability."""

    title: str
    depends_on: tuple[str, ...] = ()


@dataclass
class TurnTranscript:
    """Normalized output of one inner-harness turn, decoupled from the SDK's
    message types so the classifier is testable in isolation."""

    tool_calls: list[ToolCall] = field(default_factory=list)
    text: str = ""

    def _team_task_calls(self) -> list[ToolCall]:
        # Match the MCP-namespaced tool name (mcp__<server>__team_task) by suffix.
        return [tc for tc in self.tool_calls if tc.name.endswith("team_task")]

    def team_task_actions(self) -> set[str]:
        return {tc.action for tc in self._team_task_calls() if tc.action}

    def decomposed_children(self) -> list[ChildSpec]:
        """The child specs from this turn's team_task create calls, in order. The
        broker has already created them as a side effect of the calls; this is the
        decomposition the orchestrator observed."""
        specs: list[ChildSpec] = []
        for tc in self._team_task_calls():
            if tc.action not in _DECOMPOSE_ACTIONS:
                continue
            title = str(tc.args.get("title", "")).strip()
            raw_deps = tc.args.get("depends_on") or []
            deps = tuple(str(d).strip() for d in raw_deps if str(d).strip())
            specs.append(ChildSpec(title=title, depends_on=deps))
        return specs


# team_task actions that terminate a turn, grouped by the outcome they imply.
# Grounded in the broker's real action vocabulary (internal/team/broker_tasks*.go).
_COMPLETE_ACTIONS = frozenset({"complete", "done"})
_SUBMIT_ACTIONS = frozenset({"submit_for_review"})
_BLOCK_ACTIONS = frozenset({"block"})
# create == the broker's sub-task creation action (server_tasks.go -> /tasks).
_DECOMPOSE_ACTIONS = frozenset({"create"})


def classify_outcome(state: State, transcript: TurnTranscript) -> TurnOutcome:
    """Map one turn's observable signals to a TurnOutcome.

    Priority: an explicit terminal team_task action wins (complete > submit >
    block); then a turn that CREATED child tasks is a decomposition (DECOMPOSED);
    then a PLANNING turn that ran without any of those produced a plan to approve
    (PLAN_READY); any other turn needs more work (CONTINUE). Deliberately driven
    by the agent's real tool calls, not by parsing prose.
    """
    actions = transcript.team_task_actions()
    if actions & _COMPLETE_ACTIONS:
        return TurnOutcome.COMPLETED
    if actions & _SUBMIT_ACTIONS:
        return TurnOutcome.SUBMITTED_FOR_REVIEW
    if actions & _BLOCK_ACTIONS:
        return TurnOutcome.BLOCKED
    if actions & _DECOMPOSE_ACTIONS:
        return TurnOutcome.DECOMPOSED
    if state is State.PLANNING:
        return TurnOutcome.PLAN_READY
    return TurnOutcome.CONTINUE


def _prompt_for(task: TaskRun) -> str:
    """Build the user turn from the re-hydrated messages, falling back to a
    minimal directive so a turn always has something to act on."""
    parts = [
        str(m.get("content", "")).strip()
        for m in (task.get("messages") or [])
        if isinstance(m, dict) and str(m.get("content", "")).strip()
    ]
    body = "\n\n".join(parts).strip()
    return body or (
        "Continue your assigned task. When the work is ready for review call the "
        "team_task tool with action=submit_for_review; if it is fully done use "
        "action=complete; if you are blocked use action=block."
    )


def _permission_mode_for(state: State) -> str:
    # Planning is read-only (the broker's native plan mode); work turns may edit.
    return "plan" if state is State.PLANNING else "acceptEdits"


# --------------------------------------------------------------------------- #
# Harness implementations.
# --------------------------------------------------------------------------- #


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

    The teammcp MCP server + broker env are passed exactly as the existing CLI
    providers do (internal/provider/claude.go): env-var NAMES arrive on the wire
    (McpServer.env_passthrough) and are resolved to values HERE from the
    orchestrator's own environment, never on the wire. The SDK call is lazy and
    isolated; the outcome classifier (classify_outcome) carries the policy.
    """

    def __init__(self, *, model: str, mcp: dict[str, McpServer], env: dict | None = None, max_turns: int = 24):
        self._model = model
        self._mcp = mcp
        self._env = env if env is not None else os.environ
        self._max_turns = max_turns

    def _mcp_servers_config(self) -> dict:
        """SDK-shaped stdio MCP config. Resolves env_passthrough NAMES to values
        from the orchestrator env (this is where names become values — the wire
        only ever carried names)."""
        out: dict = {}
        for name, srv in self._mcp.items():
            out[name] = {
                "type": "stdio",
                "command": srv.command,
                "args": list(srv.args),
                "env": {key: self._env.get(key, "") for key in srv.env_passthrough},
            }
        return out

    def _require_sdk(self):
        try:
            import claude_agent_sdk  # noqa: F401  (lazy: optional dependency)
        except ImportError as e:  # pragma: no cover - unreachable when SDK is installed
            raise RuntimeError(
                "ClaudeAgentHarness requires the 'claude' extra: pip install claude-agent-sdk"
            ) from e

    def _build_options(self, system_prompt: str, permission_mode: str):  # pragma: no cover - needs SDK
        from claude_agent_sdk import ClaudeAgentOptions  # lazy

        return ClaudeAgentOptions(
            model=self._model or None,
            system_prompt=system_prompt or None,
            mcp_servers=self._mcp_servers_config(),
            permission_mode=permission_mode,
            max_turns=self._max_turns,
        )

    async def _collect_transcript(self, prompt: str, options) -> TurnTranscript:  # pragma: no cover - needs SDK + key
        from claude_agent_sdk import query  # lazy

        tool_calls: list[ToolCall] = []
        texts: list[str] = []
        async for message in query(prompt=prompt, options=options):
            for block in getattr(message, "content", None) or []:
                kind = type(block).__name__
                if kind == "ToolUseBlock":
                    raw = getattr(block, "input", None)
                    args = raw if isinstance(raw, dict) else {}
                    action = str(args.get("action", ""))
                    tool_calls.append(ToolCall(name=getattr(block, "name", ""), action=action, args=args))
                elif kind == "TextBlock":
                    text = getattr(block, "text", "")
                    if text:
                        texts.append(text)
        return TurnTranscript(tool_calls=tool_calls, text="\n".join(texts))

    def run_turn(self, task: TaskRun) -> TurnResult:
        self._require_sdk()
        state = State(task["lifecycle_state"])
        prompt = _prompt_for(task)
        options = self._build_options(task.get("system_prompt", ""), _permission_mode_for(state))  # pragma: no cover
        transcript = asyncio.run(self._collect_transcript(prompt, options))  # pragma: no cover - needs SDK + key
        return TurnResult(outcome=classify_outcome(state, transcript), text=transcript.text)  # pragma: no cover


def build_harness(model: str, mcp: dict[str, McpServer]) -> Harness:
    """Pick a harness for a dispatch: ClaudeAgentHarness when the SDK is importable,
    else FakeHarness so the service stays runnable key-free (degrade-safe). The
    service injects this; tests override with an explicit FakeHarness."""
    try:
        import claude_agent_sdk  # noqa: F401
    except ImportError:
        _log.warning(
            "claude-agent-sdk not installed; falling back to FakeHarness — no real "
            "agent will run. Install the 'claude' extra (pip install claude-agent-sdk) "
            "for live dispatch."
        )
        return FakeHarness()
    return ClaudeAgentHarness(model=model, mcp=mcp)
