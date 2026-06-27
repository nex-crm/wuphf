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


@dataclass(frozen=True)
class Provider:
    id: str
    label: str
    available: bool
    via: str  # how it's available: "api_key" | "cli" | "none"


def _env_any(*names: str) -> bool:
    return any(os.getenv(n, "").strip() for n in names)


def detect_providers() -> list[Provider]:
    """Which inference providers are usable right now. BYOK = set the key (never
    stored by the harness) or install the CLI."""
    out: list[Provider] = []

    if _env_any("ANTHROPIC_API_KEY"):
        out.append(Provider("anthropic", "Anthropic API", True, "api_key"))
    elif shutil.which("claude"):
        out.append(Provider("anthropic", "Claude Code (CLI auth)", True, "cli"))
    else:
        out.append(Provider("anthropic", "Anthropic", False, "none"))

    out.append(Provider("openai", "OpenAI API", _env_any("OPENAI_API_KEY", "WUPHF_OPENAI_API_KEY"), "api_key" if _env_any("OPENAI_API_KEY", "WUPHF_OPENAI_API_KEY") else "none"))

    if shutil.which("codex"):
        out.append(Provider("codex", "Codex (CLI)", True, "cli"))

    return out


def providers_payload() -> dict:
    provs = detect_providers()
    return {
        "providers": [p.__dict__ for p in provs],
        "any_available": any(p.available for p in provs),
    }
