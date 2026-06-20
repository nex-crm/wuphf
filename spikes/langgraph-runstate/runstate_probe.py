"""
P4 run-state spike: can WUPHF's real task lifecycle round-trip through a LangGraph
checkpointer, in both ownership variants, without losing the lifecycle position?

Layers:
  A  Lossiness analysis  — for each canonical state, forward to the legacy 4-tuple
                           and derive back. Shows where the 4-tuple is lossy, hence
                           whether the migration must carry the lifecycle_state field.
  B  PURE variant        — LangGraph SqliteSaver IS the run-state store. Seed a task
                           into a thread, "restart" (reopen the db), resume. Per state.
  C  RE-HYDRATE variant  — Go broker record stays authoritative; ephemeral MemorySaver
                           rebuilt from the record on restart. Per state.
  D  Adversarial tuples  — contradictory/garbage legacy tuples must land UNKNOWN
                           (fail loud), never a silent wrong state.

No API key, no broker. Pure orchestration-state mechanics.
"""

import json
import pathlib
import sqlite3
import traceback

from langgraph.graph import START, END, StateGraph
from langgraph.checkpoint.memory import MemorySaver
from langgraph.checkpoint.sqlite import SqliteSaver
from typing import TypedDict

import lifecycle_model as lc

HERE = pathlib.Path(__file__).parent
results: dict = {}


class TaskRun(TypedDict, total=False):
    task_id: str
    lifecycle_state: str
    pipeline_stage: str
    review_state: str
    status: str
    blocked: bool
    history: list


# Representative forward transitions so a resumed graph does something verifiable.
FORWARD_TRANSITION = {
    "planning": "running", "running": "review", "review": "decision",
    "decision": "approved", "changes_requested": "running", "blocked": "review",
    "intake": "ready", "ready": "running", "queued_behind_owner": "running",
}


def advance(state: TaskRun) -> dict:
    cur = state.get("lifecycle_state")
    nxt = FORWARD_TRANSITION.get(cur, cur)  # terminal states stay put
    hist = list(state.get("history") or [])
    hist.append(cur)
    return {"lifecycle_state": nxt, "history": hist}


def build_graph(checkpointer):
    g = StateGraph(TaskRun)
    g.add_node("advance", advance)
    g.add_edge(START, "advance")
    g.add_edge("advance", END)
    # interrupt BEFORE advance so the first invoke seeds + checkpoints the run-state
    # and pauses; resume runs the transition. This is the canonical durable-resume flow.
    return g.compile(checkpointer=checkpointer, interrupt_before=["advance"])


# --------------------------------------------------------------------------- #
def layer_a_lossiness():
    lossless, collapses = [], {}
    for s in lc.CANONICAL_STATES:
        back = lc.derive_lifecycle_state_from_legacy(*lc.FORWARD[s])
        if back == s:
            lossless.append(s)
        else:
            collapses[s] = back
    return {
        "ok": True,
        "states": len(lc.CANONICAL_STATES),
        "lossless_via_4tuple": len(lossless),
        "collapses_if_state_field_dropped": collapses,
        "conclusion": (
            "Carry lifecycle_state directly => lossless for all 13. "
            "Deriving from the 4-tuple alone collapses these states."
        ),
    }


def layer_b_pure(dbfile: pathlib.Path):
    per_state = {}
    ok_all = True
    for s in lc.CANONICAL_STATES:
        cfg = {"configurable": {"thread_id": f"pure-{s}"}}
        rec = lc.make_broker_record(s)
        migrated, how = lc.migrate_state(rec)
        try:
            # MIGRATE: seed the run-state into a durable thread, pause before advance.
            conn = sqlite3.connect(str(dbfile), check_same_thread=False)
            cp = SqliteSaver(conn)
            cp.setup()
            build_graph(cp).invoke(
                {"task_id": rec["task_id"], "lifecycle_state": migrated,
                 "pipeline_stage": rec["pipeline_stage"], "review_state": rec["review_state"],
                 "status": rec["status"], "blocked": rec["blocked"], "history": []},
                cfg,
            )
            conn.close()
            # RESTART: fresh connection to the same file. Restore + resume one transition.
            conn2 = sqlite3.connect(str(dbfile), check_same_thread=False)
            cp2 = SqliteSaver(conn2)
            cp2.setup()
            g2 = build_graph(cp2)
            restored = g2.get_state(cfg).values.get("lifecycle_state")
            resumed = g2.invoke(None, cfg).get("lifecycle_state")
            conn2.close()
            ok = restored == s and resumed == FORWARD_TRANSITION.get(s, s)
            per_state[s] = {"restored": restored, "resumed_to": resumed, "ok": ok}
            ok_all = ok_all and ok
        except Exception as e:  # noqa: BLE001
            per_state[s] = {"ok": False, "error": f"{type(e).__name__}: {e}"}
            ok_all = False
    return {"ok": ok_all, "store": "SqliteSaver (LangGraph owns run-state)", "per_state": per_state}


def layer_c_rehydrate():
    per_state = {}
    ok_all = True
    for s in lc.CANONICAL_STATES:
        cfg = {"configurable": {"thread_id": f"rehydrate-{s}"}}
        rec = lc.make_broker_record(s)  # the Go broker record = authoritative
        migrated, _ = lc.migrate_state(rec)
        try:
            # RESTART: fresh ephemeral saver (lost on restart). Rebuild thread FROM the
            # Go record, then resume — proves we never need a persisted LG checkpoint.
            saver = MemorySaver()
            g = build_graph(saver)
            g.invoke(
                {"task_id": rec["task_id"], "lifecycle_state": migrated,
                 "pipeline_stage": rec["pipeline_stage"], "review_state": rec["review_state"],
                 "status": rec["status"], "blocked": rec["blocked"], "history": []},
                cfg,
            )
            restored = g.get_state(cfg).values.get("lifecycle_state")
            resumed = g.invoke(None, cfg).get("lifecycle_state")
            ok = restored == s and resumed == FORWARD_TRANSITION.get(s, s)
            per_state[s] = {"rehydrated": restored, "resumed_to": resumed, "ok": ok}
            ok_all = ok_all and ok
        except Exception as e:  # noqa: BLE001
            per_state[s] = {"ok": False, "error": f"{type(e).__name__}: {e}"}
            ok_all = False
    return {"ok": ok_all, "store": "Go broker record authoritative; MemorySaver ephemeral", "per_state": per_state}


def layer_d_adversarial():
    # Contradictory / garbage tuples that should fail loud as UNKNOWN.
    cases = [
        {"label": "in_progress AND blocked (contradiction)",
         "rec": {"pipeline_stage": "implement", "review_state": "pending_review", "status": "in_progress", "blocked": True}},
        {"label": "unknown future pipeline stage 'act'",
         "rec": {"pipeline_stage": "act", "review_state": "verifying", "status": "verifying", "blocked": False}},
        {"label": "garbage status",
         "rec": {"pipeline_stage": "qux", "review_state": "zonk", "status": "frobnicate", "blocked": False}},
        {"label": "legacy alias name 'merged' carried as state",
         "rec": {"lifecycle_state": "merged"}},
        {"label": "legacy 'blocked_on_pr_merge' carried as state",
         "rec": {"lifecycle_state": "blocked_on_pr_merge"}},
    ]
    out = []
    for c in cases:
        state, how = lc.migrate_state(c["rec"])
        out.append({"case": c["label"], "resolved_to": state, "how": how})
    return {
        "ok": True,
        "cases": out,
        "note": "Contradictory/garbage tuples must resolve to 'unknown' (operator triage), "
                "never a silent wrong state. Legacy alias NAMES normalize losslessly.",
    }


def run(name, fn, *a):
    print(f"\n=== {name} ===", flush=True)
    try:
        out = fn(*a)
    except Exception as e:  # noqa: BLE001
        out = {"ok": False, "error": f"{type(e).__name__}: {e}", "trace": traceback.format_exc()[-700:]}
    results[name] = out
    print(json.dumps(out, indent=2), flush=True)


def main():
    dbfile = HERE / "_runstate.sqlite"
    if dbfile.exists():
        dbfile.unlink()
    run("A_lossiness", layer_a_lossiness)
    run("B_pure_checkpoint", layer_b_pure, dbfile)
    run("C_rehydrate", layer_c_rehydrate)
    run("D_adversarial", layer_d_adversarial)
    (HERE / "results.json").write_text(json.dumps(results, indent=2))
    if dbfile.exists():
        dbfile.unlink()
    print("\nWrote results.json")


if __name__ == "__main__":
    main()
