import { act, render, screen, waitFor } from "@testing-library/react";
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

  it("accepts resize messages only from the matching iframe window", async () => {
    render(<RichArtifactFrame title="Resizable" html="<h1>Plan</h1>" />);

    const frame = screen.getByTitle("Resizable") as HTMLIFrameElement;
    const srcdoc = frame.getAttribute("srcdoc") ?? "";
    const frameId = srcdoc.match(/const id="([^"]+)"/)?.[1];

    expect(frameId).toBeTruthy();
    expect(frame.contentWindow).toBeTruthy();

    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: {
            type: "wuphf:rich-artifact:resize",
            id: frameId,
            height: 900,
          },
          source: null,
        }),
      );
    });
    expect(frame.style.height).toBe("");

    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: {
            type: "wuphf:rich-artifact:resize",
            id: frameId,
            height: 345,
          },
          source: frame.contentWindow,
        }),
      );
    });

    await waitFor(() => expect(frame.style.height).toBe("345px"));
  });

  it("does not inject resize wiring when auto-resize is disabled", () => {
    render(
      <RichArtifactFrame
        title="Modal artifact"
        html="<h1>Plan</h1>"
        variant="modal"
      />,
    );

    expect(screen.getByTitle("Modal artifact")).not.toHaveAttribute(
      "srcdoc",
      expect.stringContaining("wuphf:rich-artifact:resize"),
    );
  });
});
