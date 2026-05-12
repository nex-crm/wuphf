// HTTP listener that owns the broker's loopback surface.
//
// Routes (branch-4 + branch-5 scope):
//   GET  /api-token                       — bootstrap. Loopback-guarded. NO bearer required.
//   GET  /api/health                      — bearer required. Returns {"ok":true}.
//   GET  /api/events                      — bearer required. SSE stream with initial `ready`.
//   POST /api/receipts                    — bearer required. Body: receipt JSON.
//                                           201 on insert, 409 on id collision.
//   GET  /api/receipts/:id                — bearer required. 200 on hit, 404 on miss.
//   GET  /api/threads/:tid/receipts       — bearer required. List receipts in a thread.
//   GET  /                                — static (renderer bundle) or 404 if disabled.
//   GET  /index.html                      — static or 404.
//   GET  /assets/*                        — static or 404.
//   *    /*                               — 404.
//
// Every HTTP request goes through `checkLoopbackRequest` first (DNS-rebinding
// guard). Bearer auth is enforced per-route — `/api-token` is the bootstrap
// and intentionally does not require a token (the renderer cannot have one
// before calling it), but it is still loopback-guarded.

import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";

import {
  type ApiBootstrap,
  type ApiToken,
  apiBootstrapToJson,
  asBrokerPort,
  asBrokerUrl,
  type BrokerPort,
} from "@wuphf/protocol";
import { WebSocketServer } from "ws";

import { extractBearerFromHeader, tokenMatches } from "./auth.ts";
import { type CostRouteDeps, handleCostRoute } from "./cost-ledger/routes.ts";
import { checkLoopbackRequest } from "./dns-rebinding-guard.ts";
import { InMemoryReceiptStore, type ReceiptStore } from "./receipt-store.ts";
import { handleReceiptCreate, handleReceiptGet, handleThreadReceiptsList } from "./receipts.ts";
import { createStaticHandler, type StaticHandler } from "./serve-static.ts";
import { startSseSession } from "./sse.ts";
import { attachTerminalUpgrade } from "./terminal-ws.ts";
import { generateApiToken } from "./token.ts";
import { type BrokerConfig, type BrokerHandle, type BrokerLogger, NOOP_LOGGER } from "./types.ts";

const LOOPBACK_HOST = "127.0.0.1";

export async function createBroker(config: BrokerConfig = {}): Promise<BrokerHandle> {
  const logger: BrokerLogger = config.logger ?? NOOP_LOGGER;
  const token: ApiToken = config.token ?? generateApiToken();
  const staticHandler = createStaticHandler(config.renderer ?? null);
  const trustedOrigins = normalizeTrustedOrigins(config.trustedOrigins);
  // Default to an in-memory store when the host doesn't supply one. Branch
  // 6 hosts will pass a durable event-log-backed store; this default keeps
  // the package self-contained for tests and dev runs.
  const receiptStore: ReceiptStore = config.receiptStore ?? new InMemoryReceiptStore();
  const cost =
    config.cost === undefined
      ? null
      : ({
          ledger: config.cost.ledger,
          db: config.cost.db,
          logger,
          nowMs: () => Date.now(),
        } satisfies CostRouteDeps);
  const server = createServer((req, res) => {
    routeRequest(req, res, {
      token,
      staticHandler,
      logger,
      trustedOrigins,
      receiptStore,
      cost,
    }).catch((err: unknown) => {
      logger.error("listener_route_failed", {
        error: err instanceof Error ? err.message : String(err),
        path: req.url ?? null,
      });
      if (!res.writableEnded) {
        res.statusCode = 500;
        res.setHeader("Content-Type", "text/plain; charset=utf-8");
        res.end("internal_error");
      }
    });
  });

  const wss = new WebSocketServer({ noServer: true });
  attachTerminalUpgrade(server, { wss, token, logger });

  const port = await listen(server, config.port ?? 0);
  const url = `http://${LOOPBACK_HOST}:${port}`;
  logger.info("listener_started", { port, url });

  let stopInflight: Promise<void> | null = null;
  const stop = (): Promise<void> => {
    // Per-handle stop guard. Multiple `stop()` calls share one closure so
    // listeners and `wss.close` only run once; subsequent callers wait on
    // the same promise.
    if (stopInflight === null) {
      stopInflight = doStop(server, wss, logger);
    }
    return stopInflight;
  };

  return {
    url,
    port: asBrokerPort(port),
    token,
    stop,
  };
}

interface RouteDeps {
  readonly token: ApiToken;
  readonly staticHandler: StaticHandler;
  readonly logger: BrokerLogger;
  readonly trustedOrigins: ReadonlySet<string>;
  readonly receiptStore: ReceiptStore;
  readonly cost: CostRouteDeps | null;
}

async function routeRequest(
  req: IncomingMessage,
  res: ServerResponse,
  deps: RouteDeps,
): Promise<void> {
  // No global method gate: each route's dispatch knows which methods it
  // supports and sends the correct `Allow:` header. A global gate that
  // emitted "Allow: GET, HEAD" would lie about `/api/receipts` (which
  // accepts POST) and vice versa.
  const guard = checkLoopbackRequest({
    hostHeader: req.headers.host,
    remoteAddress: req.socket.remoteAddress ?? undefined,
  });
  if (!guard.allowed) {
    deps.logger.warn("listener_loopback_denied", {
      reason: guard.reason ?? null,
      path: req.url ?? null,
    });
    forbidden(res, `loopback_${guard.reason ?? "denied"}`);
    return;
  }
  const url = parseRequestUrl(req);
  if (url === null) {
    badRequest(res, "bad_url");
    return;
  }
  const pathname = url.pathname;

  // Reject `..` segments and NUL bytes BEFORE the route dispatch. The URL
  // parser normalizes `/assets/foo/../bar` to `/assets/bar` before this
  // function ever sees it; check the raw `req.url` so a request that
  // started with a traversal segment is denied even when normalization
  // resolves it back inside the renderer dir. Static handlers (which
  // resolve paths under their root) cannot detect this on their own.
  if (req.url !== undefined && containsRawTraversalOrNul(req.url)) {
    notFound(res);
    return;
  }

  // Default-deny bearer policy: any `/api`-namespace route requires
  // Authorization by construction. This is structural rather than
  // per-handler discipline — a future contributor adding `/api/receipts`
  // (or an exact `/api`) can't accidentally ship a loopback-only,
  // bearerless endpoint by forgetting to call authorize().
  //
  // `/api-token` is deliberately excluded: it's the bootstrap and the
  // renderer cannot have a bearer yet (loopback + Origin gates are the
  // only checks). The shape comparison covers both:
  //   - exact `/api` (no trailing slash) → gated. Future namespace-root
  //     route would otherwise be bearerless.
  //   - `/api/...` → gated.
  //   - `/api-token` → NOT gated (different prefix shape entirely).
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    if (!authorize(req, res, deps.token, deps.logger, pathname)) return;
  }

  if (pathname === "/api-token") {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res);
      return;
    }
    handleApiToken(req, res, deps);
    return;
  }
  if (pathname === "/api/health") {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res);
      return;
    }
    handleHealth(res);
    return;
  }
  if (pathname === "/api/events") {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res);
      return;
    }
    // HEAD on /api/events must NOT allocate a session — startSseSession
    // attaches a 30s keepalive setInterval and a `close` handler. Node
    // strips the body for HEAD but the interval keeps running until the
    // client disconnects, leaking a timer per probe. Respond with the
    // SSE content-type headers and end immediately for HEAD.
    if (req.method === "HEAD") {
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-store",
      });
      res.end();
      return;
    }
    handleEvents(res);
    return;
  }
  // POST /api/receipts is the create endpoint; GET /api/receipts/:id is the
  // read endpoint. Other methods on either path return 405 from the handler.
  if (pathname === "/api/receipts") {
    await handleReceiptCreate(req, res, {
      receiptStore: deps.receiptStore,
      logger: deps.logger,
    });
    return;
  }
  if (pathname.startsWith("/api/receipts/")) {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res);
      return;
    }
    await handleReceiptGet(pathname, res, {
      receiptStore: deps.receiptStore,
      logger: deps.logger,
    });
    return;
  }
  if (pathname.startsWith("/api/threads/") && pathname.endsWith("/receipts")) {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res);
      return;
    }
    await handleThreadReceiptsList(req, res, {
      receiptStore: deps.receiptStore,
      logger: deps.logger,
    });
    return;
  }
  // Cost-ledger routes under /api/v1/cost/* — mounted only when the host
  // supplied a `cost` block at `createBroker` time. When absent the path
  // falls through to the 404 catch-all below.
  if (deps.cost !== null && pathname.startsWith("/api/v1/cost/")) {
    const handled = await handleCostRoute(req, res, pathname, deps.cost);
    if (handled) return;
  }
  // Authenticated catch-all for unknown `/api/*` routes. Without this,
  // `POST /api/no-such-route` (with a valid bearer) would fall into the
  // static method gate below and return `405 Allow: GET, HEAD` — a false
  // contract for generated Go/Rust clients, since no GET resource exists
  // either. A 404 here also keeps the API/static surfaces semantically
  // distinct after the method-gate refactor that pushed enforcement down
  // to each route.
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    notFound(res);
    return;
  }
  // Static surfaces are open (loopback-guarded but no bearer): serving the
  // renderer bundle from `/` on first window load is a same-origin fetch
  // that has not yet learned the bootstrap token. Subsequent renderer
  // calls go to /api/* and DO carry the bearer (enforced above).
  //
  // Static serving accepts only GET/HEAD — any other method on a known
  // static path is a 405. Without this gate a POST to `/foo.js` would be
  // answered by serving the file (Node strips body writes for non-HEAD
  // but the semantic is wrong — clients should learn the resource doesn't
  // support their method). The 405 is scoped to *known* static paths
  // (`/`, `/index.html`, `/assets/*`); a POST to an unknown non-API path
  // like `/no-such-route` falls through to 404 instead of falsely
  // advertising "GET, HEAD" via Allow.
  const isKnownStaticPath =
    pathname === "/" || pathname === "/index.html" || pathname.startsWith("/assets/");
  if (isKnownStaticPath) {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res);
      return;
    }
    const handled = await deps.staticHandler.serve(pathname, res);
    if (handled) return;
  }

  notFound(res);
}

function handleApiToken(req: IncomingMessage, res: ServerResponse, deps: RouteDeps): void {
  // Bootstrap returns the bearer + broker URL. The Host header is already
  // validated by the loopback guard; we synthesize the broker_url from the
  // listener's bound port (taken from the request socket) so the response
  // is honest about where the broker is listening even when a forwarded
  // Host header tries to lie.
  const localAddress = req.socket.localAddress ?? LOOPBACK_HOST;
  const localPort = req.socket.localPort ?? 0;
  if (localPort === 0) {
    // The socket should always know its bound port for an active connection;
    // a 0 here means the socket is in an unexpected state. Fail loudly so
    // we never emit an `http://127.0.0.1:0` URL that the protocol codec
    // would (correctly) reject.
    deps.logger.error("listener_no_local_port");
    res.statusCode = 500;
    res.setHeader("Content-Type", "text/plain; charset=utf-8");
    res.end("no_local_port");
    return;
  }
  // asBrokerUrl validates the full shape (http: scheme, explicit non-default
  // port, loopback host) at the wire boundary. We just built the URL from
  // socket.localAddress + localPort which are both already validated; this
  // is belt-and-suspenders against future refactors that might produce a
  // malformed URL silently. Throwing here would 500 the caller via the
  // routeRequest catch in createBroker — preferable to emitting a wire
  // value the codec would reject on read.
  const brokerUrl = asBrokerUrl(`http://${normalizeLoopback(localAddress)}:${localPort}`);

  // Browser-hardening: if the request carries an Origin header (every
  // browser-context fetch does), it MUST match the broker's bound origin.
  // This blocks cross-origin fetches from same-machine browser contexts
  // (Chrome extensions, dev-tool consoles attached to other pages, third-
  // party local web apps that happen to be running). It does NOT block
  // same-machine curl/headless callers that omit Origin — see
  // docs/modules/security-model.md "Loopback Trust Model" for why that
  // residual surface is documented rather than gated here.
  // Sec-Fetch-Site is a parallel signal: when present, only same-origin or
  // "none" (user-initiated, e.g. typing the URL in the address bar) are
  // accepted. "cross-site"/"same-site"/"cross-origin" are rejected.
  const originGate = checkApiTokenOrigin(req, brokerUrl, deps.trustedOrigins);
  if (!originGate.allowed) {
    deps.logger.warn("api_token_origin_denied", {
      reason: originGate.reason,
    });
    forbidden(res, originGate.reason);
    return;
  }

  const bootstrap: ApiBootstrap = { token: deps.token, brokerUrl };
  const wire = apiBootstrapToJson(bootstrap);
  const body = JSON.stringify(wire);
  res.statusCode = 200;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.setHeader("Cache-Control", "no-store");
  // Hint future CORS layers (and any well-behaved HTTP cache) to vary by
  // origin. /api-token is loopback-only and cache-busted by `no-store`,
  // but the contract should be honest about origin-sensitivity from day 1
  // so a later branch adding dev-mode CORS does not have to retroactively
  // add this header to a route renderers may already be calling.
  res.setHeader("Vary", "Origin");
  res.setHeader("Content-Length", String(Buffer.byteLength(body, "utf8")));
  res.end(body);
}

function handleHealth(res: ServerResponse): void {
  const body = JSON.stringify({ ok: true });
  res.statusCode = 200;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.setHeader("Cache-Control", "no-store");
  res.setHeader("Content-Length", String(Buffer.byteLength(body, "utf8")));
  res.end(body);
}

function handleEvents(res: ServerResponse): void {
  startSseSession(res);
}

function authorize(
  req: IncomingMessage,
  res: ServerResponse,
  expected: ApiToken,
  logger: BrokerLogger,
  pathname: string,
): boolean {
  const presented = extractBearerFromHeader(headerString(req.headers.authorization));
  if (!tokenMatches(presented, expected)) {
    // Low-cardinality auth-reject so on-call can distinguish a stale
    // bearer (after broker restart or rotation) from a never-reached
    // route. `reason` carries the failure mode; we deliberately do NOT
    // log the raw path or the presented token. `routeFamily` is the
    // top-level /api segment only, so query strings, dynamic ids, and
    // long attacker payloads can't expand the log surface.
    logger.warn("api_auth_rejected", {
      reason: presented === null ? "missing_bearer" : "invalid_bearer",
      routeFamily: classifyApiRoute(pathname),
    });
    unauthorized(res);
    return false;
  }
  return true;
}

// Bucket an `/api/...` pathname into a fixed enum of known route families.
// Anything else (or shapes attackers could exploit to inflate logs) falls
// into `unknown`. Keeping cardinality bounded makes the log surface
// predictable for alerts. The exact namespace root `/api` is its own
// family — collapsing it into `health` would mislabel bearerless hits
// at the namespace boundary as if they were targeting the health probe.
function classifyApiRoute(pathname: string): string {
  if (pathname === "/api") return "api_root";
  if (pathname === "/api/health") return "health";
  if (pathname === "/api/events") return "events";
  if (pathname === "/api/receipts") return "receipts";
  if (pathname.startsWith("/api/receipts/")) return "receipt";
  if (pathname.startsWith("/api/threads/") && pathname.endsWith("/receipts")) {
    return "thread_receipts";
  }
  return "unknown";
}

async function listen(server: Server, port: number): Promise<number> {
  return await new Promise<number>((resolveFn, rejectFn) => {
    const onError = (err: Error): void => {
      server.off("listening", onListening);
      rejectFn(err);
    };
    const onListening = (): void => {
      server.off("error", onError);
      const address = server.address();
      const bound = isAddressInfo(address) ? address.port : null;
      if (bound === null) {
        rejectFn(new Error("listener: server.address() did not return an AddressInfo"));
        return;
      }
      resolveFn(bound);
    };
    server.once("error", onError);
    server.once("listening", onListening);
    server.listen(port, LOOPBACK_HOST);
  });
}

async function doStop(server: Server, wss: WebSocketServer, logger: BrokerLogger): Promise<void> {
  if (!server.listening) {
    logger.info("listener_stop_noop");
    return;
  }
  for (const ws of wss.clients) {
    ws.close(1001, "server_shutdown");
  }
  // Await BOTH wss.close and server.close. Without awaiting wss.close,
  // upgraded WebSocket sockets are not drained by server.closeAllConnections
  // and pending close-frame bytes can be dropped — the broker would report
  // a graceful stop before the kernel actually flushed the terminal bytes,
  // creating a confusing client-visible truncation once the agent bridge
  // lands.
  const wssClosed = new Promise<void>((res) => {
    wss.close(() => res());
  });
  const serverClosed = new Promise<void>((res) => {
    server.close(() => res());
  });
  server.closeAllConnections?.();
  await Promise.all([wssClosed, serverClosed]);
  logger.info("listener_stopped");
}

function isAddressInfo(value: unknown): value is AddressInfo {
  return typeof value === "object" && value !== null && "port" in value && "address" in value;
}

function headerString(value: string | string[] | undefined): string | undefined {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return undefined;
}

function parseRequestUrl(req: IncomingMessage): URL | null {
  if (typeof req.url !== "string") return null;
  const host = req.headers.host ?? LOOPBACK_HOST;
  try {
    return new URL(req.url, `http://${host}`);
  } catch {
    return null;
  }
}

function containsRawTraversalOrNul(rawUrl: string): boolean {
  if (rawUrl.includes("\0") || rawUrl.includes("%00")) return true;
  // Drop the query string before scanning for traversal segments — a
  // legitimate `?q=foo/../bar` query value should not 404. Path-only check.
  const pathOnly = rawUrl.split("?", 1)[0] ?? "";
  // Decode percent-encoding so `%2e%2e` is treated the same as `..`.
  // `decodeURIComponent` throws on malformed sequences; treat that as a
  // hostile request and reject.
  let decoded: string;
  try {
    decoded = decodeURIComponent(pathOnly);
  } catch {
    return true;
  }
  if (decoded.includes("\0")) return true;
  const segments = decoded.split("/");
  return segments.some((seg) => seg === "..");
}

function normalizeLoopback(address: string): string {
  // `socket.localAddress` reports `::ffff:127.0.0.1` for IPv4 connections on
  // dual-stack listeners. The broker URL must round-trip through the
  // protocol codec's loopback check, which only accepts bare `127.0.0.1`,
  // `::1`, or `localhost`. Strip the v4-mapped-v6 prefix.
  if (address.startsWith("::ffff:")) return address.slice(7);
  return address;
}

interface ApiTokenOriginDecision {
  readonly allowed: boolean;
  readonly reason: string;
}

// Defense-in-depth gate for browser-style cross-origin requests to
// `/api-token`. See the call site in `handleApiToken` for the policy
// rationale.
function checkApiTokenOrigin(
  req: IncomingMessage,
  brokerUrl: string,
  trustedOrigins: ReadonlySet<string>,
): ApiTokenOriginDecision {
  // Sec-Fetch-Site — sent by all modern browsers, omitted by curl/Node.
  const secFetchSite = headerString(req.headers["sec-fetch-site"]);
  if (secFetchSite !== undefined) {
    if (secFetchSite !== "same-origin" && secFetchSite !== "none") {
      return { allowed: false, reason: "cross_origin_api_token" };
    }
  }
  // Origin: must either be absent (curl/Electron WebView) or match the
  // broker's bound origin. The literal string `"null"` is a browser-
  // generated value from opaque origins (file://, sandboxed iframes,
  // data: / blob: contexts, srcdoc iframes) — reject it explicitly so a
  // malicious local file or sandboxed page cannot trigger the bootstrap.
  // The previous gate treated `Origin: null` as "no Origin", which let it
  // bypass the check entirely.
  const origin = headerString(req.headers.origin);
  if (typeof origin === "string" && origin.length > 0) {
    if (origin === "null") {
      return { allowed: false, reason: "null_origin" };
    }
    let originUrl: URL;
    try {
      originUrl = new URL(origin);
    } catch {
      return { allowed: false, reason: "malformed_origin" };
    }
    if (originUrl.origin !== new URL(brokerUrl).origin && !trustedOrigins.has(originUrl.origin)) {
      return { allowed: false, reason: "cross_origin_api_token" };
    }
  }
  return { allowed: true, reason: "ok" };
}

// Pre-validate each trusted origin at config time: must parse, must be a
// loopback origin (we never want to accept arbitrary trusted cross-origin
// callers), must contain no path/query/fragment. Invalid entries throw at
// broker-construction time rather than silently failing per-request.
function normalizeTrustedOrigins(values: readonly string[] | undefined): ReadonlySet<string> {
  if (values === undefined || values.length === 0) return new Set();
  const set = new Set<string>();
  for (const raw of values) {
    let parsed: URL;
    try {
      parsed = new URL(raw);
    } catch {
      throw new Error(`createBroker: trustedOrigins entry is not a URL: ${raw}`);
    }
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new Error(`createBroker: trustedOrigins entry must use http/https: ${raw}`);
    }
    if (
      parsed.hostname !== "127.0.0.1" &&
      parsed.hostname !== "localhost" &&
      parsed.hostname !== "::1" &&
      parsed.hostname !== "[::1]"
    ) {
      throw new Error(`createBroker: trustedOrigins entry must be loopback: ${raw}`);
    }
    // Compare against parsed.origin (URL.origin strips path/query/hash).
    set.add(parsed.origin);
  }
  return set;
}

function notFound(res: ServerResponse): void {
  res.statusCode = 404;
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end("not_found");
}

function unauthorized(res: ServerResponse): void {
  res.statusCode = 401;
  res.setHeader("WWW-Authenticate", 'Bearer realm="wuphf-broker"');
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end("unauthorized");
}

function forbidden(res: ServerResponse, reason: string): void {
  res.statusCode = 403;
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end(reason);
}

function badRequest(res: ServerResponse, reason: string): void {
  res.statusCode = 400;
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end(reason);
}

function methodNotAllowed(res: ServerResponse): void {
  res.statusCode = 405;
  res.setHeader("Allow", "GET, HEAD");
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end("method_not_allowed");
}

export type { BrokerHandle, BrokerPort };
