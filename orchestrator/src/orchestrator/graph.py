"""The LangGraph orchestration graph — WUPHF's coordination, rebuilt.

One invocation = one orchestration step for a task that has been re-hydrated from
the Go record:

    START ─route─▶ dispatch_turn ─▶ (human_gate | apply_continue) ─▶ END
              └──▶ human_gate (already awaiting a decision)
              └──▶ END (idle: nothing to do this tick)

dispatch_turn calls the inner harness (Claude Code/Codex — untouched). Human gates
use LangGraph interrupt(), which the Go broker surfaces through its existing approval
surface and resolves via Command(resume=decision).
"""

from __future__ import annotations

from langgraph.checkpoint.memory import MemorySaver
from langgraph.graph import END, START, StateGraph
from langgraph.types import Command, interrupt

from . import lifecycle as lc
from .harness import Harness
from .runstate import TaskRun


def _route_entry(state: TaskRun) -> str:
    return lc.route(lc.State(state["lifecycle_state"])).value  # dispatch | human | idle


def _dispatch_turn(state: TaskRun, harness: Harness) -> dict:
    res = harness.run_turn(state)
    cur = lc.State(state["lifecycle_state"])
    out: dict = {
        "last_outcome": res.outcome.value,
        "last_text": res.text,
        "history": list(state.get("history") or []) + [state["lifecycle_state"]],
    }
    gate = lc.gate_for_outcome(cur, res.outcome)
    if gate is not None:
        out["gate_kind"] = gate.value
    return out


def _after_dispatch(state: TaskRun) -> str:
    cur = lc.State(state["lifecycle_state"])
    outcome = lc.TurnOutcome(state["last_outcome"])
    return "enter_gate" if lc.gate_for_outcome(cur, outcome) is not None else "continue"


def _enter_gate(state: TaskRun) -> dict:
    """Commit the pending human-gate lifecycle state BEFORE the interrupt suspends
    the run. This is load-bearing for the re-hydrate path: `drive` reads the
    checkpoint after the interrupt, so the projection must already reflect the gate
    state (review/decision) — otherwise the broker sees the pre-gate executable
    state (running/planning), never surfaces the gate, and re-dispatches forever.
    A node only commits its channel update when it RETURNS; `interrupt()` raises, so
    the state write has to happen in a separate node that completes first."""
    gate = lc.GateKind(state.get("gate_kind") or lc.GateKind.REVIEW.value)
    return {"lifecycle_state": lc.gate_pending_state(gate).value}


def _human_gate(state: TaskRun) -> dict:
    gate = lc.GateKind(state.get("gate_kind") or lc.GateKind.REVIEW.value)
    decision = interrupt(
        {
            "type": "approval_required",
            "task_id": state.get("task_id", ""),
            "gate_kind": gate.value,
            "text": state.get("last_text", ""),
        }
    )
    dec = decision.get("decision") if isinstance(decision, dict) else decision
    new_state = lc.apply_human_decision(gate, lc.HumanDecision(dec))
    return {"lifecycle_state": new_state.value}


def _apply_continue(state: TaskRun) -> dict:
    cur = lc.State(state["lifecycle_state"])
    outcome = lc.TurnOutcome(state["last_outcome"])
    return {"lifecycle_state": lc.apply_turn_outcome(cur, outcome).value}


def build_graph(harness: Harness, checkpointer=None):
    g = StateGraph(TaskRun)
    g.add_node("dispatch_turn", lambda s: _dispatch_turn(s, harness))
    g.add_node("enter_gate", _enter_gate)
    g.add_node("human_gate", _human_gate)
    g.add_node("apply_continue", _apply_continue)
    g.add_conditional_edges(
        START, _route_entry, {"dispatch": "dispatch_turn", "human": "human_gate", "idle": END}
    )
    g.add_conditional_edges(
        "dispatch_turn", _after_dispatch, {"enter_gate": "enter_gate", "continue": "apply_continue"}
    )
    g.add_edge("enter_gate", "human_gate")
    g.add_edge("human_gate", END)
    g.add_edge("apply_continue", END)
    return g.compile(checkpointer=checkpointer or MemorySaver())


def _collect_interrupts(snapshot) -> list:
    out = []
    for tsk in (snapshot.tasks or ()):
        for it in (getattr(tsk, "interrupts", ()) or ()):
            out.append(it.value)
    return out


def drive(graph, run: TaskRun | None, thread_id: str, resume=None) -> dict:
    """Run one orchestration step (resume=None) or resume a human gate.

    Returns {status: 'done'|'interrupted', state: <values>, interrupt?: <payload>}.
    'interrupted' means a human gate is pending; the broker should surface it and
    call drive(..., resume=<decision>) once the human decides.
    """
    cfg = {"configurable": {"thread_id": thread_id}}
    graph.invoke(Command(resume=resume) if resume is not None else run, cfg)
    snap = graph.get_state(cfg)
    interrupts = _collect_interrupts(snap)
    if interrupts:
        return {"status": "interrupted", "interrupt": interrupts[0], "state": dict(snap.values)}
    return {"status": "done", "state": dict(snap.values)}
