import type { ReactElement } from "react";
import ReactMarkdown from "react-markdown";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  buildMarkdownComponents,
  buildRemarkPlugins,
  resolveRelativeWikiPath,
} from "./wikiMarkdownConfig";

describe("resolveRelativeWikiPath", () => {
  it("resolves a sibling .md link against the article directory", () => {
    expect(resolveRelativeWikiPath("company.md", "team/about/README.md")).toBe(
      "team/about/company.md",
    );
  });

  it("resolves an explicit current-directory link", () => {
    expect(resolveRelativeWikiPath("./owner.md", "team/about/README.md")).toBe(
      "team/about/owner.md",
    );
  });

  it("walks parent segments for ../ paths", () => {
    expect(
      resolveRelativeWikiPath("../people/nazz.md", "team/about/README.md"),
    ).toBe("team/people/nazz.md");
  });

  it("returns null for absolute URLs", () => {
    expect(
      resolveRelativeWikiPath("https://example.com/x.md", "team/about/x.md"),
    ).toBeNull();
  });

  it("returns null for mailto: and other schemes", () => {
    expect(
      resolveRelativeWikiPath("mailto:foo@bar.com", "team/about/x.md"),
    ).toBeNull();
  });

  it("returns null for in-page anchors", () => {
    expect(resolveRelativeWikiPath("#section", "team/about/x.md")).toBeNull();
  });

  it("returns null for server-absolute paths", () => {
    expect(
      resolveRelativeWikiPath("/static/file.md", "team/about/x.md"),
    ).toBeNull();
  });

  it("returns null for non-.md targets", () => {
    expect(
      resolveRelativeWikiPath("logo.png", "team/about/README.md"),
    ).toBeNull();
  });

  it("returns null when .. would escape the wiki root", () => {
    expect(resolveRelativeWikiPath("../../escape.md", "team/x.md")).toBeNull();
  });

  it("preserves the rest of the article path when the link adds segments", () => {
    expect(
      resolveRelativeWikiPath("nested/page.md", "team/about/README.md"),
    ).toBe("team/about/nested/page.md");
  });
});

function renderMd(md: string, articlePath: string, onNavigate?: () => void) {
  const resolver = () => true;
  return render(
    <ReactMarkdown
      remarkPlugins={buildRemarkPlugins(resolver)}
      components={
        buildMarkdownComponents({
          resolver,
          articlePath,
          onNavigate,
        }) as never
      }
    >
      {md}
    </ReactMarkdown>,
  ) as unknown as ReactElement;
}

describe("buildMarkdownComponents anchor override", () => {
  it("rewrites relative .md links to the wiki hash route", () => {
    renderMd(
      "- [company.md](company.md)\n- [owner](./owner.md)\n",
      "team/about/README.md",
    );
    const company = screen.getByRole("link", { name: "company.md" });
    expect(company.getAttribute("href")).toBe("#/wiki/team/about/company.md");
    const owner = screen.getByRole("link", { name: "owner" });
    expect(owner.getAttribute("href")).toBe("#/wiki/team/about/owner.md");
  });

  it("leaves external links untouched", () => {
    renderMd("[Anthropic](https://anthropic.com)", "team/about/README.md");
    const link = screen.getByRole("link", { name: "Anthropic" });
    expect(link.getAttribute("href")).toBe("https://anthropic.com");
  });

  it("intercepts clicks on rewritten links when onNavigate is provided", () => {
    const onNavigate = vi.fn();
    renderMd("[company.md](company.md)", "team/about/README.md", onNavigate);
    const link = screen.getByRole("link", { name: "company.md" });
    link.click();
    expect(onNavigate).toHaveBeenCalledWith("team/about/company.md");
  });

  it("is a no-op when articlePath is not supplied", () => {
    const resolver = () => true;
    render(
      <ReactMarkdown
        remarkPlugins={buildRemarkPlugins(resolver)}
        components={buildMarkdownComponents({ resolver }) as never}
      >
        {"[company.md](company.md)"}
      </ReactMarkdown>,
    );
    const link = screen.getByRole("link", { name: "company.md" });
    expect(link.getAttribute("href")).toBe("company.md");
  });
});
