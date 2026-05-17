import type { OfficeMember } from "../api/client";

export type HarnessKind =
  | "claude-code"
  | "codex"
  | "opencode"
  | "openclaw"
  | "hermes-agent";

// `openclaw-http` is the OpenAI-compat transport for OpenClaw; treat it as the
// same harness visually as the WebSocket bridge so users see one OpenClaw
// identity. Hermes is its own harness.
const VALID_KINDS: Record<string, HarnessKind> = {
  "claude-code": "claude-code",
  claude: "claude-code",
  codex: "codex",
  opencode: "opencode",
  openclaw: "openclaw",
  "openclaw-http": "openclaw",
  "hermes-agent": "hermes-agent",
  hermes: "hermes-agent",
};

function normalize(raw: string | undefined | null): HarnessKind | null {
  if (!raw) return null;
  return VALID_KINDS[raw.toLowerCase()] ?? null;
}

export function resolveHarness(
  provider: OfficeMember["provider"],
  fallback: HarnessKind = "claude-code",
): HarnessKind {
  if (typeof provider === "string") {
    return normalize(provider) ?? fallback;
  }
  if (provider && typeof provider === "object") {
    return normalize(provider.kind) ?? fallback;
  }
  return fallback;
}

export function harnessLabel(kind: HarnessKind): string {
  switch (kind) {
    case "claude-code":
      return "Claude Code";
    case "codex":
      return "Codex";
    case "opencode":
      return "Opencode";
    case "openclaw":
      return "OpenClaw";
    case "hermes-agent":
      return "Hermes";
  }
}
