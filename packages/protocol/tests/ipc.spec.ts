import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  ALLOWED_LOOPBACK_HOSTS,
  type ApprovalSubmitResponse,
  asApiToken,
  asBrokerPort,
  asKeychainHandleId,
  asRequestId,
  isAllowedLoopbackHost,
  isApiToken,
  isBrokerPort,
  isKeychainHandleId,
  isLoopbackRemoteAddress,
  isRequestId,
  validateApprovalSubmitRequest,
} from "../src/ipc.ts";
import {
  asIdempotencyKey,
  asReceiptId,
  type ReceiptId,
  type SignedApprovalToken,
} from "../src/receipt.ts";
import { sha256Hex } from "../src/sha256.ts";

describe("isAllowedLoopbackHost", () => {
  it("accepts canonical loopback hosts", () => {
    for (const h of ALLOWED_LOOPBACK_HOSTS) {
      expect(isAllowedLoopbackHost(h)).toBe(true);
    }
  });

  it("accepts loopback host with valid port", () => {
    expect(isAllowedLoopbackHost("127.0.0.1:8080")).toBe(true);
    expect(isAllowedLoopbackHost("localhost:3000")).toBe(true);
    expect(isAllowedLoopbackHost("Localhost:3000")).toBe(true); // case-insensitive
    expect(isAllowedLoopbackHost("[::1]")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:8080")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:0")).toBe(true);
    expect(isAllowedLoopbackHost("[::1]:65535")).toBe(true);
  });

  it("rejects rebound hosts", () => {
    expect(isAllowedLoopbackHost("evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1.evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("0.0.0.0")).toBe(false);
    expect(isAllowedLoopbackHost("localhost.evil.com")).toBe(false);
    expect(isAllowedLoopbackHost("169.254.169.254")).toBe(false);
    expect(isAllowedLoopbackHost("10.0.0.1")).toBe(false);
    expect(isAllowedLoopbackHost("192.168.1.1")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.2")).toBe(false); // not 127.0.0.1
  });

  it("rejects malformed loopback-looking hosts", () => {
    expect(isAllowedLoopbackHost("[::1]junk")).toBe(false);
    expect(isAllowedLoopbackHost("::1:8080")).toBe(false); // unbracketed IPv6+port
    expect(isAllowedLoopbackHost("0:0:0:0:0:0:0:1")).toBe(false); // expanded IPv6
    expect(isAllowedLoopbackHost("localhost:abc")).toBe(false);
    expect(isAllowedLoopbackHost("localhost:")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1:")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1:abc")).toBe(false);
    expect(isAllowedLoopbackHost("127.0.0.1:65536")).toBe(false); // port out of range
    expect(isAllowedLoopbackHost("127.0.0.1:99999")).toBe(false);
    expect(isAllowedLoopbackHost("[localhost]")).toBe(false);
    expect(isAllowedLoopbackHost("[127.0.0.1]:8080")).toBe(false);
    expect(isAllowedLoopbackHost("[]")).toBe(false);
    expect(isAllowedLoopbackHost("localhost, evil.com")).toBe(false);
  });

  it("rejects empty and obviously malformed hosts", () => {
    expect(isAllowedLoopbackHost("")).toBe(false);
    expect(isAllowedLoopbackHost(":")).toBe(false);
    expect(isAllowedLoopbackHost(" 127.0.0.1")).toBe(false);
  });

  it("property: any non-allowlisted host is rejected", () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1, maxLength: 32 }).filter((s) => {
          const lower = s.toLowerCase();
          return (
            !lower.includes("127.0.0.1") &&
            !lower.includes("localhost") &&
            !lower.includes("::1") &&
            !lower.includes(":")
          );
        }),
        (host) => isAllowedLoopbackHost(host) === false,
      ),
      { numRuns: 500 },
    );
  });
});

describe("isLoopbackRemoteAddress", () => {
  it("accepts ::1 and 127.0.0.0/8", () => {
    expect(isLoopbackRemoteAddress("::1")).toBe(true);
    expect(isLoopbackRemoteAddress("127.0.0.1")).toBe(true);
    expect(isLoopbackRemoteAddress("127.255.255.255")).toBe(true);
    expect(isLoopbackRemoteAddress("127.1.2.3")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.0.0.1")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.42.42.42")).toBe(true);
  });

  it("rejects non-loopback addresses", () => {
    expect(isLoopbackRemoteAddress("0.0.0.0")).toBe(false);
    expect(isLoopbackRemoteAddress("128.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("10.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("192.168.1.1")).toBe(false);
    expect(isLoopbackRemoteAddress("169.254.169.254")).toBe(false);
    expect(isLoopbackRemoteAddress("::ffff:192.168.1.1")).toBe(false);
    expect(isLoopbackRemoteAddress("fe80::1")).toBe(false);
    expect(isLoopbackRemoteAddress("")).toBe(false);
    expect(isLoopbackRemoteAddress("not-an-ip")).toBe(false);
    expect(isLoopbackRemoteAddress("127.0.0")).toBe(false); // truncated
    expect(isLoopbackRemoteAddress("127.0.0.256")).toBe(false); // out-of-range octet
  });
});

describe("IPC brand constructors", () => {
  describe("BrokerPort", () => {
    it("accepts integers in 1..65535", () => {
      expect(asBrokerPort(1) as number).toBe(1);
      expect(asBrokerPort(8080) as number).toBe(8080);
      expect(asBrokerPort(65535) as number).toBe(65535);
      expect(isBrokerPort(8080)).toBe(true);
    });

    it("rejects out-of-range, non-integer, and non-number values", () => {
      expect(() => asBrokerPort(0)).toThrow();
      expect(() => asBrokerPort(-1)).toThrow();
      expect(() => asBrokerPort(65536)).toThrow();
      expect(() => asBrokerPort(8080.5)).toThrow();
      expect(() => asBrokerPort(Number.NaN)).toThrow();
      expect(isBrokerPort("8080")).toBe(false);
      expect(isBrokerPort(0)).toBe(false);
      expect(isBrokerPort(65536)).toBe(false);
    });
  });

  describe("ApiToken", () => {
    it("accepts URL-safe tokens of bounded length", () => {
      const t = "a".repeat(32);
      expect(asApiToken(t) as string).toBe(t);
      expect(isApiToken("a".repeat(16))).toBe(true);
      expect(isApiToken("a".repeat(512))).toBe(true);
    });

    it("rejects too short, too long, or unsafe characters", () => {
      expect(() => asApiToken("short")).toThrow();
      expect(() => asApiToken("a".repeat(513))).toThrow();
      expect(() => asApiToken(`${"a".repeat(15)} `)).toThrow();
      expect(() => asApiToken(`${"a".repeat(20)} \n`)).toThrow();
      expect(() => asApiToken("")).toThrow();
      expect(isApiToken("a".repeat(15))).toBe(false);
      expect(isApiToken(123)).toBe(false);
    });
  });

  describe("RequestId", () => {
    it("accepts ULID/uuid-shaped identifiers", () => {
      expect(asRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAV") as string).toBe(
        "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      );
      expect(asRequestId("req_42") as string).toBe("req_42");
      expect(isRequestId("a")).toBe(true);
    });

    it("rejects empty, leading-special, or oversized identifiers", () => {
      expect(() => asRequestId("")).toThrow();
      expect(() => asRequestId(".leading-dot")).toThrow();
      expect(() => asRequestId("a".repeat(129))).toThrow();
      expect(() => asRequestId("has space")).toThrow();
      expect(isRequestId(123)).toBe(false);
    });
  });

  describe("KeychainHandleId", () => {
    it("accepts opaque handle identifiers", () => {
      expect(asKeychainHandleId("kh_abc123") as string).toBe("kh_abc123");
      expect(isKeychainHandleId("kh_abc123")).toBe(true);
    });

    it("rejects empty, leading-special, or unsafe characters", () => {
      expect(() => asKeychainHandleId("")).toThrow();
      expect(() => asKeychainHandleId(".bad")).toThrow();
      expect(() => asKeychainHandleId("kh abc")).toThrow();
      expect(isKeychainHandleId(undefined)).toBe(false);
    });
  });
});

describe("approval submission IPC", () => {
  it("validates request receiptId against token claims receiptId", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const otherReceiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const approvalToken = approvalTokenFor(receiptId);

    expect(
      validateApprovalSubmitRequest({
        receiptId,
        approvalToken,
        idempotencyKey: asIdempotencyKey("approval-submit-01"),
      }),
    ).toEqual({ ok: true });
    expect(
      validateApprovalSubmitRequest({
        receiptId: otherReceiptId,
        approvalToken,
        idempotencyKey: asIdempotencyKey("approval-submit-01"),
      }),
    ).toEqual({
      ok: false,
      reason: "receiptId must match approvalToken.claims.receiptId",
    });
  });

  it("rejects invalid idempotency keys on approval requests", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = approvalTokenFor(receiptId);

    for (const idempotencyKey of ["", "a".repeat(129), "has\ncontrol", "has/slash"]) {
      expect(
        validateApprovalSubmitRequest({
          receiptId,
          approvalToken,
          idempotencyKey,
        }),
      ).toEqual({
        ok: false,
        reason: "idempotencyKey must match /^[A-Za-z0-9_-]{1,128}$/",
      });
    }
  });

  it("rejects approval tokens missing algorithm", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = { ...approvalTokenFor(receiptId) } as Record<string, unknown>;
    Reflect.deleteProperty(approvalToken, "algorithm");

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, approvalToken),
      /algorithm.*required/,
    );
  });

  it("rejects approval tokens with non-ed25519 algorithms", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, {
        ...approvalTokenFor(receiptId),
        algorithm: "rsa",
      }),
      /algorithm.*ed25519/,
    );
  });

  it("rejects approval tokens missing signerKeyId", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = { ...approvalTokenFor(receiptId) } as Record<string, unknown>;
    Reflect.deleteProperty(approvalToken, "signerKeyId");

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, approvalToken),
      /signerKeyId.*required/,
    );
  });

  it("rejects approval tokens with empty signatures", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, {
        ...approvalTokenFor(receiptId),
        signature: "",
      }),
      /signature.*non-empty base64/,
    );
  });

  it("rejects unknown approval token envelope keys", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, {
        ...approvalTokenFor(receiptId),
        evil: true,
      }),
      /evil.*not allowed/,
    );
  });

  it("rejects unknown approval token claim keys", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = approvalTokenFor(receiptId);

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, {
        ...approvalToken,
        claims: {
          ...approvalToken.claims,
          evil: true,
        },
      }),
      /evil.*not allowed/,
    );
  });

  it("rejects approval token claims missing required fields", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = approvalTokenFor(receiptId);
    const claims = { ...approvalToken.claims } as Record<string, unknown>;
    Reflect.deleteProperty(claims, "frozenArgsHash");

    expectApprovalSubmitRejected(
      approvalRequestFor(receiptId, {
        ...approvalToken,
        claims,
      }),
      /frozenArgsHash.*required/,
    );
  });

  it("carries idempotencyKey on queued approval responses", () => {
    const idempotencyKey = asIdempotencyKey("approval-submit-01");
    const response: ApprovalSubmitResponse = {
      accepted: true,
      state: "queued",
      acceptedAt: "2026-05-08T18:01:00.000Z",
      receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
      idempotencyKey,
    };

    expect(response.idempotencyKey).toBe(idempotencyKey);
  });
});

function approvalRequestFor(receiptId: ReceiptId, approvalToken: unknown): unknown {
  return {
    receiptId,
    approvalToken,
    idempotencyKey: asIdempotencyKey("approval-submit-01"),
  };
}

function expectApprovalSubmitRejected(request: unknown, reason: RegExp): void {
  const result = validateApprovalSubmitRequest(request);
  expect(result.ok).toBe(false);
  if (!result.ok) {
    expect(result.reason).toMatch(reason);
  }
}

function approvalTokenFor(receiptId: ReceiptId): SignedApprovalToken {
  return {
    claims: {
      signerIdentity: "fran@example.com",
      role: "approver",
      receiptId,
      frozenArgsHash: sha256Hex("approval-submit-frozen-args"),
      riskClass: "low",
      issuedAt: new Date("2026-05-08T18:00:00.000Z"),
      expiresAt: new Date("2026-05-08T18:30:00.000Z"),
    },
    algorithm: "ed25519",
    signerKeyId: "key_ed25519_01",
    signature: "YXBwcm92YWwtdG9rZW4tc2lnbmF0dXJl",
  };
}
