"""Live inner-harness check (Tier 1): drive a REAL Claude turn through
ClaudeAgentHarness and classify it. No broker, no MCP — proves the SDK seam
(build_harness -> run_turn -> transcript -> classify_outcome) works against a live
model. Requires claude-agent-sdk installed and Claude Code authenticated.

Run:  .venv/bin/python scripts/live_harness_check.py
"""

from __future__ import annotations

import sys

from orchestrator.harness import ClaudeAgentHarness, FakeHarness, build_harness
from orchestrator.lifecycle import State, TurnOutcome


def main() -> int:
    harness = build_harness(model="", mcp={})
    if isinstance(harness, FakeHarness):
        print("FAIL: build_harness returned FakeHarness — claude-agent-sdk not installed")
        return 1
    assert isinstance(harness, ClaudeAgentHarness)
    print(f"harness = {type(harness).__name__} (SDK live)")

    # A PLANNING turn with no team_task tool available: the agent produces a plan,
    # calls no terminal action, so the pure classifier should return PLAN_READY.
    task = {
        "task_id": "live-1",
        "lifecycle_state": State.PLANNING.value,
        "messages": [{
            "role": "user",
            "content": (
                "In 3 lines or fewer, outline a minimal plan to add a /healthz "
                "endpoint to a Go HTTP server. Do not write code or edit files."
            ),
        }],
    }

    print("dispatching a real Claude turn (planning, read-only)...")
    result = harness.run_turn(task)

    print("--- turn text (first 500 chars) ---")
    print((result.text or "")[:500])
    print("--- outcome ---")
    print(f"outcome = {result.outcome.value}")

    ok = result.outcome is TurnOutcome.PLAN_READY and bool((result.text or "").strip())
    print("RESULT:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
