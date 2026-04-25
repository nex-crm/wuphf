import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Hatnote from "./Hatnote";

describe("<Hatnote>", () => {
  it("renders cross-reference content", () => {
    render(
      <Hatnote>
        See also the <em>onboarding</em> playbook.
      </Hatnote>,
    );
    expect(screen.getByText(/See also the/)).toBeInTheDocument();
    expect(screen.getByText("onboarding")).toBeInTheDocument();
  });

  it("renders plain string children", () => {
    render(<Hatnote>Plain hatnote.</Hatnote>);
    expect(screen.getByText("Plain hatnote.")).toBeInTheDocument();
  });
});
