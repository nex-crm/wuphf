"""The BUILD agent: a plain-language description -> a deterministic WorkflowSpec.

S0 ships a pure, keyword-driven STUB compiler (a server-side port of the FE's
operator/builder/planWorkflow.ts) so /build/stream is real end to end without a
model. Slice S2 swaps `build_agent()` to the real LangChain deep agent (planning +
gbrain/browsersniff tools) while keeping this exact output contract; the executor
and FE do not change. Degrade-safe: build_agent() returns the stub unless the
`agent` extra (deepagents) is importable.

The agent only FIGURES OUT the workflow. It never executes it (see executor.py).
"""

from __future__ import annotations

import importlib.util
import logging
import os
import re
from collections.abc import AsyncIterator
from typing import Any, Protocol

from .providers import detect_providers
from .wire import ClarifyQuestion, WorkflowSpec, WorkflowStep

_log = logging.getLogger(__name__)


class BuildAgent(Protocol):
    def compile(self, message: str, tool_id: str | None = None) -> WorkflowSpec:  # pragma: no cover - protocol
        ...

    def stream(self, message: str, tool_id: str | None = None) -> AsyncIterator[dict]:  # pragma: no cover - protocol
        """Yield build events: one {"type":"step", ...} per assembled step (the FE's
        staggered reveal), then {"type":"spec", ...} with the full WorkflowSpec."""
        ...


def _has(text: str, *words: str) -> bool:
    return any(w in text for w in words)


def _detect_subject(t: str) -> tuple[str, str, str]:
    """(source, tool name, tool_id) — mirrors planWorkflow.detectSubject."""
    if _has(t, "ticket", "escalation", "support", "incident"):
        return "support ticket", "Support escalation triage", "support-escalations"
    if _has(t, "expense", "invoice", "reimburse", "spend", "purchase order"):
        return "expense over policy", "Expense exception routing", "expense-exceptions"
    if _has(t, "demo", "trial", "lead", "signup", "sign-up", "form", "inbound"):
        return "demo request", "Inbound demo-request routing", "inbound-routing"
    return "inbound item", "Inbound routing", "inbound-routing"


def _detect_amount(t: str) -> str | None:
    m = re.search(r"\$\s?(\d[\d,]*\s?[km]?)", t, re.IGNORECASE)
    return f"${m.group(1).replace(' ', '')}" if m else None


class StubBuildAgent:
    """Deterministic keyword compiler. Pure (no I/O), so it carries a regression
    test and gives the prototype a believable 'the AI understood you' build."""

    def compile(self, message: str, tool_id: str | None = None) -> WorkflowSpec:
        t = message.lower().strip()
        source, name, resolved_tool = _detect_subject(t)
        steps: list[WorkflowStep] = [
            WorkflowStep(id="p-trigger", kind="trigger", title=f"New {source}", detail=f"Starts when a {source} arrives."),
            WorkflowStep(id="p-enrich", kind="enrich", title="Enrich the record", detail="Look up the related account + history."),
            WorkflowStep(id="p-ai", kind="ai", title="Score it", detail="An AI step rates fit/urgency from the enriched context."),
        ]
        amount = _detect_amount(t)
        threshold_label = amount or "the threshold"
        steps.append(WorkflowStep(
            id="p-decision", kind="decision",
            title=f"Decide on {threshold_label}",
            detail=f"Branch on whether the score/amount crosses {threshold_label}.",
        ))
        # The action mutates an external system -> gated to the human approval card (CQ1).
        steps.append(WorkflowStep(
            id="p-action", kind="action", title="Route it",
            detail="Notify the right owner and record the routing decision.",
            integration="Slack", gated=True,
        ))

        # Ask exactly one sharp clarifying question, in place, like the FE does.
        clarify: ClarifyQuestion | None = None
        if amount is None:
            clarify = ClarifyQuestion(
                field="threshold", step_id="p-decision",
                prompt="What score or dollar amount should send this down the priority path?",
            )
        elif not _has(t, "slack", "email", "channel", "notify"):
            clarify = ClarifyQuestion(
                field="channel", step_id="p-action",
                prompt="Where should I route it — a Slack channel, or email an owner?",
            )

        return WorkflowSpec(
            name=name, tool_id=tool_id or resolved_tool, steps=steps, clarify=clarify,
            narration=f"Got it — a {name.lower()}. I assembled {len(steps)} deterministic steps; one question to lock it.",
        )

    async def stream(self, message: str, tool_id: str | None = None) -> AsyncIterator[dict]:
        spec = self.compile(message, tool_id)
        for step in spec.steps:
            yield {"type": "step", "step": step.model_dump()}
        yield {"type": "spec", "spec": spec.model_dump()}


_BUILD_SYSTEM_PROMPT = """You are the BUILD agent for an operator tool-builder. The \
operator describes, in plain language, an internal workflow they want. Your job is to \
FIGURE OUT a small, deterministic workflow and emit it — you do NOT execute anything.

Design an ordered pipeline of steps. Each step has a kind:
- trigger : what starts the workflow (an inbound item, a form, a ticket)
- enrich  : look up related context
- ai      : a model step that scores/classifies from the enriched context
- decision: branch on a threshold/condition
- action  : do something in an external system (set gated=true — it needs human approval)
- branch  : a conditional fork

Keep it tight (3-6 steps). Any step that mutates an external system MUST be gated.
Ask AT MOST ONE sharp clarifying question (a threshold or a destination channel) only \
if you genuinely cannot proceed. When done, call submit_workflow exactly once with the \
full spec. Do not narrate after the tool call."""


def _spec_from_capture(captured: dict[str, Any], fallback_tool_id: str | None) -> WorkflowSpec:
    """Build a validated WorkflowSpec from the agent's submit_workflow arguments. Pure
    + tolerant of missing optional fields, so it is unit-testable without a model."""
    steps = [WorkflowStep(**s) if not isinstance(s, WorkflowStep) else s for s in captured.get("steps", [])]
    clarify_raw = captured.get("clarify")
    clarify = ClarifyQuestion(**clarify_raw) if isinstance(clarify_raw, dict) else None
    return WorkflowSpec(
        name=str(captured.get("name") or "Untitled workflow"),
        tool_id=str(captured.get("tool_id") or fallback_tool_id or "inbound-routing"),
        steps=steps,
        narration=str(captured.get("narration") or ""),
        clarify=clarify,
    )


class DeepAgentBuildAgent:
    """The real BUILD agent: a LangChain deep agent (deepagents) that plans and emits a
    WorkflowSpec via a submit_workflow tool. Same output contract as the stub, so the
    executor and FE are unchanged. Requires a model API key (deepagents/LangChain does
    NOT use Claude Code auth) — `build_agent()` only selects this when one is present."""

    def __init__(self, model: str | None = None):
        self._model = model or os.getenv("HARNESS_MODEL") or "anthropic:claude-sonnet-4-5"

    def _invoke(self, message: str, tool_id: str | None) -> dict[str, Any]:  # pragma: no cover - needs a model key
        from deepagents import create_deep_agent

        captured: dict[str, Any] = {}

        def submit_workflow(name: str, tool_id: str, steps: list[dict], narration: str = "", clarify: dict | None = None) -> str:
            """Record the final deterministic workflow spec. Call exactly once."""
            captured.update(name=name, tool_id=tool_id, steps=steps, narration=narration, clarify=clarify)
            return "workflow recorded"

        agent = create_deep_agent(model=self._model, tools=[submit_workflow], system_prompt=_BUILD_SYSTEM_PROMPT)
        prompt = message if not tool_id else f"{message}\n\n(Refine the existing tool: {tool_id})"
        agent.invoke({"messages": [{"role": "user", "content": prompt}]})
        if "name" not in captured:
            raise RuntimeError("deep agent finished without calling submit_workflow")
        return captured

    def compile(self, message: str, tool_id: str | None = None) -> WorkflowSpec:  # pragma: no cover - needs a model key
        return _spec_from_capture(self._invoke(message, tool_id), tool_id)

    async def stream(self, message: str, tool_id: str | None = None) -> AsyncIterator[dict]:  # pragma: no cover - needs a model key
        spec = self.compile(message, tool_id)
        for step in spec.steps:
            yield {"type": "step", "step": step.model_dump()}
        yield {"type": "spec", "spec": spec.model_dump()}


def _model_key_available() -> bool:
    """Whether a real LangChain model can authenticate (an api_key provider). Claude
    Code CLI auth does NOT count — deepagents/LangChain needs a key."""
    return any(p.available and p.via == "api_key" for p in detect_providers())


def build_agent() -> BuildAgent:
    """Pick the BUILD agent: the real deepagents agent when it is installed AND a model
    key is configured (BYOK); otherwise the deterministic stub so the harness always
    runs key-free. Keyless authoring uses the stub today; a key unlocks the real agent."""
    if importlib.util.find_spec("deepagents") is None:
        return StubBuildAgent()
    if not _model_key_available():
        _log.info("deepagents installed but no model key — using the deterministic stub (set a provider key for the real agent)")
        return StubBuildAgent()
    return DeepAgentBuildAgent()
