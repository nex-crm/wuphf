// Bearer-token authentication.
//
// Two extraction sites:
//   - `Authorization: Bearer <token>` header (HTTP, SSE).
//   - `?token=<token>` query parameter (WebSocket — browsers cannot set
//     custom Authorization headers on the WebSocket handshake; the v0
//     broker uses the same convention).
//
// Comparison is constant-time so a remote attacker timing the loopback
// listener (the only realistic threat for an attacker who has already
// reached `127.0.0.1`) cannot recover the token byte-by-byte.

import { timingSafeEqual } from "node:crypto";

import type { ApiToken } from "@wuphf/protocol";

const BEARER_PREFIX = "Bearer ";

export function extractBearerFromHeader(headerValue: string | undefined): string | null {
  if (typeof headerValue !== "string") return null;
  if (!headerValue.startsWith(BEARER_PREFIX)) return null;
  const token = headerValue.slice(BEARER_PREFIX.length).trim();
  return token.length === 0 ? null : token;
}

export function tokenMatches(presented: string | null, expected: ApiToken): boolean {
  if (presented === null) return false;
  const a = Buffer.from(presented, "utf8");
  const b = Buffer.from(expected, "utf8");
  if (a.length !== b.length) return false;
  return timingSafeEqual(a, b);
}
