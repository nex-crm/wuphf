import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { generateGraphHtml } from "../../src/lib/graph-html.js";
import type { GraphData } from "../../src/lib/graph-html.js";

function makeData(overrides?: Partial<GraphData>): GraphData {
  return {
    nodes: [],
    edges: [],
    relationship_definitions: [],
    total_nodes: 0,
    total_edges: 0,
    ...overrides,
  };
}

describe("generateGraphHtml", () => {
  it("returns HTML containing sigma and graphology references", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.includes("sigma"), "should reference sigma");
    assert.ok(html.includes("graphology"), "should reference graphology");
    assert.ok(html.includes("<!DOCTYPE html>"), "should be a full HTML document");
  });

  it("renders empty graph without error", () => {
    const html = generateGraphHtml(makeData());
    assert.ok(html.length > 0);
    assert.ok(html.includes("0 entities"));
    assert.ok(html.includes("0 relationships"));
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
});
