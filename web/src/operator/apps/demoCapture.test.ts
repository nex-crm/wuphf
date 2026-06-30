import { describe, expect, it } from "vitest";

import {
  assembleDemoCapture,
  captureCounts,
  capturePromptSeed,
  type DemoCaptureLine,
  demoCaptureFromDraft,
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

describe("demoCaptureFromDraft (real-call converter)", () => {
  it("coerces loose model output into the typed capture and drops empties", () => {
    const capture = demoCaptureFromDraft(
      {
        goal: "  Route urgent tickets to the on-call engineer  ",
        summary: "Drafted a routing tool",
        screens: [{ label: "Zendesk" }, { label: "" }],
        selectors: [
          { label: "Priority field", role: "DROPDOWN", selector: "#prio" },
          { label: "no selector", selector: "" },
        ],
        apiCalls: [
          { method: "post", endpoint: "/api/v2/tickets", integration: "Zendesk" },
          { endpoint: "" },
        ],
        entities: [{ kind: "Channel", value: "#oncall" }, { value: "" }],
      },
      { mode: "build", transcript: [] },
    );

    expect(capture.goal).toBe("Route urgent tickets to the on-call engineer");
    // Empty-label screen, selector-less element, endpoint-less call, and
    // value-less entity are all dropped.
    expect(capture.screens).toHaveLength(1);
    expect(capture.selectors).toHaveLength(1);
    expect(capture.apiCalls).toHaveLength(1);
    expect(capture.entities).toHaveLength(1);
    // Unknown role/kind coerce to safe defaults; method upper-cases.
    expect(capture.selectors[0].role).toBe("input");
    expect(capture.apiCalls[0].method).toBe("POST");
    expect(capture.entities[0].kind).toBe("channel");
  });

  it("carries the tool identity in modify mode", () => {
    const capture = demoCaptureFromDraft(
      { goal: "Archive under 40" },
      {
        mode: "modify",
        tool: { id: "inbound-routing", name: "Inbound routing" },
        transcript: [{ who: "you", text: "archive them" }],
      },
    );
    expect(capture.mode).toBe("modify");
    expect(capture.toolId).toBe("inbound-routing");
    expect(capture.transcript).toHaveLength(1);
  });
});
