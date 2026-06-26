"""Live decompose check (Tier 2): drive a REAL Claude turn that calls a team_task
tool to CREATE child tasks, through the harness's OWN transcript collection, then
run the PRODUCTION classifier. Proves classify_outcome / decomposed_children work
against real agent tool calls (not hand-built ToolCalls).

The team_task tool is an in-process SDK MCP stub (records calls, returns ok) — no
broker needed, but the agent's real tool calls flow through the exact production
path: ClaudeAgentHarness._collect_transcript -> TurnTranscript -> classify_outcome.

Run:  PYTHONPATH=src .venv/bin/python scripts/live_decompose_check.py
"""

from __future__ import annotations

import asyncio
import sys

from claude_agent_sdk import ClaudeAgentOptions, create_sdk_mcp_server, tool

from orchestrator.harness import ClaudeAgentHarness
from orchestrator.lifecycle import State, TurnOutcome

_CALLS: list[dict] = []


@tool(
    "team_task",
    "Create or update a team task. Use action='create' with a title to create a subtask.",
    {"action": str, "title": str, "depends_on": list},
)
async def team_task(args: dict) -> dict:
    _CALLS.append(args)
    return {"content": [{"type": "text", "text": f"ok: {args.get('action')} {args.get('title','')}"}]}


async def collect() -> object:
    server = create_sdk_mcp_server("office", tools=[team_task])
    options = ClaudeAgentOptions(
        mcp_servers={"office": server},
        allowed_tools=["mcp__office__team_task"],
        permission_mode="acceptEdits",
        max_turns=6,
    )
    harness = ClaudeAgentHarness(model="", mcp={})  # mcp unused; we pass options directly
    prompt = (
        "You are decomposing a goal into two ordered subtasks. Use the team_task "
        "tool TWICE: first action='create', title='scaffold the /healthz endpoint'; "
        "then action='create', title='add an httptest for /healthz', "
        "depends_on=['scaffold the /healthz endpoint']. Call the tool, do not write "
        "code or other files, then stop."
    )
    return await harness._collect_transcript(prompt, options)


def main() -> int:
    transcript = asyncio.run(collect())
    print(f"raw tool calls recorded by the stub: {len(_CALLS)}")
    for c in _CALLS:
        print("  ", {k: c.get(k) for k in ("action", "title", "depends_on")})

    actions = transcript.team_task_actions()
    outcome = classify(transcript)
    specs = transcript.decomposed_children()
    print(f"team_task actions seen by classifier: {sorted(actions)}")
    print(f"classified outcome: {outcome.value}")
    print(f"decomposed children: {[(s.title, s.depends_on) for s in specs]}")

    ok = (
        "create" in actions
        and outcome is TurnOutcome.DECOMPOSED
        and len(specs) >= 2
        and all(s.title for s in specs)
    )
    print("RESULT:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


def classify(transcript) -> TurnOutcome:
    from orchestrator.harness import classify_outcome

    # A decompose turn is dispatched while the goal is RUNNING.
    return classify_outcome(State.RUNNING, transcript)


if __name__ == "__main__":
    sys.exit(main())
