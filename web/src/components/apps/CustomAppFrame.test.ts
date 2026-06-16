import { describe, expect, it } from "vitest";

import {
  isAllowedGetPath,
  parseErrorPayload,
  parseSelectPayload,
  withAppCsp,
} from "./CustomAppFrame";

describe("isAllowedGetPath", () => {
  it("allows whitelisted read-only office data paths", () => {
    expect(isAllowedGetPath("/apps")).toBe(true);
    expect(isAllowedGetPath("/tasks")).toBe(true);
    expect(isAllowedGetPath("/tasks?status=open")).toBe(true);
    expect(isAllowedGetPath("/office-members")).toBe(true);
    expect(isAllowedGetPath("/wiki/list")).toBe(true);
    expect(isAllowedGetPath("/apps/app_0123456789abcdef")).toBe(true);
  });

  it("rejects non-whitelisted, mutating, or off-broker paths", () => {
    // Not on the allowlist.
    expect(isAllowedGetPath("/config")).toBe(false);
    expect(isAllowedGetPath("/web-token")).toBe(false);
    expect(isAllowedGetPath("/memory")).toBe(false);
    // Prefix-spoofing must not pass (/tasksomething is not /tasks/*).
    expect(isAllowedGetPath("/tasks-secret")).toBe(false);
    expect(isAllowedGetPath("/wiki/write")).toBe(false);
    // Path traversal must be normalized away before the allowlist check, since
    // the browser resolves it before sending (security review finding 1).
    expect(isAllowedGetPath("/tasks/../config")).toBe(false);
    expect(isAllowedGetPath("/tasks/../../memory")).toBe(false);
    expect(isAllowedGetPath("/apps/../wiki/write")).toBe(false);
    // Absolute / protocol-relative / non-rooted are rejected outright.
    expect(isAllowedGetPath("https://evil.example/tasks")).toBe(false);
    expect(isAllowedGetPath("//evil.example/tasks")).toBe(false);
    expect(isAllowedGetPath("tasks")).toBe(false);
    expect(isAllowedGetPath("")).toBe(false);
  });
});

describe("withAppCsp", () => {
  it("injects a connect-src 'none' CSP into the document head", () => {
    const out = withAppCsp(
      "<!doctype html><html><head><title>x</title></head><body>hi</body></html>",
    );
    expect(out).toContain("Content-Security-Policy");
    expect(out).toContain("connect-src 'none'");
    // CSP meta must land inside <head> so it parses before any inline script.
    const headIdx = out.indexOf("<head>");
    const cspIdx = out.indexOf("Content-Security-Policy");
    expect(headIdx).toBeGreaterThanOrEqual(0);
    expect(cspIdx).toBeGreaterThan(headIdx);
  });

  it("is not shadowed by a commented-out <head> (security review finding 2)", () => {
    const out = withAppCsp(
      "<!-- <head> decoy --><html><head><script>fetch('https://evil.example')</script></head><body>x</body></html>",
    );
    // The decoy comment is stripped, so the CSP lands in the REAL head.
    expect(out).toContain("connect-src 'none'");
    expect(out).not.toContain("<!--");
    // CSP precedes the inline script in document order.
    const cspIdx = out.indexOf("Content-Security-Policy");
    const scriptIdx = out.indexOf("<script>");
    expect(cspIdx).toBeGreaterThanOrEqual(0);
    expect(cspIdx).toBeLessThan(scriptIdx);
  });

  it("wraps fragments lacking <head>/<html> in a full CSP-protected document", () => {
    const out = withAppCsp("<div>just a fragment</div>");
    expect(out).toContain("connect-src 'none'");
    expect(out).toContain("just a fragment");
    expect(out.startsWith("<!doctype html>")).toBe(true);
  });

  it("does not introduce any broker GET path beyond the existing allowlist", () => {
    // The inspector messages are display-only and must NOT widen the allowlist.
    // Spot-check that nothing new slipped into the permitted prefixes.
    for (const p of ["/select", "/error", "/config", "/web-token"]) {
      expect(isAllowedGetPath(p)).toBe(false);
    }
  });
});

describe("parseSelectPayload", () => {
  it("accepts a well-formed selection and floors numeric fields", () => {
    const sel = parseSelectPayload({
      file: "components/Button.tsx",
      line: 12.9,
      col: 4.2,
      tag: "BUTTON",
      label: "Save",
    });
    expect(sel).toEqual({
      file: "components/Button.tsx",
      line: 12,
      col: 4,
      tag: "BUTTON",
      label: "Save",
    });
  });

  it("returns null when there is no usable file location", () => {
    expect(parseSelectPayload(null)).toBeNull();
    expect(parseSelectPayload({})).toBeNull();
    expect(parseSelectPayload({ file: "" })).toBeNull();
    expect(parseSelectPayload({ file: 42 })).toBeNull();
    expect(parseSelectPayload("not an object")).toBeNull();
  });

  it("caps an over-long label and coerces bad numbers to 0", () => {
    const long = "x".repeat(500);
    const sel = parseSelectPayload({
      file: "a.tsx",
      line: -3,
      col: Number.NaN,
      tag: "div",
      label: long,
    });
    expect(sel).not.toBeNull();
    expect(sel?.label.length).toBe(120);
    // Negative / NaN numbers are not trusted location data → 0.
    expect(sel?.line).toBe(0);
    expect(sel?.col).toBe(0);
  });

  it("caps an over-long file path and tag", () => {
    const sel = parseSelectPayload({
      file: `src/${"a".repeat(400)}.tsx`,
      tag: "z".repeat(100),
      label: "ok",
    });
    expect(sel?.file.length).toBe(256);
    expect(sel?.tag.length).toBe(32);
  });
});

describe("parseErrorPayload", () => {
  it("caps message and stack to the host limit", () => {
    const err = parseErrorPayload({
      message: "m".repeat(900),
      stack: "s".repeat(900),
    });
    expect(err.message.length).toBe(600);
    expect(err.stack.length).toBe(600);
  });

  it("yields empty strings for missing or non-string fields", () => {
    expect(parseErrorPayload(null)).toEqual({ message: "", stack: "" });
    expect(parseErrorPayload({ message: 5, stack: {} })).toEqual({
      message: "",
      stack: "",
    });
    expect(parseErrorPayload({ message: "boom" })).toEqual({
      message: "boom",
      stack: "",
    });
  });
});
