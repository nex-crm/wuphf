import { request as httpRequest, type IncomingMessage, type OutgoingHttpHeaders } from "node:http";
import type { Socket } from "node:net";

import { asApiToken } from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";
import { WebSocket } from "ws";
import type { BrokerHandle } from "../src/index.ts";
import { createBroker } from "../src/index.ts";
import { decideTerminalUpgrade } from "../src/terminal-ws.ts";

const FIXED_TOKEN = asApiToken("ws-test-token-AAAAAAAAAAAAAAAAAAAAAAAAAAA");

interface FakeHeaders {
  host: string;
  origin?: string;
}

function fakeRequest(args: {
  url: string;
  host?: string;
  origin?: string;
  remoteAddress?: string;
}): IncomingMessage {
  const headers: FakeHeaders = { host: args.host ?? "127.0.0.1:7891" };
  if (args.origin !== undefined) headers.origin = args.origin;
  const socket = { remoteAddress: args.remoteAddress ?? "127.0.0.1" } as unknown as Socket;
  return {
    url: args.url,
    headers,
    socket,
  } as unknown as IncomingMessage;
}

describe("decideTerminalUpgrade", () => {
  it("rejects non-terminal paths with 404", () => {
    expect(decideTerminalUpgrade(fakeRequest({ url: "/api/health" }), FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 404,
      reason: "not_found",
    });
  });

  it("rejects bad host with 403", () => {
    const req = fakeRequest({ url: "/terminal/agents/foo?token=x", host: "evil.example.com" });
    const decision = decideTerminalUpgrade(req, FIXED_TOKEN);
    expect(decision.kind).toBe("reject");
    if (decision.kind !== "reject") return;
    expect(decision.status).toBe(403);
    expect(decision.reason).toMatch(/^loopback_/);
  });

  it("rejects non-loopback origin with 403", () => {
    const req = fakeRequest({
      url: `/terminal/agents/foo?token=${FIXED_TOKEN}`,
      origin: "https://evil.example.com",
    });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 403,
      reason: "non_loopback_origin",
    });
  });

  it("accepts absent origin (Electron WebView)", () => {
    const req = fakeRequest({
      url: `/terminal/agents/foo?token=${FIXED_TOKEN}`,
    });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({ kind: "accept" });
  });

  it("rejects bad token with 401", () => {
    const req = fakeRequest({ url: "/terminal/agents/foo?token=wrong" });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 401,
      reason: "bad_token",
    });
  });

  it("rejects malformed origin URL", () => {
    const req = fakeRequest({
      url: `/terminal/agents/foo?token=${FIXED_TOKEN}`,
      origin: "not a url",
    });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 403,
      reason: "bad_origin",
    });
  });

  it("rejects port-less loopback origin (default-port-stripped URL)", () => {
    // `new URL("https://127.0.0.1").host === "127.0.0.1"` — the default
    // port gets stripped. Without an explicit-port check, a malicious
    // listener on https://127.0.0.1/ could pass the host gate.
    const req = fakeRequest({
      url: `/terminal/agents/foo?token=${FIXED_TOKEN}`,
      origin: "https://127.0.0.1",
    });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 403,
      reason: "non_loopback_origin",
    });
  });

  it("rejects non-http(s) origin scheme (chrome-extension://, etc.)", () => {
    // chrome-extension URLs parse cleanly through `new URL` but must not
    // pass the broker's origin gate — the renderer is loaded from
    // http://127.0.0.1:<port>/ and any other scheme is foreign.
    const req = fakeRequest({
      url: `/terminal/agents/foo?token=${FIXED_TOKEN}`,
      origin: "chrome-extension://abcdefghijklmnopabcdefghijklmnop",
    });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 403,
      reason: "bad_origin_scheme",
    });
  });

  it("rejects multi-segment slugs (`/terminal/agents/foo/bar`) with bad_agent_slug", () => {
    const req = fakeRequest({
      url: `/terminal/agents/foo/bar?token=${FIXED_TOKEN}`,
    });
    expect(decideTerminalUpgrade(req, FIXED_TOKEN)).toEqual({
      kind: "reject",
      status: 404,
      reason: "bad_agent_slug",
    });
  });

  it("rejects slugs whose decoded form fails the protocol asAgentSlug brand", () => {
    // protocol AGENT_SLUG_RE: /^[a-z0-9][a-z0-9_-]{0,127}$/. Uppercase
    // letters, leading dash, and special chars are rejected.
    const reqA = fakeRequest({
      url: `/terminal/agents/UPPERCASE?token=${FIXED_TOKEN}`,
    });
    expect(decideTerminalUpgrade(reqA, FIXED_TOKEN).kind).toBe("reject");
    if (decideTerminalUpgrade(reqA, FIXED_TOKEN).kind === "reject") {
      expect((decideTerminalUpgrade(reqA, FIXED_TOKEN) as { reason: string }).reason).toBe(
        "bad_agent_slug",
      );
    }

    const reqB = fakeRequest({
      url: `/terminal/agents/-leading-dash?token=${FIXED_TOKEN}`,
    });
    expect(decideTerminalUpgrade(reqB, FIXED_TOKEN).kind).toBe("reject");
  });

  it("rejects percent-encoded slash in slug (cannot smuggle multi-segment via %2F)", () => {
    // `/terminal/agents/foo%2Fbar` decodes to `foo/bar` which is multi-
    // segment and must not pass. The slug check decodes once and reapplies
    // the single-segment + brand check on the decoded value.
    const req = fakeRequest({
      url: `/terminal/agents/foo%2Fbar?token=${FIXED_TOKEN}`,
    });
    const decision = decideTerminalUpgrade(req, FIXED_TOKEN);
    expect(decision.kind).toBe("reject");
    if (decision.kind === "reject") expect(decision.reason).toBe("bad_agent_slug");
  });
});

describe("WebSocket end-to-end", () => {
  let broker: BrokerHandle | null = null;

  afterEach(async () => {
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
  });

  it("accepts the upgrade with a valid token then closes 1011 not_implemented", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const wsUrl = broker.url.replace(/^http/, "ws");
    const ws = new WebSocket(`${wsUrl}/terminal/agents/test-agent?token=${FIXED_TOKEN}`);
    const closeFrame = await new Promise<{ code: number; reason: string }>((resolve, reject) => {
      ws.on("close", (code, reason) => resolve({ code, reason: reason.toString("utf8") }));
      ws.on("error", reject);
    });
    expect(closeFrame.code).toBe(1011);
    expect(closeFrame.reason).toBe("not_implemented");
  });

  it("rejects upgrade with bad token via HTTP 401 response", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const wsUrl = broker.url.replace(/^http/, "ws");
    const ws = new WebSocket(`${wsUrl}/terminal/agents/test-agent?token=wrong`);
    const err = await new Promise<Error>((resolve) => {
      ws.on("unexpected-response", (_req, res) => {
        const status = res.statusCode ?? 0;
        resolve(new Error(`http_${String(status)}`));
      });
      ws.on("error", resolve);
    });
    expect(err.message).toMatch(/401|Unexpected server response: 401/);
  });

  it("redacts case-variant token params and strips fragments from log payloads", async () => {
    // Triangulation pass-2 (security lens): the original redactedPath
    // matched only lowercase `token=` and didn't strip the fragment.
    // `?Token=<bearer>` (uppercase param name) would have leaked the
    // bearer verbatim; `#token=<bearer>` would have logged unchanged.
    // Direct unit test via http.request so we control raw URL casing.
    interface LogCall {
      readonly level: "info" | "warn" | "error";
      readonly event: string;
      readonly payload: Record<string, unknown> | undefined;
    }
    const calls: LogCall[] = [];
    const logger = {
      info: (event: string, payload?: Record<string, unknown>) => {
        calls.push({ level: "info", event, payload });
      },
      warn: (event: string, payload?: Record<string, unknown>) => {
        calls.push({ level: "warn", event, payload });
      },
      error: (event: string, payload?: Record<string, unknown>) => {
        calls.push({ level: "error", event, payload });
      },
    };
    const handle = await createBroker({ token: FIXED_TOKEN, logger });
    broker = handle;
    // Use http.request with raw upgrade headers so we can force the
    // case-variant param + fragment. The reject path will log the path.
    const SECRET = `${FIXED_TOKEN}-uppercase`;
    await new Promise<void>((resolve, reject) => {
      const req = httpRequest(
        {
          host: "127.0.0.1",
          port: handle.port,
          path: `/terminal/agents/a?Token=${SECRET}#token=${SECRET}`,
          headers: {
            Connection: "Upgrade",
            Upgrade: "websocket",
            "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==",
            "Sec-WebSocket-Version": "13",
          } as OutgoingHttpHeaders,
        },
        () => resolve(),
      );
      req.on("upgrade", () => resolve());
      req.on("response", () => resolve());
      req.on("error", () => resolve()); // socket destroy on reject
      req.end();
      setTimeout(reject, 2000, new Error("upgrade timeout"));
    }).catch(() => undefined);

    // The secret must not appear in any logged path.
    for (const call of calls) {
      const path = call.payload?.["path"];
      if (typeof path === "string") {
        expect(path).not.toContain(SECRET);
        // Fragment must be stripped entirely from the logged path.
        expect(path).not.toContain("#");
      }
    }
  });

  it("redacts ?token=<bearer> from upgrade log payloads", async () => {
    interface LogCall {
      readonly level: "info" | "warn" | "error";
      readonly event: string;
      readonly payload: Record<string, unknown> | undefined;
    }
    const calls: LogCall[] = [];
    const logger = {
      info: (event: string, payload?: Record<string, unknown>) => {
        calls.push({ level: "info", event, payload });
      },
      warn: (event: string, payload?: Record<string, unknown>) => {
        calls.push({ level: "warn", event, payload });
      },
      error: (event: string, payload?: Record<string, unknown>) => {
        calls.push({ level: "error", event, payload });
      },
    };
    broker = await createBroker({ token: FIXED_TOKEN, logger });
    const wsUrl = broker.url.replace(/^http/, "ws");

    // Both reject and accept paths log a `path:` field. Trigger reject
    // (wrong token) and accept (valid token) so we exercise both code
    // sites. Use the same FIXED_TOKEN so the secret is potentially present
    // in the URL.
    const ws1 = new WebSocket(`${wsUrl}/terminal/agents/test-agent?token=${FIXED_TOKEN}-wrong`);
    await new Promise<void>((resolve) => {
      ws1.on("unexpected-response", () => resolve());
      ws1.on("error", () => resolve());
    });
    const ws2 = new WebSocket(`${wsUrl}/terminal/agents/test-agent?token=${FIXED_TOKEN}`);
    await new Promise<void>((resolve) => {
      ws2.on("close", () => resolve());
      ws2.on("error", () => resolve());
    });

    // No logged path should contain the raw bearer.
    for (const call of calls) {
      const payloadPath = call.payload?.["path"];
      if (typeof payloadPath === "string") {
        expect(payloadPath).not.toContain(FIXED_TOKEN);
        if (payloadPath.includes("token=")) {
          expect(payloadPath).toMatch(/token=redacted/);
        }
      }
    }
  });
});
