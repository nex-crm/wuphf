// WebSocket terminal upgrade. Branch-4 scope: gate the upgrade on
// origin + token + slug, accept the handshake, then close with
// `1011 not_implemented`. Subsequent branches add the agent stdio bridge
// over the same path.
//
// Path shape: `/terminal/agents/:slug?token=<token>`. Slug must match
// protocol `isAgentSlug` (single segment, length-bounded, lowercase URL-
// safe alphabet). Tokens MUST NEVER appear in log payloads — the upgrade
// logger redacts via `redactedPath`.
//
// Origin policy: accept absent origins (Electron WebView + Node clients) and
// loopback origins. Browsers attached over a non-loopback page would carry
// a remote origin; the broker rejects those.

import type { IncomingMessage, Server } from "node:http";
import type { Duplex } from "node:stream";
import { type ApiToken, isAgentSlug, isAllowedLoopbackHost } from "@wuphf/protocol";
import type { WebSocketServer } from "ws";

import { tokenMatches } from "./auth.ts";
import { checkLoopbackRequest } from "./dns-rebinding-guard.ts";
import type { BrokerLogger } from "./types.ts";

const TERMINAL_PATH_PREFIX = "/terminal/agents/";

export interface TerminalUpgradeDeps {
  readonly wss: WebSocketServer;
  readonly token: ApiToken;
  readonly logger: BrokerLogger;
}

export function attachTerminalUpgrade(server: Server, deps: TerminalUpgradeDeps): void {
  server.on("upgrade", (req, socket, head) => {
    if (!isTerminalRequest(req)) {
      // Not our path — refuse the upgrade rather than letting node hang the
      // socket. A 404 over the upgrade socket reads "this URL has no upgrade
      // handler" which is correct.
      rejectUpgrade(socket, 404, "not_found");
      return;
    }
    const decision = decideTerminalUpgrade(req, deps.token);
    if (decision.kind === "reject") {
      // Log the path with `?token=...` redacted. Logging req.url verbatim
      // would deposit the bearer in every reject/accept log payload, where
      // any future log-forwarder (we just added one in the supervisor)
      // could surface it on disk or in telemetry.
      deps.logger.warn("ws_upgrade_rejected", {
        reason: decision.reason,
        path: redactedPath(req.url),
      });
      rejectUpgrade(socket, decision.status, decision.reason);
      return;
    }
    deps.wss.handleUpgrade(req, socket, head, (ws) => {
      // Branch-4: the agent stdio bridge does not exist yet. Accept the
      // upgrade so the renderer learns the gate let it through, then close
      // with 1011 ("server error / not implemented") so callers do not
      // wait on a stream that will never speak. Subsequent branches replace
      // this with the real agent bridge.
      deps.logger.info("ws_upgrade_accepted_not_implemented", {
        path: redactedPath(req.url),
      });
      ws.close(1011, "not_implemented");
    });
  });
}

interface AcceptDecision {
  readonly kind: "accept";
}

interface RejectDecision {
  readonly kind: "reject";
  readonly status: 400 | 401 | 403 | 404;
  readonly reason: string;
}

type UpgradeDecision = AcceptDecision | RejectDecision;

export function decideTerminalUpgrade(req: IncomingMessage, token: ApiToken): UpgradeDecision {
  if (!isTerminalRequest(req)) {
    return { kind: "reject", status: 404, reason: "not_found" };
  }
  const guard = checkLoopbackRequest({
    hostHeader: req.headers.host,
    remoteAddress: req.socket.remoteAddress ?? undefined,
  });
  if (!guard.allowed) {
    return { kind: "reject", status: 403, reason: `loopback_${guard.reason ?? "denied"}` };
  }
  const origin = req.headers.origin;
  if (typeof origin === "string" && origin.length > 0) {
    let parsed: URL;
    try {
      parsed = new URL(origin);
    } catch {
      return { kind: "reject", status: 403, reason: "bad_origin" };
    }
    // Require http(s) AND an explicit port. `new URL("https://127.0.0.1")`
    // strips the default port; allowing port-less origins lets a malicious
    // local listener on https://127.0.0.1/ pass the host check. The
    // renderer is loaded from `http://127.0.0.1:<port>/` (explicit port),
    // so this constraint is consistent with the only legitimate caller.
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return { kind: "reject", status: 403, reason: "bad_origin_scheme" };
    }
    if (parsed.port === "") {
      return { kind: "reject", status: 403, reason: "non_loopback_origin" };
    }
    if (!isAllowedLoopbackHost(parsed.host)) {
      return { kind: "reject", status: 403, reason: "non_loopback_origin" };
    }
  }
  const url = parseRequestUrl(req);
  if (url === null) {
    return { kind: "reject", status: 400, reason: "bad_url" };
  }
  // Validate the slug segment via protocol's brand check. Without this,
  // anything starting with `/terminal/agents/` would reach the upgrade
  // handshake — including `/terminal/agents/foo/bar` (multi-segment) and
  // percent-encoded edge cases. Slug must be exactly one decoded segment
  // matching protocol's lowercase URL-safe alphabet.
  const slugSegment = url.pathname.slice(TERMINAL_PATH_PREFIX.length);
  if (slugSegment.length === 0 || slugSegment.includes("/")) {
    return { kind: "reject", status: 404, reason: "bad_agent_slug" };
  }
  let decodedSlug: string;
  try {
    decodedSlug = decodeURIComponent(slugSegment);
  } catch {
    return { kind: "reject", status: 404, reason: "bad_agent_slug" };
  }
  if (!isAgentSlug(decodedSlug)) {
    return { kind: "reject", status: 404, reason: "bad_agent_slug" };
  }
  const presented = url.searchParams.get("token");
  if (!tokenMatches(presented, token)) {
    return { kind: "reject", status: 401, reason: "bad_token" };
  }
  return { kind: "accept" };
}

function isTerminalRequest(req: IncomingMessage): boolean {
  if (typeof req.url !== "string") return false;
  // Strip query string before matching the prefix.
  const queryIdx = req.url.indexOf("?");
  const path = queryIdx >= 0 ? req.url.slice(0, queryIdx) : req.url;
  return path.startsWith(TERMINAL_PATH_PREFIX) && path.length > TERMINAL_PATH_PREFIX.length;
}

function parseRequestUrl(req: IncomingMessage): URL | null {
  if (typeof req.url !== "string") return null;
  // The `Host` header has already been validated as loopback by the guard
  // upstream; using it as the URL base preserves the query string for
  // searchParams parsing without needing a separate parser.
  const host = req.headers.host ?? "127.0.0.1";
  try {
    return new URL(req.url, `http://${host}`);
  } catch {
    return null;
  }
}

function rejectUpgrade(socket: Duplex, status: number, reason: string): void {
  const body = reason;
  const payload =
    `HTTP/1.1 ${status} ${statusText(status)}\r\n` +
    `Content-Type: text/plain; charset=utf-8\r\n` +
    `Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n` +
    `Connection: close\r\n` +
    "\r\n" +
    body;
  socket.write(payload);
  socket.destroy();
}

// Redact `token` from the query string portion of a request URL before
// logging. Case-insensitive on the parameter name so `?Token=...` and
// `?TOKEN=...` are also redacted — a raw HTTP client can put the bearer
// under any case variant and the WebSocket auth's case-sensitive `token`
// read won't validate them, but the rejected request still gets logged
// and the value would leak. Any `#fragment` portion is stripped entirely
// from the logged path since some clients smuggle tokens there. Returns
// null for missing/non-string urls so the log payload preserves the
// original "no path known" signal.
function redactedPath(rawUrl: string | undefined): string | null {
  if (typeof rawUrl !== "string") return null;
  // Strip fragment first so any `#token=...` never reaches the log.
  const hashIdx = rawUrl.indexOf("#");
  const beforeHash = hashIdx >= 0 ? rawUrl.slice(0, hashIdx) : rawUrl;
  const queryIdx = beforeHash.indexOf("?");
  if (queryIdx < 0) return beforeHash;
  const path = beforeHash.slice(0, queryIdx);
  const params = new URLSearchParams(beforeHash.slice(queryIdx + 1));
  // Rebuild the query case-insensitively: any key whose lowercase form
  // is "token" has its value replaced with "redacted". Other params keep
  // their original casing and order.
  const rebuilt = new URLSearchParams();
  for (const [key, value] of params.entries()) {
    rebuilt.append(key, key.toLowerCase() === "token" ? "redacted" : value);
  }
  const redactedQuery = rebuilt.toString();
  return redactedQuery.length > 0 ? `${path}?${redactedQuery}` : path;
}

function statusText(status: number): string {
  switch (status) {
    case 400:
      return "Bad Request";
    case 401:
      return "Unauthorized";
    case 403:
      return "Forbidden";
    case 404:
      return "Not Found";
    default:
      return "Error";
  }
}
