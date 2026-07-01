"""The chat agent's create_tool tool: it registers a callable tool in the store.

Covers the tool the agent actually calls (make_create_tool), input coercion of the
loose shapes a model emits, and re-teaching updating in place. No model key needed —
the tool function is pure over an in-memory ToolStore.
"""

from __future__ import annotations

from harness.tools import ToolStore, make_create_tool
from harness.wire import Tool


def test_create_tool_registers_a_callable_tool():
    store = ToolStore()
    create_tool = make_create_tool(store)

    msg = create_tool(
        name="score_and_route_lead",
        title="Score & route a lead",
        purpose="Score a lead's fit and route hot ones to the right AE.",
        inputs=["lead"],
    )

    assert "score_and_route_lead" in msg
    tool = store.get("score_and_route_lead")
    assert isinstance(tool, Tool)
    assert tool.title == "Score & route a lead"
    assert [i.name for i in tool.inputs] == ["lead"]
    assert store.list() == [tool]


def test_create_tool_coerces_loose_input_shapes():
    store = ToolStore()
    create_tool = make_create_tool(store)
    create_tool(
        name="draft_followup",
        title="Draft a follow-up",
        purpose="Draft a follow-up email for a stalled deal.",
        inputs=["deal", {"name": "tone", "type": "string"}, 123],  # 123 is skipped
    )
    tool = store.get("draft_followup")
    assert [(i.name, i.type) for i in tool.inputs] == [
        ("deal", "string"),
        ("tone", "string"),
    ]


def test_reteaching_updates_the_tool_in_place():
    store = ToolStore()
    create_tool = make_create_tool(store)
    create_tool(name="daily_digest", title="Digest", purpose="v1")
    create_tool(name="daily_digest", title="Digest", purpose="v2 — sharper")
    assert len(store.list()) == 1
    assert store.get("daily_digest").purpose == "v2 — sharper"


def test_title_falls_back_to_name_when_blank():
    store = ToolStore()
    make_create_tool(store)(name="do_thing", title="  ", purpose="does the thing")
    assert store.get("do_thing").title == "do_thing"
