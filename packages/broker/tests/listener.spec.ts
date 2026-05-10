import { type OutgoingHttpHeaders, request } from "node:http";
import { apiBootstrapFromJson, asApiToken } from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";
import type { BrokerHandle } from "../src/index.ts";
import { createBroker } from "../src/index.ts";

interface RawResponse {
  readonly status: number;
  readonly body: string;
}

interface RawTestHeaders {
  Host?: string;
  Authorization?: string;
}

function rawGet(args: {
  port: number;
  path: string;
  hostHeader?: string;
  authorization?: string;
}): Promise<RawResponse> {
  return new Promise((resolveFn, rejectFn) => {
    const headers: RawTestHeaders = {};
    if (args.hostHeader !== undefined) headers.Host = args.hostHeader;
    if (args.authorization !== undefined) headers.Authorization = args.authorization;
    const req = request(
      {
        host: "127.0.0.1",
        port: args.port,
        path: args.path,
        method: "GET",
        // OutgoingHttpHeaders has an index signature; the local typed
        // builder above keeps `headers.Host` accessible under
        // `noPropertyAccessFromIndexSignature` while satisfying http.request.
        headers: headers as OutgoingHttpHeaders,
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (c: Buffer) => chunks.push(c));
        res.on("end", () =>
          resolveFn({ status: res.statusCode ?? 0, body: Buffer.concat(chunks).toString("utf8") }),
        );
      },
    );
    req.on("error", rejectFn);
    req.end();
  });
}

const FIXED_TOKEN = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");

describe("createBroker", () => {
  let broker: BrokerHandle | null = null;

  afterEach(async () => {
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
  });

  it("binds to 127.0.0.1 on an ephemeral port and returns a usable url", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    expect(broker.url).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
    expect(broker.port).toBeGreaterThan(0);
    expect(broker.token).toBe(FIXED_TOKEN);
  });

  it("returns the api-token bootstrap with the expected wire shape", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/api-token`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toMatch(/^application\/json/);
    const json: unknown = await res.json();
    const bootstrap = apiBootstrapFromJson(json);
    expect(bootstrap.token).toBe(FIXED_TOKEN);
    expect(bootstrap.brokerUrl).toBe(broker.url);
  });

  it("rejects api-token requests whose Host header is not loopback", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    // `fetch` (undici) strips the `Host` header on send; use http.request so
    // we can inject an attacker-supplied Host through the loopback connection.
    const res = await rawGet({
      port: broker.port,
      path: "/api-token",
      hostHeader: "evil.example.com",
    });
    expect(res.status).toBe(403);
    expect(res.body).toMatch(/^loopback_/);
  });

  it("requires bearer auth on /api/health and accepts the issued token", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const noAuth = await fetch(`${broker.url}/api/health`);
    expect(noAuth.status).toBe(401);
    expect(noAuth.headers.get("www-authenticate")).toMatch(/^Bearer realm=/);
    const withAuth = await fetch(`${broker.url}/api/health`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    expect(withAuth.status).toBe(200);
    const body: unknown = await withAuth.json();
    expect(body).toEqual({ ok: true });
  });

  it("rejects /api/health with a wrong bearer", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/api/health`, {
      headers: { Authorization: "Bearer not-the-right-token-aaaaaaaaaaaa" },
    });
    expect(res.status).toBe(401);
  });

  it("404s unknown routes", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/no/such/route`);
    expect(res.status).toBe(404);
  });

  it("405s non-GET methods", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/api-token`, { method: "POST" });
    expect(res.status).toBe(405);
    expect(res.headers.get("allow")).toBe("GET, HEAD");
  });

  it("survives stop being called twice", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    await Promise.all([broker.stop(), broker.stop()]);
    // Idempotent: a third call should also resolve.
    await broker.stop();
    broker = null;
  });

  it("auto-generates a token when none is supplied", async () => {
    broker = await createBroker();
    expect(typeof broker.token).toBe("string");
    expect(broker.token.length).toBeGreaterThanOrEqual(16);
  });

  it("emits a ready SSE event to authenticated subscribers", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const controller = new AbortController();
    const res = await fetch(`${broker.url}/api/events`, {
      headers: {
        Authorization: `Bearer ${FIXED_TOKEN}`,
        Accept: "text/event-stream",
      },
      signal: controller.signal,
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toMatch(/^text\/event-stream/);
    const reader = res.body?.getReader();
    expect(reader).toBeDefined();
    if (reader === undefined) throw new Error("missing body reader");
    const { value } = await reader.read();
    const text = new TextDecoder().decode(value);
    expect(text).toMatch(/event: ready/);
    expect(text).toMatch(/"emittedAt":/);
    controller.abort();
  });

  it("rejects /api/events without a bearer (and does not flush SSE headers)", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/api/events`);
    expect(res.status).toBe(401);
    // Defence-in-depth: a 401 must NOT advertise itself as text/event-stream
    // — an unauthenticated client should never see the SSE handshake. A
    // future refactor flipping the route order would silently break this.
    expect(res.headers.get("content-type")).toMatch(/^text\/plain/);
    expect(res.headers.get("www-authenticate")).toMatch(/^Bearer realm=/);
  });

  it("HEAD /api/events does not allocate an SSE session (no leaked keepalive timer)", async () => {
    // CodeRabbit pass-3: a HEAD probe used to fall through to handleEvents,
    // which calls startSseSession — that attaches a 30s setInterval per
    // request. Node strips the response body on HEAD but the timer keeps
    // running until the client disconnects, leaking one timer per probe.
    // The handler now short-circuits HEAD with a stub response so no
    // session is allocated.
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/api/events`, {
      method: "HEAD",
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toMatch(/^text\/event-stream/);
    // Body must be empty for HEAD; res.body may be null in some runtimes.
    // The point is: the connection closes immediately (no keepalive).
    const text = await res.text();
    expect(text).toBe("");
  });

  it("emits Vary: Origin on /api-token so future CORS layers cache by origin", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/api-token`);
    expect(res.status).toBe(200);
    expect(res.headers.get("vary")).toBe("Origin");
  });

  it("enforces bearer auth structurally on every /api/* route (default-deny)", async () => {
    // Regression: a future contributor adding `/api/anything-new` should
    // get bearer-required by construction, even without calling authorize()
    // in the handler. The default-deny gate handles this before route
    // dispatch. /api/no-such-route returns 401, not 404 — proving the
    // bearer check ran before the handler-lookup.
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await rawGet({ port: broker.port, path: "/api/no-such-route" });
    expect(res.status).toBe(401);
    // With a valid bearer, route falls through to the 404 handler.
    const withAuth = await rawGet({
      port: broker.port,
      path: "/api/no-such-route",
      authorization: `Bearer ${FIXED_TOKEN}`,
    });
    expect(withAuth.status).toBe(404);
  });

  it("/api-token remains accessible without a bearer (renderer cannot have one yet)", async () => {
    // The structural bearer gate keys off `/api/` (with trailing slash);
    // `/api-token` does not start with that prefix so the gate skips it.
    // Loopback guard is the only check on this route by design.
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await rawGet({ port: broker.port, path: "/api-token" });
    expect(res.status).toBe(200);
  });

  it("/api-token rejects cross-origin browser fetches (Origin header mismatch)", async () => {
    // Defense-in-depth for the documented loopback-trust model. A browser
    // page on a different origin (chrome-extension, local web app on a
    // different port) tries to fetch the broker's bootstrap; the broker
    // rejects because Origin does not match the bound URL.
    const handle = await createBroker({ token: FIXED_TOKEN });
    broker = handle;
    // Use http.request directly so we can inject an attacker-supplied
    // Origin — fetch() pins Origin to the calling page's origin.
    const res = await new Promise<{ status: number; body: string }>((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port: handle.port,
          path: "/api-token",
          method: "GET",
          headers: { Origin: "http://other.local:12345" } as OutgoingHttpHeaders,
        },
        (response) => {
          const chunks: Buffer[] = [];
          response.on("data", (c: Buffer) => chunks.push(c));
          response.on("end", () =>
            resolve({
              status: response.statusCode ?? 0,
              body: Buffer.concat(chunks).toString("utf8"),
            }),
          );
        },
      );
      req.on("error", reject);
      req.end();
    });
    expect(res.status).toBe(403);
    expect(res.body).toBe("cross_origin_api_token");
  });

  it("/api-token rejects Sec-Fetch-Site=cross-site (browser-hardening)", async () => {
    const handle = await createBroker({ token: FIXED_TOKEN });
    broker = handle;
    const res = await new Promise<{ status: number; body: string }>((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port: handle.port,
          path: "/api-token",
          method: "GET",
          headers: { "Sec-Fetch-Site": "cross-site" } as OutgoingHttpHeaders,
        },
        (response) => {
          const chunks: Buffer[] = [];
          response.on("data", (c: Buffer) => chunks.push(c));
          response.on("end", () =>
            resolve({
              status: response.statusCode ?? 0,
              body: Buffer.concat(chunks).toString("utf8"),
            }),
          );
        },
      );
      req.on("error", reject);
      req.end();
    });
    expect(res.status).toBe(403);
    expect(res.body).toBe("cross_origin_api_token");
  });

  it("/api-token rejects Origin: null (opaque origins like file://, sandboxed iframes)", async () => {
    // Triangulation pass-2 (security lens): browsers send `Origin: null`
    // for opaque origins — file://, sandboxed iframes, data:/blob:
    // contexts. The original gate treated `null` as "no Origin" and let
    // it bypass; a hostile local HTML file or a sandboxed page could
    // then trigger the bootstrap. Reject explicitly.
    const handle = await createBroker({ token: FIXED_TOKEN });
    broker = handle;
    const res = await new Promise<{ status: number; body: string }>((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port: handle.port,
          path: "/api-token",
          method: "GET",
          headers: { Origin: "null" } as OutgoingHttpHeaders,
        },
        (response) => {
          const chunks: Buffer[] = [];
          response.on("data", (c: Buffer) => chunks.push(c));
          response.on("end", () =>
            resolve({
              status: response.statusCode ?? 0,
              body: Buffer.concat(chunks).toString("utf8"),
            }),
          );
        },
      );
      req.on("error", reject);
      req.end();
    });
    expect(res.status).toBe(403);
    expect(res.body).toBe("null_origin");
  });

  it("/api-token accepts a trusted dev-server origin (cross-origin in dev mode)", async () => {
    // Triangulation pass-2 (electron lens): in dev mode the renderer
    // loads from electron-vite (http://localhost:5173) and the broker
    // listens on a different loopback port. The pass-1 Origin gate
    // rejected this legitimate cross-origin bootstrap. trustedOrigins
    // is the per-config allowlist; main/index.ts plumbs the dev URL
    // through WUPHF_DEV_RENDERER_ORIGIN.
    const handle = await createBroker({
      token: FIXED_TOKEN,
      trustedOrigins: ["http://localhost:5173"],
    });
    broker = handle;
    const res = await new Promise<{ status: number }>((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port: handle.port,
          path: "/api-token",
          method: "GET",
          headers: { Origin: "http://localhost:5173" } as OutgoingHttpHeaders,
        },
        (response) => resolve({ status: response.statusCode ?? 0 }),
      );
      req.on("error", reject);
      req.end();
    });
    expect(res.status).toBe(200);
  });

  it("trustedOrigins rejects non-loopback entries at config time", async () => {
    await expect(
      createBroker({ token: FIXED_TOKEN, trustedOrigins: ["https://evil.com"] }),
    ).rejects.toThrow(/loopback/);
  });

  it("/api requires bearer (closes the exact-/api namespace-root gap)", async () => {
    // Triangulation pass-2 (architecture lens): the original structural
    // bearer gate was `pathname.startsWith("/api/")`, which excluded
    // `/api` exactly. A future route at `pathname === "/api"` would have
    // been bearerless. Gate now also matches the exact form.
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await rawGet({ port: broker.port, path: "/api" });
    expect(res.status).toBe(401);
  });

  it("/api-token allows same-origin browser fetches", async () => {
    const handle = await createBroker({ token: FIXED_TOKEN });
    broker = handle;
    const res = await new Promise<{ status: number }>((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port: handle.port,
          path: "/api-token",
          method: "GET",
          headers: { Origin: handle.url, "Sec-Fetch-Site": "same-origin" } as OutgoingHttpHeaders,
        },
        (response) => resolve({ status: response.statusCode ?? 0 }),
      );
      req.on("error", reject);
      req.end();
    });
    expect(res.status).toBe(200);
  });
});
