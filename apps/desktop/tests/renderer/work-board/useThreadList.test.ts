import { describe, expect, it } from "vitest";

import { partitionByColumn } from "../../../src/renderer/work-board/useThreadList.ts";

import { sampleThreadView } from "./fixtures.ts";

describe("partitionByColumn", () => {
  it("buckets every thread by its boardColumn", () => {
    const threads = [
      sampleThreadView({ boardColumn: "needs_me" }),
      sampleThreadView({ boardColumn: "needs_me" }),
      sampleThreadView({ boardColumn: "running" }),
      sampleThreadView({ boardColumn: "review" }),
      sampleThreadView({ boardColumn: "done" }),
    ];
    const buckets = partitionByColumn(threads);

    expect(buckets.needs_me).toHaveLength(2);
    expect(buckets.running).toHaveLength(1);
    expect(buckets.review).toHaveLength(1);
    expect(buckets.done).toHaveLength(1);
  });

  it("returns empty arrays for every column when given no threads", () => {
    const buckets = partitionByColumn([]);
    expect(buckets.needs_me).toEqual([]);
    expect(buckets.running).toEqual([]);
    expect(buckets.review).toEqual([]);
    expect(buckets.done).toEqual([]);
  });

  it("preserves input order within each bucket", () => {
    const a = sampleThreadView({ boardColumn: "running", title: "first" });
    const b = sampleThreadView({ boardColumn: "running", title: "second" });
    const buckets = partitionByColumn([a, b]);
    expect(buckets.running.map((t) => t.title)).toEqual(["first", "second"]);
  });
});
