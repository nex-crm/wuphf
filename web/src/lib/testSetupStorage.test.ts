import { describe, expect, it } from "vitest";

describe("test localStorage polyfill", () => {
  it("treats inherited object keys as absent until stored", () => {
    localStorage.clear();

    expect(localStorage.getItem("toString")).toBeNull();
    expect(localStorage.getItem("__proto__")).toBeNull();

    localStorage.setItem("toString", "method-name");
    localStorage.setItem("__proto__", "prototype-name");

    expect(localStorage.getItem("toString")).toBe("method-name");
    expect(localStorage.getItem("__proto__")).toBe("prototype-name");
  });
});
