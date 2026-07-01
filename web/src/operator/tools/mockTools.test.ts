import { describe, expect, it } from "vitest";

import {
  authorToolFromDescription,
  callTool,
  sampleArgsFor,
  seedToolsForApp,
} from "./mockTools";

describe("authorToolFromDescription", () => {
  it("recognizes the score-and-route workflow with a plain title", () => {
    const t = authorToolFromDescription(
      "When a new lead comes in, score its fit and route hot ones to the AE",
    );
    expect(t.name).toBe("scoreAndRouteLead");
    expect(t.title).toBe("Score & route a lead");
    expect(t.inputs.map((i) => i.name)).toEqual(["lead"]);
    expect(t.script).toContain("async function scoreAndRouteLead(lead)");
    expect(t.createdFrom).toContain("score its fit");
  });

  it("derives a plain-language title for an unknown workflow", () => {
    // Leading "When … ," trigger is dropped so the title names the action.
    const t = authorToolFromDescription(
      "When an invoice arrives, file it in the folder",
    );
    expect(t.title).toBe("File it in the folder");
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

describe("seedToolsForApp", () => {
  it("lists the app's already-built tools (no create-a-tool UI here)", () => {
    const seeded = seedToolsForApp("Pipeline");
    expect(seeded.map((t) => t.name)).toEqual([
      "weeklyPipelineSummary",
      "scoreAndRouteLead",
    ]);
  });
});
