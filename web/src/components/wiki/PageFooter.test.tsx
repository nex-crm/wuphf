import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import PageFooter from "./PageFooter";

describe("<PageFooter>", () => {
  it("renders the edited-by line and default actions", () => {
    render(
      <PageFooter
        lastEditedBy="CEO"
        lastEditedTs="2026-04-19T16:24:00Z"
        articlePath="people/customer-x"
      />,
    );
    expect(screen.getByText(/CEO/)).toBeInTheDocument();
    expect(screen.getByText(/2026-04-19 at 16:24 UTC/)).toBeInTheDocument();
    expect(screen.getByText("View git history")).toBeInTheDocument();
  });

  it("invokes custom action handlers", () => {
    const onClick = vi.fn();
    render(
      <PageFooter
        lastEditedBy="PM"
        lastEditedTs="2026-04-19T00:00:00Z"
        articlePath="a"
        actions={[{ label: "Custom", onClick }]}
      />,
    );
    fireEvent.click(screen.getByText("Custom"));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("falls back to raw timestamp on bad input", () => {
    render(
      <PageFooter
        lastEditedBy="PM"
        lastEditedTs="not-a-date"
        articlePath="a"
      />,
    );
    expect(screen.getByText(/not-a-date/)).toBeInTheDocument();
  });
});
