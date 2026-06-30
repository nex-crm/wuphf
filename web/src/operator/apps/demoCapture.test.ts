import { describe, expect, it } from "vitest";

import {
  assembleDemoCapture,
  captureCounts,
  capturePromptSeed,
  type DemoCaptureLine,
} from "./demoCapture";

const BUILD_TRANSCRIPT: DemoCaptureLine[] = [
  { who: "ai", text: "Walk me through it." },
  { who: "you", text: "A request comes into this form." },
  { who: "ai", text: "Got it. I've drafted a tool. Want to see it?" },
];

const MODIFY_TRANSCRIPT: DemoCaptureLine[] = [
  { who: "ai", text: "Show me the change." },
  { who: "you", text: "Archive anything under 40." },
  { who: "ai", text: "Got it. I've drafted the change. Want to see it?" },
];

describe("assembleDemoCapture", () => {
  it("captures rich screen, selector, API, and entity context for a build", () => {
    const capture = assembleDemoCapture({
      mode: "build",
      transcript: BUILD_TRANSCRIPT,
    });

    expect(capture.mode).toBe("build");
    expect(capture.toolId).toBeUndefined();
    // Every kind of context the screen share is meant to gather is present.
    const counts = captureCounts(capture);
    expect(counts.screens).toBeGreaterThan(0);
    expect(counts.selectors).toBeGreaterThan(0);
    expect(counts.apiCalls).toBeGreaterThan(0);
    expect(counts.entities).toBeGreaterThan(0);
    // Selectors carry concrete metadata the AI can drive.
    expect(capture.selectors.every((s) => s.selector.length > 0)).toBe(true);
    // The summary is the AI's final reflect-back.
    expect(capture.summary).toMatch(/drafted a tool/i);
  });

  it("scopes a modify capture to the demonstrated tool and branch", () => {
    const capture = assembleDemoCapture({
      mode: "modify",
      tool: { id: "inbound-routing", name: "Inbound routing" },
      transcript: MODIFY_TRANSCRIPT,
    });

    expect(capture.mode).toBe("modify");
    expect(capture.toolId).toBe("inbound-routing");
    expect(capture.toolName).toBe("Inbound routing");
    expect(capture.goal).toMatch(/below 40|under 40/i);
    expect(capture.entities.some((e) => e.kind === "threshold")).toBe(true);
  });
});

describe("capturePromptSeed", () => {
  it("leads with the goal and appends the captured apps + APIs", () => {
    const capture = assembleDemoCapture({
      mode: "build",
      transcript: BUILD_TRANSCRIPT,
    });
    const seed = capturePromptSeed(capture);

    expect(seed.startsWith(capture.goal)).toBe(true);
    expect(seed).toMatch(/HubSpot/);
    expect(seed).toMatch(/Slack/);
    expect(seed).toMatch(/captured from your screen share/i);
  });

  it("returns just the goal when nothing API-level was captured", () => {
    const capture = assembleDemoCapture({
      mode: "modify",
      tool: { id: "inbound-routing", name: "Inbound routing" },
      transcript: MODIFY_TRANSCRIPT,
    });
    // The modify scenario has no sniffed API calls, so the seed is the bare goal.
    expect(capturePromptSeed(capture)).toBe(capture.goal);
  });
});
