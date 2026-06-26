import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { DecisionPacket } from "../lib/types/lifecycle";
import * as client from "./client";
import { getDecisionPacket } from "./lifecycle";

/**
 * Minimal valid packet the broker would return at the top level of
 * GET /tasks/{id}. The nested `task` snapshot (added below per test) is the
 * source taskDetailResponse adds for display fields — it is NOT on the
 * DecisionPacket type, so we attach it with a cast at the call site.
 */
function basePacket(overrides: Partial<DecisionPacket> = {}): DecisionPacket {
  return {
    taskId: "task-7",
    title: "",
    lifecycleState: "running",
    ownerSlug: "",
    worktreePath: "",
    createdAt: "2026-06-10T00:00:00Z",
    updatedAt: "2026-06-10T00:00:00Z",
    spec: {} as unknown as DecisionPacket["spec"],
    sessionReport: {} as unknown as DecisionPacket["sessionReport"],
    changedFiles: [],
    reviewerGrades: [],
    dependencies: {} as unknown as DecisionPacket["dependencies"],
    subIssues: [],
    reviewers: [],
    banners: [],
    regeneratedFromMemory: false,
    ...overrides,
  };
}

describe("getDecisionPacket normalizer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("lifts title, owner, and channel off the task snapshot on a direct fetch", async () => {
    const raw = {
      ...basePacket(),
      task: {
        channel: "  task-7-build  ",
        title: "  Build the thing  ",
        owner: "  pm  ",
      },
    };
    vi.spyOn(client, "get").mockResolvedValue(raw);

    const packet = await getDecisionPacket("task-7");

    expect(packet.channel).toBe("task-7-build");
    expect(packet.title).toBe("Build the thing");
    expect(packet.ownerSlug).toBe("pm");
  });

  it("prefers the snapshot's ownerSlug key when present", async () => {
    const raw = {
      ...basePacket({ ownerSlug: "fallback" }),
      task: { ownerSlug: "pam", owner: "ignored" },
    };
    vi.spyOn(client, "get").mockResolvedValue(raw);

    const packet = await getDecisionPacket("task-7");

    expect(packet.ownerSlug).toBe("pam");
  });

  it("falls back to packet values when the snapshot omits display fields", async () => {
    const raw = {
      ...basePacket({ title: "Existing title", ownerSlug: "eng" }),
      task: { channel: "task-7-build" },
    };
    vi.spyOn(client, "get").mockResolvedValue(raw);

    const packet = await getDecisionPacket("task-7");

    expect(packet.title).toBe("Existing title");
    expect(packet.ownerSlug).toBe("eng");
    expect(packet.channel).toBe("task-7-build");
  });

  it("falls back when the snapshot is absent entirely", async () => {
    const raw = basePacket({
      title: "No snapshot title",
      ownerSlug: "design",
      channel: "general",
    });
    vi.spyOn(client, "get").mockResolvedValue(raw);

    const packet = await getDecisionPacket("task-7");

    expect(packet.title).toBe("No snapshot title");
    expect(packet.ownerSlug).toBe("design");
    expect(packet.channel).toBe("general");
  });

  it("ignores whitespace-only snapshot fields and falls back to packet values", async () => {
    const raw = {
      ...basePacket({ title: "Kept title", ownerSlug: "kept-owner" }),
      task: { title: "   ", owner: "   " },
    };
    vi.spyOn(client, "get").mockResolvedValue(raw);

    const packet = await getDecisionPacket("task-7");

    expect(packet.title).toBe("Kept title");
    expect(packet.ownerSlug).toBe("kept-owner");
  });
});
