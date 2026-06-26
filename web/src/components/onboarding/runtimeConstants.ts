/**
 * Shared runtime configuration constants for the pre-office provider picker.
 *
 * These were previously split between wizard/constants.ts (deleted in Phase 5
 * cleanup) and runtimes.ts (CLI runtimes only). This file exports the API-key,
 * local-provider, and sanitization helpers so PrePickScreen and its
 * sub-components can import from one place.
 */

// API_KEY_FIELDS: each cloud provider has two valid auth paths -- CLI login
// (e.g. `claude login`) or pasting a raw API key. The row component defaults
// to the CLI path; the user opts into paste mode explicitly.
export const API_KEY_FIELDS = [
  {
    key: "ANTHROPIC_API_KEY" as const,
    label: "Anthropic",
    hint: "Powers Claude-based agents",
    cliLoginCmd: "claude login",
    configField: "anthropic_api_key" as const,
  },
  {
    key: "OPENAI_API_KEY" as const,
    label: "OpenAI",
    hint: "Powers GPT-based agents",
    cliLoginCmd: "codex login",
    configField: "openai_api_key" as const,
  },
  {
    key: "GOOGLE_API_KEY" as const,
    label: "Google",
    hint: "Powers Gemini-based agents",
    cliLoginCmd: "gcloud auth application-default login",
    configField: "gemini_api_key" as const,
  },
] as const;

export type ApiKeyFieldDef = (typeof API_KEY_FIELDS)[number];

// LOCAL_PROVIDER_LABELS is shared between LocalProviderPicker and the
// PrePickScreen submit logic. Kept here to avoid three-way drift if a
// runtime is renamed or added.
//
// Gateway-style runtimes (Hermes Agent, OpenClaw Gateway) used to live here
// alongside the directly-dispatchable local LLMs. They were removed because
// they are gateways for importing existing agents into the team, not LLM
// runtimes for backing WUPHF-created agents. Surfacing them as runtime tiles
// confused users about whether picking "OpenClaw" meant "host my agents on
// OpenClaw" or "import OpenClaw agents into the team." The Integrations app
// (Settings → Integrations) is now the single place gateways are configured.
export const LOCAL_PROVIDER_LABELS = [
  { kind: "mlx-lm" as const, label: "MLX-LM", blurb: "macOS / Apple Silicon" },
  { kind: "ollama" as const, label: "Ollama", blurb: "macOS / Linux" },
  { kind: "exo" as const, label: "Exo", blurb: "Multi-device pool" },
] as const;

export type LocalProviderMeta = (typeof LOCAL_PROVIDER_LABELS)[number];

// CTRL_CHARS matches C0 (0x00-0x1F), DEL (0x7F), and C1 (0x80-0x9F).
// biome-ignore lint/suspicious/noControlCharactersInRegex: intentional -- we need to match and strip these characters.
const CTRL_CHARS = /[\x00-\x1F\x7F-\x9F]/g;

/**
 * Strips ASCII and C1 control characters from a user-supplied string before
 * writing it to POST /config. Preserves printable ASCII, Unicode, and all
 * chars that legitimately appear in API keys and endpoint URLs.
 *
 * This is the minimum sanitizer required by the security hard rule:
 * "any user-controlled string flowing into config goes through a sanitizer
 * that strips control characters at minimum."
 */
export function sanitizeConfigString(value: string): string {
  return value.replace(CTRL_CHARS, "");
}
