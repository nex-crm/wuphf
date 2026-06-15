import { describe, expect, it } from "vitest";

import { isAllowedGetPath, withAppCsp } from "./CustomAppFrame";

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
});
