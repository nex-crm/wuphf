import { describe, test, expect } from "bun:test";
import { generateGraphHtml } from "../../src/lib/graph-html.ts";
import type { GraphData } from "../../src/lib/graph-html.ts";

function makeData(overrides?: Partial<GraphData>): GraphData {
  return {
    nodes: [],
    edges: [],
    context_edges: [],
    insight_nodes: [],
    relationship_definitions: [],
    insights: {},
    total_nodes: 0,
    total_edges: 0,
    total_context_edges: 0,
    ...overrides,
  };
}

describe("generateGraphHtml", () => {
  test("returns HTML containing D3 force-graph reference", () => {
    const html = generateGraphHtml(makeData());
    expect(html.includes("d3@7")).toBeTruthy();
    expect(html.includes("forceSimulation")).toBeTruthy();
    expect(html.includes("<!DOCTYPE html>")).toBeTruthy();
  });

  test("renders empty graph without error", () => {
    const html = generateGraphHtml(makeData());
    expect(html.length > 0).toBeTruthy();
    expect(html.includes("0 entities")).toBeTruthy();
    expect(html.includes("0 connections")).toBeTruthy();
  });

  test("embeds node data as JSON", () => {
    const data = makeData({
      nodes: [
        { id: "1", name: "Alice", type: "person", definition_slug: "person", primary_attribute: "alice@co.com" },
      ],
      total_nodes: 1,
    });
    const html = generateGraphHtml(data);
    expect(html.includes("Alice")).toBeTruthy();
    expect(html.includes("graph-data")).toBeTruthy();
  });

  test("escapes HTML special characters to prevent XSS", () => {
    const data = makeData({
      nodes: [
        { id: "1", name: '<script>alert(1)</script>', type: "person", definition_slug: "person" },
      ],
      total_nodes: 1,
    });
    const html = generateGraphHtml(data);
    expect(!html.includes("<script>alert(1)</script>")).toBeTruthy();
    expect(
      html.includes("\\u003cscript\\u003e") || html.includes("&lt;script&gt;"),
    ).toBeTruthy();
  });

  test("includes edge type legend items including insight", () => {
    const html = generateGraphHtml(makeData());
    expect(html.includes("Formal")).toBeTruthy();
    expect(html.includes("Context")).toBeTruthy();
    expect(html.includes("Triplet")).toBeTruthy();
    expect(html.includes("Insight")).toBeTruthy();
  });

  test("computes stats bar with combined connection count", () => {
    const data = makeData({ total_edges: 3, total_context_edges: 7 });
    const html = generateGraphHtml(data);
    expect(html.includes("10 connections")).toBeTruthy();
  });

  test("counts total insights across all entities and insight nodes", () => {
    const data = makeData({
      insights: {
        "1": [{ content: "a", confidence: 0.9, type: "fact" }],
        "2": [{ content: "b", confidence: 0.8, type: "context" }, { content: "c", confidence: 0.7, type: "status" }],
      },
      insight_nodes: [
        { id: "ei:1", content: "test", type: "fact", confidence: 0.9, source: "entity_insight" },
      ],
    });
    const html = generateGraphHtml(data);
    expect(html.includes("4 insights")).toBeTruthy();
  });

  test("renders ghost nodes in legend when present", () => {
    const data = makeData({
      nodes: [
        { id: "ghost:123", name: "Some Topic", type: "ghost", definition_slug: "ghost" },
      ],
      total_nodes: 1,
    });
    const html = generateGraphHtml(data);
    expect(html.includes("ghost")).toBeTruthy();
    expect(html.includes("#8B5CF6")).toBeTruthy();
  });

  test("renders insight nodes in legend when present", () => {
    const data = makeData({
      insight_nodes: [
        { id: "ei:1", content: "test insight", type: "fact", confidence: 0.9, source: "entity_insight" },
      ],
    });
    const html = generateGraphHtml(data);
    expect(html.includes("entity_insight")).toBeTruthy();
  });
});
