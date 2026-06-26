import { describe, expect, it } from "vitest";

import { isResumableStatus } from "./AgentProfilePanel";

describe("isResumableStatus", () => {
  it("offers Resume for in-flight states that can silently stall", () => {
    for (const s of [
      "blocked",
      "in_progress",
      "in-progress",
      "running",
      "queued",
      "queued_behind_owner",
      "Queued Behind Owner",
      "planning",
    ]) {
      expect(isResumableStatus(s)).toBe(true);
    }
  });

  it("does not offer Resume for terminal or human-gated states", () => {
    for (const s of [
      "done",
      "completed",
      "review",
      "approved",
      "rejected",
      "cancelled",
      "archived",
      "drafting",
      "ready",
      "open",
    ]) {
      expect(isResumableStatus(s)).toBe(false);
    }
  });
});
