import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  __resetOnboardingV2FlagForTests,
  isOnboardingV2Enabled,
} from "./featureFlags";

function setSearch(search: string) {
  window.history.replaceState({}, "", `/${search}`);
}

beforeEach(() => {
  __resetOnboardingV2FlagForTests();
  setSearch("");
});

afterEach(() => {
  __resetOnboardingV2FlagForTests();
  setSearch("");
});

describe("isOnboardingV2Enabled", () => {
  it("defaults to false without any opt-in signal", () => {
    expect(isOnboardingV2Enabled()).toBe(false);
  });

  it("returns true when the URL carries ?onboardingV2=1", () => {
    setSearch("?onboardingV2=1");
    expect(isOnboardingV2Enabled()).toBe(true);
  });

  it("persists the URL choice so subsequent calls return the same value", () => {
    setSearch("?onboardingV2=1");
    expect(isOnboardingV2Enabled()).toBe(true);
    setSearch("");
    expect(isOnboardingV2Enabled()).toBe(true);
  });

  it("lets ?onboardingV2=0 explicitly disable an earlier opt-in", () => {
    setSearch("?onboardingV2=1");
    isOnboardingV2Enabled();
    setSearch("?onboardingV2=0");
    expect(isOnboardingV2Enabled()).toBe(false);
    setSearch("");
    expect(isOnboardingV2Enabled()).toBe(false);
  });
});
