// Internal helpers for `broker.ts` — pure functions for parsing the broker
// subprocess IPC grammar (ready/alive/broker_log), filtering log payloads
// to the safe-key allowlist, and small error-shape coercions.
//
// These are factored out for direct unit testing in
// `tests/broker-helpers.spec.ts`; production code imports them through
// `broker.ts` (which re-exports nothing — these are internal). Treat any
// import of this file from outside `apps/desktop/src/main/broker*.ts` or
// `apps/desktop/tests/` as a refactor breakage, not an API.
//
// @internal — not part of the @wuphf/desktop package's stable surface.
// A future change that promotes these to a published contract (e.g. for
// a non-TS broker implementation) must move them to @wuphf/protocol with
// versioning + conformance fixtures, not just re-export them here.

import { type BrokerUrl, isBrokerUrl } from "@wuphf/protocol";

import { isSafePayloadKey, type LogPayloadValue } from "./logger.ts";

export type BrokerSubLogLevel = "info" | "warn" | "error";

export interface ReadyMessage {
  readonly brokerUrl: BrokerUrl;
}

export interface BrokerLogPayload {
  readonly broker_log: BrokerSubLogLevel;
  readonly event: string;
  readonly payload: Readonly<Record<string, unknown>> | undefined;
}

/**
 * Recognize the broker entry's `{ ready, brokerUrl }` handshake. Validates
 * the URL via the protocol's `isBrokerUrl` brand check — the subprocess is
 * trusted (same machine, our own code), but a malformed message from a
 * future broker version (or a misbehaving fake in tests) should be
 * rejected at the IPC boundary rather than handed downstream as a
 * "string" the supervisor later trusts as a fetch origin.
 */
export function readReadyMessage(message: unknown): ReadyMessage | null {
  if (typeof message !== "object" || message === null) return null;
  if (!Object.hasOwn(message, "ready") || !Object.hasOwn(message, "brokerUrl")) return null;
  const record = message as { readonly ready?: unknown; readonly brokerUrl?: unknown };
  if (record.ready !== true) return null;
  // Snapshot the value to a local before validation so an accessor or
  // mutating Proxy can't return a different value on the second read.
  // utilityProcess uses structured clone in practice (plain objects),
  // making this defense theoretical — but the cost is one local variable
  // and the invariant "validated == returned" matters for any future
  // caller that hands ReadyMessage a non-cloned source.
  const brokerUrl = record.brokerUrl;
  if (!isBrokerUrl(brokerUrl)) return null;
  return { brokerUrl };
}

/**
 * Recognize the broker subprocess's structured-log message:
 *   { broker_log: "info"|"warn"|"error", event: string, payload?: object }
 *
 * Anything else is foreign and returns null so the message handler can
 * fall through to its no-op. The payload MUST be an object or absent —
 * non-object payloads (string, number, array, boolean) are rejected
 * here so the IPC grammar stays map-shaped. Downstream
 * `filterPayloadToSafeKeys` then validates each key against the
 * desktop logger's allowlist and counts dropped keys for observability.
 */
export function readBrokerLogMessage(message: unknown): BrokerLogPayload | null {
  if (typeof message !== "object" || message === null) return null;
  if (!Object.hasOwn(message, "broker_log")) return null;
  const record = message as { broker_log?: unknown; event?: unknown; payload?: unknown };
  if (
    record.broker_log !== "info" &&
    record.broker_log !== "warn" &&
    record.broker_log !== "error"
  ) {
    return null;
  }
  if (typeof record.event !== "string") return null;
  // Payload must be a plain object or absent. Reject Array (which is
  // typeof "object"), null, and every scalar so the codec contract
  // matches the documented `payload?: object` shape.
  let payload: Readonly<Record<string, unknown>> | undefined;
  if (record.payload === undefined) {
    payload = undefined;
  } else if (
    typeof record.payload === "object" &&
    record.payload !== null &&
    !Array.isArray(record.payload)
  ) {
    payload = record.payload as Readonly<Record<string, unknown>>;
  } else {
    return null;
  }
  return { broker_log: record.broker_log, event: record.event, payload };
}

// Mirror logger.ts's LOG_NAME_PATTERN. The forwarder rejects subprocess
// event names that wouldn't pass the assertLogName check downstream —
// without this pre-check, every broker_* log would attempt and silently
// fail at the logger boundary.
const LOG_NAME_PATTERN = /^[a-z0-9_.:-]+$/;

export function sanitizeBrokerEventName(event: string): string | null {
  return LOG_NAME_PATTERN.test(event) ? event : null;
}

export function filterPayloadToSafeKeys(payload: unknown): {
  readonly safePayload: Record<string, LogPayloadValue>;
  readonly droppedKeyCount: number;
} {
  const safe: Record<string, LogPayloadValue> = {};
  let droppedKeyCount = 0;
  if (typeof payload !== "object" || payload === null) {
    return { safePayload: safe, droppedKeyCount: 0 };
  }
  for (const [key, value] of Object.entries(payload)) {
    if (!isSafePayloadKey(key)) {
      droppedKeyCount += 1;
      continue;
    }
    if (
      value === null ||
      typeof value === "string" ||
      typeof value === "number" ||
      typeof value === "boolean"
    ) {
      safe[key] = value;
    } else {
      droppedKeyCount += 1;
    }
  }
  return { safePayload: safe, droppedKeyCount };
}

// Parse the bound port out of the broker URL for safe logging. Returns
// null on malformed input so the logger still records a structured event
// even when the subprocess hands back something unparseable.
export function brokerUrlPort(url: string): number | null {
  try {
    const parsed = new URL(url);
    const port = Number(parsed.port);
    return Number.isFinite(port) && port > 0 ? port : null;
  } catch {
    return null;
  }
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function errorCode(error: unknown): string | null {
  if (typeof error !== "object" || error === null || !Object.hasOwn(error, "code")) {
    return null;
  }

  const code = (error as { readonly code?: unknown }).code;
  return typeof code === "string" ? code : null;
}
