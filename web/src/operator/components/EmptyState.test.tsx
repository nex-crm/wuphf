// Regression: an actionLabel without an onAction handler used to render an
// enabled but dead primary button. Gate the button on both props.

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { EmptyState } from "./EmptyState";

afterEach(cleanup);

describe("EmptyState action button", () => {
  it("renders no button when actionLabel is set but onAction is omitted", () => {
    render(
      <EmptyState
        glyph="•"
        title="Nothing yet"
        hint="Try a thing"
        actionLabel="Do it"
      />,
    );

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("renders the button when both actionLabel and onAction are provided", () => {
    render(
      <EmptyState
        glyph="•"
        title="Nothing yet"
        hint="Try a thing"
        actionLabel="Do it"
        onAction={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: /do it/i })).toBeInTheDocument();
  });
});
