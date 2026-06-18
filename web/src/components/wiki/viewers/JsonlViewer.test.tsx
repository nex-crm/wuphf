import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) => `/wiki/file?path=${encodeURIComponent(path)}`,
}));

import JsonlViewer from "./JsonlViewer";

function mockFetchText(text: string, ok = true): void {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok,
      status: ok ? 200 : 500,
      text: async () => text,
    })),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("<JsonlViewer>", () => {
  it("renders a table when every record is a flat object with shared keys", async () => {
    mockFetchText(
      [
        JSON.stringify({ name: "Acme", stage: "lead" }),
        JSON.stringify({ name: "Globex", stage: "won" }),
      ].join("\n"),
    );
    render(<JsonlViewer path="team/companies/pipeline.jsonl" />);

    await waitFor(() =>
      expect(
        screen.getByRole("columnheader", { name: "name" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("columnheader", { name: "stage" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Acme" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "won" })).toBeInTheDocument();
    expect(screen.getByText("2 records")).toBeInTheDocument();
  });

  it("unions keys across records and shows an em-dash for missing values", async () => {
    mockFetchText(
      [JSON.stringify({ a: "1" }), JSON.stringify({ a: "2", b: "extra" })].join(
        "\n",
      ),
    );
    render(<JsonlViewer path="team/data/x.jsonl" />);

    await waitFor(() =>
      expect(
        screen.getByRole("columnheader", { name: "b" }),
      ).toBeInTheDocument(),
    );
    // First record has no `b`, so its cell renders the missing-value dash.
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("falls back to per-record cards for non-object records", async () => {
    mockFetchText(['"just a string"', "[1, 2, 3]"].join("\n"));
    render(<JsonlViewer path="team/data/mixed.jsonl" />);

    await waitFor(() =>
      expect(screen.getByText("2 records")).toBeInTheDocument(),
    );
    // No table is rendered for heterogeneous/non-object records.
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText(/just a string/)).toBeInTheDocument();
  });

  it("keeps an unparseable line verbatim and counts it", async () => {
    mockFetchText(
      [JSON.stringify({ ok: true }), "{ not valid json"].join("\n"),
    );
    render(<JsonlViewer path="team/data/broken.jsonl" />);

    await waitFor(() =>
      expect(screen.getByText(/1 unparseable line/)).toBeInTheDocument(),
    );
    expect(screen.getByText(/not valid json/)).toBeInTheDocument();
  });

  it("shows an empty state when the file has no records", async () => {
    mockFetchText("\n  \n");
    render(<JsonlViewer path="team/data/empty.jsonl" />);
    await waitFor(() =>
      expect(screen.getByText(/this jsonl file is empty/i)).toBeInTheDocument(),
    );
  });

  it("surfaces a load error", async () => {
    mockFetchText("", false);
    render(<JsonlViewer path="team/data/x.jsonl" />);
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/HTTP 500/),
    );
  });
});
