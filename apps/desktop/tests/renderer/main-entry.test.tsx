// @vitest-environment happy-dom

import { beforeEach, describe, expect, it, vi } from "vitest";

const reactDomMock = vi.hoisted(() => {
  const render = vi.fn<(node: unknown) => void>();
  const createRoot = vi.fn<(element: Element) => { render: (node: unknown) => void }>(() => ({
    render,
  }));
  return { createRoot, render };
});

vi.mock("react-dom/client", () => ({
  createRoot: reactDomMock.createRoot,
}));

vi.mock("../../src/renderer/app/App.tsx", () => ({
  App: () => "app",
}));

describe("renderer main entry", () => {
  beforeEach(() => {
    vi.resetModules();
    reactDomMock.createRoot.mockClear();
    reactDomMock.render.mockClear();
    document.body.innerHTML = '<div id="app"></div>';
  });

  it("renders the fallback pane before mounting React", async () => {
    const root = document.querySelector("#app");
    if (root === null) throw new Error("missing test root");

    await import("../../src/renderer/main.tsx");

    expect(root).toHaveTextContent("Starting renderer");
    expect(reactDomMock.createRoot).toHaveBeenCalledWith(root);
    expect(reactDomMock.render).toHaveBeenCalledTimes(1);
  });

  it("throws when the mount point is missing", async () => {
    document.body.innerHTML = "";

    await expect(import("../../src/renderer/main.tsx")).rejects.toThrow(
      "Missing #app mount point",
    );
  });
});
