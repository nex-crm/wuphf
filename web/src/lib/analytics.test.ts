import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The subset of PostHog init config the suite inspects.
interface InitCfg {
  loaded?: (ph: unknown) => void;
  api_host?: string;
  autocapture?: boolean;
  capture_pageview?: boolean;
  persistence?: string;
  disable_session_recording?: boolean;
  session_recording?: { maskAllInputs?: boolean; maskTextSelector?: string };
  sanitize_properties?: (p: Record<string, unknown>) => Record<string, unknown>;
}

// Mock posthog-js. init() invokes the `loaded` callback synchronously so the
// recording-on-load path runs, mirroring the real SDK.
const mockPosthog = {
  init: vi.fn((_key: string, cfg?: InitCfg) => {
    cfg?.loaded?.(mockPosthog);
  }),
  capture: vi.fn(),
  startSessionRecording: vi.fn(),
  stopSessionRecording: vi.fn(),
  opt_in_capturing: vi.fn(),
  opt_out_capturing: vi.fn(),
  setPersonProperties: vi.fn(),
  group: vi.fn(),
};

vi.mock("posthog-js", () => ({ default: mockPosthog }));

import {
  __resetAnalyticsForTests,
  configureAnalytics,
  isValidEmail,
  recordOnboardingEmailCaptured,
  recordOnboardingEmailStarted,
  recordOnboardingEmailViewed,
  setAnalyticsConsent,
  track,
} from "./analytics";

/** Let the dynamic-import + .then chain in the module settle. */
async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

beforeEach(() => {
  vi.clearAllMocks();
  __resetAnalyticsForTests();
});

afterEach(() => {
  vi.unstubAllEnvs();
});

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
    expect(isValidEmail(input as string)).toBe(expected);
  });
});

describe("dormant by default", () => {
  it("never loads or inits posthog when no key resolves", async () => {
    track("task_created");
    recordOnboardingEmailViewed();
    recordOnboardingEmailCaptured("maya@nex.ai");
    await flush();
    expect(mockPosthog.init).not.toHaveBeenCalled();
    expect(mockPosthog.capture).not.toHaveBeenCalled();
  });
});

describe("build-time key fallback", () => {
  it("inits from VITE_PUBLIC_POSTHOG_KEY when no runtime config is set", async () => {
    vi.stubEnv("VITE_PUBLIC_POSTHOG_KEY", "phc_build");
    track("task_created", { source: "home" });
    await vi.waitFor(() => expect(mockPosthog.init).toHaveBeenCalledTimes(1));
    expect(mockPosthog.init.mock.calls[0][0]).toBe("phc_build");
    await vi.waitFor(() =>
      expect(mockPosthog.capture).toHaveBeenCalledWith("task_created", {
        source: "home",
      }),
    );
  });
});

describe("configured via runtime injection", () => {
  it("runtime key wins; cookies disabled, autocapture off, recording opt-in", async () => {
    configureAnalytics({
      configured: true,
      posthog_key: "phc_runtime",
      posthog_host: "https://eu.i.posthog.com/",
      telemetry_enabled: true,
      session_recording_enabled: false,
    });
    await vi.waitFor(() => expect(mockPosthog.init).toHaveBeenCalledTimes(1));
    const [key, cfg] = mockPosthog.init.mock.calls[0];
    expect(key).toBe("phc_runtime");
    expect(cfg?.api_host).toBe("https://eu.i.posthog.com");
    expect(cfg?.autocapture).toBe(false);
    expect(cfg?.capture_pageview).toBe(false);
    expect(cfg?.persistence).toBe("localStorage");
    expect(cfg?.disable_session_recording).toBe(true);
    // Recording channel off => not started on load.
    expect(mockPosthog.startSessionRecording).not.toHaveBeenCalled();
  });

  it("starts recording on load with strict masking when recording is on", async () => {
    configureAnalytics({
      configured: true,
      posthog_key: "phc_runtime",
      telemetry_enabled: true,
      session_recording_enabled: true,
    });
    await vi.waitFor(() => expect(mockPosthog.init).toHaveBeenCalled());
    const cfg = mockPosthog.init.mock.calls[0][1];
    expect(cfg?.session_recording?.maskAllInputs).toBe(true);
    expect(cfg?.session_recording?.maskTextSelector).toBe("*");
    await vi.waitFor(() =>
      expect(mockPosthog.startSessionRecording).toHaveBeenCalledTimes(1),
    );
  });

  it("stays dormant when telemetry is off even with a key", async () => {
    configureAnalytics({
      configured: true,
      posthog_key: "phc_runtime",
      telemetry_enabled: false,
      session_recording_enabled: true,
    });
    track("task_created");
    await flush();
    expect(mockPosthog.init).not.toHaveBeenCalled();
  });
});

describe("onboarding email (the single PII egress)", () => {
  beforeEach(() => {
    vi.stubEnv("VITE_PUBLIC_POSTHOG_KEY", "phc_test");
  });

  it("funnel events carry source only, never an address", async () => {
    recordOnboardingEmailViewed();
    recordOnboardingEmailStarted();
    await vi.waitFor(() =>
      expect(mockPosthog.capture).toHaveBeenCalledWith(
        "onboarding_email_viewed",
        { source: "onboarding-welcome" },
      ),
    );
    expect(mockPosthog.capture).toHaveBeenCalledWith(
      "onboarding_email_started",
      { source: "onboarding-welcome" },
    );
    expect(JSON.stringify(mockPosthog.capture.mock.calls)).not.toContain("@");
    expect(mockPosthog.setPersonProperties).not.toHaveBeenCalled();
  });

  it("captured attaches the email via setPersonProperties, not the event", async () => {
    recordOnboardingEmailCaptured("  maya@nex.ai  ");
    await vi.waitFor(() =>
      expect(mockPosthog.setPersonProperties).toHaveBeenCalledWith({
        email: "maya@nex.ai",
      }),
    );
    expect(mockPosthog.capture).toHaveBeenCalledWith(
      "onboarding_email_captured",
      { source: "onboarding-welcome" },
    );
    const captureCall = mockPosthog.capture.mock.calls.find(
      (c) => c[0] === "onboarding_email_captured",
    );
    expect(JSON.stringify(captureCall?.[1])).not.toContain("@");
  });

  it("ignores a blank email", async () => {
    recordOnboardingEmailCaptured("   ");
    await flush();
    expect(mockPosthog.setPersonProperties).not.toHaveBeenCalled();
    expect(mockPosthog.capture).not.toHaveBeenCalled();
  });
});

describe("live consent changes", () => {
  it("turning recording on starts it; turning telemetry off opts out + stops", async () => {
    configureAnalytics({
      configured: true,
      posthog_key: "phc_runtime",
      telemetry_enabled: true,
      session_recording_enabled: false,
    });
    await vi.waitFor(() => expect(mockPosthog.init).toHaveBeenCalled());
    expect(mockPosthog.startSessionRecording).not.toHaveBeenCalled();

    setAnalyticsConsent({ recording: true });
    await vi.waitFor(() =>
      expect(mockPosthog.startSessionRecording).toHaveBeenCalled(),
    );

    setAnalyticsConsent({ telemetry: false });
    await vi.waitFor(() =>
      expect(mockPosthog.opt_out_capturing).toHaveBeenCalled(),
    );
    expect(mockPosthog.stopSessionRecording).toHaveBeenCalled();
  });
});

describe("sanitize_properties", () => {
  it("enriches events with the active theme from the DOM", async () => {
    document.documentElement.setAttribute("data-theme", "noir-gold");
    configureAnalytics({
      configured: true,
      posthog_key: "phc_runtime",
      telemetry_enabled: true,
      session_recording_enabled: false,
    });
    await vi.waitFor(() => expect(mockPosthog.init).toHaveBeenCalled());
    const cfg = mockPosthog.init.mock.calls[0][1];
    const enriched = cfg?.sanitize_properties?.({ a: 1 }) ?? {};
    expect(enriched.theme).toBe("noir-gold");
    document.documentElement.removeAttribute("data-theme");
  });
});
