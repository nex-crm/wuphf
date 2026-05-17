// Feature flag plumbing for staged onboarding rollouts.
//
// Phase 1 of the onboarding-into-office redesign (docs/specs/
// onboarding-into-office.md) replaces the 9-step wizard with a single
// pre-office provider picker plus an empty-shell office entry. The
// existing wizard stays mounted behind this flag so we can roll back
// without redeploying.
//
// Resolution order:
//   1. URL search param (?onboardingV2=1 enables, =0 disables). Persisted
//      to localStorage so subsequent visits respect the explicit choice.
//   2. localStorage value from a prior URL toggle.
//   3. Default: false (OFF) — the wizard stays the live path until we
//      flip the default in a follow-up.

const STORAGE_KEY = "wuphf:onboarding-v2";

function readStorage(): boolean | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw === "1" || raw === "true") return true;
    if (raw === "0" || raw === "false") return false;
    return null;
  } catch {
    return null;
  }
}

function writeStorage(value: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, value ? "1" : "0");
  } catch {
    // Ignore quota / privacy-mode storage errors. The flag falls back
    // to per-load query-param resolution in that case.
  }
}

function readSearchParamOverride(): boolean | null {
  if (typeof window === "undefined") return null;
  try {
    const params = new URLSearchParams(window.location.search);
    const raw = params.get("onboardingV2");
    if (raw === null) return null;
    if (raw === "1" || raw === "true") return true;
    if (raw === "0" || raw === "false") return false;
    return null;
  } catch {
    return null;
  }
}

export function isOnboardingV2Enabled(): boolean {
  const fromParam = readSearchParamOverride();
  if (fromParam !== null) {
    writeStorage(fromParam);
    return fromParam;
  }
  const fromStorage = readStorage();
  if (fromStorage !== null) return fromStorage;
  return false;
}

// Test-only helper. Call between tests to keep the localStorage-backed
// flag from leaking state across cases.
export function __resetOnboardingV2FlagForTests(): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}
