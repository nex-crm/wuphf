"""A tiny stdio MCP server exposing a `team_task` tool, standing in for the
broker's teammcp during a live service check (no broker needed). It records each
call to STUB_CALLS_FILE (one JSON object per line) so the runner can verify what
the real agent did.
"""

from __future__ import annotations

import json
import os

from mcp.server.fastmcp import FastMCP

mcp = FastMCP("office")
_CALLS_FILE = os.environ.get("STUB_CALLS_FILE", "")


@mcp.tool()
def team_task(action: str, title: str = "", depends_on: list[str] | None = None) -> str:
    """Create or update a team task. action='create' makes a subtask;
    action='submit_for_review' submits the current task; action='complete' finishes it."""
    rec = {"action": action, "title": title, "depends_on": depends_on or []}
    if _CALLS_FILE:
        with open(_CALLS_FILE, "a", encoding="utf-8") as fh:
            fh.write(json.dumps(rec) + "\n")
    return f"ok: {action} {title}".strip()


if __name__ == "__main__":
    mcp.run()
