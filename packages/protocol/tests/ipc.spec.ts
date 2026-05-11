import { readFileSync } from "node:fs";
import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  MAX_APPROVAL_SIGNATURE_BYTES,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  MAX_SIGNER_IDENTITY_BYTES,
  MAX_WEBAUTHN_ASSERTION_BYTES,
} from "../src/budgets.ts";
import { canonicalJSON } from "../src/canonical-json.ts";
import { lsnFromV1Number } from "../src/event-lsn.ts";
import {
  assertJcsValue,
  isAgentSlug,
  isApprovalId,
  isIdempotencyKey,
  isProviderKind,
  isReceiptId,
  isTaskId,
  isToolCallId,
  isWriteId,
} from "../src/index.ts";
import {
  ALLOWED_LOOPBACK_HOSTS,
  type ApprovalSubmitResponse,
  apiBootstrapFromJson,
  apiBootstrapToJson,
  approvalClaimsToSigningBytes,
  approvalSubmitRequestFromJson,
  asApiToken,
  asBrokerPort,
  asBrokerUrl,
  asKeychainHandleId,
  asRequestId,
  type BrokerHttpResponse,
  type ApiBootstrapWire as IpcApiBootstrapWire,
  type ApprovalClaimsWire as IpcApprovalClaimsWire,
  type ApprovalSubmitRequestWire as IpcApprovalSubmitRequestWire,
  type SignedApprovalTokenWire as IpcSignedApprovalTokenWire,
  isAllowedLoopbackHost,
  isApiToken,
  isBrokerPort,
  isBrokerUrl,
  isKeychainHandleId,
  isLoopbackRemoteAddress,
  isRequestId,
  isStreamEventKind,
  isWsFrameType,
  STREAM_EVENT_KIND_VALUES,
  type StreamEvent,
  type StreamEventKind,
  type ThreadInvalidationPayload,
  type ThreadStreamEvent,
  validateApprovalSubmitRequest,
  validateThreadStreamEvent,
  WS_FRAME_TYPE_VALUES,
  type WsFrame,
} from "../src/ipc.ts";
import {
  type ApprovalClaims,
  asIdempotencyKey,
  asReceiptId,
  asSignerIdentity,
  asThreadId,
  asWriteId,
  type ReceiptId,
  type RiskClass,
  type SignedApprovalToken,
} from "../src/receipt.ts";
import { asSha256Hex, sha256Hex } from "../src/sha256.ts";

const TEXT_DECODER = new TextDecoder();

type WireKeysOf<T> = readonly (keyof T)[];

const VALID_IPC_WIRE_KEY_TUPLES = [
  ["token", "broker_url"] as const satisfies WireKeysOf<IpcApiBootstrapWire>,
  [
    "receipt_id",
    "approval_token",
    "idempotency_key",
  ] as const satisfies WireKeysOf<IpcApprovalSubmitRequestWire>,
  [
    "claims",
    "algorithm",
    "signer_key_id",
    "signature",
  ] as const satisfies WireKeysOf<IpcSignedApprovalTokenWire>,
  [
    "signer_identity",
    "role",
    "receipt_id",
    "write_id",
    "frozen_args_hash",
    "risk_class",
    "issued_at",
    "expires_at",
    "webauthn_assertion",
  ] as const satisfies WireKeysOf<IpcApprovalClaimsWire>,
];

const INVALID_API_BOOTSTRAP_WIRE_KEYS = [
  "token",
  // @ts-expect-error wire key typos must fail typecheck.
  "broker_urll",
] as const satisfies WireKeysOf<IpcApiBootstrapWire>;

void VALID_IPC_WIRE_KEY_TUPLES;
void INVALID_API_BOOTSTRAP_WIRE_KEYS;

describe("isAllowedLoopbackHost", () => {
  it("accepts canonical loopback hosts", () => {
    for (const h of ALLOWED_LOOPBACK_HOSTS) {
      expect(isAllowedLoopbackHost(h)).toBe(true);
    }
  });

  it("accepts loopback host with valid port", () => {
    expect(isAllowedLoopbackHost("127.0.0.1:8080")).toBe(true);
    expect(isAllowedLoopbackHost("127.0.0.1:0")).toBe(true);
    expect(isAllowedLoopbackHost("127.0.0.1:65535")).toBe(true);
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
    expect(isAllowedLoopbackHost("localhost:-1")).toBe(false);
    expect(isAllowedLoopbackHost("localhost:65535x")).toBe(false);
    expect(isAllowedLoopbackHost("localhost: 1")).toBe(false);
    expect(isAllowedLoopbackHost("localhost:1 ")).toBe(false);
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
        fc
          .string({ minLength: 1, maxLength: 64 })
          .filter((host) => !isDocumentedAllowedLoopbackHost(host)),
        (host) => isAllowedLoopbackHost(host) === false,
      ),
      { numRuns: 500 },
    );
  });
});

describe("isLoopbackRemoteAddress", () => {
  it("accepts ::1 and 127.0.0.0/8", () => {
    expect(isLoopbackRemoteAddress("::1")).toBe(true);
    expect(isLoopbackRemoteAddress("127.0.0.0")).toBe(true);
    expect(isLoopbackRemoteAddress("127.0.0.1")).toBe(true);
    expect(isLoopbackRemoteAddress("127.255.255.255")).toBe(true);
    expect(isLoopbackRemoteAddress("127.1.2.3")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.0.0.0")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.0.0.1")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.42.42.42")).toBe(true);
    expect(isLoopbackRemoteAddress("::ffff:127.255.255.255")).toBe(true);
  });

  it("rejects non-loopback addresses", () => {
    expect(isLoopbackRemoteAddress("0.0.0.0")).toBe(false);
    expect(isLoopbackRemoteAddress("128.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("10.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("192.168.1.1")).toBe(false);
    expect(isLoopbackRemoteAddress("169.254.169.254")).toBe(false);
    expect(isLoopbackRemoteAddress("::ffff:192.168.1.1")).toBe(false);
    expect(isLoopbackRemoteAddress("::ffff:128.0.0.1")).toBe(false);
    expect(isLoopbackRemoteAddress("::ffff:127.0.0.256")).toBe(false);
    expect(isLoopbackRemoteAddress("fe80::1")).toBe(false);
    expect(isLoopbackRemoteAddress("")).toBe(false);
    expect(isLoopbackRemoteAddress("not-an-ip")).toBe(false);
    expect(isLoopbackRemoteAddress("127.0.0")).toBe(false); // truncated
    expect(isLoopbackRemoteAddress("127.0.0.256")).toBe(false); // out-of-range octet
    expect(isLoopbackRemoteAddress("127.0.0.1:1234")).toBe(false);
    expect(isLoopbackRemoteAddress("[::1]")).toBe(false);
    expect(isLoopbackRemoteAddress("::1:1234")).toBe(false);
    expect(isLoopbackRemoteAddress("127.0.0.1, 10.0.0.1")).toBe(false);
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
    it("accepts base64url tokens of bounded length", () => {
      const t = "a".repeat(32);
      expect(asApiToken(t) as string).toBe(t);
      expect(isApiToken("a".repeat(16))).toBe(true);
      expect(isApiToken("a".repeat(512))).toBe(true);
      // base64url alphabet: A-Z, a-z, 0-9, _, -. No `.`, `~`, `+`, `/`.
      expect(isApiToken("Az09_-Az09_-Az09_-")).toBe(true);
    });

    it("rejects too short, too long, or non-base64url characters", () => {
      expect(() => asApiToken("short")).toThrow();
      expect(() => asApiToken("a".repeat(513))).toThrow();
      expect(() => asApiToken(`${"a".repeat(15)} `)).toThrow();
      expect(() => asApiToken(`${"a".repeat(20)} \n`)).toThrow();
      expect(() => asApiToken(`${"a".repeat(15)}=`)).toThrow();
      expect(() => asApiToken(`${"a".repeat(15)}\u{1f608}`)).toThrow();
      expect(() => asApiToken("")).toThrow();
      // Triangulation review: narrowed from URL-safe to base64url so the
      // token round-trips through `?token=` query strings unchanged.
      // URLSearchParams treats `+` as space and decodes `/` ambiguously;
      // `.` and `~` are RFC-3986 unreserved but not emitted by any broker.
      expect(() => asApiToken("a".repeat(15) + "+")).toThrow();
      expect(() => asApiToken("a".repeat(15) + "/")).toThrow();
      expect(() => asApiToken("a".repeat(15) + ".")).toThrow();
      expect(() => asApiToken("a".repeat(15) + "~")).toThrow();
      expect(isApiToken("a".repeat(15))).toBe(false);
      expect(isApiToken(123)).toBe(false);
    });
  });

  describe("BrokerUrl", () => {
    it("brands valid loopback URLs and round-trips equality", () => {
      const u = asBrokerUrl("http://127.0.0.1:54321");
      expect(u as string).toBe("http://127.0.0.1:54321");
      expect(isBrokerUrl("http://127.0.0.1:54321")).toBe(true);
      expect(isBrokerUrl("http://localhost:1024")).toBe(true);
      expect(isBrokerUrl("http://[::1]:1")).toBe(true);
      // Bare root path is allowed (URL parser preserves "/" for hosts).
      expect(isBrokerUrl("http://127.0.0.1:54321/")).toBe(true);
    });

    it("rejects everything assertApiBootstrapBrokerUrl would reject", () => {
      expect(() => asBrokerUrl("https://127.0.0.1:8080")).toThrow();
      expect(() => asBrokerUrl("http://evil.com:8080")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:80")).toThrow(); // default port stripped
      expect(() => asBrokerUrl("javascript:alert(1)")).toThrow();
      expect(() => asBrokerUrl("file:///etc/passwd")).toThrow();
      expect(() => asBrokerUrl("")).toThrow();
      expect(isBrokerUrl(42)).toBe(false);
      expect(isBrokerUrl(null)).toBe(false);
      expect(isBrokerUrl("not a url")).toBe(false);
    });

    // Triangulation pass-2 (types lens): the brand claims "IS the broker
    // origin." A URL with userinfo, path, query, or fragment passes the
    // protocol/port/host checks but breaks downstream string-concatenation
    // (`${brokerUrl}/api/health` becomes malformed) and leaks credentials
    // as request components. Lock those forms out of the brand.
    it("rejects URLs with userinfo / non-root path / query / fragment", () => {
      // Construct userinfo URLs via concatenation so secretlint's basic-auth
      // detector doesn't match the literal string. These test fixtures are
      // deliberate negative inputs proving the brand rejects them.
      const userinfo = `${"u"}:${"p"}@`;
      expect(() => asBrokerUrl(`http://${userinfo}127.0.0.1:54321`)).toThrow(
        /no userinfo, path, query, or fragment/,
      );
      expect(() => asBrokerUrl("http://127.0.0.1:54321/api-token")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:54321/foo")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:54321/%2e")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:54321/%2e%2e")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:54321/%2E%2E")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:54321?x=1")).toThrow();
      expect(() => asBrokerUrl("http://127.0.0.1:54321#frag")).toThrow();
      expect(() => asBrokerUrl(`http://${userinfo}127.0.0.1:54321/api-token?x=1#f`)).toThrow();
      expect(isBrokerUrl(`http://${userinfo}127.0.0.1:54321`)).toBe(false);
      expect(isBrokerUrl("http://127.0.0.1:54321/foo")).toBe(false);
      expect(isBrokerUrl("http://127.0.0.1:54321/%2e%2e")).toBe(false);
    });
  });

  describe("RequestId", () => {
    it("accepts ULID/uuid-shaped identifiers", () => {
      expect(asRequestId("01ARZ3NDEKTSV4RRFFQ69G5FAV") as string).toBe(
        "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      );
      expect(asRequestId("req_42") as string).toBe("req_42");
      expect(isRequestId("a")).toBe(true);
      expect(isRequestId("a".repeat(128))).toBe(true);
      expect(isRequestId("A.Z_9-0")).toBe(true);
    });

    it("rejects empty, leading-special, or oversized identifiers", () => {
      expect(() => asRequestId("")).toThrow();
      expect(() => asRequestId(".leading-dot")).toThrow();
      expect(() => asRequestId("_leading-underscore")).toThrow();
      expect(() => asRequestId("-leading-hyphen")).toThrow();
      expect(() => asRequestId("a".repeat(129))).toThrow();
      expect(() => asRequestId("has space")).toThrow();
      expect(() => asRequestId("has/slash")).toThrow();
      expect(isRequestId(123)).toBe(false);
    });
  });

  describe("KeychainHandleId", () => {
    it("accepts opaque handle identifiers", () => {
      expect(asKeychainHandleId("kh_abc123") as string).toBe("kh_abc123");
      expect(isKeychainHandleId("kh_abc123")).toBe(true);
      expect(isKeychainHandleId("a")).toBe(true);
      expect(isKeychainHandleId("a".repeat(128))).toBe(true);
      expect(isKeychainHandleId("A.Z_9-0")).toBe(true);
    });

    it("rejects empty, leading-special, or unsafe characters", () => {
      expect(() => asKeychainHandleId("")).toThrow();
      expect(() => asKeychainHandleId(".bad")).toThrow();
      expect(() => asKeychainHandleId("_bad")).toThrow();
      expect(() => asKeychainHandleId("-bad")).toThrow();
      expect(() => asKeychainHandleId("kh abc")).toThrow();
      expect(() => asKeychainHandleId("a".repeat(129))).toThrow();
      expect(() => asKeychainHandleId("kh/abc")).toThrow();
      expect(isKeychainHandleId(undefined)).toBe(false);
    });
  });
});

describe("public index export coverage", () => {
  it("exposes receipt brand guards through src/index.ts", () => {
    const ulid = "01ARZ3NDEKTSV4RRFFQ69G5FAV";

    expect(isReceiptId(ulid)).toBe(true);
    expect(isTaskId(ulid)).toBe(true);
    expect(isAgentSlug("agent_01")).toBe(true);
    expect(isProviderKind("openai")).toBe(true);
    expect(isToolCallId("tool.call-01")).toBe(true);
    expect(isApprovalId("approval_01")).toBe(true);
    expect(isWriteId("write_01")).toBe(true);
    expect(isIdempotencyKey("approval-submit-01")).toBe(true);

    expect(isReceiptId("not-a-ulid")).toBe(false);
    expect(isAgentSlug("_leading")).toBe(false);
    expect(isProviderKind("unsupported")).toBe(false);
  });

  it("exposes canonical JSON assertions through src/index.ts", () => {
    expect(() => assertJcsValue({ ok: true })).not.toThrow();
    expect(() => assertJcsValue({ bad: undefined })).toThrow(/undefined/);
  });
});

type MutableApprovalRequest = Record<string, unknown> & {
  receiptId?: unknown;
  approvalToken?: unknown;
  idempotencyKey?: unknown;
  extra?: unknown;
};

type MutableApprovalToken = Record<string, unknown> & {
  claims?: unknown;
  algorithm?: unknown;
  signerKeyId?: unknown;
  signature?: unknown;
  extraTokenField?: unknown;
};

type MutableApprovalClaims = Record<string, unknown> & {
  signerIdentity?: unknown;
  role?: unknown;
  receiptId?: unknown;
  writeId?: unknown;
  frozenArgsHash?: unknown;
  riskClass?: unknown;
  issuedAt?: unknown;
  expiresAt?: unknown;
  webauthnAssertion?: unknown;
  extraClaimField?: unknown;
};

type ApprovalSubmitFailureCase = {
  readonly name: string;
  readonly mutate: (request: MutableApprovalRequest) => unknown;
  readonly reason: RegExp;
};

const APPROVAL_SUBMIT_FAILURE_CASES = [
  {
    name: "rejects non-object requests",
    mutate: () => null,
    reason: /request must be an object/,
  },
  {
    name: "rejects unknown request keys",
    mutate: (request) => {
      request.extra = 1;
      return request;
    },
    reason: /extra.*not allowed/,
  },
  {
    name: "rejects missing receiptId",
    mutate: (request) => {
      Reflect.deleteProperty(request, "receiptId");
      return request;
    },
    reason: /receiptId is required/,
  },
  {
    name: "rejects missing idempotencyKey",
    mutate: (request) => {
      Reflect.deleteProperty(request, "idempotencyKey");
      return request;
    },
    reason: /idempotencyKey is required/,
  },
  {
    name: "rejects missing approvalToken",
    mutate: (request) => {
      Reflect.deleteProperty(request, "approvalToken");
      return request;
    },
    reason: /approvalToken is required/,
  },
  {
    name: "rejects invalid receiptId brands",
    mutate: (request) => {
      request.receiptId = "not-a-receipt-id";
      return request;
    },
    reason: /receiptId must be an uppercase ULID ReceiptId/,
  },
  {
    name: "rejects invalid idempotencyKey brands",
    mutate: (request) => {
      request.idempotencyKey = "has/slash";
      return request;
    },
    reason: /idempotencyKey must match/,
  },
  {
    name: "rejects non-object approvalToken",
    mutate: (request) => {
      request.approvalToken = "not-a-token";
      return request;
    },
    reason: /approvalToken must be an object/,
  },
  {
    name: "rejects unknown approval token envelope keys",
    mutate: (request) => {
      tokenOf(request).extraTokenField = true;
      return request;
    },
    reason: /extraTokenField.*not allowed/,
  },
  {
    name: "rejects missing token claims",
    mutate: (request) => {
      Reflect.deleteProperty(tokenOf(request), "claims");
      return request;
    },
    reason: /approvalToken\.claims is required/,
  },
  {
    name: "rejects non-object token claims",
    mutate: (request) => {
      tokenOf(request).claims = "not-claims";
      return request;
    },
    reason: /approvalToken\.claims must be an object/,
  },
  {
    name: "rejects missing token algorithm",
    mutate: (request) => {
      Reflect.deleteProperty(tokenOf(request), "algorithm");
      return request;
    },
    reason: /approvalToken\.algorithm is required/,
  },
  {
    name: "rejects undefined token algorithm values",
    mutate: (request) => {
      tokenOf(request).algorithm = undefined;
      return request;
    },
    reason: /approvalToken\.algorithm is required/,
  },
  {
    name: "rejects non-ed25519 token algorithms",
    mutate: (request) => {
      tokenOf(request).algorithm = "rsa";
      return request;
    },
    reason: /approvalToken\.algorithm must be ed25519/,
  },
  {
    name: "rejects missing signerKeyId",
    mutate: (request) => {
      Reflect.deleteProperty(tokenOf(request), "signerKeyId");
      return request;
    },
    reason: /approvalToken\.signerKeyId is required/,
  },
  {
    name: "rejects non-string signerKeyId",
    mutate: (request) => {
      tokenOf(request).signerKeyId = 42;
      return request;
    },
    reason: /approvalToken\.signerKeyId must be a string/,
  },
  {
    name: "rejects missing signatures",
    mutate: (request) => {
      Reflect.deleteProperty(tokenOf(request), "signature");
      return request;
    },
    reason: /approvalToken\.signature is required/,
  },
  {
    name: "rejects empty signatures",
    mutate: (request) => {
      tokenOf(request).signature = "";
      return request;
    },
    reason: /approvalToken\.signature must be a non-empty base64 string/,
  },
  {
    name: "rejects invalid-base64 signatures",
    mutate: (request) => {
      tokenOf(request).signature = "not base64!";
      return request;
    },
    reason: /approvalToken\.signature must be a non-empty base64 string/,
  },
  {
    name: "rejects unknown approval token claim keys",
    mutate: (request) => {
      claimsOf(request).extraClaimField = true;
      return request;
    },
    reason: /extraClaimField.*not allowed/,
  },
  {
    name: "rejects missing signerIdentity claims",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "signerIdentity");
      return request;
    },
    reason: /approvalToken\.claims\.signerIdentity is required/,
  },
  {
    name: "rejects non-string signerIdentity claims",
    mutate: (request) => {
      claimsOf(request).signerIdentity = 42;
      return request;
    },
    reason: /approvalToken\.claims\.signerIdentity must be a string/,
  },
  {
    name: "rejects missing role claims",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "role");
      return request;
    },
    reason: /approvalToken\.claims\.role is required/,
  },
  {
    name: "rejects invalid role claims",
    mutate: (request) => {
      claimsOf(request).role = "admin";
      return request;
    },
    reason: /approvalToken\.claims\.role must be a valid approval role/,
  },
  {
    name: "rejects missing claim receiptId",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "receiptId");
      return request;
    },
    reason: /approvalToken\.claims\.receiptId is required/,
  },
  {
    name: "rejects invalid claim receiptId",
    mutate: (request) => {
      claimsOf(request).receiptId = "not-a-receipt-id";
      return request;
    },
    reason: /approvalToken\.claims\.receiptId must be an uppercase ULID ReceiptId/,
  },
  {
    name: "rejects invalid optional writeId",
    mutate: (request) => {
      claimsOf(request).writeId = "bad write id";
      return request;
    },
    reason: /approvalToken\.claims\.writeId must be a valid WriteId/,
  },
  {
    name: "rejects missing frozenArgsHash claims",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "frozenArgsHash");
      return request;
    },
    reason: /approvalToken\.claims\.frozenArgsHash is required/,
  },
  {
    name: "rejects invalid frozenArgsHash claims",
    mutate: (request) => {
      claimsOf(request).frozenArgsHash = "not-a-sha";
      return request;
    },
    reason: /approvalToken\.claims\.frozenArgsHash must be a sha256 hex digest/,
  },
  {
    name: "rejects missing riskClass claims",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "riskClass");
      return request;
    },
    reason: /approvalToken\.claims\.riskClass is required/,
  },
  {
    name: "rejects invalid riskClass claims",
    mutate: (request) => {
      claimsOf(request).riskClass = "severe";
      return request;
    },
    reason: /approvalToken\.claims\.riskClass must be a valid risk class/,
  },
  {
    name: "rejects missing issuedAt claims",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "issuedAt");
      return request;
    },
    reason: /approvalToken\.claims\.issuedAt is required/,
  },
  {
    name: "rejects invalid issuedAt claims",
    mutate: (request) => {
      claimsOf(request).issuedAt = "2026-05-08T18:00:00.000Z";
      return request;
    },
    reason: /approvalToken\.claims\.issuedAt must be a valid Date/,
  },
  {
    name: "rejects missing expiresAt claims",
    mutate: (request) => {
      Reflect.deleteProperty(claimsOf(request), "expiresAt");
      return request;
    },
    reason: /approvalToken\.claims\.expiresAt is required/,
  },
  {
    name: "rejects invalid expiresAt claims",
    mutate: (request) => {
      claimsOf(request).expiresAt = "2026-05-08T18:30:00.000Z";
      return request;
    },
    reason: /approvalToken\.claims\.expiresAt must be a valid Date/,
  },
  {
    name: "rejects non-string optional WebAuthn assertions",
    mutate: (request) => {
      claimsOf(request).webauthnAssertion = 42;
      return request;
    },
    reason: /approvalToken\.claims\.webauthnAssertion must be a string/,
  },
  {
    name: "rejects high-risk claims without WebAuthn assertions",
    mutate: (request) => {
      claimsOf(request).riskClass = "high";
      return request;
    },
    reason: /webauthnAssertion must be a non-empty string for high\/critical risk/,
  },
  {
    name: "rejects critical-risk claims with empty WebAuthn assertions",
    mutate: (request) => {
      const claims = claimsOf(request);
      claims.riskClass = "critical";
      claims.webauthnAssertion = "";
      return request;
    },
    reason: /webauthnAssertion must be a non-empty string for high\/critical risk/,
  },
  {
    name: "rejects expiry equal to issuance",
    mutate: (request) => {
      const claims = claimsOf(request);
      const issuedAt = new Date("2026-05-08T18:00:00.000Z");
      claims.issuedAt = issuedAt;
      claims.expiresAt = issuedAt;
      return request;
    },
    reason: /expiresAt must be strictly after issuedAt/,
  },
  {
    name: "rejects expiry before issuance",
    mutate: (request) => {
      const claims = claimsOf(request);
      claims.issuedAt = new Date("2026-05-08T18:00:00.000Z");
      claims.expiresAt = new Date("2026-05-08T17:59:59.999Z");
      return request;
    },
    reason: /expiresAt must be strictly after issuedAt/,
  },
  {
    name: "rejects lifetimes over the maximum cap",
    mutate: (request) => {
      const claims = claimsOf(request);
      const issuedAt = new Date("2026-05-08T18:00:00.000Z");
      claims.issuedAt = issuedAt;
      claims.expiresAt = new Date(issuedAt.getTime() + MAX_APPROVAL_TOKEN_LIFETIME_MS + 1);
      return request;
    },
    reason: /MAX_APPROVAL_TOKEN_LIFETIME_MS/,
  },
] as const satisfies readonly ApprovalSubmitFailureCase[];

describe("approval submission IPC", () => {
  it.each(APPROVAL_SUBMIT_FAILURE_CASES)("$name", ({ mutate, reason }) => {
    expectApprovalSubmitRejected(mutate(mutableApprovalRequestFor()), reason);
  });

  it.each([
    "receiptId",
    "idempotencyKey",
    "approvalToken",
  ] as const)("rejects %s accessors without invoking getters", (fieldName) => {
    let getterInvoked = false;
    const request = mutableApprovalRequestFor();
    Reflect.deleteProperty(request, fieldName);
    Object.defineProperty(request, fieldName, {
      enumerable: true,
      get() {
        getterInvoked = true;
        return "should-never-be-read";
      },
    });

    expectApprovalSubmitRejected(request, new RegExp(`${fieldName}.*data property`));
    expect(getterInvoked).toBe(false);
  });

  it.each([
    "writeId",
    "webauthnAssertion",
  ] as const)("rejects optional claim %s accessors without invoking getters", (fieldName) => {
    let getterInvoked = false;
    const request = mutableApprovalRequestFor();
    const claims = claimsOf(request);
    Object.defineProperty(claims, fieldName, {
      enumerable: true,
      get() {
        getterInvoked = true;
        return "should-never-be-read";
      },
    });

    expectApprovalSubmitRejected(
      request,
      new RegExp(`approvalToken\\.claims\\.${fieldName}.*data property`),
    );
    expect(getterInvoked).toBe(false);
  });

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

  it("accepts approval token claims at the maximum lifetime cap", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = approvalTokenFor(receiptId);
    const issuedAt = new Date("2026-05-08T18:00:00.000Z");

    expect(
      validateApprovalSubmitRequest(
        approvalRequestFor(receiptId, {
          ...approvalToken,
          claims: {
            ...approvalToken.claims,
            issuedAt,
            expiresAt: new Date(issuedAt.getTime() + MAX_APPROVAL_TOKEN_LIFETIME_MS),
          },
        }),
      ),
    ).toEqual({ ok: true });
  });

  it("accepts valid optional writeId claims", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const approvalToken = approvalTokenFor(receiptId);

    expect(
      validateApprovalSubmitRequest(
        approvalRequestFor(receiptId, {
          ...approvalToken,
          claims: {
            ...approvalToken.claims,
            writeId: asWriteId("write_01"),
          },
        }),
      ),
    ).toEqual({ ok: true });
  });

  it("produces deterministic approval-claims signing bytes over ISO date projections", () => {
    const baseIssuedAt = new Date("2026-05-08T18:00:00.000Z");

    fc.assert(
      fc.property(fc.integer({ min: 0, max: 999 }), (issuedMs) => {
        const issuedAt = new Date(baseIssuedAt.getTime() + issuedMs);
        const expiresAt = new Date(issuedAt.getTime() + 60_000);
        const claims: ApprovalClaims = {
          ...approvalTokenFor(asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV")).claims,
          issuedAt,
          expiresAt,
        };
        const expected = canonicalJSON({
          signerIdentity: claims.signerIdentity,
          role: claims.role,
          receiptId: claims.receiptId,
          frozenArgsHash: claims.frozenArgsHash,
          riskClass: claims.riskClass,
          issuedAt: issuedAt.toISOString(),
          expiresAt: expiresAt.toISOString(),
        });

        expect(signingBytesText(claims)).toBe(expected);
        expect(signingBytesText(claims)).toBe(signingBytesText(claims));
      }),
      { numRuns: 50 },
    );
  });

  it("matches the approval-claims golden signing vector", () => {
    const vector = approvalClaimsVectorNamed("approval_claims_high_write_bound");

    expect(signingBytesText(approvalClaimsFromVector(vector))).toBe(vector.expected.signingBytes);
  });

  it("decodes approval submit JSON with ISO-string dates into the runtime Date shape", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const token = approvalTokenFor(receiptId);
    const wire = approvalSubmitRequestWireFor(receiptId, token);

    const decoded = approvalSubmitRequestFromJson(wire);

    expect(decoded.approvalToken.claims.issuedAt).toStrictEqual(token.claims.issuedAt);
    expect(decoded.approvalToken.claims.expiresAt).toStrictEqual(token.claims.expiresAt);
    expect(validateApprovalSubmitRequest(decoded)).toEqual({ ok: true });
    expect(signingBytesText(decoded.approvalToken.claims)).toBe(signingBytesText(token.claims));
  });

  it("rejects oversized signerIdentity claims while decoding approval submit JSON", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const wire = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));
    wire.approvalToken.claims.signerIdentity = "x".repeat(MAX_SIGNER_IDENTITY_BYTES + 1);

    expect(() => approvalSubmitRequestFromJson(wire)).toThrow(
      /approvalSubmitRequest\.approvalToken\.claims\/signerIdentity: not a SignerIdentity/,
    );
  });

  it("decodes snake_case approval submit wire JSON into the camelCase runtime shape", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const token = approvalTokenFor(receiptId);

    const decoded = approvalSubmitRequestFromJson(
      snakeCaseApprovalSubmitRequestWireFor(receiptId, token),
    );

    expect(decoded).toMatchObject({
      receiptId,
      idempotencyKey: "approval-submit-01",
      approvalToken: {
        signerKeyId: token.signerKeyId,
        claims: {
          signerIdentity: token.claims.signerIdentity,
          receiptId,
          issuedAt: token.claims.issuedAt,
        },
      },
    });
    expect(validateApprovalSubmitRequest(decoded)).toEqual({ ok: true });
  });

  it("decodes approval submit JSON with optional writeId claims", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const token = approvalTokenFor(receiptId);
    const writeId = asWriteId("write_01");
    const wire = approvalSubmitRequestWireFor(receiptId, {
      ...token,
      claims: {
        ...token.claims,
        writeId,
      },
    });

    expect(approvalSubmitRequestFromJson(wire).approvalToken.claims.writeId).toBe(writeId);
  });

  it("rejects approval submit JSON optional claim accessors without invoking getters", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const wire = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));
    let getterInvoked = false;
    Object.defineProperty(wire.approvalToken.claims, "writeId", {
      enumerable: true,
      get() {
        getterInvoked = true;
        return "write_01";
      },
    });

    expect(() => approvalSubmitRequestFromJson(wire)).toThrow(/writeId.*data property/);
    expect(getterInvoked).toBe(false);
  });

  it("rejects malformed approval submit JSON dates", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const wire = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));
    const token = wire.approvalToken;
    token.claims.issuedAt = "2026-05-08T18:00:00Z";

    expect(() => approvalSubmitRequestFromJson(wire)).toThrow(/issuedAt.*ISO 8601/);

    const invalidInstant = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));
    invalidInstant.approvalToken.claims.expiresAt = "2026-02-30T18:00:00.000Z";
    expect(() => approvalSubmitRequestFromJson(invalidInstant)).toThrow(/expiresAt.*valid/);
  });

  it("rejects missing approval submit JSON fields", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const missingReceiptId = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));
    Reflect.deleteProperty(missingReceiptId, "receiptId");
    expect(() => approvalSubmitRequestFromJson(missingReceiptId)).toThrow(/receiptId is required/);

    const missingClaimDate = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));
    Reflect.deleteProperty(missingClaimDate.approvalToken.claims, "issuedAt");
    expect(() => approvalSubmitRequestFromJson(missingClaimDate)).toThrow(/issuedAt is required/);
  });

  it.each([
    {
      name: "invalid token algorithm",
      mutate: (wire: ApprovalSubmitRequestWire) => {
        wire.approvalToken.algorithm = "rsa";
      },
      reason: /algorithm.*one of/,
    },
    {
      name: "invalid request receiptId",
      mutate: (wire: ApprovalSubmitRequestWire) => {
        wire.receiptId = "bad-receipt";
      },
      reason: /receiptId.*uppercase ULID/,
    },
    {
      name: "invalid idempotencyKey",
      mutate: (wire: ApprovalSubmitRequestWire) => {
        wire.idempotencyKey = "bad/key";
      },
      reason: /idempotencyKey.*match/,
    },
    {
      name: "invalid optional writeId",
      mutate: (wire: ApprovalSubmitRequestWire) => {
        wire.approvalToken.claims.writeId = "bad write";
      },
      reason: /writeId.*valid WriteId/,
    },
    {
      name: "invalid frozenArgsHash",
      mutate: (wire: ApprovalSubmitRequestWire) => {
        wire.approvalToken.claims.frozenArgsHash = "not-a-sha";
      },
      reason: /frozenArgsHash.*sha256/,
    },
    {
      name: "non-string optional WebAuthn assertion",
      mutate: (wire: ApprovalSubmitRequestWire) => {
        wire.approvalToken.claims.webauthnAssertion = 42;
      },
      reason: /webauthnAssertion.*string/,
    },
  ])("rejects approval submit JSON with $name", ({ mutate, reason }) => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const wire = approvalSubmitRequestWireFor(receiptId, approvalTokenFor(receiptId));

    mutate(wire);

    expect(() => approvalSubmitRequestFromJson(wire)).toThrow(reason);
  });

  it("enforces approval signature length before base64 regex validation", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const token = approvalTokenFor(receiptId);

    expect(
      validateApprovalSubmitRequest(
        approvalRequestFor(receiptId, {
          ...token,
          signature: "A".repeat(MAX_APPROVAL_SIGNATURE_BYTES),
        }),
      ),
    ).toEqual({ ok: true });

    expect(
      validateApprovalSubmitRequest(
        approvalRequestFor(receiptId, {
          ...token,
          signature: "A".repeat(MAX_APPROVAL_SIGNATURE_BYTES + 1),
        }),
      ),
    ).toEqual({
      ok: false,
      reason: "approvalToken.signature exceeds MAX_APPROVAL_SIGNATURE_BYTES",
    });
  });

  it("enforces WebAuthn assertion length before high-risk non-empty validation", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const token = approvalTokenFor(receiptId);
    const highRiskClaims = {
      ...token.claims,
      riskClass: "high" as const,
    };

    expect(
      validateApprovalSubmitRequest(
        approvalRequestFor(receiptId, {
          ...token,
          claims: {
            ...highRiskClaims,
            webauthnAssertion: "x".repeat(MAX_WEBAUTHN_ASSERTION_BYTES),
          },
        }),
      ),
    ).toEqual({ ok: true });

    expect(
      validateApprovalSubmitRequest(
        approvalRequestFor(receiptId, {
          ...token,
          claims: {
            ...highRiskClaims,
            webauthnAssertion: "x".repeat(MAX_WEBAUTHN_ASSERTION_BYTES + 1),
          },
        }),
      ),
    ).toEqual({
      ok: false,
      reason: "approvalToken.claims.webauthnAssertion exceeds MAX_WEBAUTHN_ASSERTION_BYTES",
    });
  });

  it("enforces WebAuthn assertion byte caps for non-ASCII strings", () => {
    const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
    const token = approvalTokenFor(receiptId);
    const highRiskClaims = {
      ...token.claims,
      riskClass: "high" as const,
    };

    for (const webauthnAssertion of [
      "é".repeat(MAX_WEBAUTHN_ASSERTION_BYTES / 2 + 1),
      "😀".repeat(MAX_WEBAUTHN_ASSERTION_BYTES / 4 + 1),
      "\ud800".repeat(Math.floor(MAX_WEBAUTHN_ASSERTION_BYTES / 3) + 1),
    ]) {
      expect(
        validateApprovalSubmitRequest(
          approvalRequestFor(receiptId, {
            ...token,
            claims: {
              ...highRiskClaims,
              webauthnAssertion,
            },
          }),
        ),
      ).toEqual({
        ok: false,
        reason: "approvalToken.claims.webauthnAssertion exceeds MAX_WEBAUTHN_ASSERTION_BYTES",
      });
    }
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

  it("constructs every ApprovalSubmitResponse variant", () => {
    const idempotencyKey = asIdempotencyKey("approval-submit-01");
    const responses = [
      {
        accepted: true,
        state: "executed",
        appliedAt: "2026-05-08T18:01:00.000Z",
        executionResult: "applied",
        idempotencyKey,
      },
      {
        accepted: true,
        state: "queued",
        acceptedAt: "2026-05-08T18:01:00.000Z",
        receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
        idempotencyKey,
      },
      { accepted: false, reason: "tampered" },
      { accepted: false, reason: "expired" },
      { accepted: false, reason: "wrong_hash" },
      { accepted: false, reason: "policy_denied" },
    ] satisfies readonly ApprovalSubmitResponse[];

    expect(responses.map((response) => response.accepted)).toStrictEqual([
      true,
      true,
      false,
      false,
      false,
      false,
    ]);
  });
});

describe("BrokerHttpResponse", () => {
  it("constructs success, no-content, and error variants", () => {
    const responses = [
      { ok: true, status: 200, body: { value: "ok" } },
      { ok: true, status: 201, body: { value: "created" } },
      { ok: true, status: 202, body: { value: "queued" } },
      { ok: true, status: 204 },
      {
        ok: false,
        status: 429,
        error: {
          code: "rate_limited",
          message: "retry later",
          retryable: true,
          retryAfterMs: 1000,
        },
      },
    ] satisfies readonly BrokerHttpResponse<{ value: string }>[];

    expect(responses.map((response) => response.status)).toStrictEqual([200, 201, 202, 204, 429]);
  });
});

describe("apiBootstrap codec", () => {
  it("property: round-trip preserves valid bootstrap values", () => {
    fc.assert(
      fc.property(
        fc.stringMatching(/^[A-Za-z0-9_-]{16,96}$/),
        // Exclude port 80 — it's HTTP's default port, so `new URL("http://h:80")`
        // strips it, and the codec contract requires an explicit non-default
        // port (see assertApiBootstrapBrokerUrl). Brokers always bind to
        // ephemeral high ports in practice; the property should not exercise
        // input shapes the codec is documented to reject.
        fc.integer({ min: 1, max: 65535 }).filter((p) => p !== 80),
        (tokenValue, port) => {
          const bootstrap = {
            token: asApiToken(tokenValue),
            brokerUrl: asBrokerUrl(`http://127.0.0.1:${port}`),
          };

          expect(apiBootstrapFromJson(apiBootstrapToJson(bootstrap))).toStrictEqual(bootstrap);
        },
      ),
      { numRuns: 200 },
    );
  });

  it("rejects http://<loopback>:80 because URL parser strips HTTP default port", () => {
    // Regression for fast-check finding (seed -795955676): the round-trip
    // property previously generated port 80, which `new URL` normalizes away.
    // Codec contract intentionally requires an explicit non-default port.
    expect(() =>
      apiBootstrapFromJson({ token: "tok-bootstrap-abcdef", broker_url: "http://127.0.0.1:80" }),
    ).toThrow(/apiBootstrap\.broker_url/);
  });

  it("decodes the v0 wire shape with snake_case broker_url", () => {
    const wire = { token: "tok-bootstrap-abcdef", broker_url: "http://127.0.0.1:54321" };
    const bootstrap = apiBootstrapFromJson(wire);
    expect(bootstrap.token as string).toBe("tok-bootstrap-abcdef");
    expect(bootstrap.brokerUrl).toBe("http://127.0.0.1:54321");
  });

  it.each([
    "http://127.0.0.1:8080",
    "http://localhost:54321",
    "http://[::1]:8080",
  ])("accepts loopback broker_url %s", (brokerUrl) => {
    expect(apiBootstrapFromJson({ token: "tok-bootstrap-abcdef", broker_url: brokerUrl })).toEqual({
      token: "tok-bootstrap-abcdef",
      brokerUrl,
    });
  });

  it.each([
    "https://127.0.0.1:8080",
    "http://evil.com:8080",
    "http://127.0.0.1",
    "javascript:alert(1)",
    "file:///etc/passwd",
  ])("rejects non-loopback or malformed broker_url %s", (brokerUrl) => {
    expect(() =>
      apiBootstrapFromJson({ token: "tok-bootstrap-abcdef", broker_url: brokerUrl }),
    ).toThrow(/apiBootstrap\.broker_url: must be http:\/\/<loopback>:<explicit-port>/);
  });

  it.each([
    "http://127.0.0.1:54321",
    "http://localhost:54321",
    "http://[::1]:54321",
  ])("emits the v0 wire shape with snake_case broker_url (%s)", (brokerUrl) => {
    // Mirror the decoder's loopback acceptance matrix on the encoder side
    // so a future hardening that narrowed the allowlist (e.g. dotted-quad
    // only) would fail tests on both halves of the codec, not just the
    // decoder.
    const json = apiBootstrapToJson({
      token: asApiToken("tok-bootstrap-abcdef"),
      brokerUrl: asBrokerUrl(brokerUrl),
    });
    expect(json).toStrictEqual({
      token: "tok-bootstrap-abcdef",
      broker_url: brokerUrl,
    });
  });

  it("apiBootstrapToJson revalidates the token, not just broker_url", () => {
    // Triangulation pass-2 (types lens): the original encoder revalidated
    // broker_url but emitted token verbatim. A caller forging the brand
    // via `as` could produce wire bytes the decoder rejects (e.g. a
    // token with invalid chars or wrong length). Encoder/decoder symmetry
    // requires revalidating both fields.
    const forgedToken = "x" as unknown as ReturnType<typeof asApiToken>;
    expect(() =>
      apiBootstrapToJson({
        token: forgedToken,
        brokerUrl: asBrokerUrl("http://127.0.0.1:54321"),
      }),
    ).toThrow(/apiBootstrap\.token/);
    // Token containing `+` (no longer valid post-narrowing) also rejects.
    const plusToken = "Az09+_-Az09+_-Az09+_-" as unknown as ReturnType<typeof asApiToken>;
    expect(() =>
      apiBootstrapToJson({
        token: plusToken,
        brokerUrl: asBrokerUrl("http://127.0.0.1:54321"),
      }),
    ).toThrow(/apiBootstrap\.token/);
  });

  it.each([
    ["http://127.0.0.1:80", "default-port URL"],
    ["http://evil.com:8080", "non-loopback host"],
    ["https://127.0.0.1:8080", "wrong protocol"],
    ["http://127.0.0.1", "missing port"],
    ["javascript:alert(1)", "javascript-scheme URL"],
    ["file:///etc/passwd", "file-scheme URL"],
    // Userinfo URL constructed by concatenation so secretlint's basic-auth
    // detector doesn't flag the literal string.
    [`http://${"u"}:${"p"}@127.0.0.1:54321`, "URL with userinfo"],
    ["http://127.0.0.1:54321/api-token", "URL with non-root path"],
    ["http://127.0.0.1:54321/foo", "URL with non-root path"],
    ["http://127.0.0.1:54321/%2e", "URL with encoded dot path segment"],
    ["http://127.0.0.1:54321/%2e%2e", "URL with encoded dot-dot path segment"],
    ["http://127.0.0.1:54321/%2E%2E", "URL with encoded uppercase dot-dot path segment"],
    ["http://127.0.0.1:54321?x=1", "URL with query"],
    ["http://127.0.0.1:54321#frag", "URL with fragment"],
  ])("rejects encoder-side broker_url that the decoder would reject (%s — %s)", (brokerUrl) => {
    // Encoder/decoder symmetry: a TS producer MUST NOT be able to emit
    // a wire value that this same codec would reject on read. Without
    // this guard, a producer could write bytes that fail to round-trip,
    // weakening the wire-shape stability story. Cases mirror the decoder
    // rejection matrix above so the property is true by construction.
    // Cast via `as` because brokerUrl values that already fail validation
    // would be rejected by asBrokerUrl at construction; the test asserts
    // the encoder's defensive re-validation independent of the brand.
    expect(() =>
      apiBootstrapToJson({
        token: asApiToken("tok-bootstrap-abcdef"),
        brokerUrl: brokerUrl as unknown as ReturnType<typeof asBrokerUrl>,
      }),
    ).toThrow(/apiBootstrap\.broker_url/);
  });

  it("rejects camelCase brokerUrl on the wire (lint-enforced shape mismatch)", () => {
    expect(() =>
      apiBootstrapFromJson({
        token: "tok-too-short-but-tests-key-shape",
        brokerUrl: "http://127.0.0.1:1",
      }),
    ).toThrow(/broker_url|brokerUrl/);
  });

  it("rejects runtime ApiBootstrap objects as non-canonical wire input", () => {
    expect(() =>
      apiBootstrapFromJson({
        token: asApiToken("tok-bootstrap-abcdef"),
        brokerUrl: "http://127.0.0.1:1",
      }),
    ).toThrow(/broker_url|brokerUrl/);
  });

  it("rejects unknown wire keys", () => {
    expect(() =>
      apiBootstrapFromJson({
        token: "tok-too-short-but-tests-key-shape",
        broker_url: "http://127.0.0.1:1",
        extra: 1,
      }),
    ).toThrow(/extra/);
  });

  it("rejects non-string token", () => {
    expect(() => apiBootstrapFromJson({ token: 1, broker_url: "http://127.0.0.1:1" })).toThrow(
      /token/,
    );
  });

  it("rejects accessor wire fields without invoking getters", () => {
    let getterInvoked = false;
    const wire = { token: "tok-bootstrap-abcdef" };
    Object.defineProperty(wire, "broker_url", {
      enumerable: true,
      get() {
        getterInvoked = true;
        return "http://127.0.0.1:1";
      },
    });

    expect(() => apiBootstrapFromJson(wire)).toThrow(/broker_url.*data property/);
    expect(getterInvoked).toBe(false);
  });

  it("rejects non-record input", () => {
    expect(() => apiBootstrapFromJson(null)).toThrow(/apiBootstrap/);
    expect(() => apiBootstrapFromJson("string")).toThrow(/apiBootstrap/);
    expect(() => apiBootstrapFromJson([])).toThrow(/apiBootstrap/);
  });
});

describe("stream and WebSocket frame runtime guards", () => {
  it("accepts every tuple-backed stream event kind and rejects non-tuple values", () => {
    for (const kind of STREAM_EVENT_KIND_VALUES) {
      expect(isStreamEventKind(kind)).toBe(true);
    }

    expect(isStreamEventKind("receipt.deleted")).toBe(false);
    expect(isStreamEventKind("stdout")).toBe(false);
    expect(isStreamEventKind(null)).toBe(false);
  });

  it("models thread stream events as invalidation-only payloads", () => {
    const payload: ThreadInvalidationPayload = {
      threadId: asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY"),
      headLsn: lsnFromV1Number(42),
    };
    const events: readonly StreamEvent<ThreadInvalidationPayload>[] = [
      {
        id: "evt-thread-created",
        kind: "thread.created",
        emittedAt: "2026-05-08T18:00:00.000Z",
        payload,
      },
      {
        id: "evt-thread-updated",
        kind: "thread.updated",
        emittedAt: "2026-05-08T18:00:01.000Z",
        payload,
      },
      {
        id: "evt-thread-pins",
        kind: "thread.pinned_approvals.changed",
        emittedAt: "2026-05-08T18:00:02.000Z",
        payload,
      },
    ];

    expect(events.map((event) => event.kind).every(isStreamEventKind)).toBe(true);
    expect(
      events.every((event) => Object.keys(event.payload).sort().join(",") === "headLsn,threadId"),
    ).toBe(true);

    const invalidThreadStreamEvent: StreamEvent<
      ThreadInvalidationPayload & { readonly content: "secret" }
    > = {
      id: "evt-thread-secret",
      kind: "thread.updated",
      emittedAt: "2026-05-08T18:00:03.000Z",
      payload: {
        ...payload,
        // @ts-expect-error thread.* stream events are constrained to invalidation-only payloads.
        content: "secret" as const,
      },
    };
    void invalidThreadStreamEvent;
  });

  it("validates thread stream events and rejects payload data leakage", () => {
    const event: ThreadStreamEvent = {
      id: "evt-thread-updated",
      kind: "thread.updated",
      emittedAt: "2026-05-08T18:00:00.000Z",
      payload: {
        threadId: asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY"),
        headLsn: lsnFromV1Number(42),
      },
    };

    expect(validateThreadStreamEvent(event)).toEqual({ ok: true });
    expect(
      validateThreadStreamEvent({
        ...event,
        payload: {
          ...event.payload,
          content: "secret",
        },
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/payload/content", message: "is not allowed" }],
    });
    expect(validateThreadStreamEvent({ ...event, kind: "receipt.updated" }).ok).toBe(false);
    expect(validateThreadStreamEvent({ ...event, emittedAt: "2026-02-31T00:00:00.000Z" })).toEqual({
      ok: false,
      errors: [{ path: "/emittedAt", message: "must be a valid ISO 8601 instant" }],
    });
    expect(validateThreadStreamEvent({ ...event, receiptId: "not-a-receipt" })).toEqual({
      ok: false,
      errors: [{ path: "/receiptId", message: "must be an uppercase ULID ReceiptId" }],
    });
    expect(
      validateThreadStreamEvent({ ...event, payload: { ...event.payload, headLsn: "v1:01" } }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/payload/headLsn", message: "parseLsn: malformed v1 LSN: v1:01" }],
    });
  });

  it("accepts every tuple-backed WebSocket frame type and rejects non-tuple values", () => {
    for (const frameType of WS_FRAME_TYPE_VALUES) {
      expect(isWsFrameType(frameType)).toBe(true);
    }

    expect(isWsFrameType("receipt.created")).toBe(false);
    expect(isWsFrameType("close")).toBe(false);
    expect(isWsFrameType(undefined)).toBe(false);
  });

  it("keeps runtime tuples exhaustive against the TypeScript unions", () => {
    const streamEventKindCoverage = {
      "receipt.created": true,
      "receipt.updated": true,
      "receipt.finalized": true,
      "approval.requested": true,
      "approval.decided": true,
      "cost.exceeded": true,
      "agent.online": true,
      "agent.offline": true,
      "agent.message": true,
      "tool.call.started": true,
      "tool.call.completed": true,
      "thread.created": true,
      "thread.updated": true,
      "thread.pinned_approvals.changed": true,
      backpressure: true,
    } satisfies Record<StreamEventKind, true>;
    const wsFrameTypeCoverage = {
      stdout: true,
      stderr: true,
      stdin: true,
      resize: true,
      exit: true,
      ping: true,
      pong: true,
    } satisfies Record<WsFrame["t"], true>;

    expect(Object.keys(streamEventKindCoverage).sort()).toStrictEqual(
      [...STREAM_EVENT_KIND_VALUES].sort(),
    );
    expect(Object.keys(wsFrameTypeCoverage).sort()).toStrictEqual([...WS_FRAME_TYPE_VALUES].sort());
  });
});

function isDocumentedAllowedLoopbackHost(host: string): boolean {
  if (host === "::1") return true;
  if (host.startsWith("[")) {
    const closeBracketIdx = host.indexOf("]");
    if (closeBracketIdx < 0) return false;
    if (host.slice(1, closeBracketIdx) !== "::1") return false;
    const suffix = host.slice(closeBracketIdx + 1);
    if (suffix === "") return true;
    return suffix.startsWith(":") && isDocumentedPort(suffix.slice(1));
  }
  if (host.includes(":") && host.indexOf(":") !== host.lastIndexOf(":")) {
    return false;
  }
  const colonIdx = host.lastIndexOf(":");
  const bareHost = colonIdx >= 0 ? host.slice(0, colonIdx) : host;
  if (colonIdx >= 0 && !isDocumentedPort(host.slice(colonIdx + 1))) {
    return false;
  }
  return bareHost === "127.0.0.1" || bareHost.toLowerCase() === "localhost";
}

function isDocumentedPort(port: string): boolean {
  if (!/^\d+$/.test(port)) return false;
  const parsed = Number(port);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 65535;
}

interface ApprovalClaimsVector {
  readonly name: string;
  readonly input: {
    readonly signerIdentity: string;
    readonly role: ApprovalClaims["role"];
    readonly receiptId: string;
    readonly writeId?: string;
    readonly frozenArgsHash: string;
    readonly riskClass: RiskClass;
    readonly issuedAt: string;
    readonly expiresAt: string;
    readonly webauthnAssertion?: string;
  };
  readonly expected: {
    readonly signingBytes: string;
  };
}

interface ApprovalClaimsVectorFixture {
  readonly approvalClaimsVectors: readonly ApprovalClaimsVector[];
}

interface ApprovalSubmitRequestWire {
  receiptId?: string;
  approvalToken: {
    claims: Record<string, unknown> & {
      signerIdentity?: unknown;
      issuedAt?: unknown;
      expiresAt?: unknown;
      writeId?: unknown;
      frozenArgsHash?: unknown;
      webauthnAssertion?: unknown;
    };
    algorithm: string;
    signerKeyId: string;
    signature: string;
  };
  idempotencyKey: string;
}

function signingBytesText(claims: ApprovalClaims): string {
  return TEXT_DECODER.decode(approvalClaimsToSigningBytes(claims));
}

function approvalClaimsVectorNamed(name: string): ApprovalClaimsVector {
  const fixture = JSON.parse(
    readFileSync(new URL("../testdata/audit-event-vectors.json", import.meta.url), "utf8"),
  ) as ApprovalClaimsVectorFixture;
  const vector = fixture.approvalClaimsVectors.find((candidate) => candidate.name === name);
  if (vector === undefined) {
    throw new Error(`missing approval claims vector: ${name}`);
  }
  return vector;
}

function approvalClaimsFromVector(vector: ApprovalClaimsVector): ApprovalClaims {
  const { input } = vector;
  return {
    signerIdentity: asSignerIdentity(input.signerIdentity),
    role: input.role,
    receiptId: asReceiptId(input.receiptId),
    ...(input.writeId === undefined ? {} : { writeId: asWriteId(input.writeId) }),
    frozenArgsHash: asSha256Hex(input.frozenArgsHash),
    riskClass: input.riskClass,
    issuedAt: new Date(input.issuedAt),
    expiresAt: new Date(input.expiresAt),
    ...(input.webauthnAssertion === undefined
      ? {}
      : { webauthnAssertion: input.webauthnAssertion }),
  };
}

function approvalSubmitRequestWireFor(
  receiptId: ReceiptId,
  approvalToken: SignedApprovalToken,
): ApprovalSubmitRequestWire {
  const { claims } = approvalToken;
  return {
    receiptId,
    idempotencyKey: "approval-submit-01",
    approvalToken: {
      claims: {
        signerIdentity: claims.signerIdentity,
        role: claims.role,
        receiptId: claims.receiptId,
        ...(claims.writeId === undefined ? {} : { writeId: claims.writeId }),
        frozenArgsHash: claims.frozenArgsHash,
        riskClass: claims.riskClass,
        issuedAt: claims.issuedAt.toISOString(),
        expiresAt: claims.expiresAt.toISOString(),
        ...(claims.webauthnAssertion === undefined
          ? {}
          : { webauthnAssertion: claims.webauthnAssertion }),
      },
      algorithm: approvalToken.algorithm,
      signerKeyId: approvalToken.signerKeyId,
      signature: approvalToken.signature,
    },
  };
}

function snakeCaseApprovalSubmitRequestWireFor(
  receiptId: ReceiptId,
  approvalToken: SignedApprovalToken,
): Record<string, unknown> {
  const { claims } = approvalToken;
  return {
    receipt_id: receiptId,
    idempotency_key: "approval-submit-01",
    approval_token: {
      claims: {
        signer_identity: claims.signerIdentity,
        role: claims.role,
        receipt_id: claims.receiptId,
        ...(claims.writeId === undefined ? {} : { write_id: claims.writeId }),
        frozen_args_hash: claims.frozenArgsHash,
        risk_class: claims.riskClass,
        issued_at: claims.issuedAt.toISOString(),
        expires_at: claims.expiresAt.toISOString(),
        ...(claims.webauthnAssertion === undefined
          ? {}
          : { webauthn_assertion: claims.webauthnAssertion }),
      },
      algorithm: approvalToken.algorithm,
      signer_key_id: approvalToken.signerKeyId,
      signature: approvalToken.signature,
    },
  };
}

function mutableApprovalRequestFor(
  receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
): MutableApprovalRequest {
  const approvalToken = approvalTokenFor(receiptId);
  return {
    receiptId,
    idempotencyKey: asIdempotencyKey("approval-submit-01"),
    approvalToken: {
      ...approvalToken,
      claims: {
        ...approvalToken.claims,
      },
    },
  };
}

function tokenOf(request: MutableApprovalRequest): MutableApprovalToken {
  if (!isMutableRecord(request.approvalToken)) {
    throw new Error("test fixture expected approvalToken to be a record");
  }
  return request.approvalToken;
}

function claimsOf(request: MutableApprovalRequest): MutableApprovalClaims {
  const claims = tokenOf(request).claims;
  if (!isMutableRecord(claims)) {
    throw new Error("test fixture expected approvalToken.claims to be a record");
  }
  return claims;
}

function isMutableRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

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
      signerIdentity: asSignerIdentity("fran@example.com"),
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
