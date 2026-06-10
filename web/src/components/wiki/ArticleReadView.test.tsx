import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  ENTITY_ARTICLE_FIXTURE,
  ENTITY_ARTICLE_FIXTURE_PATH,
  ENTITY_ARTICLE_FIXTURE_TITLE,
} from "./__fixtures__/entityArticleFixture";
import ArticleReadView from "./ArticleReadView";
import { makeWikilinkResolver } from "./articleContent";

const resolver = makeWikilinkResolver([
  "team/people/eng.md",
  "team/companies/acme-corp.md",
]);

function renderFixture(
  overrides: Partial<Parameters<typeof ArticleReadView>[0]> = {},
) {
  return render(
    <ArticleReadView
      content={ENTITY_ARTICLE_FIXTURE}
      title={ENTITY_ARTICLE_FIXTURE_TITLE}
      articlePath={ENTITY_ARTICLE_FIXTURE_PATH}
      resolver={resolver}
      {...overrides}
    />,
  );
}

describe("<ArticleReadView> — B2 entity article", () => {
  it("renders the Summary definition list as a right-floating infobox", () => {
    renderFixture();
    const infobox = screen.getByTestId("wk-infobox");
    expect(infobox).toHaveTextContent("Acme Corp");
    expect(infobox).toHaveTextContent("Kind");
    expect(infobox).toHaveTextContent("company");
    expect(infobox).toHaveTextContent("Facts on record");
    // The raw Summary heading does not render in the body.
    expect(
      screen.queryByRole("heading", { name: /Summary/ }),
    ).not.toBeInTheDocument();
  });

  it("renders [^n] citations as footnote-ref links and a References block", () => {
    const { container } = renderFixture();
    const refs = container.querySelectorAll("a[data-footnote-ref]");
    expect(refs.length).toBe(2);
    // The footnote section is relabeled References and visible.
    expect(
      screen.getByRole("heading", { name: "References" }),
    ).toBeInTheDocument();
    const backrefs = container.querySelectorAll("a[data-footnote-backref]");
    expect(backrefs.length).toBe(2);
  });

  it("renders wikilinks blue when the target exists, red when it does not", () => {
    const { container } = render(
      <ArticleReadView
        content={"See [[people/eng]] and [[companies/ghost-co]]."}
        title="Links"
        articlePath="team/notes/links.md"
        resolver={resolver}
      />,
    );
    const blue = container.querySelector(
      'a[data-wikilink="true"][data-broken="false"]',
    );
    const red = container.querySelector(
      'a[data-wikilink="true"][data-broken="true"]',
    );
    expect(blue).toHaveTextContent("people/eng");
    expect(red).toHaveTextContent("companies/ghost-co");
  });

  it("shows the generated-article hatnote for marker-stamped bodies only", () => {
    renderFixture();
    expect(screen.getByText(/auto-generated from team activity/)).toBeVisible();
  });

  it("shows no hatnote and no infobox for a plain article", () => {
    render(
      <ArticleReadView
        content={"Just prose."}
        title="Plain"
        articlePath="team/notes/plain.md"
        resolver={resolver}
      />,
    );
    expect(
      screen.queryByText(/auto-generated from team activity/),
    ).not.toBeInTheDocument();
    expect(screen.queryByTestId("wk-infobox")).not.toBeInTheDocument();
  });

  it("adds the [ edit ] affordance to H2s and fires onEditSection", () => {
    const onEditSection = vi.fn();
    renderFixture({ onEditSection });
    const editButtons = screen.getAllByRole("button", {
      name: "Edit this section",
    });
    expect(editButtons.length).toBeGreaterThan(0);
    fireEvent.click(editButtons[0]);
    expect(onEditSection).toHaveBeenCalledTimes(1);
  });

  it("shows a hover preview card for a blue wikilink", async () => {
    const fetchPreview = vi.fn().mockResolvedValue({
      title: "Eng",
      body: "Eng is a person in the team knowledge graph…",
    });
    const { container } = renderFixture({ fetchPreview, previewDelayMs: 0 });
    const blue = container.querySelector(
      'a[data-wikilink="true"][data-broken="false"]',
    );
    expect(blue).not.toBeNull();
    if (!blue) return;
    fireEvent.mouseOver(blue);
    await waitFor(() =>
      expect(screen.getByTestId("wk-hover-preview")).toBeInTheDocument(),
    );
    expect(fetchPreview).toHaveBeenCalledWith("people/eng");
    expect(screen.getByTestId("wk-hover-preview")).toHaveTextContent("Eng is");
  });
});
