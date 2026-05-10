// DNS-rebinding guard. Both checks must pass:
//
//   1. Host header must be a loopback hostname (per
//      `@wuphf/protocol#isAllowedLoopbackHost`).
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

import { isAllowedLoopbackHost, isLoopbackRemoteAddress } from "@wuphf/protocol";

export interface GuardResult {
  readonly allowed: boolean;
  readonly reason?: "missing_host" | "bad_host" | "missing_peer" | "bad_peer";
}

export function checkLoopbackRequest(args: {
  readonly hostHeader: string | undefined;
  readonly remoteAddress: string | undefined;
}): GuardResult {
  const { hostHeader, remoteAddress } = args;
  if (typeof hostHeader !== "string" || hostHeader.length === 0) {
    return { allowed: false, reason: "missing_host" };
  }
  if (!isAllowedLoopbackHost(hostHeader)) {
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
