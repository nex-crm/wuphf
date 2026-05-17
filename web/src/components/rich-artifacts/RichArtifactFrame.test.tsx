import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import RichArtifactFrame from "./RichArtifactFrame";

describe("<RichArtifactFrame>", () => {
  it("renders artifact HTML in a borderless sandboxed iframe with resize wiring", () => {
    render(<RichArtifactFrame title="Visual plan" html="<h1>Plan</h1>" />);

    const frame = screen.getByTitle("Visual plan");

    expect(frame).toHaveClass("rich-artifact-frame-inline");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    expect(frame).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("Content-Security-Policy"),
    );
    expect(frame).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("wuphf:rich-artifact:resize"),
    );
    expect(frame).toHaveAttribute("srcdoc", expect.stringContaining("Plan"));
  });

  it("injects sandbox metadata into complete HTML documents without nesting them", () => {
    render(
      <RichArtifactFrame
        title="Complete document"
        html="<!doctype html><html><head><title>Artifact</title></head><body><h1>Plan</h1></body></html>"
      />,
    );

    const srcdoc = screen
      .getByTitle("Complete document")
      .getAttribute("srcdoc");

    expect(srcdoc).toContain("<title>Artifact</title>");
    expect(srcdoc).toContain("Content-Security-Policy");
    expect(srcdoc).toContain("wuphf:rich-artifact:resize");
    expect(srcdoc).not.toContain("<body><!doctype html>");
  });
});
