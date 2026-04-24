import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Marginalia from "./Marginalia";

describe("<Marginalia>", () => {
  it("renders the tag and body", () => {
    render(<Marginalia tag="Q">what about elasticity?</Marginalia>);
    expect(screen.getByText("Q:")).toBeInTheDocument();
    expect(screen.getByText(/what about elasticity\?/)).toBeInTheDocument();
  });

  it("exposes role=note with the tag in the label", () => {
    render(<Marginalia tag="Next">follow up with CRO</Marginalia>);
    expect(
      screen.getByRole("note", { name: "Margin note: Next" }),
    ).toBeInTheDocument();
  });

  it("defaults the tag to Q", () => {
    render(<Marginalia>quick thought</Marginalia>);
    expect(screen.getByText("Q:")).toBeInTheDocument();
  });
});
