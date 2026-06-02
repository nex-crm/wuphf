import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/wiki", () => ({
  wikiFileUrl: (path: string) =>
    `/wiki/file?path=${encodeURIComponent(path)}&token=test`,
}));

import PdfViewer from "./PdfViewer";

describe("PdfViewer", () => {
  it("renders an iframe pointed at the tokened wiki file URL", () => {
    render(<PdfViewer path="team/assets/report.pdf" />);

    const frame = screen.getByTitle("PDF document: report.pdf");
    expect(frame.tagName).toBe("IFRAME");
    expect(frame.getAttribute("src")).toContain(
      "path=team%2Fassets%2Freport.pdf",
    );
  });

  it("exposes download + open-in-new-tab actions for the file", () => {
    render(<PdfViewer path="team/assets/report.pdf" />);

    const download = screen.getByRole("link", { name: /download/i });
    expect(download).toHaveAttribute("download", "report.pdf");

    const openNewTab = screen.getByRole("link", { name: /open in new tab/i });
    expect(openNewTab).toHaveAttribute("target", "_blank");
    expect(openNewTab).toHaveAttribute("rel", "noreferrer");
  });

  it("derives the filename from the deepest path segment", () => {
    render(<PdfViewer path="team/decisions/q3/budget.pdf" />);
    expect(screen.getByLabelText("PDF: budget.pdf")).toBeInTheDocument();
  });
});
