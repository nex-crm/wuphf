import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { generateGraphHtml } from "../../src/lib/graph-html.js";
import type { GraphData } from "../../src/lib/graph-html.js";

function makeData(overrides?: Partial<GraphData>): GraphData {
  return {
    nodes: [],
    edges: [],
    context_edges: [],
    relationship_definitions: [],
    insights: {},
    total_nodes: 0,
    total_edges: 0,
    total_context_edges: 0,
    ...overrides,
  };
}

describe("generateGraphHtml", () => {
  it("returns HTML containing force-graph reference", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.includes("force-graph"), "should reference force-graph");
    assert.ok(html.includes("<!DOCTYPE html>"), "should be a full HTML document");
  });

  it("renders empty graph without error", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.length > 0);
    assert.ok(html.includes("0 entities"));
    assert.ok(html.includes("0 connections"));
    assert.ok(html.includes("0 insights"));
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

  it("includes context edge and triplet edge legend items", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.includes("Formal"), "should have Formal edge legend");
    assert.ok(html.includes("Context"), "should have Context edge legend");
    assert.ok(html.includes("Triplet"), "should have Triplet edge legend");
  });

  it("computes stats bar with combined connection count", () => {
    const data = makeData({
      total_nodes: 5,
      total_edges: 3,
      total_context_edges: 2,
    });
    const html = generateGraphHtml(data);
    assert.ok(html.includes("5 entities"), "should show total nodes");
    assert.ok(html.includes("5 connections"), "should show combined edge count (3+2)");
  });

  it("counts total insights across all entities", () => {
    const data = makeData({
      insights: {
        "1": [
          { content: "Likes coffee", confidence: 0.9, type: "preference" },
          { content: "Works remotely", confidence: 0.8, type: "behavior" },
        ],
        "2": [
          { content: "CEO", confidence: 0.95, type: "role" },
        ],
      },
    });
    const html = generateGraphHtml(data);
    assert.ok(html.includes("3 insights"), "should show total insight count");
  });
});
