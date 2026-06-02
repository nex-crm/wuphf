import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) =>
    `/wiki/file?path=${encodeURIComponent(path)}&token=test`,
}));

import SourceViewer from "./SourceViewer";

const fetchMock = vi.fn();

function textResponse(status: number, body: string): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "text/plain" },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SourceViewer", () => {
  it("renders highlighted source for a known language", async () => {
    fetchMock.mockResolvedValue(
      textResponse(200, "const greeting = 'hi';\nconsole.log(greeting);"),
    );

    const { container } = render(
      <SourceViewer path="team/assets/snippet.ts" />,
    );

    await waitFor(() => {
      // lowlight may split the identifier across declaration + usage spans,
      // so several elements can carry the token text.
      expect(screen.getAllByText(/greeting/).length).toBeGreaterThan(0);
    });
    // lowlight emits hljs-* token spans, converted to real React <span>
    // elements (no innerHTML). `const` is a keyword in the TS grammar.
    const tokenSpans = container.querySelectorAll("code span[class*='hljs-']");
    expect(tokenSpans.length).toBeGreaterThan(0);
    // Language label derived from the .ts extension.
    expect(screen.getByText("typescript")).toBeInTheDocument();
    // Line gutter is rendered for each line.
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("renders plain text for an unknown extension without crashing", async () => {
    fetchMock.mockResolvedValue(textResponse(200, "no <highlight> here"));

    render(<SourceViewer path="team/assets/notes.unknownext" />);

    await waitFor(() => {
      // The raw text survives (escaped) rather than being interpreted as HTML.
      expect(screen.getByText(/no <highlight> here/)).toBeInTheDocument();
    });
  });

  it("shows the error state when the fetch fails", async () => {
    fetchMock.mockResolvedValue(textResponse(404, "missing"));

    render(<SourceViewer path="team/assets/gone.go" />);

    await waitFor(() => {
      expect(screen.getByRole("alert")).toBeInTheDocument();
    });
    expect(screen.getByText(/could not load this file/i)).toBeInTheDocument();
  });

  it("shows the error state when fetch rejects", async () => {
    fetchMock.mockRejectedValue(new Error("network down"));

    render(<SourceViewer path="team/assets/broken.py" />);

    await waitFor(() => {
      expect(screen.getByRole("alert")).toBeInTheDocument();
    });
    expect(screen.getByText(/network down/i)).toBeInTheDocument();
  });

  it("exposes Download and Raw actions plus a wrap toggle", async () => {
    fetchMock.mockResolvedValue(textResponse(200, "select 1;"));

    render(<SourceViewer path="team/assets/query.sql" />);

    await waitFor(() => {
      expect(screen.getByText("sql")).toBeInTheDocument();
    });

    expect(screen.getByRole("link", { name: /download/i })).toHaveAttribute(
      "download",
      "query.sql",
    );
    expect(screen.getByRole("link", { name: /^raw$/i })).toHaveAttribute(
      "target",
      "_blank",
    );

    const wrapToggle = screen.getByRole("button", { name: /wrap/i });
    expect(wrapToggle).toHaveAttribute("aria-pressed", "false");
  });

  it("caps very large files with a notice", async () => {
    const huge = "x".repeat(512 * 1024 + 10);
    fetchMock.mockResolvedValue(textResponse(200, huge));

    render(<SourceViewer path="team/assets/big.txt" />);

    await waitFor(() => {
      expect(screen.getByText(/this file is large/i)).toBeInTheDocument();
    });
  });

  it("shows the empty state for a zero-byte file", async () => {
    fetchMock.mockResolvedValue(textResponse(200, ""));

    render(<SourceViewer path="team/assets/empty.json" />);

    await waitFor(() => {
      expect(screen.getByText(/this file is empty/i)).toBeInTheDocument();
    });
  });
});
