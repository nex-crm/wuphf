import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  isValidEmail,
  recordOnboardingEmailCaptured,
  recordOnboardingEmailStarted,
  recordOnboardingEmailViewed,
} from "./analytics";

const KEY = "VITE_PUBLIC_POSTHOG_KEY";
const HOST = "VITE_PUBLIC_POSTHOG_HOST";
const fetchMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockResolvedValue(new Response(null, { status: 200 }));
  // Each test starts from a clean session so the visit id is freshly minted.
  sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

/** The parsed JSON body of the Nth fetch call. */
function bodyOf(call: number): Record<string, unknown> {
  const init = fetchMock.mock.calls[call]?.[1] as RequestInit | undefined;
  return JSON.parse(init?.body as string) as Record<string, unknown>;
}

/** The `properties` object of the Nth captured event. */
function propsOf(call: number): Record<string, unknown> {
  return bodyOf(call).properties as Record<string, unknown>;
}

describe("isValidEmail", () => {
  it.each([
    ["maya@nex.ai", true],
    ["  sam@dunder.co  ", true],
    ["nope", false],
    ["no@domain", false],
    ["@no-local.com", false],
    ["spaces in@email.com", false],
    ["", false],
  ])("%s -> %s", (input, expected) => {
    expect(isValidEmail(input)).toBe(expected);
  });
});

describe("dormant by default", () => {
  it("does not call fetch when the PostHog key is unset", () => {
    recordOnboardingEmailViewed();
    recordOnboardingEmailStarted();
    recordOnboardingEmailCaptured("maya@nex.ai");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("with a PostHog key configured", () => {
  beforeEach(() => {
    vi.stubEnv(KEY, "phc_test_key");
  });

  it("posts funnel events to the default host's /capture/ with no PII", () => {
    recordOnboardingEmailViewed();
    recordOnboardingEmailStarted();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0][0]).toBe(
      "https://us.i.posthog.com/capture/",
    );
    expect(fetchMock.mock.calls[0][1]?.keepalive).toBe(true);

    expect(bodyOf(0)).toMatchObject({
      api_key: "phc_test_key",
      event: "onboarding_email_viewed",
    });
    expect(bodyOf(1)).toMatchObject({ event: "onboarding_email_started" });
    expect(propsOf(0)).toEqual({ source: "onboarding-welcome" });
    expect(propsOf(0)).not.toHaveProperty("$set");
    expect(JSON.stringify(bodyOf(0))).not.toContain("@");
  });

  it("attaches the email to the person via $set on capture", () => {
    recordOnboardingEmailCaptured("  maya@nex.ai  ");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(bodyOf(0)).toMatchObject({ event: "onboarding_email_captured" });
    expect(propsOf(0).$set).toEqual({ email: "maya@nex.ai" });
  });

  it("ignores a blank email", () => {
    recordOnboardingEmailCaptured("   ");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("reuses one distinct id across events in a session", () => {
    recordOnboardingEmailViewed();
    recordOnboardingEmailStarted();
    recordOnboardingEmailCaptured("maya@nex.ai");

    const ids = new Set([
      bodyOf(0).distinct_id,
      bodyOf(1).distinct_id,
      bodyOf(2).distinct_id,
    ]);
    expect(ids.size).toBe(1);
  });

  it("swallows a fetch rejection without throwing", () => {
    fetchMock.mockRejectedValue(new Error("network down"));
    expect(() => recordOnboardingEmailCaptured("maya@nex.ai")).not.toThrow();
  });
});

describe("host override", () => {
  it("honors a custom host and trims a trailing slash", () => {
    vi.stubEnv(KEY, "phc_test_key");
    vi.stubEnv(HOST, "https://eu.i.posthog.com/");

    recordOnboardingEmailViewed();

    expect(fetchMock.mock.calls[0][0]).toBe(
      "https://eu.i.posthog.com/capture/",
    );
  });
});
