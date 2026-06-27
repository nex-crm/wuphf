"""wuphf operator harness.

Agentic-build / deterministic-execute. The BUILD agent (deepagents) turns an
operator's plain-language description into a deterministic WorkflowSpec; the
EXECUTE path runs that spec deterministically. The FE talks to it over a thin
HTTP/SSE API (service.py). No broker, no office, no multi-agent coordination.

See docs/specs/operator-harness-clean-start.md.
"""

from . import build_agent, executor, providers, service, wire

__all__ = ["build_agent", "executor", "providers", "service", "wire"]
