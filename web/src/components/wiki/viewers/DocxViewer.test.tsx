import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import DocxViewer from "./DocxViewer";

// Mock the wiki API so the viewer resolves a deterministic URL without
// touching the real client / auth-token machinery.
vi.mock("../../../api/wiki", () => ({
  wikiFileUrl: (path: string) => `https://wiki.test/file?path=${path}`,
}));

// Mock the heavy docx renderer: we are testing the viewer's fetch + state
// orchestration, not docx-preview's OOXML parsing. The mock writes a sentinel
// node into the container it is handed so we can assert it received the
// element we own.
const renderAsync = vi.fn(
  async (
    _blob: Blob,
    container: HTMLElement,
    _style: HTMLElement | undefined,
    _opts: unknown,
  ) => {
    const node = document.createElement("p");
    node.textContent = "rendered-docx-content";
    container.appendChild(node);
  },
);

vi.mock("docx-preview", () => ({
  renderAsync: (
    blob: Blob,
    container: HTMLElement,
    style: HTMLElement | undefined,
    opts: unknown,
  ) => renderAsync(blob, container, style, opts),
}));

const PATH = "team/assets/report.docx";

describe("<DocxViewer>", () => {
  beforeEach(() => {
    renderAsync.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("fetches the file and renders the document into a container it owns", async () => {
    const blob = new Blob(["fake-docx-bytes"]);
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      blob: async () => blob,
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<DocxViewer path={PATH} />);

    await waitFor(() => {
      expect(screen.getByText("rendered-docx-content")).toBeInTheDocument();
    });

    // It fetched the tokened wiki URL for this path.
    expect(fetchMock).toHaveBeenCalledWith(
      `https://wiki.test/file?path=${PATH}`,
    );
    // It handed renderAsync the blob and a real container element.
    expect(renderAsync).toHaveBeenCalledTimes(1);
    const [passedBlob, container] = renderAsync.mock.calls[0];
    expect(passedBlob).toBe(blob);
    expect(container).toBeInstanceOf(HTMLElement);

    // The filename is surfaced in the toolbar.
    expect(screen.getByText("report.docx")).toBeInTheDocument();
  });

  it("shows the error state when the fetch fails", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error("network down"));
    vi.stubGlobal("fetch", fetchMock);

    render(<DocxViewer path={PATH} />);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("network down");
    expect(renderAsync).not.toHaveBeenCalled();
  });

  it("shows the error state on a non-ok HTTP response", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      blob: async () => new Blob([]),
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<DocxViewer path={PATH} />);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("404");
  });

  it("neutralizes dangerous hyperlink hrefs the library injects, keeps safe ones", async () => {
    // docx-preview writes anchors with author-controlled hrefs straight into
    // our origin DOM. The viewer must strip javascript:/data:/vbscript: links
    // (drop the href so the link text still renders) while leaving http(s),
    // mailto, fragment, and relative links navigable.
    renderAsync.mockImplementationOnce(
      async (
        _blob: Blob,
        container: HTMLElement,
        _style: HTMLElement | undefined,
        _opts: unknown,
      ) => {
        container.innerHTML = `
          <a id="js" href="javascript:alert(1)">click</a>
          <a id="data" href="data:text/html,<script>alert(1)</script>">data</a>
          <a id="vb" href="vbscript:msgbox(1)">vb</a>
          <a id="http" href="https://example.com/safe">safe</a>
          <a id="mail" href="mailto:team@example.com">mail</a>
          <a id="frag" href="#section">frag</a>
          <a id="rel" href="./other.docx">rel</a>
        `;
      },
    );

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      blob: async () => new Blob(["fake-docx-bytes"]),
    });
    vi.stubGlobal("fetch", fetchMock);

    const { container } = render(<DocxViewer path={PATH} />);

    await waitFor(() => {
      expect(container.querySelector("#http")).not.toBeNull();
    });

    const hrefOf = (id: string) =>
      container.querySelector(`#${id}`)?.getAttribute("href");

    // Dangerous schemes are stripped entirely (link text survives, no nav).
    expect(hrefOf("js")).toBeNull();
    expect(hrefOf("data")).toBeNull();
    expect(hrefOf("vb")).toBeNull();
    expect(container.querySelector("#js")?.textContent).toBe("click");

    // Safe links keep their href.
    expect(hrefOf("http")).toBe("https://example.com/safe");
    expect(hrefOf("mail")).toBe("mailto:team@example.com");
    expect(hrefOf("frag")).toBe("#section");
    expect(hrefOf("rel")).toBe("./other.docx");
  });
});
