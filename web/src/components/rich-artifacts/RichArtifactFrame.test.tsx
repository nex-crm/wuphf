import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import RichArtifactFrame from "./RichArtifactFrame";

describe("<RichArtifactFrame>", () => {
  it("renders artifact HTML in a sandboxed iframe with an inline CSP", () => {
    render(<RichArtifactFrame title="Visual plan" html="<h1>Plan</h1>" />);

    const frame = screen.getByTitle("Visual plan");

    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    expect(frame).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("Content-Security-Policy"),
    );
    expect(frame).toHaveAttribute("srcdoc", expect.stringContaining("Plan"));
  });
});
