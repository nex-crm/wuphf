import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  CURRENT_VERSION,
  clearDraft,
  consumeStaleBannerDays,
  type DraftableWizardState,
  extractDraftableState,
  LOCAL_STORAGE_KEY,
  loadDraft,
  MAX_AGE_MS,
  STALE_BANNER_KEY,
  saveDraft,
} from "./onboardingDraft";

// State that intentionally extends the typed shape with secret-bearing
// fields so the negative test can verify they are stripped. We only ever
// pass this through unknown into extractDraftableState — never directly.
interface StateWithSecrets extends DraftableWizardState {
  apiKeys: Record<string, string>;
  nexEmail: string;
}

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("extractDraftableState", () => {
  it("includes only the persisted fields, never API keys", () => {
    // The mapper accepts a typed DraftableWizardState. As an extra
    // regression guard we cast through unknown to slip in fields that
    // would never appear in the real type — and assert they don't make
    // it into the saved payload.
    const stateWithSecrets: StateWithSecrets = {
      step: "setup",
      selectedBlueprint: "niche-crm",
      company: "Acme",
      description: "We do things",
      priority: "growth",
      runtimePriority: ["Claude Code"],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "Reach out to first 10 customers",
      // Secrets that must never be persisted:
      apiKeys: {
        ANTHROPIC_API_KEY: "sk-test-placeholder",
        OPENAI_API_KEY: "sk-test-placeholder",
        GOOGLE_API_KEY: "google-test-placeholder",
      },
      nexEmail: "user@example.com",
    };

    // Cast through unknown so the secret-bearing fields are present at
    // runtime but the typed surface still matches DraftableWizardState.
    const draft = extractDraftableState(
      stateWithSecrets as unknown as DraftableWizardState,
    );
    const serialized = JSON.stringify(draft);

    expect(serialized).not.toContain("sk-test-placeholder");
    expect(serialized).not.toContain("google-test-placeholder");
    expect(serialized).not.toContain("ANTHROPIC_API_KEY");
    expect(serialized).not.toContain("OPENAI_API_KEY");
    expect(serialized).not.toContain("GOOGLE_API_KEY");
    expect(serialized).not.toContain("apiKeys");
    expect(serialized).not.toContain("nexEmail");
    expect(serialized).not.toContain("user@example.com");

    expect(draft.company).toBe("Acme");
    expect(draft.runtimePriority).toEqual(["Claude Code"]);
    expect(draft.version).toBe(CURRENT_VERSION);
    expect(typeof draft.savedAt).toBe("string");
  });
});

describe("save/load round-trip", () => {
  it("save → load returns equal object", () => {
    const source = extractDraftableState({
      step: "team",
      selectedBlueprint: null,
      company: "Acme",
      description: "We do things",
      priority: "",
      runtimePriority: ["Claude Code", "Codex"],
      localProvider: "",
      selectedTaskTemplate: "first-customer",
      taskText: "First task",
    });
    saveDraft(source);
    const loaded = loadDraft();
    expect(loaded).toEqual(source);
  });

  it("does not persist API keys when saved alongside the draft", () => {
    // Even if a caller mistakenly saved a draft directly, the saved JSON
    // is exactly the OnboardingDraft shape and contains no secret keys.
    const source = extractDraftableState({
      step: "setup",
      selectedBlueprint: null,
      company: "",
      description: "",
      priority: "",
      runtimePriority: [],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "",
    });
    saveDraft(source);
    const raw = window.localStorage.getItem(LOCAL_STORAGE_KEY) ?? "";
    expect(raw).not.toMatch(/api[_-]?key/i);
    expect(raw).not.toMatch(/anthropic/i);
    expect(raw).not.toMatch(/openai/i);
  });
});

describe("saveDraft defensive whitelist", () => {
  it("strips secret-bearing fields even when bypassing extractDraftableState", () => {
    // Simulate a future caller that builds an OnboardingDraft-shaped
    // object directly and smuggles secrets onto it. The storage-boundary
    // re-projection inside saveDraft must drop them.
    const tainted = {
      version: CURRENT_VERSION,
      step: "setup",
      selectedBlueprint: null,
      company: "Acme",
      description: "",
      priority: "",
      runtimePriority: ["Claude Code"],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "",
      savedAt: new Date().toISOString(),
      // Fields that must never reach storage.
      apiKeys: {
        ANTHROPIC_API_KEY: "sk-test-placeholder",
        OPENAI_API_KEY: "sk-test-placeholder",
      },
      nexEmail: "user@example.com",
    };

    saveDraft(tainted as unknown as Parameters<typeof saveDraft>[0]);
    const raw = window.localStorage.getItem(LOCAL_STORAGE_KEY) ?? "";

    expect(raw).not.toContain("sk-test-placeholder");
    expect(raw).not.toContain("apiKeys");
    expect(raw).not.toContain("ANTHROPIC_API_KEY");
    expect(raw).not.toContain("OPENAI_API_KEY");
    expect(raw).not.toContain("nexEmail");
    expect(raw).not.toContain("user@example.com");

    const reloaded = loadDraft();
    expect(reloaded?.company).toBe("Acme");
    expect(reloaded?.runtimePriority).toEqual(["Claude Code"]);
  });
});

describe("loadDraft validation", () => {
  it("returns null for version mismatch and clears storage", () => {
    window.localStorage.setItem(
      LOCAL_STORAGE_KEY,
      JSON.stringify({
        version: 0,
        step: "welcome",
        selectedBlueprint: null,
        company: "",
        description: "",
        priority: "",
        runtimePriority: [],
        localProvider: "",
        selectedTaskTemplate: null,
        taskText: "",
        savedAt: new Date().toISOString(),
      }),
    );
    expect(loadDraft()).toBeNull();
    expect(window.localStorage.getItem(LOCAL_STORAGE_KEY)).toBeNull();
  });

  it("returns null and clears storage for stale drafts (>30 days)", () => {
    vi.useFakeTimers();
    const now = new Date("2026-05-07T00:00:00Z");
    vi.setSystemTime(now);
    const draft = extractDraftableState({
      step: "team",
      selectedBlueprint: null,
      company: "Acme",
      description: "x",
      priority: "",
      runtimePriority: [],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "",
    });
    // Backdate savedAt by 31 days.
    draft.savedAt = new Date(now.getTime() - MAX_AGE_MS - 1000).toISOString();
    window.localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(draft));

    expect(loadDraft()).toBeNull();
    expect(window.localStorage.getItem(LOCAL_STORAGE_KEY)).toBeNull();
    expect(window.sessionStorage.getItem(STALE_BANNER_KEY)).not.toBeNull();
  });

  it("returns null on malformed JSON without throwing", () => {
    window.localStorage.setItem(LOCAL_STORAGE_KEY, "{not valid json");
    expect(() => loadDraft()).not.toThrow();
    expect(loadDraft()).toBeNull();
  });

  it("returns null when shape is wrong (missing fields)", () => {
    window.localStorage.setItem(
      LOCAL_STORAGE_KEY,
      JSON.stringify({ version: CURRENT_VERSION, step: "welcome" }),
    );
    expect(loadDraft()).toBeNull();
  });

  it("returns null when step is unknown", () => {
    const draft = extractDraftableState({
      step: "welcome",
      selectedBlueprint: null,
      company: "",
      description: "",
      priority: "",
      runtimePriority: [],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "",
    });
    const tampered = { ...draft, step: "not-a-step" };
    window.localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(tampered));
    expect(loadDraft()).toBeNull();
  });
});

describe("saveDraft resilience", () => {
  it("swallows QuotaExceededError silently", () => {
    const setItem = vi
      .spyOn(window.localStorage, "setItem")
      .mockImplementation(() => {
        throw new Error("QuotaExceededError");
      });

    const draft = extractDraftableState({
      step: "welcome",
      selectedBlueprint: null,
      company: "",
      description: "",
      priority: "",
      runtimePriority: [],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "",
    });
    expect(() => saveDraft(draft)).not.toThrow();
    expect(setItem).toHaveBeenCalled();
  });
});

describe("clearDraft", () => {
  it("removes the saved draft", () => {
    const draft = extractDraftableState({
      step: "welcome",
      selectedBlueprint: null,
      company: "",
      description: "",
      priority: "",
      runtimePriority: [],
      localProvider: "",
      selectedTaskTemplate: null,
      taskText: "",
    });
    saveDraft(draft);
    expect(window.localStorage.getItem(LOCAL_STORAGE_KEY)).not.toBeNull();
    clearDraft();
    expect(window.localStorage.getItem(LOCAL_STORAGE_KEY)).toBeNull();
  });
});

describe("consumeStaleBannerDays", () => {
  it("returns null when no flag is set", () => {
    expect(consumeStaleBannerDays()).toBeNull();
  });
  it("returns the day count once and clears the flag", () => {
    window.sessionStorage.setItem(STALE_BANNER_KEY, "42");
    expect(consumeStaleBannerDays()).toBe(42);
    expect(consumeStaleBannerDays()).toBeNull();
  });
});
