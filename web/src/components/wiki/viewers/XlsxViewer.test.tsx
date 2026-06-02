import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { utils, write } from "xlsx";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) => `https://test.local/wiki/file?path=${path}`,
}));

import XlsxViewer from "./XlsxViewer";

/** Build a real XLSX arrayBuffer fixture so the SheetJS parse path is exercised. */
function buildWorkbook(
  sheets: Record<string, (string | number)[][]>,
): ArrayBuffer {
  const wb = utils.book_new();
  for (const [name, aoa] of Object.entries(sheets)) {
    utils.book_append_sheet(wb, utils.aoa_to_sheet(aoa), name);
  }
  // SheetJS `type: "array"` returns an ArrayBuffer directly — matching what
  // `Response.arrayBuffer()` hands the component in production.
  return write(wb, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
}

function mockFetchBuffer(buf: ArrayBuffer, ok = true, status = 200) {
  const fetchMock = vi.fn(async () => ({
    ok,
    status,
    arrayBuffer: async () => buf,
  }));
  vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
  return fetchMock;
}

describe("<XlsxViewer>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    cleanup();
  });

  it("renders the active sheet as a table", async () => {
    const buf = buildWorkbook({
      Sheet1: [
        ["name", "score"],
        ["Sarah", 42],
        ["Alex", 7],
      ],
    });
    mockFetchBuffer(buf);
    render(<XlsxViewer path="team/assets/data.xlsx" />);

    await waitFor(() =>
      expect(
        screen.getByRole("columnheader", { name: "name" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("cell", { name: "Sarah" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "42" })).toBeInTheDocument();
  });

  it("renders sheet tabs and switches the active sheet", async () => {
    const buf = buildWorkbook({
      Alpha: [["a"], ["1"]],
      Beta: [["b"], ["2"]],
    });
    mockFetchBuffer(buf);
    render(<XlsxViewer path="team/assets/multi.xlsx" />);

    const betaTab = await screen.findByRole("tab", { name: "Sheet Beta" });
    expect(screen.getByRole("tab", { name: "Sheet Alpha" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    betaTab.click();
    await waitFor(() =>
      expect(betaTab).toHaveAttribute("aria-selected", "true"),
    );
    expect(screen.getByRole("cell", { name: "2" })).toBeInTheDocument();
  });

  it("implements the WAI-ARIA tabs pattern: tablist/tabpanel wiring + roving tabindex", async () => {
    const buf = buildWorkbook({
      Alpha: [["a"], ["1"]],
      Beta: [["b"], ["2"]],
    });
    mockFetchBuffer(buf);
    render(<XlsxViewer path="team/assets/multi.xlsx" />);

    const alphaTab = await screen.findByRole("tab", { name: "Sheet Alpha" });
    const betaTab = screen.getByRole("tab", { name: "Sheet Beta" });

    // The tablist is labelled and carries both tabs.
    const tablist = screen.getByRole("tablist", { name: "Sheets" });
    expect(tablist).toContainElement(alphaTab);

    // Roving tabindex: only the active tab is in the Tab order.
    expect(alphaTab).toHaveAttribute("tabindex", "0");
    expect(betaTab).toHaveAttribute("tabindex", "-1");

    // The panel is wired to the active tab in both directions.
    const panel = screen.getByRole("tabpanel");
    expect(alphaTab).toHaveAttribute("aria-controls", panel.id);
    expect(panel).toHaveAttribute("aria-labelledby", alphaTab.id);
  });

  it("moves selection with arrow keys (roving focus) per the tabs pattern", async () => {
    const buf = buildWorkbook({
      Alpha: [["a"], ["1"]],
      Beta: [["b"], ["2"]],
    });
    mockFetchBuffer(buf);
    render(<XlsxViewer path="team/assets/multi.xlsx" />);

    const alphaTab = await screen.findByRole("tab", { name: "Sheet Alpha" });
    const betaTab = screen.getByRole("tab", { name: "Sheet Beta" });

    alphaTab.focus();
    fireEvent.keyDown(alphaTab, { key: "ArrowRight" });

    await waitFor(() =>
      expect(betaTab).toHaveAttribute("aria-selected", "true"),
    );
    expect(betaTab).toHaveFocus();
    expect(betaTab).toHaveAttribute("tabindex", "0");
    expect(alphaTab).toHaveAttribute("tabindex", "-1");

    // End jumps to the last tab; Home jumps back to the first, wrapping correctly.
    fireEvent.keyDown(betaTab, { key: "Home" });
    await waitFor(() =>
      expect(alphaTab).toHaveAttribute("aria-selected", "true"),
    );
    expect(alphaTab).toHaveFocus();
  });

  it("exposes Download + open-in-new-tab actions in the toolbar", async () => {
    const buf = buildWorkbook({ Sheet1: [["a"], ["1"]] });
    mockFetchBuffer(buf);
    render(<XlsxViewer path="team/assets/data.xlsx" />);

    await waitFor(() =>
      expect(
        screen.getByRole("link", { name: /download/i }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: /download/i })).toHaveAttribute(
      "download",
      "data.xlsx",
    );
    expect(
      screen.getByRole("link", { name: /open in new tab/i }),
    ).toHaveAttribute("target", "_blank");
  });

  it("shows the error state when the fetch fails", async () => {
    mockFetchBuffer(new ArrayBuffer(0), false, 500);
    render(<XlsxViewer path="team/assets/missing.xlsx" />);

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/HTTP 500/),
    );
  });

  it("caps huge sheets and surfaces a truncation notice", async () => {
    const rows: (string | number)[][] = [["idx"]];
    for (let i = 0; i < 600; i++) rows.push([i]);
    const buf = buildWorkbook({ Big: rows });
    mockFetchBuffer(buf);
    render(<XlsxViewer path="team/assets/big.xlsx" />);

    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent(
        /Showing the first 500 of 601 rows/i,
      ),
    );
    // Only MAX_ROWS rows are rendered even though the sheet has 601.
    expect(screen.getAllByRole("row")).toHaveLength(500);
  });
});
