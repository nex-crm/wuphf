import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { DataTable } from "../../../src/tui/components/data-table.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

describe("DataTable", () => {
  const headers = ["Name", "Type", "Count"];
  const rows = [
    ["Acme Corp", "company", "42"],
    ["Jane Doe", "person", "7"],
    ["Project X", "project", "13"],
  ];

  it("renders all headers", () => {
    const { lastFrame } = render(
      <DataTable headers={headers} rows={rows} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Name"), "should show Name header");
    assert.ok(frame.includes("Type"), "should show Type header");
    assert.ok(frame.includes("Count"), "should show Count header");
  });

  it("renders all row data", () => {
    const { lastFrame } = render(
      <DataTable headers={headers} rows={rows} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Acme Corp"), "should show Acme Corp");
    assert.ok(frame.includes("Jane Doe"), "should show Jane Doe");
    assert.ok(frame.includes("Project X"), "should show Project X");
    assert.ok(frame.includes("company"), "should show company type");
  });

  it("shows row count footer", () => {
    const { lastFrame } = render(
      <DataTable headers={headers} rows={rows} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("3 rows"), "should show 3 rows in footer");
  });

  it("shows singular row for single item", () => {
    const { lastFrame } = render(
      <DataTable headers={["Name"]} rows={[["Alice"]]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("1 row"), "should show 1 row");
    assert.ok(!frame.includes("1 rows"), "should not show 1 rows");
  });

  it("renders separator line between header and rows", () => {
    const { lastFrame } = render(
      <DataTable headers={headers} rows={rows} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("\u2500"), "should contain horizontal line chars");
    assert.ok(frame.includes("\u253C"), "should contain cross connector");
  });

  it("truncates long cell values with ellipsis", () => {
    const longRows = [["This is a very long company name that exceeds limit", "type", "1"]];
    const { lastFrame } = render(
      <DataTable headers={headers} rows={longRows} maxColWidth={20} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("\u2026"), "should contain ellipsis for truncated text");
    assert.ok(
      !frame.includes("This is a very long company name that exceeds limit"),
      "should not show full long text",
    );
  });

  it("right-aligns specified columns", () => {
    const { lastFrame } = render(
      <DataTable headers={headers} rows={rows} alignRight={[2]} />,
    );
    const frame = strip(lastFrame() ?? "");
    // The Count column values should be right-aligned (padded with leading spaces)
    // "42" in a column of width 5 should be "   42"
    const lines = frame.split("\n");
    // Find a row containing "42" — it should have leading spaces before the number
    const acmeLine = lines.find((l) => l.includes("Acme Corp"));
    assert.ok(acmeLine, "should find Acme Corp line");
    // Right-aligned "42" in width 5 → " 42" (3 spaces before)
    assert.ok(acmeLine.includes(" 42"), "count should be right-padded (leading space)");
  });

  it("auto-sizes columns based on content", () => {
    const wideRows = [
      ["Short", "x"],
      ["A much wider value", "y"],
    ];
    const { lastFrame } = render(
      <DataTable headers={["Label", "V"]} rows={wideRows} />,
    );
    const frame = strip(lastFrame() ?? "");
    // Both rows should be rendered properly without truncation at default maxColWidth
    assert.ok(frame.includes("A much wider value"), "should show wide value fully");
    assert.ok(frame.includes("Short"), "should show short value");
  });

  it("handles empty rows", () => {
    const { lastFrame } = render(
      <DataTable headers={["Name"]} rows={[]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Name"), "should still show header");
    assert.ok(frame.includes("0 rows"), "should show 0 rows");
  });

  it("handles missing cell values gracefully", () => {
    const sparseRows = [
      ["Alice"],
      ["Bob", "person", "5"],
    ];
    const { lastFrame } = render(
      <DataTable headers={headers} rows={sparseRows} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Alice"), "should show Alice");
    assert.ok(frame.includes("Bob"), "should show Bob");
  });
});
