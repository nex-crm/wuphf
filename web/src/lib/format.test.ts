import { describe, expect, it } from "vitest";

import { pluralize } from "./format";

describe("pluralize", () => {
  it("returns plural for zero", () => {
    expect(pluralize(0, "article")).toBe("articles");
  });

  it("returns singular for one", () => {
    expect(pluralize(1, "article")).toBe("article");
  });

  it("returns plural for many", () => {
    expect(pluralize(2, "article")).toBe("articles");
  });

  it("accepts a custom plural form", () => {
    expect(pluralize(3, "person", "people")).toBe("people");
  });
});
