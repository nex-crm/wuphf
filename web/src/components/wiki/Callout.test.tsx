import type { ReactElement } from "react";
import ReactMarkdown from "react-markdown";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  buildMarkdownComponents,
  buildRemarkPlugins,
} from "../../lib/wikiMarkdownConfig";
import Callout, { CalloutBlockquote } from "./Callout";

describe("<Callout>", () => {
  it("renders the type label, title, and body", () => {
    render(
      <Callout type="info" title="Heads up" folded={false} defaultOpen={false}>
        <p>Body content</p>
      </Callout>,
    );
    expect(screen.getByText("Info")).toBeInTheDocument();
    expect(screen.getByText("Heads up")).toBeInTheDocument();
    expect(screen.getByText("Body content")).toBeInTheDocument();
  });

  it("renders without a title when none is provided", () => {
    render(
      <Callout type="note" title="" folded={false} defaultOpen={false}>
        <p>Just body</p>
      </Callout>,
    );
    expect(screen.getByText("Note")).toBeInTheDocument();
    expect(screen.getByText("Just body")).toBeInTheDocument();
  });

  it("uses <details>/<summary> for folded variants", () => {
    const { container } = render(
      <Callout type="warning" title="" folded={true} defaultOpen={true}>
        <p>Expanded body</p>
      </Callout>,
    );
    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    expect(details).toHaveAttribute("open");
    expect(container.querySelector("summary")).not.toBeNull();
  });

  it("renders closed by default when defaultOpen=false", () => {
    const { container } = render(
      <Callout type="caution" title="" folded={true} defaultOpen={false}>
        <p>Hidden body</p>
      </Callout>,
    );
    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    expect(details).not.toHaveAttribute("open");
  });

  it("applies the type-specific css class", () => {
    const { container } = render(
      <Callout type="caution" title="" folded={false} defaultOpen={false}>
        <p>x</p>
      </Callout>,
    );
    expect(container.querySelector(".wk-callout-caution")).not.toBeNull();
  });
});

describe("<CalloutBlockquote>", () => {
  it("falls through to a plain blockquote when not tagged as a callout", () => {
    const props = {
      children: "ordinary quote",
    } as unknown as Parameters<typeof CalloutBlockquote>[0];
    const el = CalloutBlockquote(props) as ReactElement;
    expect(el.type).toBe("blockquote");
  });
});

describe("Callout integration with the wiki markdown pipeline", () => {
  function renderWiki(source: string, slugs: string[] = []) {
    const known = new Set(slugs);
    const remarkPlugins = buildRemarkPlugins((s: string) => known.has(s));
    const components = buildMarkdownComponents({
      resolver: (s: string) => known.has(s),
    });
    return render(
      <ReactMarkdown remarkPlugins={remarkPlugins} components={components}>
        {source}
      </ReactMarkdown>,
    );
  }

  it("renders a `> [!note]` blockquote as a callout with title and body markdown", () => {
    const { container } = renderWiki(
      "> [!note] Heads up\n> Body has **bold** text\n",
    );
    expect(container.querySelector(".wk-callout-note")).not.toBeNull();
    expect(screen.getByText("Note")).toBeInTheDocument();
    expect(screen.getByText("Heads up")).toBeInTheDocument();
    const strong = container.querySelector("strong");
    expect(strong).not.toBeNull();
    expect(strong?.textContent).toBe("bold");
  });

  it("renders `> [!warning]+` as a folded callout open by default", () => {
    const { container } = renderWiki("> [!warning]+\n> Expanded body\n");
    expect(container.querySelector(".wk-callout-warning")).not.toBeNull();
    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    expect(details).toHaveAttribute("open");
  });

  it("renders `> [!caution]-` as a folded callout closed by default", () => {
    const { container } = renderWiki("> [!caution]-\n> Collapsed body\n");
    const details = container.querySelector("details");
    expect(details).not.toBeNull();
    expect(details).not.toHaveAttribute("open");
  });

  it("keeps wikilinks inside the callout body rendered as wikilinks", () => {
    const { container } = renderWiki(
      "> [!info]\n> See [[people/nazz]] for more.\n",
      ["people/nazz"],
    );
    const link = container.querySelector("a[data-wikilink='true']");
    expect(link).not.toBeNull();
    expect(link).toHaveAttribute("data-slug", "people/nazz");
    expect(link?.textContent).toBe("people/nazz");
  });

  it("leaves a regular blockquote rendered as a plain blockquote", () => {
    const { container } = renderWiki("> just an ordinary quote\n");
    expect(container.querySelector(".wk-callout")).toBeNull();
    expect(container.querySelector("blockquote")).not.toBeNull();
  });
});
