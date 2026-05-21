// @vitest-environment happy-dom

import { describe, expect, it } from "vitest";

import { renderStatusPaneFallback } from "../../src/renderer/status-pane.tsx";

describe("renderStatusPaneFallback", () => {
  it("renders a pre-React loading pane into the mount root", () => {
    const root = document.createElement("div");

    renderStatusPaneFallback(root);

    expect(root).toHaveTextContent("WUPHF");
    expect(root).toHaveTextContent("Starting renderer");
    expect(root.querySelector("main")).toHaveAttribute("aria-label", "Loading WUPHF");
  });
});
