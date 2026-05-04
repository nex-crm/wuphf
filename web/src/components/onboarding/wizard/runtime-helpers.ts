import { LOCAL_PROVIDER_LABELS, RUNTIMES } from "./constants";
import type { PrereqResult } from "./types";

// Find the prereq detection record for a binary (e.g. "claude", "codex").
// Pure helper used by both runtimeIsReady and the SetupStep tile renderer.
export function detectedBinary(
  prereqs: PrereqResult[],
  binary: string,
): PrereqResult | undefined {
  return prereqs.find((p) => p.name === binary);
}

// runtimeIsReady centralizes the "should this runtime label count as a
// configured LLM?" predicate used at the SetupStep gate, the keyboard
// gate (Wizard's ⌘+Enter handler), and the ReadyStep summary. Three call
// sites had drifted apart once already (see PR #367 review) — keeping
// the rule in one place stops the next drift in its tracks.
//
// Rules:
//   - Unknown labels (not in RUNTIMES) never count.
//   - Runtimes with provider:null (Cursor/Windsurf) NEVER count, in
//     either branch. finishOnboarding silently drops them from
//     providerPriority, so a Cursor-only selection would let the gate
//     pass and /config would persist no llm_provider.
//   - With a non-null provider AND prereqs detection succeeded, the
//     runtime's binary must be on PATH (detection.found).
//   - With a non-null provider AND prereqs detection FAILED
//     (prereqsError truthy), trust the user's selection.
export function runtimeIsReady(
  label: string,
  prereqs: PrereqResult[],
  prereqsError: string,
): boolean {
  const spec = RUNTIMES.find((r) => r.label === label);
  if (!spec || spec.provider === null) return false;
  if (prereqsError) return true;
  return Boolean(detectedBinary(prereqs, spec.binary)?.found);
}

interface ProviderConfigSnapshot {
  llm_provider?: string;
  llm_provider_configured?: boolean;
  llm_provider_priority?: readonly string[];
}

// Convert the broker's persisted/effective provider config into wizard labels.
// The broker always returns an effective llm_provider, even when it is just
// the install default. Only trust llm_provider when the server says it came
// from env/config; otherwise let CLI auto-detection choose the first installed
// runtime so Codex-only machines do not get prefilled with Claude.
export function runtimeLabelsFromProviderConfig(
  config: ProviderConfigSnapshot,
): string[] {
  const providers = [
    ...(config.llm_provider_configured ? [config.llm_provider] : []),
    ...(config.llm_provider_priority ?? []),
  ];
  const labels: string[] = [];
  const seen = new Set<string>();
  for (const rawProvider of providers) {
    const provider = rawProvider?.trim().toLowerCase();
    if (!provider) continue;
    const label =
      RUNTIMES.find((runtime) => runtime.provider === provider)?.label ??
      LOCAL_PROVIDER_LABELS.find((runtime) => runtime.kind === provider)?.label;
    if (!label || seen.has(label)) continue;
    seen.add(label);
    labels.push(label);
  }
  return labels;
}

export function localProviderKindFromRuntimePriority(
  labels: readonly string[],
): string | null {
  for (const label of labels) {
    const kind = LOCAL_PROVIDER_LABELS.find(
      (meta) => meta.label === label,
    )?.kind;
    if (kind) return kind;
  }
  return null;
}

interface SetupContinueInput {
  runtimePriority: string[];
  prereqs: PrereqResult[];
  prereqsError: string;
  apiKeys: Record<string, string>;
  localProvider: string;
}

export function canSetupContinue({
  runtimePriority,
  prereqs,
  prereqsError,
  apiKeys,
  localProvider,
}: SetupContinueInput): boolean {
  const hasInstalledSelection = runtimePriority.some((label) =>
    runtimeIsReady(label, prereqs, prereqsError),
  );
  const hasAnyApiKey = Object.values(apiKeys).some((v) => v.trim().length > 0);
  const hasLocalProvider = localProvider.trim().length > 0;
  return hasInstalledSelection || hasAnyApiKey || hasLocalProvider;
}
