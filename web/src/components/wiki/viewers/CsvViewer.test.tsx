import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) => `https://test.local/wiki/file?path=${path}`,
}));

import CsvViewer from "./CsvViewer";

function mockFetchText(text: string, ok = true, status = 200) {
  const fetchMock = vi.fn(async () => ({
    ok,
    status,
    text: async () => text,
  }));
  vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
  return fetchMock;
}

describe("<CsvViewer>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    cleanup();
  });

  it("renders headers and rows for a simple CSV", async () => {
    mockFetchText("name,role\nSarah,PM\nAlex,Eng\n");
    render(<CsvViewer path="team/assets/people.csv" />);

    await waitFor(() =>
      expect(
        screen.getByRole("columnheader", { name: "name" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("columnheader", { name: "role" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Sarah" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Eng" })).toBeInTheDocument();
    // Two data rows reported in the toolbar.
    expect(screen.getByText(/2 rows · 2 columns/)).toBeInTheDocument();
  });

  it("handles quoted fields with embedded commas and newlines", async () => {
    mockFetchText('a,b\n"x,y","line1\nline2"\n');
    render(<CsvViewer path="team/assets/quoted.csv" />);

    await waitFor(() =>
      expect(screen.getByRole("cell", { name: "x,y" })).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("cell", { name: /line1\s+line2/ }),
    ).toBeInTheDocument();
  });

  it("handles escaped quotes and CRLF line endings", async () => {
    mockFetchText('h1,h2\r\n"say ""hi""",plain\r\n');
    render(<CsvViewer path="team/assets/crlf.csv" />);

    await waitFor(() =>
      expect(
        screen.getByRole("cell", { name: 'say "hi"' }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("cell", { name: "plain" })).toBeInTheDocument();
  });

  it("exposes Download + open-in-new-tab actions for the file", async () => {
    mockFetchText("name,role\nSarah,PM\n");
    render(<CsvViewer path="team/assets/people.csv" />);

    await waitFor(() =>
      expect(
        screen.getByRole("link", { name: /download/i }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: /download/i })).toHaveAttribute(
      "download",
      "people.csv",
    );
    expect(
      screen.getByRole("link", { name: /open in new tab/i }),
    ).toHaveAttribute("target", "_blank");
  });

  it("shows the error state when the fetch fails", async () => {
    mockFetchText("", false, 404);
    render(<CsvViewer path="team/assets/missing.csv" />);

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/HTTP 404/),
    );
  });

  it("shows the empty state for an empty file", async () => {
    mockFetchText("");
    render(<CsvViewer path="team/assets/empty.csv" />);

    await waitFor(() => expect(screen.getByText(/empty/i)).toBeInTheDocument());
  });
});
