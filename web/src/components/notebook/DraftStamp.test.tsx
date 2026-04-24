import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import DraftStamp from "./DraftStamp";

describe("<DraftStamp>", () => {
  it("renders DRAFT text", () => {
    render(<DraftStamp />);
    expect(screen.getByText("DRAFT")).toBeInTheDocument();
  });

  it("uses role=img with accessible default label", () => {
    render(<DraftStamp />);
    expect(
      screen.getByRole("img", { name: "Draft entry, not yet reviewed" }),
    ).toBeInTheDocument();
  });

  it("honors an override label", () => {
    render(<DraftStamp label="Custom draft state" />);
    expect(
      screen.getByRole("img", { name: "Custom draft state" }),
    ).toBeInTheDocument();
  });
});
