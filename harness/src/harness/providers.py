"""Provider detection + BYOK — salvaged from the wuphf provider config concept.

The harness is multi-provider: it detects which inference backends are usable from
the environment (an API key, or an installed CLI like Claude Code) so the FE
Settings surface can show what's connected and prompt BYOK for the rest. No keys
are ever returned — only whether each provider is available.
"""

from __future__ import annotations

import os
import shutil
from dataclasses import dataclass
from typing import Literal


# How a provider is reachable. Mirrors agent/src/providers.ts Provider["via"].
Via = Literal["subscription_cli", "api_key", "local", "none"]


@dataclass(frozen=True)
class Provider:
    id: str
    label: str
    available: bool
    via: Via  # "subscription_cli" | "api_key" | "local" | "none"


def _env_any(*names: str) -> bool:
    return any(os.getenv(n, "").strip() for n in names)


def detect_providers() -> list[Provider]:
    """Which inference providers are usable right now. Always returns exactly the
    same three rows (anthropic, codex, ollama) so the FE Settings surface can render
    a stable list; availability/via reflect a key (BYOK, never stored), a logged-in
    subscription CLI, or a local runtime. Matches agent/src/providers.ts."""
    if _env_any("ANTHROPIC_API_KEY"):
        anthropic = Provider("anthropic", "Anthropic API", True, "api_key")
    elif shutil.which("claude"):
        anthropic = Provider("anthropic", "Claude Code (subscription)", True, "subscription_cli")
    else:
        anthropic = Provider("anthropic", "Anthropic", False, "none")

    if _env_any("OPENAI_API_KEY", "WUPHF_OPENAI_API_KEY"):
        codex = Provider("codex", "OpenAI / Codex", True, "api_key")
    elif shutil.which("codex"):
        codex = Provider("codex", "OpenAI / Codex (subscription)", True, "subscription_cli")
    else:
        codex = Provider("codex", "OpenAI / Codex", False, "none")

    if shutil.which("ollama"):
        ollama = Provider("ollama", "Ollama (local / open-weight)", True, "local")
    else:
        ollama = Provider("ollama", "Ollama (local / open-weight)", False, "none")

    return [anthropic, codex, ollama]


def providers_payload() -> dict:
    provs = detect_providers()
    return {
        "providers": [p.__dict__ for p in provs],
        "any_available": any(p.available for p in provs),
    }
