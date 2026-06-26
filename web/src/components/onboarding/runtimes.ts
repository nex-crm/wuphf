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
  /**
   * True when the broker ran the runtime's session-status subcommand
   * (claude auth status, codex login status, opencode providers list).
   * False means "no probe wired" — frontend should fall back to legacy
   * "Detected" behavior. See issue #932.
   */
  session_probed?: boolean;
  /**
   * True only when session_probed === true AND the runtime reports an
   * active auth session.
   */
  signed_in?: boolean;
  /**
   * Suggested shell command to copy to clipboard when session_probed
   * is true and signed_in is false (the "Sign in" CTA).
   */
  sign_in_command?: string;
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
