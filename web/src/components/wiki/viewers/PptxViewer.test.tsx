import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import PptxViewer from "./PptxViewer";

// Mock the wiki API so the viewer resolves a deterministic URL without
// touching the real client / auth-token machinery.
vi.mock("../../../api/wiki", () => ({
  wikiFileUrl: (path: string) => `https://wiki.test/file?path=${path}`,
}));

// Mock the heavy pptx renderer: we are testing the viewer's fetch + lifecycle
// orchestration, not pptx-preview's OOXML parsing. `init` returns a fake
// previewer whose `preview` writes a sentinel node into the container so we can
// assert it rendered into the element we own. `destroy` is observable so we can
// assert teardown on unmount.
const preview = vi.fn(async (_buf: ArrayBuffer): Promise<unknown> => undefined);
const destroy = vi.fn();
const init = vi.fn((container: HTMLElement, _options: unknown) => {
  return {
    preview: async (buf: ArrayBuffer) => {
      const node = document.createElement("div");
      node.textContent = "rendered-pptx-slides";
      container.appendChild(node);
      return preview(buf);
    },
    destroy,
  };
});

vi.mock("pptx-preview", () => ({
  init: (container: HTMLElement, options: unknown) => init(container, options),
}));

const PATH = "team/assets/deck.pptx";

describe("<PptxViewer>", () => {
  beforeEach(() => {
    init.mockClear();
    preview.mockClear();
    destroy.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("fetches the file and renders slides into a container it owns", async () => {
    const buffer = new ArrayBuffer(8);
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      arrayBuffer: async () => buffer,
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PptxViewer path={PATH} />);

    await waitFor(() => {
      expect(screen.getByText("rendered-pptx-slides")).toBeInTheDocument();
    });

    // It fetched the tokened wiki URL for this path.
    expect(fetchMock).toHaveBeenCalledWith(
      `https://wiki.test/file?path=${PATH}`,
    );
    // init received a real container element; preview received the buffer.
    expect(init).toHaveBeenCalledTimes(1);
    expect(init.mock.calls[0][0]).toBeInstanceOf(HTMLElement);
    expect(preview).toHaveBeenCalledWith(buffer);

    // The filename is surfaced in the toolbar.
    expect(screen.getByText("deck.pptx")).toBeInTheDocument();
  });

  it("destroys the previewer on unmount", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      arrayBuffer: async () => new ArrayBuffer(8),
    });
    vi.stubGlobal("fetch", fetchMock);

    const { unmount } = render(<PptxViewer path={PATH} />);
    await waitFor(() => {
      expect(screen.getByText("rendered-pptx-slides")).toBeInTheDocument();
    });

    unmount();
    expect(destroy).toHaveBeenCalled();
  });

  it("shows the error state when the fetch fails", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error("network down"));
    vi.stubGlobal("fetch", fetchMock);

    render(<PptxViewer path={PATH} />);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("network down");
    expect(init).not.toHaveBeenCalled();
  });

  it("shows the error state on a non-ok HTTP response", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      arrayBuffer: async () => new ArrayBuffer(0),
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PptxViewer path={PATH} />);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("500");
  });
});
