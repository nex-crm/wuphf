import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { generateGraphHtml } from "../../src/lib/graph-html.js";
import type { GraphData } from "../../src/lib/graph-html.js";

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
  it("returns HTML containing D3 force-graph reference", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.includes("d3@7"), "should reference D3.js v7");
    assert.ok(html.includes("forceSimulation"), "should use D3 force-graph simulation");
    assert.ok(html.includes("<!DOCTYPE html>"), "should be a full HTML document");
  });

  it("renders empty graph without error", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.length > 0);
    assert.ok(html.includes("0 entities"));
    assert.ok(html.includes("0 connections"));
  });

  it("embeds node data as JSON", () => {
    const data = makeData({
      nodes: [
        { id: "1", name: "Alice", type: "person", definition_slug: "person", primary_attribute: "alice@co.com" },
      ],
      total_nodes: 1,
    });
    const html = generateGraphHtml(data);
    assert.ok(html.includes("Alice"), "node name should appear in output");
    assert.ok(html.includes("graph-data"), "should contain graph-data script block");
  });

  it("escapes HTML special characters to prevent XSS", () => {
    const data = makeData({
      nodes: [
        { id: "1", name: '<script>alert(1)</script>', type: "person", definition_slug: "person" },
      ],
      total_nodes: 1,
    });
    const html = generateGraphHtml(data);
    assert.ok(!html.includes("<script>alert(1)</script>"), "raw script tag must not appear");
    assert.ok(
      html.includes("\\u003cscript\\u003e") || html.includes("&lt;script&gt;"),
      "script tag should be escaped",
    );
  });

  it("includes edge type legend items including insight", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.includes("Formal"), "should include Formal edge legend");
    assert.ok(html.includes("Context"), "should include Context edge legend");
    assert.ok(html.includes("Triplet"), "should include Triplet edge legend");
    assert.ok(html.includes("Insight"), "should include Insight edge legend");
  });

  it("computes stats bar with combined connection count", () => {
    const data = makeData({ total_edges: 3, total_context_edges: 7 });
    const html = generateGraphHtml(data);
    assert.ok(html.includes("10 connections"), "should show sum of edges + context_edges");
  });

  it("counts total insights across all entities and insight nodes", () => {
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
    assert.ok(html.includes("4 insights"), "should count 3 from map + 1 from insight_nodes");
  });

  it("renders ghost nodes in legend when present", () => {
    const data = makeData({
      nodes: [
        { id: "ghost:123", name: "Some Topic", type: "ghost", definition_slug: "ghost" },
      ],
      total_nodes: 1,
    });
    const html = generateGraphHtml(data);
    assert.ok(html.includes("ghost"), "should include ghost in legend");
    assert.ok(html.includes("#8B5CF6"), "should use purple color for ghost");
  });

  it("renders insight nodes in legend when present", () => {
    const data = makeData({
      insight_nodes: [
        { id: "ei:1", content: "test insight", type: "fact", confidence: 0.9, source: "entity_insight" },
      ],
    });
    const html = generateGraphHtml(data);
    assert.ok(html.includes("entity_insight"), "should include entity_insight in legend");
  });
});
