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

import { isSafePayloadKey, LOG_NAME_PATTERN, type LogPayloadValue } from "./logger.ts";

export type BrokerSubLogLevel = "info" | "warn" | "error";

export interface ReadyMessage {
  readonly kind: "ok";
  readonly brokerUrl: BrokerUrl;
}

export type BrokerReadyInvalidReason =
  | "fragment_present"
  | "invalid_port"
  | "missing_port"
  | "non_canonical_origin"
  | "non_data_property"
  | "non_http_protocol"
  | "non_loopback_host"
  | "non_string_url"
  | "query_present"
  | "unparseable_url"
  | "userinfo_present";

export interface InvalidReadyMessage {
  readonly kind: "invalid";
  readonly reason: BrokerReadyInvalidReason;
}

export type ReadReadyMessageResult = InvalidReadyMessage | ReadyMessage;

export interface BrokerLogPayload {
  readonly broker_log: BrokerSubLogLevel;
  readonly event: string;
  readonly payload: Readonly<Record<string, unknown>> | undefined;
}

const DATA_PROPERTY_ACCESSOR = Symbol("data_property_accessor");
const DATA_PROPERTY_MISSING = Symbol("data_property_missing");

/**
 * Recognize the broker entry's `{ ready, brokerUrl }` handshake. Valid
 * messages return the protocol-branded URL; ready-shaped invalid URLs return
 * a shape-only diagnostic reason so the supervisor can log wire drift without
 * leaking the rejected URL.
 */
export function readReadyMessage(message: unknown): ReadReadyMessageResult | null {
  if (typeof message !== "object" || message === null) return null;
  const ready = readOwnDataProperty(message, "ready");
  const brokerUrl = readOwnDataProperty(message, "brokerUrl");
  if (ready === DATA_PROPERTY_MISSING || brokerUrl === DATA_PROPERTY_MISSING) return null;
  if (ready !== true) return null;
  if (brokerUrl === DATA_PROPERTY_ACCESSOR) {
    return { kind: "invalid", reason: "non_data_property" };
  }
  if (!isBrokerUrl(brokerUrl)) {
    return { kind: "invalid", reason: classifyBrokerUrlRejection(brokerUrl) };
  }
  return { kind: "ok", brokerUrl };
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
  const brokerLog = readOwnDataProperty(message, "broker_log");
  if (brokerLog !== "info" && brokerLog !== "warn" && brokerLog !== "error") {
    return null;
  }
  const event = readOwnDataProperty(message, "event");
  if (typeof event !== "string") return null;
  // Payload must be a plain object or absent. Reject Array (which is
  // typeof "object"), null, and every scalar so the codec contract
  // matches the documented `payload?: object` shape.
  let payload: Readonly<Record<string, unknown>> | undefined;
  const payloadValue = readOwnDataProperty(message, "payload");
  if (payloadValue === DATA_PROPERTY_MISSING) {
    payload = undefined;
  } else if (
    typeof payloadValue === "object" &&
    payloadValue !== null &&
    !Array.isArray(payloadValue)
  ) {
    payload = payloadValue as Readonly<Record<string, unknown>>;
  } else {
    return null;
  }
  return { broker_log: brokerLog, event, payload };
}

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
  for (const key in payload) {
    const value = readOwnDataProperty(payload, key);
    if (value === DATA_PROPERTY_MISSING) continue;
    if (value === DATA_PROPERTY_ACCESSOR) {
      droppedKeyCount += 1;
      continue;
    }
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

function readOwnDataProperty(
  record: object,
  key: string,
): unknown | typeof DATA_PROPERTY_ACCESSOR | typeof DATA_PROPERTY_MISSING {
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined) return DATA_PROPERTY_MISSING;
  if (!("value" in descriptor)) return DATA_PROPERTY_ACCESSOR;
  return descriptor.value;
}

function classifyBrokerUrlRejection(value: unknown): BrokerReadyInvalidReason {
  if (typeof value !== "string") return "non_string_url";

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return "unparseable_url";
  }

  if (parsed.protocol !== "http:") return "non_http_protocol";
  if (parsed.port === "") return "missing_port";
  if (!isReadyBrokerLoopbackHost(parsed.hostname)) return "non_loopback_host";
  const port = Number(parsed.port);
  if (port < 1) return "invalid_port";
  if (parsed.username !== "" || parsed.password !== "") return "userinfo_present";
  if (parsed.search !== "") return "query_present";
  if (parsed.hash !== "") return "fragment_present";
  return "non_canonical_origin";
}

function isReadyBrokerLoopbackHost(hostname: string): boolean {
  return hostname === "127.0.0.1" || hostname === "localhost" || hostname === "[::1]";
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
