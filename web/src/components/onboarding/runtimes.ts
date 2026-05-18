/**
 * Runtime constants and types for the pre-office provider picker.
 *
 * These are the types and constants PrePickScreen needs after the wizard
 * directory is removed. The full wizard constants lived in wizard/constants.ts;
 * only the RUNTIMES table and the two types it depends on are kept here.
 */

// Each runtime has a display label, the binary name the broker's prereqs
// check looks for, a canonical install page to link to when missing, and
// — for the runtimes the broker can actually dispatch agents to — the
// provider id the broker expects on POST /config.
export interface RuntimeSpec {
  label: string;
  binary: string;
  installUrl: string;
  provider: "claude-code" | "codex" | "opencode" | null;
}

export interface PrereqResult {
  name: string;
  required: boolean;
  found: boolean;
  ok?: boolean;
  version?: string;
  install_url?: string;
}

export const RUNTIMES: readonly RuntimeSpec[] = [
  {
    label: "Claude Code",
    binary: "claude",
    installUrl: "https://claude.ai/code",
    provider: "claude-code",
  },
  {
    label: "Codex",
    binary: "codex",
    installUrl: "https://github.com/openai/codex",
    provider: "codex",
  },
  {
    label: "Opencode",
    binary: "opencode",
    installUrl: "https://opencode.ai",
    provider: "opencode",
  },
  {
    label: "Cursor",
    binary: "cursor",
    installUrl: "https://cursor.com/",
    provider: null,
  },
  {
    label: "Windsurf",
    binary: "windsurf",
    installUrl: "https://codeium.com/windsurf",
    provider: null,
  },
] as const;
