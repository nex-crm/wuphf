import { describe, expect, it } from "vitest";

import { pickIdleCopy } from "./officeIdleDictionary";

describe("pickIdleCopy", () => {
  it("slug override wins over role", () => {
    // tess has a slug override; passing engineer role should still hit the
    // slug copy ("drafting a thought" at idleMs=0), not the engineer table
    // ("watching tests" at idleMs=0).
    const slugCopy = pickIdleCopy({
      slug: "tess",
      role: "engineer",
      idleMs: 0,
    });
    const engineerCopy = pickIdleCopy({
      slug: "unknown",
      role: "engineer",
      idleMs: 0,
    });
    expect(slugCopy).not.toBe(engineerCopy);
    expect(slugCopy).toBe("drafting a thought");
    expect(engineerCopy).toBe("watching tests");
  });

  it("each role table is reachable", () => {
    expect(pickIdleCopy({ slug: "x", role: "engineer", idleMs: 0 })).toBe(
      "watching tests",
    );
    expect(pickIdleCopy({ slug: "x", role: "developer", idleMs: 0 })).toBe(
      "watching tests",
    );
    expect(pickIdleCopy({ slug: "x", role: "dev", idleMs: 0 })).toBe(
      "watching tests",
    );
    expect(pickIdleCopy({ slug: "x", role: "designer", idleMs: 0 })).toBe(
      "doodling in Figma",
    );
    expect(pickIdleCopy({ slug: "x", role: "pm", idleMs: 0 })).toBe(
      "combing Linear",
    );
    expect(pickIdleCopy({ slug: "x", role: "product", idleMs: 0 })).toBe(
      "combing Linear",
    );
    expect(pickIdleCopy({ slug: "x", role: "devops", idleMs: 0 })).toBe(
      "watching dashboards",
    );
    expect(pickIdleCopy({ slug: "x", role: "sre", idleMs: 0 })).toBe(
      "watching dashboards",
    );
    expect(pickIdleCopy({ slug: "x", role: "platform", idleMs: 0 })).toBe(
      "watching dashboards",
    );
    expect(pickIdleCopy({ slug: "x", role: "marketing", idleMs: 0 })).toBe(
      "scrolling X",
    );
    expect(pickIdleCopy({ slug: "x", role: "growth", idleMs: 0 })).toBe(
      "scrolling X",
    );
  });

  it("normalizes role with trim + lowercase", () => {
    expect(pickIdleCopy({ slug: "x", role: "  ENGINEER  ", idleMs: 0 })).toBe(
      "watching tests",
    );
  });

  it("unknown role falls back to generalist (does not crash, does not return empty)", () => {
    const result = pickIdleCopy({ slug: "x", role: "alchemist", idleMs: 0 });
    expect(result).toBe("looking at memes");
    expect(result.length).toBeGreaterThan(0);
  });

  it("missing role falls back to generalist", () => {
    const result = pickIdleCopy({ slug: "x", idleMs: 0 });
    expect(result).toBe("looking at memes");
  });

  it("empty role string falls back to generalist", () => {
    const result = pickIdleCopy({ slug: "x", role: "   ", idleMs: 0 });
    expect(result).toBe("looking at memes");
  });

  it("same slug + same idleMs returns same copy (deterministic)", () => {
    const a = pickIdleCopy({ slug: "tess", idleMs: 25_000 });
    const b = pickIdleCopy({ slug: "tess", idleMs: 25_000 });
    expect(a).toBe(b);
  });

  it("idleMs rotation cycles through array (~12s per step)", () => {
    // engineer table has 5 entries; rotation interval = 12_000ms.
    const t0 = pickIdleCopy({ slug: "x", role: "engineer", idleMs: 0 });
    const t1 = pickIdleCopy({ slug: "x", role: "engineer", idleMs: 12_000 });
    const t2 = pickIdleCopy({ slug: "x", role: "engineer", idleMs: 24_000 });
    const t3 = pickIdleCopy({ slug: "x", role: "engineer", idleMs: 36_000 });
    const t4 = pickIdleCopy({ slug: "x", role: "engineer", idleMs: 48_000 });
    const t5 = pickIdleCopy({ slug: "x", role: "engineer", idleMs: 60_000 });

    expect(t0).toBe("watching tests");
    expect(t1).toBe("reviewing the diff");
    expect(t2).toBe("skimming PRs");
    expect(t3).toBe("checking CI");
    expect(t4).toBe("reading the changelog");
    // wraps back to start
    expect(t5).toBe(t0);
  });

  it("handles negative or non-finite idleMs without crashing", () => {
    expect(pickIdleCopy({ slug: "x", role: "engineer", idleMs: -1 })).toBe(
      "watching tests",
    );
    expect(
      pickIdleCopy({ slug: "x", role: "engineer", idleMs: Number.NaN }),
    ).toBe("watching tests");
  });
});
