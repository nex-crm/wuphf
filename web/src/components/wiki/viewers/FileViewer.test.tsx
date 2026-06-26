import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// Mock the file URL helper so the eagerly-imported fallback (and any viewer
// that reaches it) does not need a real broker base URL.
vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) => `/wiki/file?path=${encodeURIComponent(path)}`,
}));

// Replace each heavy viewer module with a sentinel so we can assert routing
// without loading xlsx / docx-preview / pptx-preview / mermaid / lowlight. Each
// mock renders a marker carrying the path it received, proving the dispatcher
// both picked the right module and forwarded the prop.
function sentinel(label: string) {
  return {
    default: ({ path }: { path: string }) => (
      <div data-testid={`viewer-${label}`} data-path={path}>
        {label}
      </div>
    ),
  };
}
vi.mock("./ImageViewer", () => sentinel("image"));
vi.mock("./MediaViewer", () => sentinel("media"));
vi.mock("./PdfViewer", () => sentinel("pdf"));
vi.mock("./CsvViewer", () => sentinel("csv"));
vi.mock("./XlsxViewer", () => sentinel("xlsx"));
vi.mock("./DocxViewer", () => sentinel("docx"));
vi.mock("./PptxViewer", () => sentinel("pptx"));
vi.mock("./NotebookViewer", () => sentinel("notebook"));
vi.mock("./MermaidViewer", () => sentinel("mermaid"));
vi.mock("./SourceViewer", () => sentinel("source"));
vi.mock("./JsonlViewer", () => sentinel("jsonl"));

import FileViewer, {
  fileKindForPath,
  fileKindLabel,
  isMarkdownPath,
  isViewablePath,
} from "./FileViewer";

describe("fileKindForPath", () => {
  const cases: [string, string][] = [
    ["team/assets/logo.png", "image"],
    ["team/assets/photo.JPG", "image"],
    ["team/assets/icon.svg", "image"],
    ["team/media/demo.mp4", "media"],
    ["team/media/jingle.mp3", "media"],
    ["team/decisions/q3/budget.pdf", "pdf"],
    ["team/data/people.csv", "csv"],
    ["team/data/forecast.xlsx", "xlsx"],
    ["team/data/legacy.xls", "xlsx"],
    ["team/docs/brief.docx", "docx"],
    ["team/docs/deck.pptx", "pptx"],
    ["team/notebooks/analysis.ipynb", "notebook"],
    ["team/diagrams/flow.mmd", "mermaid"],
    ["team/diagrams/flow.mermaid", "mermaid"],
    ["team/code/server.go", "source"],
    ["team/code/app.tsx", "source"],
    ["team/notes/todo.txt", "source"],
    ["team/data/matrix.tsv", "source"],
    // JSON Lines routes to its own structured record viewer, not source.
    ["team/data/facts.jsonl", "jsonl"],
    ["team/logs/events.ndjson", "jsonl"],
    // A plain `.json` is still source (single document, not line-delimited).
    ["team/config/settings.json", "source"],
    // Unknown / binary → fallback
    ["team/assets/font.woff2", "fallback"],
    ["team/archives/backup.zip", "fallback"],
    ["team/no-extension-file", "fallback"],
    // Markdown is owned by the article view, never a file viewer.
    ["team/people/nazz.md", "fallback"],
  ];

  for (const [path, expected] of cases) {
    it(`classifies ${path} as ${expected}`, () => {
      expect(fileKindForPath(path)).toBe(expected);
    });
  }
});

describe("isMarkdownPath", () => {
  it("is true for .md and .markdown leaves", () => {
    expect(isMarkdownPath("team/people/nazz.md")).toBe(true);
    expect(isMarkdownPath("team/guides/intro.markdown")).toBe(true);
  });
  it("is false for non-markdown and bare slugs", () => {
    expect(isMarkdownPath("team/assets/x.pdf")).toBe(false);
    expect(isMarkdownPath("people/nazz")).toBe(false);
  });
});

describe("isViewablePath", () => {
  it("is true for files with a dedicated viewer", () => {
    expect(isViewablePath("team/assets/x.png")).toBe(true);
    expect(isViewablePath("team/data/x.csv")).toBe(true);
    expect(isViewablePath("team/docs/x.docx")).toBe(true);
  });
  it("is false for markdown (article view owns it)", () => {
    expect(isViewablePath("team/people/nazz.md")).toBe(false);
  });
  it("is false for unknown binaries (fallback, not a real viewer)", () => {
    expect(isViewablePath("team/assets/font.woff2")).toBe(false);
    expect(isViewablePath("team/archives/backup.zip")).toBe(false);
  });
});

describe("fileKindLabel", () => {
  it("returns a friendly label per kind", () => {
    expect(fileKindLabel("team/assets/x.png")).toBe("Image");
    expect(fileKindLabel("team/decisions/x.pdf")).toBe("PDF");
    expect(fileKindLabel("team/docs/x.pptx")).toBe("Presentation");
  });
  it("falls back to the uppercased extension for unknown files", () => {
    expect(fileKindLabel("team/assets/font.woff2")).toBe("WOFF2");
  });
});

describe("<FileViewer> routing", () => {
  it("renders the image viewer for an image path", async () => {
    render(<FileViewer path="team/assets/logo.png" />);
    await waitFor(() =>
      expect(screen.getByTestId("viewer-image")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("viewer-image")).toHaveAttribute(
      "data-path",
      "team/assets/logo.png",
    );
  });

  it("renders the pdf viewer for a pdf path", async () => {
    render(<FileViewer path="team/decisions/budget.pdf" />);
    await waitFor(() =>
      expect(screen.getByTestId("viewer-pdf")).toBeInTheDocument(),
    );
  });

  it("renders the xlsx viewer for a spreadsheet path", async () => {
    render(<FileViewer path="team/data/forecast.xlsx" />);
    await waitFor(() =>
      expect(screen.getByTestId("viewer-xlsx")).toBeInTheDocument(),
    );
  });

  it("renders the source viewer for a code path", async () => {
    render(<FileViewer path="team/code/server.go" />);
    await waitFor(() =>
      expect(screen.getByTestId("viewer-source")).toBeInTheDocument(),
    );
  });

  it("renders the jsonl viewer for a .jsonl path", async () => {
    render(<FileViewer path="team/data/facts.jsonl" />);
    await waitFor(() =>
      expect(screen.getByTestId("viewer-jsonl")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("viewer-jsonl")).toHaveAttribute(
      "data-path",
      "team/data/facts.jsonl",
    );
  });

  it("renders the fallback for an unknown binary", () => {
    render(<FileViewer path="team/assets/font.woff2" />);
    // Fallback is eager (no Suspense), so it is present synchronously.
    expect(screen.getByText(/No preview for this file/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /download/i })).toHaveAttribute(
      "download",
      "font.woff2",
    );
    expect(
      screen.getByRole("link", { name: /open in new tab/i }),
    ).toHaveAttribute("target", "_blank");
  });

  it("renders the fallback for a markdown path (article view owns .md)", () => {
    render(<FileViewer path="team/people/nazz.md" />);
    expect(screen.getByText(/No preview for this file/i)).toBeInTheDocument();
  });
});
