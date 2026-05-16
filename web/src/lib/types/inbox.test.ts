import { describe, expect, it } from "vitest";

import type { InboxItem, InboxItemKind } from "./inbox";
import { renderInboxItemKey } from "./inbox";

describe("InboxItem discriminated union", () => {
  it("closes over exactly three kinds", () => {
    const all: InboxItemKind[] = ["task", "request", "review"];
    expect(all).toHaveLength(3);
  });

  it("renderInboxItemKey returns a stable per-kind key", () => {
    const task: InboxItem = {
      kind: "task",
      taskId: "task-1",
      title: "Approve refactor",
      task: {
        taskId: "task-1",
        title: "Approve refactor",
        assignment: "",
        severityCounts: {
          critical: 0,
          major: 0,
          minor: 0,
          nitpick: 0,
          skipped: 0,
        },
        lastChangedAt: "2026-05-11T00:00:00Z",
        elapsed: "1m",
        isUrgent: false,
        state: "decision",
      },
    };
    const request: InboxItem = {
      kind: "request",
      requestId: "req-1",
      title: "Need a decision on Postgres bump",
      request: {
        kind: "approval",
        question: "Bump Postgres to 17?",
        from: "owner",
      },
    };
    const review: InboxItem = {
      kind: "review",
      reviewId: "rev-1",
      title: "Promote draft to wiki",
      review: {
        state: "pending",
        reviewerSlug: "owner",
        sourceSlug: "ada",
        targetPath: "wiki/draft.md",
      },
    };

    expect(renderInboxItemKey(task)).toBe("task:task-1");
    expect(renderInboxItemKey(request)).toBe("request:req-1");
    expect(renderInboxItemKey(review)).toBe("review:rev-1");
  });

  it("exhaustiveness gate: every kind has a renderer branch", () => {
    const sample: InboxItem[] = [
      {
        kind: "task",
        taskId: "t",
        title: "t",
        task: {
          taskId: "t",
          title: "t",
          assignment: "",
          severityCounts: {
            critical: 0,
            major: 0,
            minor: 0,
            nitpick: 0,
            skipped: 0,
          },
          lastChangedAt: "",
          elapsed: "",
          isUrgent: false,
          state: "decision",
        },
      },
      {
        kind: "request",
        requestId: "r",
        title: "r",
        request: { kind: "approval", question: "q", from: "owner" },
      },
      {
        kind: "review",
        reviewId: "v",
        title: "v",
        review: {
          state: "pending",
          reviewerSlug: "owner",
          sourceSlug: "ada",
          targetPath: "wiki/x.md",
        },
      },
    ];
    for (const item of sample) {
      expect(typeof renderInboxItemKey(item)).toBe("string");
    }
  });
});
