import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) => `https://test.local/wiki/file?path=${path}`,
}));

// DOMPurify in happy-dom can be flaky on bare SVG strings; stub it to a
// pass-through so the test asserts the viewer's own wiring, not the sanitizer.
vi.mock("dompurify", () => ({
  default: { sanitize: (s: string) => s },
}));

const parse = vi.fn(async (_src: string) => true);
const renderFn = vi.fn(async (_id: string, _src: string) => ({
  svg: '<svg aria-label="diagram"><g></g></svg>',
}));
const initialize = vi.fn((_config: unknown) => undefined);

vi.mock("mermaid", () => ({
  default: {
    initialize: (config: unknown) => initialize(config),
    parse: (src: string) => parse(src),
    render: (id: string, src: string) => renderFn(id, src),
  },
}));

import MermaidViewer from "./MermaidViewer";

function mockFetchText(text: string, ok = true, status = 200) {
  const fetchMock = vi.fn(async () => ({
    ok,
    status,
    text: async () => text,
  }));
  vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);
  return fetchMock;
}

describe("<MermaidViewer>", () => {
  beforeEach(() => {
    parse.mockClear();
    renderFn.mockClear();
    initialize.mockClear();
    parse.mockImplementation(async () => true);
    renderFn.mockImplementation(async () => ({
      svg: '<svg aria-label="diagram"><g></g></svg>',
    }));
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("fetches the source, initializes in strict mode, and renders the SVG as a data-uri image", async () => {
    mockFetchText("graph TD; A-->B;");
    render(<MermaidViewer path="team/diagrams/flow.mmd" />);

    // The SVG is rendered through an <img src="data:image/svg+xml,…"> rather
    // than injected as markup, so no <svg> element exists in the document and
    // scripts can never execute from an image context.
    const img = await screen.findByRole("img", { name: /flow\.mmd/i });
    const src = img.getAttribute("src") ?? "";
    expect(src.startsWith("data:image/svg+xml")).toBe(true);
    expect(decodeURIComponent(src)).toContain("<svg");
    expect(document.querySelector("svg")).not.toBeInTheDocument();
    // securityLevel: "strict" is the load-bearing safety flag.
    expect(initialize).toHaveBeenCalledWith(
      expect.objectContaining({ securityLevel: "strict" }),
    );
    // Parse is run before render so mermaid never injects an error glyph.
    expect(parse).toHaveBeenCalledWith("graph TD; A-->B;");
    expect(renderFn).toHaveBeenCalled();
  });

  it("shows the error state when the diagram fails to parse", async () => {
    mockFetchText("not a diagram");
    parse.mockRejectedValueOnce(new Error("Parse error on line 1"));
    render(<MermaidViewer path="team/diagrams/broken.mmd" />);

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/Parse error/),
    );
    expect(renderFn).not.toHaveBeenCalled();
  });

  it("shows the error state when the fetch fails", async () => {
    mockFetchText("", false, 404);
    render(<MermaidViewer path="team/diagrams/missing.mmd" />);

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/404/),
    );
  });

  it("toggles between the diagram and its source via the Code button", async () => {
    mockFetchText("graph TD; A-->B;");
    render(<MermaidViewer path="team/diagrams/flow.mmd" />);

    // Diagram renders first; the toggle is labelled "Code".
    await screen.findByRole("img", { name: /flow\.mmd/i });
    const toggle = screen.getByRole("button", { name: /^code$/i });
    toggle.click();

    // After toggling, the raw source is shown and the image is gone.
    await waitFor(() =>
      expect(screen.getByText(/graph TD; A-->B;/)).toBeInTheDocument(),
    );
    expect(
      screen.queryByRole("img", { name: /flow\.mmd/i }),
    ).not.toBeInTheDocument();
    // The toggle now offers a way back to the diagram.
    expect(
      screen.getByRole("button", { name: /^diagram$/i }),
    ).toBeInTheDocument();
  });

  it("exposes Copy and Download SVG actions once rendered", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    mockFetchText("graph TD; A-->B;");
    render(<MermaidViewer path="team/diagrams/flow.mmd" />);

    await screen.findByRole("img", { name: /flow\.mmd/i });
    expect(
      screen.getByRole("button", { name: /download svg/i }),
    ).toBeInTheDocument();

    screen.getByRole("button", { name: /^copy$/i }).click();
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith("graph TD; A-->B;"),
    );
  });

  it("re-renders when the path changes", async () => {
    mockFetchText("graph TD; A-->B;");
    const { rerender } = render(
      <MermaidViewer path="team/diagrams/first.mmd" />,
    );
    await waitFor(() => expect(renderFn).toHaveBeenCalledTimes(1));

    rerender(<MermaidViewer path="team/diagrams/second.mmd" />);
    await waitFor(() => expect(renderFn).toHaveBeenCalledTimes(2));
  });
});
