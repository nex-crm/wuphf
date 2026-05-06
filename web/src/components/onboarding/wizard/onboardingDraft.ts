// onboardingDraft.ts — local persistence for the onboarding wizard so a
// page refresh restores wizard progress instead of wiping it. Secrets
// (API keys) NEVER persist; only the non-sensitive shape below does.
//
// Storage shape is versioned. A version mismatch — the schema bumped but
// the user has an older draft — drops the draft cleanly rather than
// crashing on missing fields. Drafts older than MAX_AGE_MS are also
// auto-discarded; we surface a one-time banner via a sessionStorage flag
// the caller (Wizard.tsx) consumes once on mount.

import type { WizardStep } from "./types";

export const LOCAL_STORAGE_KEY = "wuphf:onboarding-draft";
export const STALE_BANNER_KEY = "wuphf:onboarding-draft-stale-banner";
export const CURRENT_VERSION = 1;
export const MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;

export interface OnboardingDraft {
  version: number;
  step: WizardStep;
  selectedBlueprint: string | null;
  company: string;
  description: string;
  priority: string;
  runtimePriority: string[];
  localProvider: string;
  selectedTaskTemplate: string | null;
  taskText: string;
  savedAt: string;
}

// Source state we accept from the wizard. We model this loosely so
// future fields can be added without breaking callers — but only the
// keys listed above are ever read into the persisted shape, so a stray
// `apiKeys` (or any other secret-bearing field) on the source object
// CANNOT slip into storage. That is the secret-exclusion guarantee.
export interface DraftableWizardState {
  step: WizardStep;
  selectedBlueprint: string | null;
  company: string;
  description: string;
  priority: string;
  runtimePriority: string[];
  localProvider: string;
  selectedTaskTemplate: string | null;
  taskText: string;
}

const VALID_STEPS: ReadonlySet<WizardStep> = new Set<WizardStep>([
  "welcome",
  "templates",
  "identity",
  "team",
  "setup",
  "task",
  "ready",
]);

export function extractDraftableState(
  state: DraftableWizardState,
): OnboardingDraft {
  return {
    version: CURRENT_VERSION,
    step: state.step,
    selectedBlueprint: state.selectedBlueprint,
    company: state.company,
    description: state.description,
    priority: state.priority,
    runtimePriority: [...state.runtimePriority],
    localProvider: state.localProvider,
    selectedTaskTemplate: state.selectedTaskTemplate,
    taskText: state.taskText,
    savedAt: new Date().toISOString(),
  };
}

function isStringOrNull(value: unknown): value is string | null {
  return value === null || typeof value === "string";
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((s) => typeof s === "string");
}

function hasValidStringFields(draft: Record<string, unknown>): boolean {
  return (
    typeof draft.company === "string" &&
    typeof draft.description === "string" &&
    typeof draft.priority === "string" &&
    typeof draft.localProvider === "string" &&
    typeof draft.taskText === "string" &&
    typeof draft.savedAt === "string"
  );
}

function isValidDraft(value: unknown): value is OnboardingDraft {
  if (typeof value !== "object" || value === null) return false;
  const draft = value as Record<string, unknown>;
  if (draft.version !== CURRENT_VERSION) return false;
  if (typeof draft.step !== "string") return false;
  if (!VALID_STEPS.has(draft.step as WizardStep)) return false;
  if (!isStringOrNull(draft.selectedBlueprint)) return false;
  if (!isStringOrNull(draft.selectedTaskTemplate)) return false;
  if (!isStringArray(draft.runtimePriority)) return false;
  return hasValidStringFields(draft);
}

function safeStorage(): Storage | null {
  try {
    return typeof window !== "undefined" ? window.localStorage : null;
  } catch {
    return null;
  }
}

function safeSessionStorage(): Storage | null {
  try {
    return typeof window !== "undefined" ? window.sessionStorage : null;
  } catch {
    return null;
  }
}

export function loadDraft(): OnboardingDraft | null {
  const storage = safeStorage();
  if (!storage) return null;
  let raw: string | null;
  try {
    raw = storage.getItem(LOCAL_STORAGE_KEY);
  } catch {
    return null;
  }
  if (raw === null) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    // Malformed JSON — drop it so we don't keep failing on every load.
    clearDraft();
    return null;
  }

  if (!isValidDraft(parsed)) {
    // Either an older schema version or shape that no longer matches.
    // Discard and start fresh — degrades safely per spec.
    clearDraft();
    return null;
  }

  const savedAtMs = new Date(parsed.savedAt).getTime();
  if (Number.isNaN(savedAtMs)) {
    clearDraft();
    return null;
  }
  const age = Date.now() - savedAtMs;
  if (age > MAX_AGE_MS) {
    // Stash a flag so Wizard.tsx can show a single banner on this load.
    const session = safeSessionStorage();
    if (session) {
      try {
        const days = Math.floor(age / (24 * 60 * 60 * 1000));
        session.setItem(STALE_BANNER_KEY, String(days));
      } catch {
        // Best-effort — banner is a nice-to-have, not load-bearing.
      }
    }
    clearDraft();
    return null;
  }

  return parsed;
}

export function saveDraft(draft: OnboardingDraft): void {
  const storage = safeStorage();
  if (!storage) return;
  try {
    storage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(draft));
  } catch {
    // QuotaExceededError, SecurityError (private mode), etc. — onboarding
    // is more important than persistence; swallow silently.
  }
}

export function clearDraft(): void {
  const storage = safeStorage();
  if (!storage) return;
  try {
    storage.removeItem(LOCAL_STORAGE_KEY);
  } catch {
    // Best-effort.
  }
}

// seedFromDraft maps a possibly-null restored draft into the initial
// values for the wizard's per-field useState calls. Centralized here so
// the Wizard component itself doesn't carry a long sequence of
// `draft?.foo ?? defaultFoo` ternaries inflating its complexity score.
export interface DraftSeed {
  step: WizardStep;
  selectedBlueprint: string | null;
  company: string;
  description: string;
  priority: string;
  runtimePriority: string[];
  localProvider: string;
  selectedTaskTemplate: string | null;
  taskText: string;
}

const EMPTY_SEED: DraftSeed = {
  step: "welcome",
  selectedBlueprint: null,
  company: "",
  description: "",
  priority: "",
  runtimePriority: [],
  localProvider: "",
  selectedTaskTemplate: null,
  taskText: "",
};

export function seedFromDraft(draft: OnboardingDraft | null): DraftSeed {
  if (draft === null) return EMPTY_SEED;
  return {
    step: draft.step,
    selectedBlueprint: draft.selectedBlueprint,
    company: draft.company,
    description: draft.description,
    priority: draft.priority,
    runtimePriority: draft.runtimePriority,
    localProvider: draft.localProvider,
    selectedTaskTemplate: draft.selectedTaskTemplate,
    taskText: draft.taskText,
  };
}

export function consumeStaleBannerDays(): number | null {
  const session = safeSessionStorage();
  if (!session) return null;
  let raw: string | null;
  try {
    raw = session.getItem(STALE_BANNER_KEY);
  } catch {
    return null;
  }
  if (raw === null) return null;
  try {
    session.removeItem(STALE_BANNER_KEY);
  } catch {
    // If we can't remove it, the banner may show twice — preferable to
    // throwing during onboarding.
  }
  const days = Number.parseInt(raw, 10);
  return Number.isFinite(days) ? days : null;
}
