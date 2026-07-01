import { describe, expect, it } from "vitest";

import {
  authorToolFromDescription,
  callTool,
  sampleArgsFor,
  seedTools,
} from "./mockTools";

describe("authorToolFromDescription", () => {
  it("recognizes the score-and-route workflow", () => {
    const t = authorToolFromDescription(
      "When a new lead comes in, score its fit and route hot ones to the AE",
    );
    expect(t.name).toBe("scoreAndRouteLead");
    expect(t.inputs.map((i) => i.name)).toEqual(["lead"]);
    expect(t.script).toContain("async function scoreAndRouteLead(lead)");
    expect(t.createdFrom).toContain("score its fit");
  });

  it("recognizes the weekly summary workflow (no inputs)", () => {
    const t = authorToolFromDescription("Every Monday summarize the pipeline");
    expect(t.name).toBe("weeklyPipelineSummary");
    expect(t.inputs).toEqual([]);
  });

  it("recognizes the follow-up draft workflow", () => {
    const t = authorToolFromDescription(
      "Draft a follow-up email for a stalled deal",
    );
    expect(t.name).toBe("draftFollowup");
    expect(t.inputs.map((i) => i.name)).toEqual(["deal"]);
  });

  it("synthesizes a camelCase name for an unknown workflow", () => {
    const t = authorToolFromDescription("Archive old records nightly");
    // Stopwords dropped, first three content words → camelCase.
    expect(t.name).toBe("archiveOldRecords");
    expect(t.inputs.map((i) => i.name)).toEqual(["input"]);
    expect(t.script).toContain("archiveOldRecords");
  });

  it("falls back to a safe name when the description is all stopwords", () => {
    const t = authorToolFromDescription("the a an my");
    expect(t.name).toBe("runWorkflow");
  });
});

describe("callTool", () => {
  it("returns the known shape's canned result", () => {
    const t = authorToolFromDescription("score and route the lead");
    const call = callTool(t, sampleArgsFor(t));
    expect(call.args).toEqual({ lead: "Acme" });
    expect(call.result).toMatch(/routed to/i);
  });

  it("echoes args for an unknown tool", () => {
    const t = authorToolFromDescription("archive old records");
    const call = callTool(t, { input: "2024" });
    expect(call.result).toContain(t.name);
    expect(call.result).toContain("2024");
  });
});

describe("seedTools", () => {
  it("seeds the library with one tool so it is not empty", () => {
    const seeded = seedTools();
    expect(seeded).toHaveLength(1);
    expect(seeded[0].name).toBe("weeklyPipelineSummary");
  });
});
