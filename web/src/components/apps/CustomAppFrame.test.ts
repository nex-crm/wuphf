import { beforeEach, describe, expect, it, vi } from "vitest";

import { get } from "../../api/client";
import {
  isAllowedGetPath,
  parseErrorPayload,
  parseSelectPayload,
  routeInboundMessage,
  withAppCsp,
} from "./CustomAppFrame";

vi.mock("../../api/client", () => ({ get: vi.fn() }));

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

describe("routeInboundMessage (the load-bearing ordering invariant)", () => {
  // A fake frame whose contentWindow is the trusted sender; serviceBrokerGet
  // replies via target.postMessage, so the window needs that method.
  function makeFrame(): {
    frame: HTMLIFrameElement;
    win: { postMessage: ReturnType<typeof vi.fn> };
  } {
    const win = { postMessage: vi.fn() };
    const frame = { contentWindow: win } as unknown as HTMLIFrameElement;
    return { frame, win };
  }

  function event(source: unknown, data: unknown): MessageEvent {
    return { source, data } as unknown as MessageEvent;
  }

  beforeEach(() => {
    vi.mocked(get).mockReset();
    vi.mocked(get).mockResolvedValue({});
  });

  it("drops a message whose source is not the frame's window (identity check)", () => {
    const { frame } = makeFrame();
    const onSelect = vi.fn();
    routeInboundMessage(
      event(
        { postMessage: vi.fn() }, // a DIFFERENT window
        {
          source: "wuphf-app",
          type: "broker",
          id: 1,
          method: "GET",
          path: "/tasks",
        },
      ),
      frame,
      "*",
      { current: onSelect },
      { current: vi.fn() },
    );
    expect(get).not.toHaveBeenCalled();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("routes a wuphf-select to the callback and NEVER touches the broker", () => {
    const { frame, win } = makeFrame();
    const onSelect = vi.fn();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "wuphf-select",
        file: "a.tsx",
        line: 3,
        col: 1,
        tag: "button",
        label: "Save",
      }),
      frame,
      "*",
      { current: onSelect },
      { current: vi.fn() },
    );
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ file: "a.tsx", tag: "button" }),
    );
    expect(get).not.toHaveBeenCalled();
  });

  it("routes a wuphf-error to the callback and NEVER touches the broker", () => {
    const { frame, win } = makeFrame();
    const onErr = vi.fn();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "wuphf-error",
        message: "boom",
        stack: "s",
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: onErr },
    );
    expect(onErr).toHaveBeenCalledWith({ message: "boom", stack: "s" });
    expect(get).not.toHaveBeenCalled();
  });

  it("services an allowlisted broker GET", () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "broker",
        id: 7,
        method: "GET",
        path: "/tasks",
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(get).toHaveBeenCalledWith("/tasks");
  });

  it("rejects a non-GET broker request before any broker call", () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "broker",
        id: 9,
        method: "POST",
        path: "/tasks",
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(get).not.toHaveBeenCalled();
    // It replied with the GET-only error rather than fetching anything.
    expect(win.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ ok: false }),
      "*",
    );
  });

  it("ignores a message that is not from the app source at all", () => {
    const { frame, win } = makeFrame();
    const onSelect = vi.fn();
    routeInboundMessage(
      event(win, {
        source: "somewhere-else",
        type: "wuphf-select",
        file: "a.tsx",
      }),
      frame,
      "*",
      { current: onSelect },
      { current: vi.fn() },
    );
    expect(onSelect).not.toHaveBeenCalled();
    expect(get).not.toHaveBeenCalled();
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
