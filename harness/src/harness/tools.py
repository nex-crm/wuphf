"""The chat agent's own **create_tool** tool — how the agent builds new tools.

The operator teaches a workflow in the chat; the chat agent turns it into a
callable capability by calling `create_tool(...)`. That is the ONLY way tools are
made — there is no build-a-tool UI. A ToolStore holds the app's tools so the agent
can call them on later turns, and the FE's Tools tab can list them.

`make_create_tool(store)` returns a plain function suitable to hand to a deep agent
(`create_deep_agent(tools=[submit_workflow, create_tool])`) — the docstring is the
agent-facing description, so keep it instructive. Kept pure (in-memory store, no
I/O) so it carries a regression test without a model key.
"""

from __future__ import annotations

import re
from collections.abc import Callable
from typing import Any, Protocol

from .wire import Tool, ToolBuildResult, ToolInput


class ToolStore:
    """The app's tools, keyed by callable name. Newest write wins (so re-teaching a
    workflow updates its tool in place rather than duplicating it)."""

    def __init__(self) -> None:
        self._tools: dict[str, Tool] = {}

    def add(self, tool: Tool) -> Tool:
        self._tools[tool.name] = tool
        return tool

    def get(self, name: str) -> Tool | None:
        return self._tools.get(name)

    def list(self) -> list[Tool]:
        return list(self._tools.values())


def _coerce_inputs(inputs: list[Any] | None) -> list[ToolInput]:
    """Accept the loose shapes a model emits: ["lead"], [{"name": "lead"}], or
    already-typed ToolInput. Anything else for an entry is skipped, not fatal."""
    out: list[ToolInput] = []
    for item in inputs or []:
        if isinstance(item, ToolInput):
            out.append(item)
        elif isinstance(item, str):
            out.append(ToolInput(name=item))
        elif isinstance(item, dict) and item.get("name"):
            out.append(ToolInput(name=str(item["name"]), type=item.get("type", "string")))
    return out


# Keyword -> tool shape, a server-side port of the FE's operator/tools/mockTools.ts
# SHAPES. So the stub tool agent (no model key) makes the SAME recognizable tools
# the FE mock does, and the FE swap from mock to real is seamless. First match wins.
_SHAPES: tuple[dict[str, Any], ...] = (
    {
        "test": re.compile(r"\b(score|fit|route|lead|assign)\b", re.I),
        "name": "scoreAndRouteLead",
        "title": "Score & route a lead",
        "purpose": "Score a lead's fit and route hot ones to the right AE.",
        "inputs": ["lead"],
        "code": (
            "async function scoreAndRouteLead(lead) {\n"
            "  const fit = await nex.ai.score(lead, { rubric: 'ICP fit' });\n"
            "  if (fit >= 75) {\n"
            "    const ae = await crm.ownerFor(lead);\n"
            "    await crm.assign(lead, ae);\n"
            "    return `Fit ${fit} -> routed to ${ae.name}`;\n"
            "  }\n"
            "  return `Fit ${fit} -> left in the queue`;\n"
            "}"
        ),
    },
    {
        "test": re.compile(r"\b(summary|summar|pipeline|digest|weekly|report|recap)\b", re.I),
        "name": "weeklyPipelineSummary",
        "title": "Weekly pipeline summary",
        "purpose": "Summarize last week's pipeline movement into a glanceable recap.",
        "inputs": [],
        "code": (
            "async function weeklyPipelineSummary() {\n"
            "  const deals = await crm.deals({ since: '7d' });\n"
            "  const moved = deals.filter((d) => d.stageChanged);\n"
            "  return nex.ai.summarize(moved, { style: 'exec recap' });\n"
            "}"
        ),
    },
    {
        "test": re.compile(r"\b(draft|follow.?up|email|reply|outreach|nudge|stall)\b", re.I),
        "name": "draftFollowup",
        "title": "Draft a follow-up email",
        "purpose": "Draft a follow-up email for a stalled deal in the rep's voice.",
        "inputs": ["deal"],
        "code": (
            "async function draftFollowup(deal) {\n"
            "  const ctx = await crm.dealContext(deal);\n"
            "  return nex.ai.write('follow-up email', { context: ctx, tone: 'warm, brief' });\n"
            "}"
        ),
    },
)

_STOPWORDS = {
    "the", "a", "an", "my", "our", "when", "then", "and", "to", "for", "of", "on",
    "in", "with", "that", "this", "it", "new", "every", "each", "from", "into",
    "by", "at", "is", "are", "do", "i", "we", "want", "need", "should", "please",
    "can", "you",
}


def _camel(words: list[str]) -> str:
    return words[0] + "".join(w[:1].upper() + w[1:] for w in words[1:])


def author_tool_spec(description: str) -> dict[str, Any]:
    """Deterministically derive a create_tool spec from a described workflow —
    matches a known shape, else synthesizes a camelCase name + human title."""
    desc = description.strip()
    for shape in _SHAPES:
        if shape["test"].search(desc):
            return {k: shape[k] for k in ("name", "title", "purpose", "inputs", "code")}
    words = [w for w in re.sub(r"[^a-z0-9\s]", " ", desc.lower()).split() if w and w not in _STOPWORDS]
    name = _camel(words[:3]) if words else "runWorkflow"
    # Human title: drop a leading "When ... ," trigger, sentence-case the rest.
    lead = re.sub(r"^when\b[^,]*,\s*", "", desc, flags=re.I)
    title_words = lead.split()[:6]
    title = (" ".join(title_words)[:1].upper() + " ".join(title_words)[1:]).rstrip(".,;:") if title_words else name
    return {
        "name": name,
        "title": title,
        "purpose": desc[:1].upper() + desc[1:],
        "inputs": ["input"],
        "code": f'async function {name}(input) {{\n  // Nex scripted this from: "{desc}"\n  return nex.run(input);\n}}',
    }


class ToolAgent(Protocol):
    def build(self, message: str, app: str | None = None) -> ToolBuildResult:  # pragma: no cover - protocol
        ...


class StubToolAgent:
    """Deterministic tool agent: turns a described workflow into a tool by calling
    create_tool — no model key. Pure enough to carry a regression test, and the
    default so /tools/build is real end to end without inference."""

    def __init__(self) -> None:
        self.tools = ToolStore()
        self._create = make_create_tool(self.tools)

    def build(self, message: str, app: str | None = None) -> ToolBuildResult:
        spec = author_tool_spec(message)
        self._create(**spec)
        tool = self.tools.get(spec["name"])
        return ToolBuildResult(tool=tool, narration=f"Built {tool.title}.")


def tool_agent() -> ToolAgent:
    """Pick the tool agent: the deep agent's create_tool when a model key is set,
    else the deterministic stub. (S: the deep-agent path reuses DeepAgentBuildAgent's
    create_tool; until that streaming chat path lands, the stub is authoritative and
    the LLM path degrades to it.)"""
    return StubToolAgent()


def make_create_tool(store: ToolStore) -> Callable[..., str]:
    """Build the agent-facing `create_tool` tool, bound to `store`."""

    def create_tool(
        name: str,
        title: str,
        purpose: str,
        inputs: list[Any] | None = None,
        code: str = "",
    ) -> str:
        """Create a new tool for this app from a workflow the operator taught you,
        and register it so you can CALL it on a later turn. Use this whenever the
        operator describes a repeatable task you don't already have a tool for.

        Args:
          name: a short callable id in snake_case, e.g. "score_and_route_lead".
          title: a plain-language name a non-technical operator reads, e.g.
            "Score & route a lead".
          purpose: one line describing what running the tool does.
          inputs: the arguments the tool needs, as names (e.g. ["lead"]).
          code: the implementation you wrote for it (may be empty for now).

        Returns a confirmation string naming the tool you created.
        """
        tool = Tool(
            name=name.strip(),
            title=title.strip() or name.strip(),
            purpose=purpose.strip(),
            inputs=_coerce_inputs(inputs),
            code=code,
        )
        store.add(tool)
        return f"Created tool {tool.name} ({tool.title})"

    return create_tool
