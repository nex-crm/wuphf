import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import ArticleStatusBanner from "./ArticleStatusBanner";

describe("<ArticleStatusBanner>", () => {
  it("renders the Live prefix when a liveAgent is set", () => {
    render(
      <ArticleStatusBanner
        message="CEO is editing this article."
        liveAgent="ceo"
        revisions={47}
        contributors={6}
        wordCount={2347}
      />,
    );
    expect(screen.getByTestId("wk-status-banner")).toBeInTheDocument();
    expect(screen.getByText(/Live:/)).toBeInTheDocument();
    expect(screen.getByText(/47 rev/)).toBeInTheDocument();
    expect(screen.getByText(/6 contrib/)).toBeInTheDocument();
    expect(screen.getByText(/2,347 words/)).toBeInTheDocument();
  });

  it("falls back to Status: when no liveAgent is set and omits meta when empty", () => {
    render(<ArticleStatusBanner message="Everything is fine." />);
    expect(screen.getByText(/Status:/)).toBeInTheDocument();
  });
});
