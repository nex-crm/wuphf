import { describe, expect, it } from "vitest";

import { selectRendererDevServerUrl } from "../src/main/renderer-dev-url.ts";

describe("selectRendererDevServerUrl", () => {
  it("ignores ELECTRON_RENDERER_URL in packaged builds", () => {
    expect(
      selectRendererDevServerUrl({ ELECTRON_RENDERER_URL: "http://localhost:5173/" }, true),
    ).toBeUndefined();
  });

  it("allows ELECTRON_RENDERER_URL only for unpackaged development builds", () => {
    expect(
      selectRendererDevServerUrl({ ELECTRON_RENDERER_URL: "http://localhost:5173/" }, false),
    ).toBe("http://localhost:5173/");
  });
});
