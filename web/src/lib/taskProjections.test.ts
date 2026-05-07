import { describe, expect, it } from "vitest";

import type { Task } from "../api/tasks";
import {
  groupTasksByAgent,
  groupTasksByLifecycle,
  groupTasksByStatus,
  groupTasksByWeek,
  normalizeTaskStatus,
  selectUnassigned,
  selectUnscheduled,
  UNASSIGNED_BUCKET,
  UNSCHEDULED_BUCKET,
} from "./taskProjections";

function task(partial: Partial<Task> & { id: string }): Task {
  return {
    title: `task-${partial.id}`,
    status: "open",
    ...partial,
  };
}

describe("normalizeTaskStatus", () => {
  it("maps wire aliases onto canonical statuses", () => {
    expect(normalizeTaskStatus("Completed")).toBe("done");
    expect(normalizeTaskStatus("in review")).toBe("review");
    expect(normalizeTaskStatus("cancelled")).toBe("canceled");
    expect(normalizeTaskStatus("In-Progress")).toBe("in_progress");
  });

  it("falls back to open for unknown or empty statuses", () => {
    expect(normalizeTaskStatus("hibernating")).toBe("open");
    expect(normalizeTaskStatus(undefined)).toBe("open");
  });
});

describe("groupTasksByStatus", () => {
  it("returns all-empty buckets for empty input", () => {
    const groups = groupTasksByStatus([]);
    expect(groups.in_progress).toEqual([]);
    expect(groups.open).toEqual([]);
    expect(groups.review).toEqual([]);
    expect(groups.pending).toEqual([]);
    expect(groups.blocked).toEqual([]);
    expect(groups.done).toEqual([]);
    expect(groups.canceled).toEqual([]);
  });

  it("buckets tasks by normalised status", () => {
    const tasks: Task[] = [
      task({ id: "1", status: "open" }),
      task({ id: "2", status: "completed" }),
      task({ id: "3", status: "in_review" }),
      task({ id: "4", status: "totally-unknown" }),
    ];
    const groups = groupTasksByStatus(tasks);
    expect(groups.open.map((t) => t.id)).toEqual(["1", "4"]);
    expect(groups.done.map((t) => t.id)).toEqual(["2"]);
    expect(groups.review.map((t) => t.id)).toEqual(["3"]);
  });
});

describe("groupTasksByAgent", () => {
  it("buckets tasks by owner slug and routes ownerless tasks to unassigned", () => {
    const tasks: Task[] = [
      task({ id: "1", owner: "pam" }),
      task({ id: "2", owner: "pam" }),
      task({ id: "3", owner: "jim" }),
      task({ id: "4" }),
      task({ id: "5", owner: "  " }),
    ];
    const groups = groupTasksByAgent(tasks);
    expect(groups.pam.map((t) => t.id)).toEqual(["1", "2"]);
    expect(groups.jim.map((t) => t.id)).toEqual(["3"]);
    expect(groups[UNASSIGNED_BUCKET].map((t) => t.id)).toEqual(["4", "5"]);
  });

  it("always includes the unassigned bucket even for empty input", () => {
    expect(groupTasksByAgent([])).toEqual({ [UNASSIGNED_BUCKET]: [] });
  });
});

describe("groupTasksByWeek", () => {
  const weekStart = new Date("2026-05-04T00:00:00.000Z"); // Monday

  it("buckets a scheduled task into its ISO day", () => {
    const tasks: Task[] = [
      task({ id: "1", due_at: "2026-05-06T14:00:00.000Z" }),
    ];
    const groups = groupTasksByWeek(tasks, weekStart);
    expect(groups["2026-05-06"].map((t) => t.id)).toEqual(["1"]);
    expect(groups[UNSCHEDULED_BUCKET]).toEqual([]);
  });

  it("routes undated and unparseable tasks to unscheduled", () => {
    const tasks: Task[] = [
      task({ id: "no-date" }),
      task({ id: "bad-date", due_at: "not-a-date" }),
    ];
    const groups = groupTasksByWeek(tasks, weekStart);
    expect(groups[UNSCHEDULED_BUCKET].map((t) => t.id)).toEqual([
      "no-date",
      "bad-date",
    ]);
  });

  it("drops tasks outside the seven-day window", () => {
    const tasks: Task[] = [
      task({ id: "next-week", due_at: "2026-05-12T00:00:00.000Z" }),
    ];
    const groups = groupTasksByWeek(tasks, weekStart);
    const totalInDays = Object.entries(groups)
      .filter(([key]) => key !== UNSCHEDULED_BUCKET)
      .reduce((sum, [, list]) => sum + list.length, 0);
    expect(totalInDays).toBe(0);
    expect(groups[UNSCHEDULED_BUCKET]).toEqual([]);
  });

  it("creates seven contiguous day buckets", () => {
    const groups = groupTasksByWeek([], weekStart);
    const dayKeys = Object.keys(groups)
      .filter((key) => key !== UNSCHEDULED_BUCKET)
      .sort();
    expect(dayKeys).toEqual([
      "2026-05-04",
      "2026-05-05",
      "2026-05-06",
      "2026-05-07",
      "2026-05-08",
      "2026-05-09",
      "2026-05-10",
    ]);
  });
});

describe("selectUnscheduled", () => {
  it("returns tasks without a parseable due_at", () => {
    const tasks: Task[] = [
      task({ id: "scheduled", due_at: "2026-05-06T00:00:00.000Z" }),
      task({ id: "no-date" }),
      task({ id: "bad-date", due_at: "nonsense" }),
    ];
    expect(selectUnscheduled(tasks).map((t) => t.id)).toEqual([
      "no-date",
      "bad-date",
    ]);
  });
});

describe("selectUnassigned", () => {
  it("returns tasks without a non-blank owner", () => {
    const tasks: Task[] = [
      task({ id: "owned", owner: "pam" }),
      task({ id: "no-owner" }),
      task({ id: "blank-owner", owner: "   " }),
    ];
    expect(selectUnassigned(tasks).map((t) => t.id)).toEqual([
      "no-owner",
      "blank-owner",
    ]);
  });
});

describe("groupTasksByLifecycle", () => {
  it("maps every status branch into the right lane", () => {
    const tasks: Task[] = [
      task({ id: "active-open", status: "open" }),
      task({ id: "active-in-progress", status: "in_progress" }),
      task({ id: "active-pending", status: "pending" }),
      task({ id: "rev", status: "review" }),
      task({ id: "block", status: "blocked" }),
      task({ id: "done", status: "done" }),
      task({ id: "cancel", status: "canceled" }),
      task({ id: "unknown", status: "mystery" }),
    ];
    const buckets = groupTasksByLifecycle(tasks);
    expect(buckets.open.map((t) => t.id)).toEqual([
      "active-open",
      "active-in-progress",
      "active-pending",
      "unknown",
    ]);
    expect(buckets.review.map((t) => t.id)).toEqual(["rev"]);
    expect(buckets.blocked.map((t) => t.id)).toEqual(["block"]);
    expect(buckets.done.map((t) => t.id)).toEqual(["done", "cancel"]);
  });

  it("returns four empty lanes for empty input", () => {
    expect(groupTasksByLifecycle([])).toEqual({
      open: [],
      review: [],
      blocked: [],
      done: [],
    });
  });
});
