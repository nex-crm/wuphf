// DNS-rebinding guard. Both checks must pass:
//
//   1. Host header must be exactly one of the broker-supported loopback
//      hostnames (`127.0.0.1` or `localhost`, with an optional port).
//   2. Peer IP must be loopback (per `@wuphf/protocol#isLoopbackRemoteAddress`).
//
// Either alone is insufficient. A browser tricked via DNS rebinding can send
// `Host: 127.0.0.1` to the broker even when the request originated from a
// non-loopback peer. A misconfigured listener bound to `0.0.0.0` would defeat
// the host check. Gate on both.
//
// Spec: docs/architecture/broker-contract.md ("DNS-rebinding guard for the
// web UI listener") and the v0 implementation at
// internal/team/broker_middleware.go (`webUIRebindGuard`). v1 reuses the same
// discipline through this guard.

import { isLoopbackRemoteAddress } from "@wuphf/protocol";

export interface GuardResult {
  readonly allowed: boolean;
  readonly reason?: "missing_host" | "bad_host" | "missing_peer" | "bad_peer";
}

export type AllowedBrokerHost = "127.0.0.1" | "localhost";

const HOST_HEADER_RE = /^(127\.0\.0\.1|localhost)(?::(\d+))?$/i;

export function checkLoopbackRequest(args: {
  readonly hostHeader: string | undefined;
  readonly remoteAddress: string | undefined;
}): GuardResult {
  const { hostHeader, remoteAddress } = args;
  if (typeof hostHeader !== "string" || hostHeader.length === 0) {
    return { allowed: false, reason: "missing_host" };
  }
  if (parseAllowedBrokerHost(hostHeader) === null) {
    return { allowed: false, reason: "bad_host" };
  }
  if (typeof remoteAddress !== "string" || remoteAddress.length === 0) {
    return { allowed: false, reason: "missing_peer" };
  }
  if (!isLoopbackRemoteAddress(remoteAddress)) {
    return { allowed: false, reason: "bad_peer" };
  }
  return { allowed: true };
}

export function parseAllowedBrokerHost(hostHeader: string): AllowedBrokerHost | null {
  const match = HOST_HEADER_RE.exec(hostHeader);
  if (match === null) return null;

  const port = match[2];
  if (port !== undefined && !isValidPort(port)) return null;

  const host = match[1]?.toLowerCase();
  if (host === "127.0.0.1" || host === "localhost") return host;
  return null;
}

function isValidPort(port: string): boolean {
  const parsed = Number(port);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 65535;
}
