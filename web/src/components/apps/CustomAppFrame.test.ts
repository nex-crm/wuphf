import { beforeEach, describe, expect, it, vi } from "vitest";

import { get, post } from "../../api/client";
import { confirm } from "../ui/ConfirmDialog";
import {
  appBrokerPath,
  isAllowedGetPath,
  parseAIArgs,
  parseErrorPayload,
  parseIntegrationArgs,
  parseSelectPayload,
  routeInboundMessage,
  withAppCsp,
} from "./CustomAppFrame";

vi.mock("../../api/client", () => ({ get: vi.fn(), post: vi.fn() }));
// Auto-accept the human confirmation so the create_task path runs to the POST.
vi.mock("../ui/ConfirmDialog", () => ({
  confirm: vi.fn((opts: { onConfirm: () => unknown }) => opts.onConfirm()),
}));
vi.mock("../ui/Toast", () => ({ showNotice: vi.fn() }));

describe("isAllowedGetPath", () => {
  it("allows whitelisted read-only office data paths", () => {
    expect(isAllowedGetPath("/apps/integrations/catalog")).toBe(true);
    expect(isAllowedGetPath("/tasks")).toBe(true);
    expect(isAllowedGetPath("/tasks?status=open")).toBe(true);
    expect(isAllowedGetPath("/office-members")).toBe(true);
    expect(isAllowedGetPath("/wiki/list")).toBe(true);
  });

  it("rejects non-whitelisted, mutating, or off-broker paths", () => {
    // Not on the allowlist.
    expect(isAllowedGetPath("/config")).toBe(false);
    expect(isAllowedGetPath("/web-token")).toBe(false);
    expect(isAllowedGetPath("/memory")).toBe(false);
    // L2: an app must NOT read another app's source bundle via GET /apps/<id>;
    // the bare "/apps" prefix is intentionally not allowlisted.
    expect(isAllowedGetPath("/apps")).toBe(false);
    expect(isAllowedGetPath("/apps/app_0123456789abcdef")).toBe(false);
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

describe("appBrokerPath", () => {
  it("upgrades a bare /tasks to the whole office task list", () => {
    expect(appBrokerPath("/tasks")).toBe(
      "/tasks?all_channels=true&include_done=true&viewer_slug=human",
    );
  });
  it("leaves an explicit task query untouched", () => {
    expect(appBrokerPath("/tasks?channel=general")).toBe(
      "/tasks?channel=general",
    );
    expect(appBrokerPath("/tasks?all_channels=true")).toBe(
      "/tasks?all_channels=true",
    );
  });
  it("leaves other allowlisted paths untouched", () => {
    expect(appBrokerPath("/office-members")).toBe("/office-members");
    expect(appBrokerPath("/wiki/list")).toBe("/wiki/list");
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
    vi.mocked(post).mockReset();
    vi.mocked(post).mockResolvedValue({ task: { id: "OFFICE-9" } });
    // Reset call history, then keep the confirm mock auto-accepting.
    vi.mocked(confirm).mockReset();
    vi.mocked(confirm).mockImplementation(
      (opts: { onConfirm: () => unknown }) => opts.onConfirm(),
    );
  });

  it("create_task confirms, then POSTs a host-parameterized task", async () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "action",
        id: 3,
        action: "create_task",
        payload: { title: "Follow up on Acme", details: "renewal due" },
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(confirm).toHaveBeenCalledTimes(1);
    // Flush the onConfirm microtask chain.
    await Promise.resolve();
    await Promise.resolve();
    expect(post).toHaveBeenCalledWith(
      "/tasks",
      expect.objectContaining({
        action: "create",
        title: "Follow up on Acme",
        created_by: "human",
        task_type: "issue",
      }),
    );
    // The app cannot set owner/type/privileged fields.
    const body = vi.mocked(post).mock.calls[0][1] as Record<string, unknown>;
    expect(body.owner).toBeUndefined();
  });

  it("rejects an unknown action without confirming or posting", () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "action",
        id: 4,
        action: "delete_everything",
        payload: {},
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(confirm).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
    expect(win.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ ok: false }),
      "*",
    );
  });

  it("requires a non-empty title before confirming", () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "action",
        id: 5,
        action: "create_task",
        payload: { title: "   " },
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(confirm).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });

  it("coalesces: a second create_task while one is pending is refused", () => {
    vi.useFakeTimers();
    // Leave the first confirmation pending (don't auto-accept).
    vi.mocked(confirm).mockImplementation(() => undefined);
    const { frame, win } = makeFrame();
    const send = (id: number): void =>
      routeInboundMessage(
        event(win, {
          source: "wuphf-app",
          type: "action",
          id,
          action: "create_task",
          payload: { title: "Spam me" },
        }),
        frame,
        "*",
        { current: vi.fn() },
        { current: vi.fn() },
      );
    send(10); // first → confirmation shown, now pending
    send(11); // second → must be refused while one awaits the human
    expect(confirm).toHaveBeenCalledTimes(1);
    const lastReply = win.postMessage.mock.calls.at(-1)?.[0] as {
      ok: boolean;
      error?: string;
    };
    expect(lastReply.ok).toBe(false);
    expect(lastReply.error).toMatch(/already awaiting/i);
    // Free the module lock so later tests aren't blocked.
    vi.advanceTimersByTime(70_000);
    vi.useRealTimers();
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
    // A bare /tasks is upgraded host-side to the whole office task list.
    expect(get).toHaveBeenCalledWith(
      "/tasks?all_channels=true&include_done=true&viewer_slug=human",
    );
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

describe("parseIntegrationArgs (Bridge v2 host validation)", () => {
  it("accepts a well-formed integration call and trims fields", () => {
    const args = parseIntegrationArgs({
      platform: "  gmail ",
      action: " GMAIL_FETCH_EMAILS ",
      params: { max_results: 5 },
    });
    expect(args).toEqual({
      platform: "gmail",
      action: "GMAIL_FETCH_EMAILS",
      params: { max_results: 5 },
    });
  });

  it("allows a params-less call", () => {
    expect(
      parseIntegrationArgs({ platform: "slack", action: "SLACK_LIST" }),
    ).toEqual({ platform: "slack", action: "SLACK_LIST" });
  });

  it("rejects a call missing platform or action", () => {
    expect(parseIntegrationArgs({ platform: "gmail" })).toBeNull();
    expect(parseIntegrationArgs({ action: "GMAIL_FETCH_EMAILS" })).toBeNull();
    expect(parseIntegrationArgs({ platform: "", action: "x" })).toBeNull();
    expect(parseIntegrationArgs(null)).toBeNull();
    expect(parseIntegrationArgs("nope")).toBeNull();
  });

  it("rejects params that are not a plain object", () => {
    expect(
      parseIntegrationArgs({ platform: "g", action: "a", params: [1, 2] }),
    ).toBeNull();
    expect(
      parseIntegrationArgs({ platform: "g", action: "a", params: "str" }),
    ).toBeNull();
  });

  it("rejects an over-cap params payload", () => {
    const big = { blob: "x".repeat(17 * 1024) };
    expect(
      parseIntegrationArgs({ platform: "g", action: "a", params: big }),
    ).toBeNull();
  });
});

describe("parseAIArgs (Bridge v2 host validation)", () => {
  it("accepts a prompt and trims it", () => {
    expect(parseAIArgs({ prompt: "  hi  " })).toEqual({
      prompt: "hi",
      json: false,
    });
  });

  it("carries input and the json flag", () => {
    expect(
      parseAIArgs({ prompt: "score", input: { a: 1 }, json: true }),
    ).toEqual({ prompt: "score", input: { a: 1 }, json: true });
  });

  it("rejects an empty or over-cap prompt", () => {
    expect(parseAIArgs({ prompt: "   " })).toBeNull();
    expect(parseAIArgs({ prompt: "p".repeat(9 * 1024) })).toBeNull();
    expect(parseAIArgs(null)).toBeNull();
  });

  it("rejects over-cap input", () => {
    expect(
      parseAIArgs({ prompt: "ok", input: "i".repeat(201 * 1024) }),
    ).toBeNull();
  });
});

describe("routeInboundMessage — Bridge v2 forwarding", () => {
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
    vi.mocked(post).mockReset();
    vi.mocked(post).mockResolvedValue({ ok: true });
    vi.mocked(get).mockReset();
    vi.mocked(get).mockResolvedValue({});
  });

  it("forwards a valid integration call to POST /apps/integrations/call", async () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "integration",
        id: 1,
        platform: "gmail",
        action: "GMAIL_FETCH_EMAILS",
        params: { max_results: 3 },
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    await Promise.resolve();
    await Promise.resolve();
    expect(post).toHaveBeenCalledWith("/apps/integrations/call", {
      platform: "gmail",
      action: "GMAIL_FETCH_EMAILS",
      params: { max_results: 3 },
    });
  });

  it("rejects an invalid integration call WITHOUT touching the broker", () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "integration",
        id: 2,
        platform: "gmail", // missing action
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(post).not.toHaveBeenCalled();
    expect(win.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ ok: false }),
      "*",
    );
  });

  it("forwards a valid ai call to POST /apps/ai", async () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "ai",
        id: 3,
        prompt: "summarize",
        input: [{ x: 1 }],
        json: true,
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    await Promise.resolve();
    await Promise.resolve();
    expect(post).toHaveBeenCalledWith("/apps/ai", {
      prompt: "summarize",
      input: [{ x: 1 }],
      json: true,
    });
  });

  it("rejects an empty ai prompt WITHOUT touching the broker", () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, { source: "wuphf-app", type: "ai", id: 4, prompt: "  " }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
    );
    expect(post).not.toHaveBeenCalled();
    expect(win.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ ok: false }),
      "*",
    );
  });

  it("forwards the host app_id on ai() and integration calls for per-app budgeting", async () => {
    const { frame, win } = makeFrame();
    routeInboundMessage(
      event(win, { source: "wuphf-app", type: "ai", id: 5, prompt: "go" }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
      "app_deadbeefdeadbeef",
    );
    routeInboundMessage(
      event(win, {
        source: "wuphf-app",
        type: "integration",
        id: 6,
        platform: "gmail",
        action: "GMAIL_FETCH_EMAILS",
      }),
      frame,
      "*",
      { current: vi.fn() },
      { current: vi.fn() },
      "app_deadbeefdeadbeef",
    );
    await Promise.resolve();
    await Promise.resolve();
    expect(post).toHaveBeenCalledWith(
      "/apps/ai",
      expect.objectContaining({ app_id: "app_deadbeefdeadbeef" }),
    );
    expect(post).toHaveBeenCalledWith(
      "/apps/integrations/call",
      expect.objectContaining({ app_id: "app_deadbeefdeadbeef" }),
    );
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
