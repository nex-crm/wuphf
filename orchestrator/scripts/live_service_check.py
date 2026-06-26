"""Live service check (Tier 3): drive the REAL orchestrator service (/run) with the
REAL ClaudeAgentHarness and a REAL Claude agent, over the real ASGI app. The agent
reaches a `team_task` tool via a stdio MCP stub (scripts/stub_team_task_mcp.py)
wired exactly as the broker would (McpServer command/args + env-NAME passthrough).

Two turns:
  A. submit  -> agent calls team_task action=submit_for_review -> classify
     SUBMITTED -> human gate -> projection lifecycle_state=review, status=interrupted.
  B. decompose -> agent calls team_task action=create twice -> classify DECOMPOSED
     (non-gated) -> projection lifecycle_state=running, status=done; stub recorded
     two create calls.

This proves the full service path the Go broker drives, end to end, live. Run:
  PYTHONPATH=src .venv/bin/python scripts/live_service_check.py
"""

from __future__ import annotations

import json
import os
import sys
import tempfile

from fastapi.testclient import TestClient

from orchestrator.service import create_app

_STUB = os.path.join(os.path.dirname(__file__), "stub_team_task_mcp.py")
_PY = sys.executable


def _mcp() -> dict:
    return {
        "office": {
            "command": _PY,
            "args": [_STUB],
            "env_passthrough": ["STUB_CALLS_FILE"],
        }
    }


def _run(client, *, task_id, lifecycle_state, prompt) -> dict:
    return client.post("/run", json={
        "schema_version": 1,
        "task_id": task_id,
        "record": {"task_id": task_id, "lifecycle_state": lifecycle_state},
        "messages": [{"role": "user", "content": prompt}],
        "mcp": _mcp(),
    }).json()


def main() -> int:
    calls_path = tempfile.mktemp(prefix="stub_calls_", suffix=".jsonl")
    os.environ["STUB_CALLS_FILE"] = calls_path
    client = TestClient(create_app())

    ok = True

    # --- Turn A: submit_for_review -> gate -> review/interrupted ---------------- #
    print("== Turn A: submit_for_review ==")
    a = _run(
        client,
        task_id="svc-live-submit",
        lifecycle_state="running",
        prompt=(
            "Your work is done. Submit it for review by calling the team_task tool "
            "with action='submit_for_review'. Do not edit files. Then stop."
        ),
    )
    print("  status     =", a.get("status"))
    print("  projection =", {k: a.get("projection", {}).get(k) for k in ("lifecycle_state", "status")})
    a_ok = a.get("status") == "interrupted" and a.get("projection", {}).get("lifecycle_state") == "review"
    print("  Turn A:", "PASS" if a_ok else "FAIL")
    ok = ok and a_ok

    # --- Turn B: decompose -> create x2 -> running/done ------------------------- #
    print("== Turn B: decompose (create two subtasks) ==")
    open(calls_path, "w").close()  # reset recorder
    b = _run(
        client,
        task_id="svc-live-decompose",
        lifecycle_state="running",
        prompt=(
            "Decompose this goal into two ordered subtasks by calling the team_task "
            "tool twice: action='create' title='scaffold endpoint', then "
            "action='create' title='write tests' depends_on=['scaffold endpoint']. "
            "Do not edit files. Then stop."
        ),
    )
    creates = []
    if os.path.exists(calls_path):
        with open(calls_path, encoding="utf-8") as fh:
            creates = [json.loads(line) for line in fh if line.strip()]
    create_actions = [c for c in creates if c.get("action") == "create"]
    print("  status     =", b.get("status"))
    print("  projection =", {k: b.get("projection", {}).get(k) for k in ("lifecycle_state", "status")})
    print("  stub create calls =", [c.get("title") for c in create_actions])
    b_ok = (
        b.get("status") == "done"
        and b.get("projection", {}).get("lifecycle_state") == "running"
        and len(create_actions) >= 2
    )
    print("  Turn B:", "PASS" if b_ok else "FAIL")
    ok = ok and b_ok

    print("RESULT:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
