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
import re
from collections.abc import AsyncIterator
from typing import Protocol

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


def build_agent() -> BuildAgent:
    """Pick the BUILD agent: the real deep agent when the `agent` extra is present,
    else the deterministic stub so the harness runs key-free."""
    if importlib.util.find_spec("deepagents") is None:
        return StubBuildAgent()
    # S2: construct and return the real LangChain deep-agent here (same contract).
    _log.info("deepagents present — real build agent lands in S2; using stub for now")
    return StubBuildAgent()
