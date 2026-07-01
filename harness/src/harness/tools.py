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

from collections.abc import Callable
from typing import Any

from .wire import Tool, ToolInput


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
