"""MCP tool wiring — env-NAME->value config, salvaged from the dead stack's
hardest-won lesson.

Tools the BUILD agent reaches (gbrain, browsersniff, Composio, browser-capture)
are MCP servers. Secrets cross as env-var NAMES only; the harness resolves them to
values from its OWN environment when it launches each server — never on any wire.
And every wired server's tools must be ALLOWED, or the agent's calls are silently
permission-denied (the bug the live run caught: the call shows in the transcript
but the tool body never runs). Both rules live here so S2 reuses them verbatim.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass(frozen=True)
class McpServer:
    command: str
    args: list[str] = field(default_factory=list)
    env_passthrough: list[str] = field(default_factory=list)  # env var NAMES, never values


def resolve_env(server: McpServer, environ: dict | None = None) -> dict[str, str]:
    """Resolve the server's passthrough NAMES to values from the harness env. The
    values are injected into the launched MCP subprocess, never returned on a wire."""
    env = environ if environ is not None else os.environ
    # Omit names that are unset: injecting a blank value masks a real, inherited
    # credential and makes "missing secret" look like "empty secret" to the server.
    return {name: env[name] for name in server.env_passthrough if name in env}


def allowed_tools(servers: dict[str, McpServer]) -> list[str]:
    """Allow every tool from each wired server (server-prefix grant). Without this
    the agent's MCP calls are permission-denied — surfaced live in the dead stack."""
    return [f"mcp__{name}" for name in servers]
