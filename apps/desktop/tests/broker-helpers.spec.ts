// Pure-function helpers extracted from broker.ts. Tested here directly
// rather than through the BrokerSupervisor event loop because:
//   - the defensive branches reject inputs that the supervisor's own
//     message validation already filters out (so they're unreachable
//     from the integration suite), AND
//   - each branch maps cleanly to one targeted assertion — the indirect
//     pattern would need ~20 supervisor setups for ~20 trivial tests.

import { describe, expect, it } from "vitest";

import {
  brokerUrlPort,
  errorCode,
  errorMessage,
  filterPayloadToSafeKeys,
  readBrokerLogMessage,
  readReadyMessage,
  sanitizeBrokerEventName,
} from "../src/main/broker.ts";

describe("readReadyMessage", () => {
  it("returns null for non-object inputs", () => {
    expect(readReadyMessage(null)).toBeNull();
    expect(readReadyMessage(undefined)).toBeNull();
    expect(readReadyMessage("ready")).toBeNull();
    expect(readReadyMessage(42)).toBeNull();
  });

  it("returns null when ready or brokerUrl keys are missing", () => {
    expect(readReadyMessage({ ready: true })).toBeNull();
    expect(readReadyMessage({ brokerUrl: "http://127.0.0.1:5000" })).toBeNull();
    expect(readReadyMessage({})).toBeNull();
  });

  it("returns null when ready is not strictly true", () => {
    expect(readReadyMessage({ ready: 1, brokerUrl: "http://127.0.0.1:5000" })).toBeNull();
    expect(readReadyMessage({ ready: "true", brokerUrl: "http://127.0.0.1:5000" })).toBeNull();
    expect(readReadyMessage({ ready: false, brokerUrl: "http://127.0.0.1:5000" })).toBeNull();
  });

  it("returns null when brokerUrl is not a valid BrokerUrl", () => {
    expect(readReadyMessage({ ready: true, brokerUrl: 42 })).toBeNull();
    expect(readReadyMessage({ ready: true, brokerUrl: "not a url" })).toBeNull();
    // Wrong host (non-loopback) is rejected by the brand.
    expect(readReadyMessage({ ready: true, brokerUrl: "http://example.com:5000" })).toBeNull();
  });

  it("returns the branded brokerUrl on a valid {ready,brokerUrl} message", () => {
    const result = readReadyMessage({ ready: true, brokerUrl: "http://127.0.0.1:5000" });
    expect(result).not.toBeNull();
    expect(result?.brokerUrl).toBe("http://127.0.0.1:5000");
  });
});

describe("readBrokerLogMessage", () => {
  it("returns null for non-object inputs", () => {
    expect(readBrokerLogMessage(null)).toBeNull();
    expect(readBrokerLogMessage("broker_log")).toBeNull();
    expect(readBrokerLogMessage(undefined)).toBeNull();
  });

  it("returns null when broker_log key is missing", () => {
    expect(readBrokerLogMessage({ event: "ok", payload: {} })).toBeNull();
  });

  it("returns null when broker_log is not one of the allowed levels", () => {
    expect(readBrokerLogMessage({ broker_log: "debug", event: "ok" })).toBeNull();
    expect(readBrokerLogMessage({ broker_log: 1, event: "ok" })).toBeNull();
    expect(readBrokerLogMessage({ broker_log: "", event: "ok" })).toBeNull();
  });

  it("returns null when event is not a string", () => {
    expect(readBrokerLogMessage({ broker_log: "info", event: 42 })).toBeNull();
    expect(readBrokerLogMessage({ broker_log: "info" })).toBeNull();
  });

  it("returns the parsed shape for each allowed level", () => {
    for (const level of ["info", "warn", "error"] as const) {
      const parsed = readBrokerLogMessage({ broker_log: level, event: "ok", payload: { k: 1 } });
      expect(parsed).toEqual({ broker_log: level, event: "ok", payload: { k: 1 } });
    }
  });

  it("passes payload through unchanged when absent or non-object", () => {
    const noPayload = readBrokerLogMessage({ broker_log: "info", event: "ok" });
    expect(noPayload?.payload).toBeUndefined();
    const stringPayload = readBrokerLogMessage({
      broker_log: "warn",
      event: "ok",
      payload: "not-an-object",
    });
    // Forwarder will reject string payloads downstream via
    // filterPayloadToSafeKeys; this codec doesn't pre-filter.
    expect(stringPayload?.payload).toBe("not-an-object");
  });
});

describe("sanitizeBrokerEventName", () => {
  it("returns the input when it matches LOG_NAME_PATTERN", () => {
    expect(sanitizeBrokerEventName("broker_listener_started")).toBe("broker_listener_started");
    expect(sanitizeBrokerEventName("a.b:c-d_e")).toBe("a.b:c-d_e");
    expect(sanitizeBrokerEventName("9digits")).toBe("9digits");
  });

  it("returns null when the input contains forbidden characters", () => {
    expect(sanitizeBrokerEventName("Has Spaces")).toBeNull();
    expect(sanitizeBrokerEventName("UPPER")).toBeNull();
    expect(sanitizeBrokerEventName("with/slash")).toBeNull();
    expect(sanitizeBrokerEventName("")).toBeNull();
  });
});

describe("filterPayloadToSafeKeys", () => {
  it("returns an empty bag for non-object payloads", () => {
    expect(filterPayloadToSafeKeys(null)).toEqual({ safePayload: {}, droppedKeyCount: 0 });
    expect(filterPayloadToSafeKeys(undefined)).toEqual({ safePayload: {}, droppedKeyCount: 0 });
    expect(filterPayloadToSafeKeys("string")).toEqual({ safePayload: {}, droppedKeyCount: 0 });
    expect(filterPayloadToSafeKeys(42)).toEqual({ safePayload: {}, droppedKeyCount: 0 });
  });

  it("keeps null, string, number, and boolean values under safe keys", () => {
    const result = filterPayloadToSafeKeys({
      pid: 1234,
      restartCount: 0,
      // Boolean value under safe key.
      force: true,
      // Null value under safe key (forwarder allows null explicitly).
      lastPingAt: null,
      // String value under safe key.
      reason: "stopping",
    });
    expect(result.droppedKeyCount).toBe(0);
    expect(result.safePayload).toEqual({
      pid: 1234,
      restartCount: 0,
      force: true,
      lastPingAt: null,
      reason: "stopping",
    });
  });

  it("counts dropped keys for banned key names without leaking their values", () => {
    const result = filterPayloadToSafeKeys({
      port: 7891,
      url: "http://leaked",
      token: "leaked-token",
      path: "/leaked",
    });
    expect(result.safePayload).toEqual({ port: 7891 });
    expect(result.droppedKeyCount).toBe(3);
  });

  it("counts dropped keys for non-scalar values under safe keys", () => {
    // `error`, `reason`, `pid` are in the SAFE_PAYLOAD_KEYS allowlist — but
    // a non-scalar value under a safe key must still be dropped (the value-
    // type check is the second gate after the key allowlist).
    const result = filterPayloadToSafeKeys({
      pid: 1,
      // Safe keys with non-scalar values — these hit the else arm of the
      // value-type if/else, NOT the banned-key continue branch.
      error: { nested: "object" },
      reason: [1, 2],
      restartCount: undefined,
      stack: Symbol("oops"),
    });
    expect(result.safePayload).toEqual({ pid: 1 });
    expect(result.droppedKeyCount).toBe(4);
  });
});

describe("brokerUrlPort", () => {
  it("returns the parsed port for a well-formed loopback URL", () => {
    expect(brokerUrlPort("http://127.0.0.1:5000")).toBe(5000);
    expect(brokerUrlPort("http://127.0.0.1:65535")).toBe(65535);
  });

  it("returns null for an unparseable URL", () => {
    expect(brokerUrlPort("not a url")).toBeNull();
    expect(brokerUrlPort("")).toBeNull();
    expect(brokerUrlPort("http://")).toBeNull();
  });

  it("returns null when the URL parses but no port is present", () => {
    // No port → URL.port === "" → Number("") === 0 → not positive → null.
    expect(brokerUrlPort("http://127.0.0.1")).toBeNull();
  });
});

describe("errorMessage", () => {
  it("returns the message property when input is an Error", () => {
    expect(errorMessage(new Error("boom"))).toBe("boom");
  });

  it("returns String(input) for non-Error values", () => {
    expect(errorMessage("string-error")).toBe("string-error");
    expect(errorMessage(42)).toBe("42");
    expect(errorMessage(null)).toBe("null");
    expect(errorMessage(undefined)).toBe("undefined");
  });
});

describe("errorCode", () => {
  it("returns the string code when present on an error-like object", () => {
    expect(errorCode({ code: "EACCES", message: "denied" })).toBe("EACCES");
    expect(errorCode(Object.assign(new Error("x"), { code: "ENOENT" }))).toBe("ENOENT");
  });

  it("returns null when the input is not an object or is missing the code key", () => {
    expect(errorCode(null)).toBeNull();
    expect(errorCode(undefined)).toBeNull();
    expect(errorCode("string")).toBeNull();
    expect(errorCode(42)).toBeNull();
    expect(errorCode({})).toBeNull();
    expect(errorCode(new Error("no code"))).toBeNull();
  });

  it("returns null when the code property exists but is not a string", () => {
    expect(errorCode({ code: 1 })).toBeNull();
    expect(errorCode({ code: null })).toBeNull();
    expect(errorCode({ code: { nested: true } })).toBeNull();
  });
});
