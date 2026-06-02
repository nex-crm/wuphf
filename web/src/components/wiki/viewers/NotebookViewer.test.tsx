import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) => `https://test.local/wiki/file?path=${path}`,
}));

import NotebookViewer from "./NotebookViewer";

interface NotebookFixture {
  cells?: unknown[];
  metadata?: Record<string, unknown>;
}

function mockFetchJson(value: NotebookFixture, ok = true, status = 200) {
  const fetchMock = vi.fn(async () => ({
    ok,
    status,
    json: async () => value,
    text: async () => JSON.stringify(value),
  }));
  vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
  return fetchMock;
}

function mockFetchReject() {
  const fetchMock = vi.fn(async () => {
    throw new Error("network down");
  });
  vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
  return fetchMock;
}

describe("<NotebookViewer>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    cleanup();
  });

  it("renders markdown cells, code cells, and stream output", async () => {
    mockFetchJson({
      metadata: { language_info: { name: "python" } },
      cells: [
        { cell_type: "markdown", source: ["# Hello\n", "\n", "Some prose."] },
        {
          cell_type: "code",
          execution_count: 1,
          source: "print('hi there')\n",
          outputs: [
            { output_type: "stream", name: "stdout", text: "hi there\n" },
          ],
        },
      ],
    });

    render(<NotebookViewer path="team/notebooks/demo.ipynb" />);

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Hello" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("Some prose.")).toBeInTheDocument();
    // Code source is highlighted but the text content survives.
    expect(screen.getByText(/print/)).toBeInTheDocument();
    // Stream output is shown verbatim.
    expect(screen.getByText("hi there")).toBeInTheDocument();
    // Toolbar summary reflects the cell counts + language.
    expect(screen.getByText(/2 cells · 1 code · python/)).toBeInTheDocument();
  });

  it("renders an image/png output as a data-uri image", async () => {
    const b64 =
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
    mockFetchJson({
      cells: [
        {
          cell_type: "code",
          source: "show_plot()\n",
          outputs: [
            {
              output_type: "display_data",
              data: { "image/png": b64 },
            },
          ],
        },
      ],
    });

    render(<NotebookViewer path="team/notebooks/plot.ipynb" />);

    const img = await screen.findByRole("img", {
      name: /notebook cell/i,
    });
    expect(img).toHaveAttribute("src", `data:image/png;base64,${b64}`);
  });

  it("uses a distinct alt per cell for image outputs", async () => {
    const b64 =
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
    mockFetchJson({
      cells: [
        { cell_type: "markdown", source: ["# Intro"] },
        {
          cell_type: "code",
          source: "show_plot()\n",
          outputs: [
            { output_type: "display_data", data: { "image/png": b64 } },
          ],
        },
      ],
    });

    render(<NotebookViewer path="team/notebooks/plot.ipynb" />);

    // The image output sits in cell index 1, so the alt references that cell.
    const img = await screen.findByRole("img", {
      name: /from notebook cell 2/i,
    });
    expect(img).toBeInTheDocument();
  });

  it("skips an oversized image/png output with a placeholder notice", async () => {
    // A base64 string longer than the cap (~2.7M chars) is replaced by a
    // placeholder rather than mounted, bounding memory on a crafted notebook.
    const huge = "A".repeat(2_700_001);
    mockFetchJson({
      cells: [
        {
          cell_type: "code",
          source: "big_image()\n",
          outputs: [
            { output_type: "display_data", data: { "image/png": huge } },
          ],
        },
      ],
    });

    render(<NotebookViewer path="team/notebooks/huge.ipynb" />);

    await waitFor(() =>
      expect(screen.getByText(/too large to display/i)).toBeInTheDocument(),
    );
    // No actual image element is mounted for the oversized payload.
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  it("renders a text/html output inside a scriptless sandboxed iframe", async () => {
    mockFetchJson({
      cells: [
        {
          cell_type: "code",
          source: "df.head()\n",
          outputs: [
            {
              output_type: "execute_result",
              data: { "text/html": "<table><tr><td>cell</td></tr></table>" },
            },
          ],
        },
      ],
    });

    render(<NotebookViewer path="team/notebooks/table.ipynb" />);

    const iframe = await screen.findByTitle("Notebook HTML output");
    // sandbox="" with no allow-scripts: embedded scripts cannot execute.
    expect(iframe).toHaveAttribute("sandbox", "");
    expect(iframe.getAttribute("srcdoc")).toContain("<td>cell</td>");
  });

  it("shows the error state when the fetch fails", async () => {
    mockFetchReject();
    render(<NotebookViewer path="team/notebooks/missing.ipynb" />);

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/network down/),
    );
  });

  it("shows the empty state when the notebook has no cells", async () => {
    mockFetchJson({ cells: [] });
    render(<NotebookViewer path="team/notebooks/empty.ipynb" />);

    await waitFor(() =>
      expect(screen.getByText(/no cells/i)).toBeInTheDocument(),
    );
  });
});
