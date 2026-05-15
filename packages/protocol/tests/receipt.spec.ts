import fc from "fast-check";
import { describe, expect, it, vi } from "vitest";
import {
  MAX_AGENT_SLUG_BYTES,
  MAX_APPROVAL_ID_BYTES,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  MAX_FROZEN_ARGS_BYTES,
  MAX_LOCAL_ID_BYTES,
  MAX_RECEIPT_BYTES,
  MAX_SANITIZED_STRING_BYTES,
  MAX_TOOL_CALL_ID_BYTES,
  MAX_TOOL_CALLS_PER_RECEIPT,
  MAX_WEBAUTHN_ASSERTION_FIELD_BYTES,
  MAX_WRITE_ID_BYTES,
} from "../src/budgets.ts";
import { asAgentId } from "../src/credential-handle.ts";
import { FrozenArgs } from "../src/frozen-args.ts";
import { approvalSubmitRequestFromJson } from "../src/ipc.ts";
import {
  asAgentSlug,
  asApprovalClaimId,
  asApprovalId,
  asApprovalTokenId,
  asIdempotencyKey,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asThreadId,
  asTimestampMs,
  asToolCallId,
  asWriteId,
  type ExternalWrite,
  isReceiptSnapshot,
  PROVIDER_KIND_VALUES,
  type ReceiptSnapshot,
  type ReceiptSnapshotV1,
  type ReceiptStatus,
  receiptFromJson,
  receiptToJson,
  type SignedApprovalToken,
  validateReceipt,
  type WriteResult,
} from "../src/receipt.ts";
import { WRITE_RESULT_VALUES } from "../src/receipt-literals.ts";
import { assertKnownKeys } from "../src/receipt-utils.ts";
import {
  APPROVAL_EVENT_KEYS,
  BROKER_TOKEN_VERDICT_KEYS,
  COMMIT_REF_KEYS,
  EXTERNAL_WRITE_KEYS,
  FILE_CHANGE_KEYS,
  FROZEN_ARGS_KEYS,
  MEMORY_WRITE_KEYS,
  RECEIPT_KEYS,
  SIGNED_APPROVAL_TOKEN_KEYS,
  SOURCE_READ_KEYS,
  TOOL_CALL_KEYS,
  WRITE_FAILURE_METADATA_KEYS,
} from "../src/receipt-validator.ts";
import { SanitizedString } from "../src/sanitized-string.ts";
import { type Sha256Hex, sha256Hex } from "../src/sha256.ts";
import {
  RECEIPT_CO_SIGN_CLAIM_KEYS,
  RECEIPT_CO_SIGN_SCOPE_KEYS,
  WEBAUTHN_ASSERTION_KEYS,
} from "../src/signed-approval-token.ts";

const RECEIPT_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";
const THREAD_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAY";

const REQUIRED_TOP_LEVEL_FIELDS = [
  "id",
  "agentSlug",
  "taskId",
  "triggerKind",
  "triggerRef",
  "startedAt",
  "status",
  "providerKind",
  "model",
  "promptHash",
  "toolManifest",
  "toolCalls",
  "approvals",
  "filesChanged",
  "commits",
  "sourceReads",
  "writes",
  "inputTokens",
  "outputTokens",
  "cacheReadTokens",
  "cacheCreationTokens",
  "costUsd",
  "notebookWrites",
  "wikiWrites",
  "schemaVersion",
] as const satisfies readonly (keyof ReceiptSnapshot)[];

const RECORD_BOUNDARIES = [
  { path: "/receipt", keys: RECEIPT_KEYS },
  { path: "/sourceReads/0", keys: SOURCE_READ_KEYS },
  { path: "/toolCalls/0", keys: TOOL_CALL_KEYS },
  { path: "/approvals/0", keys: APPROVAL_EVENT_KEYS },
  { path: "/approvals/0/tokenVerdict", keys: BROKER_TOKEN_VERDICT_KEYS },
  { path: "/filesChanged/0", keys: FILE_CHANGE_KEYS },
  { path: "/commits/0", keys: COMMIT_REF_KEYS },
  { path: "/notebookWrites/0", keys: MEMORY_WRITE_KEYS },
  { path: "/toolCalls/0/inputs", keys: FROZEN_ARGS_KEYS },
  { path: "/writes/0/failureMetadata", keys: WRITE_FAILURE_METADATA_KEYS },
  { path: "/writes/0", keys: EXTERNAL_WRITE_KEYS },
  { path: "/approvals/0/signedToken", keys: SIGNED_APPROVAL_TOKEN_KEYS },
  { path: "/approvals/0/signedToken/claim", keys: RECEIPT_CO_SIGN_CLAIM_KEYS },
  { path: "/approvals/0/signedToken/scope", keys: RECEIPT_CO_SIGN_SCOPE_KEYS },
  { path: "/approvals/0/signedToken/signature", keys: WEBAUTHN_ASSERTION_KEYS },
] as const satisfies readonly { path: string; keys: ReadonlySet<string> }[];

interface ApprovalTokenWire extends Record<string, unknown> {
  tokenId?: unknown;
  notBefore?: unknown;
  expiresAt?: unknown;
  issuedTo?: unknown;
  signature?: unknown;
  claim?: unknown;
  scope?: unknown;
}

type ReceiptCoSignToken = SignedApprovalToken & {
  readonly claim: Extract<SignedApprovalToken["claim"], { readonly kind: "receipt_co_sign" }>;
  readonly scope: Extract<SignedApprovalToken["scope"], { readonly claimKind: "receipt_co_sign" }>;
};

describe("receipt schema", () => {
  it("round-trips a valid receipt through canonical JSON", () => {
    const receipt = validReceiptFixture();

    const roundTripped = receiptFromJson(receiptToJson(receipt));

    expect(roundTripped).toEqual(receipt);
    expect(receiptToJson(roundTripped)).toBe(receiptToJson(receipt));
  });

  it("serializes byte-identical canonical JSON for shuffled field insertion order", () => {
    expect(receiptToJson(validReceiptFixture())).toBe(receiptToJson(shuffledReceiptFixture()));
  });

  it("keeps receiptToJson(receiptFromJson(jcsBytes(snapshot))) byte-stable for valid snapshots", () => {
    fc.assert(
      fc.property(validReceiptSnapshotArbitrary(), (snapshot) => {
        const jcsBytes = receiptToJson(snapshot);

        expect(receiptToJson(receiptFromJson(jcsBytes))).toBe(jcsBytes);
      }),
      { numRuns: 50 },
    );
  });

  it("accepts schemaVersion 1 without a threadId", () => {
    const receipt = validReceiptFixture();

    expect(validateReceipt(receipt)).toEqual({ ok: true });
    expect(receiptFromJson(receiptToJson(receipt))).toEqual(receipt);
  });

  it("rejects schemaVersion 1 when threadId is present", () => {
    const runtimeReceipt: Record<string, unknown> = {
      ...validReceiptFixture(),
      threadId: asThreadId(THREAD_ID),
    };
    expectReceiptValidationError(runtimeReceipt, "/threadId", /schemaVersion 1/);

    const wireReceipt = receiptJsonFixture();
    wireReceipt.threadId = THREAD_ID;
    expect(() => receiptFromJson(JSON.stringify(wireReceipt))).toThrow(
      /\/threadId: must be absent for schemaVersion 1/,
    );
  });

  it("pins ReceiptSnapshotV1 against threadId at the type boundary", () => {
    const invalidV1 = {
      ...validReceiptFixture(),
      schemaVersion: 1 as const,
      // @ts-expect-error ReceiptSnapshotV1 must not carry threadId.
      threadId: asThreadId(THREAD_ID),
    } satisfies ReceiptSnapshotV1;

    void invalidV1;
  });

  it("round-trips schemaVersion 2 with threadId without dropping the field", () => {
    const receipt: ReceiptSnapshot = {
      ...validReceiptFixture(),
      schemaVersion: 2,
      threadId: asThreadId(THREAD_ID),
    };

    const json = receiptToJson(receipt);
    const parsed = JSON.parse(json) as Record<string, unknown> & { threadId?: unknown };

    expect(parsed.threadId).toBe(THREAD_ID);
    expect(receiptFromJson(json)).toEqual(receipt);
    expect(receiptToJson(receiptFromJson(json))).toBe(json);
  });

  it("round-trips schemaVersion 2 without threadId for backward-compatible inbox receipts", () => {
    const receipt: ReceiptSnapshot = { ...validReceiptFixture(), schemaVersion: 2 };

    const json = receiptToJson(receipt);
    const parsed = JSON.parse(json) as Record<string, unknown> & { threadId?: unknown };

    expect(parsed.threadId).toBeUndefined();
    expect(receiptFromJson(json)).toEqual(receipt);
    expect(receiptToJson(receiptFromJson(json))).toBe(json);
  });

  it("rejects schemaVersion 0 because v1 has no backward migration codec", () => {
    const runtimeReceipt: Record<string, unknown> = { ...validReceiptFixture(), schemaVersion: 0 };
    expectReceiptValidationError(runtimeReceipt, "/schemaVersion", /must be 1 or 2/);

    const wireReceipt = receiptJsonFixture();
    wireReceipt.schemaVersion = 0;
    expect(() => receiptFromJson(JSON.stringify(wireReceipt))).toThrow(
      /\/schemaVersion: must be 1 or 2/,
    );
  });

  it("rejects schemaVersion 99 because no compatibility branch exists", () => {
    const runtimeReceipt: Record<string, unknown> = { ...validReceiptFixture(), schemaVersion: 99 };
    expectReceiptValidationError(runtimeReceipt, "/schemaVersion", /must be 1 or 2/);

    const wireReceipt = receiptJsonFixture();
    wireReceipt.schemaVersion = 99;
    expect(() => receiptFromJson(JSON.stringify(wireReceipt))).toThrow(
      /\/schemaVersion: must be 1 or 2/,
    );
  });

  it("rejects missing top-level required fields", () => {
    for (const field of REQUIRED_TOP_LEVEL_FIELDS) {
      const missing: Record<string, unknown> = { ...validReceiptFixture() };
      delete missing[field];

      const result = validateReceipt(missing);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.errors.some((error) => error.path === `/${field}`)).toBe(true);
      }
    }
  });

  it("rejects wrong-type and invalid literal fields", () => {
    const wrongTokens: Record<string, unknown> = {
      ...validReceiptFixture(),
      inputTokens: "not-a-number",
    };
    expect(validateReceipt(wrongTokens)).toEqual({
      ok: false,
      errors: [{ path: "/inputTokens", message: "must be a non-negative integer" }],
    });

    // Cover the negative-int / non-integer / non-finite branches of the
    // non-negative-integer validator. These are real failure modes for the
    // token-count fields, surfacing as `must be a non-negative integer`.
    const negativeTokens: Record<string, unknown> = { ...validReceiptFixture(), inputTokens: -1 };
    expect(validateReceipt(negativeTokens)).toEqual({
      ok: false,
      errors: [{ path: "/inputTokens", message: "must be a non-negative integer" }],
    });
    const fractionalTokens: Record<string, unknown> = {
      ...validReceiptFixture(),
      outputTokens: 1.5,
    };
    expect(validateReceipt(fractionalTokens)).toEqual({
      ok: false,
      errors: [{ path: "/outputTokens", message: "must be a non-negative integer" }],
    });
    const infiniteTokens: Record<string, unknown> = {
      ...validReceiptFixture(),
      cacheReadTokens: Number.POSITIVE_INFINITY,
    };
    expect(validateReceipt(infiniteTokens)).toEqual({
      ok: false,
      errors: [{ path: "/cacheReadTokens", message: "must be a non-negative integer" }],
    });

    const wrongStatus: Record<string, unknown> = {
      ...validReceiptFixture(),
      status: "done",
    };
    const statusResult = validateReceipt(wrongStatus);
    expect(statusResult.ok).toBe(false);
    if (!statusResult.ok) {
      expect(statusResult.errors).toContainEqual({
        path: "/status",
        message: "must be a valid receipt status",
      });
    }

    const wrongId: Record<string, unknown> = {
      ...validReceiptFixture(),
      id: "not-a-ulid",
    };
    const idResult = validateReceipt(wrongId);
    expect(idResult.ok).toBe(false);
    if (!idResult.ok) {
      expect(idResult.errors).toContainEqual({
        path: "/id",
        message: "must be an uppercase ULID ReceiptId",
      });
    }
  });

  it("accepts every ProviderKind tuple value through validator and codec", () => {
    for (const providerKind of PROVIDER_KIND_VALUES) {
      const receipt: ReceiptSnapshot = {
        ...validReceiptFixture(),
        providerKind: asProviderKind(providerKind),
      };

      expect(validateReceipt(receipt)).toEqual({ ok: true });
      expect(receiptFromJson(receiptToJson(receipt)).providerKind).toBe(providerKind);
    }
  });

  it("rejects unsupported ProviderKind strings at validator and codec boundaries", () => {
    const runtimeReceipt: Record<string, unknown> = {
      ...validReceiptFixture(),
      providerKind: "mistral",
    };
    expectReceiptValidationError(runtimeReceipt, "/providerKind", /supported ProviderKind/);

    const wireReceipt = receiptJsonFixture();
    wireReceipt.providerKind = "mistral";
    expect(() => receiptFromJson(JSON.stringify(wireReceipt))).toThrow(
      /\/providerKind: not a supported ProviderKind/,
    );
  });

  it("enforces receipt budgets before walking oversized arrays", () => {
    const fixture = validReceiptFixture();
    const firstToolCall = nonNull(fixture.toolCalls[0]);
    const result = validateReceipt({
      ...fixture,
      toolCalls: Array.from({ length: MAX_TOOL_CALLS_PER_RECEIPT + 1 }, () => firstToolCall),
    });

    expect(result).toEqual({
      ok: false,
      errors: [
        {
          path: "",
          message: `receipt toolCalls length exceeds budget: ${
            MAX_TOOL_CALLS_PER_RECEIPT + 1
          } > ${MAX_TOOL_CALLS_PER_RECEIPT}`,
        },
      ],
    });
  });

  it("does not invoke hostile accessors while validating receipt budgets", () => {
    let sideEffectFired = false;
    const input: Record<string, unknown> = { ...validReceiptFixture() };
    Object.defineProperty(input, "toolCalls", {
      enumerable: true,
      configurable: true,
      get() {
        sideEffectFired = true;
        return [];
      },
    });

    const result = validateReceipt(input);

    expect(sideEffectFired).toBe(false);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors).toEqual([
        {
          path: "",
          message: expect.stringMatching(/accessor property.*toolCalls/),
        },
      ]);
    }
  });

  it("rejects oversized FrozenArgs JSON before decoding tool call inputs", () => {
    const receipt = receiptJsonFixture();
    const toolCall = nonNull(receipt.toolCalls[0]);
    const canonicalJson = "x".repeat(MAX_FROZEN_ARGS_BYTES + 1);
    toolCall.inputs = {
      canonicalJson,
      hash: sha256Hex(canonicalJson),
    };
    const parseSpy = vi.spyOn(JSON, "parse");

    try {
      expect(() => receiptFromJson(JSON.stringify(receipt))).toThrow(
        /receipt toolCalls\[0\]\.inputs: FrozenArgs canonicalJson bytes exceeds budget: 1048577 > 1048576/,
      );
      expect(parseSpy).toHaveBeenCalledTimes(1);
    } finally {
      parseSpy.mockRestore();
    }
  });

  it("rejects oversized final messages before sanitizing them", () => {
    const receipt = receiptJsonFixture();
    receipt.finalMessage = "x".repeat(MAX_SANITIZED_STRING_BYTES + 1);
    const fromUnknownSpy = vi.spyOn(SanitizedString, "fromUnknown");

    try {
      expect(() => receiptFromJson(JSON.stringify(receipt))).toThrow(
        new RegExp(
          `/finalMessage: value exceeds MAX_SANITIZED_STRING_BYTES \\(got ${
            MAX_SANITIZED_STRING_BYTES + 1
          }, max ${MAX_SANITIZED_STRING_BYTES}\\)`,
        ),
      );
      expect(fromUnknownSpy).not.toHaveBeenCalled();
    } finally {
      fromUnknownSpy.mockRestore();
    }
  });

  it("rejects oversized receipt collections before walking per-field decoders", () => {
    const receipt = receiptJsonFixture();
    const firstToolCall = nonNull(receipt.toolCalls[0]);
    receipt.id = "not-a-ulid";
    receipt.toolCalls = Array.from({ length: MAX_TOOL_CALLS_PER_RECEIPT + 1 }, () => firstToolCall);

    expect(() => receiptFromJson(JSON.stringify(receipt))).toThrow(
      /receipt toolCalls length exceeds budget: 1025 > 1024/,
    );
  });

  it("receiptFromJson rejects oversized raw bytes before JSON.parse", () => {
    const parseSpy = vi.spyOn(JSON, "parse");

    try {
      expect(() => receiptFromJson(" ".repeat(MAX_RECEIPT_BYTES + 1))).toThrow(
        /receipt serialized bytes exceeds budget/,
      );
      expect(parseSpy).not.toHaveBeenCalled();
    } finally {
      parseSpy.mockRestore();
    }
  });

  it("receiptToJson rejects typed budget and semantic validation failures", () => {
    const fixture = validReceiptFixture();
    const firstToolCall = nonNull(fixture.toolCalls[0]);
    const overBudget: ReceiptSnapshot = {
      ...fixture,
      toolCalls: Array.from({ length: MAX_TOOL_CALLS_PER_RECEIPT + 1 }, () => firstToolCall),
    };
    expect(() => receiptToJson(overBudget)).toThrow(/receipt toolCalls length exceeds budget/);

    const invalidStatus = { ...fixture, status: "done" } as unknown as ReceiptSnapshot;
    expect(() => receiptToJson(invalidStatus)).toThrow(/\/status: must be a valid receipt status/);
  });

  it("receiptFromJson rejects semantic validation failures after codec decoding", () => {
    const receipt = receiptJsonFixture();
    setWireField(receipt, "finishedAt", "2026-05-08T17:59:59.999Z");

    expect(() => receiptFromJson(JSON.stringify(receipt))).toThrow(
      /\/finishedAt: must be after or equal to startedAt/,
    );
  });

  it("receiptFromJson rejects malformed hostile wire field boundaries", () => {
    const cases: readonly {
      readonly name: string;
      readonly mutate: (receipt: ReturnType<typeof receiptJsonFixture>) => void;
      readonly message: RegExp;
    }[] = [
      {
        name: "sanitized string type",
        mutate: (receipt) => {
          receipt.finalMessage = 1;
        },
        message: /\/finalMessage: must be a string/,
      },
      {
        name: "unsanitized string value",
        mutate: (receipt) => {
          receipt.finalMessage = "evil‮override";
        },
        message: /\/finalMessage: must already be sanitized/,
      },
      {
        name: "signed token assertion field",
        mutate: (receipt) => {
          const approval = approvalWireAt(receipt, 0);
          const signature = wireRecordField(approvalWireSignedToken(approval), "signature");
          setWireField(signature, "signature", "");
        },
        message:
          /\/approvals\/0\/signedToken\/signature\/signature: must be a non-empty base64url string/,
      },
      {
        name: "malformed authenticator data",
        mutate: (receipt) => {
          const approval = approvalWireAt(receipt, 0);
          const signature = wireRecordField(approvalWireSignedToken(approval), "signature");
          setWireField(signature, "authenticatorData", "");
        },
        message:
          /\/approvals\/0\/signedToken\/signature\/authenticatorData: must be a non-empty base64url string/,
      },
      {
        name: "write result literal",
        mutate: (receipt) => {
          setWireField(writeWireAt(receipt, 0), "result", "future");
        },
        message: /\/writes\/0\/result: must be one of applied, rejected, partial, rollback/,
      },
      {
        name: "array shape",
        mutate: (receipt) => {
          setWireField(receipt, "toolCalls", "not-an-array");
        },
        message: /\/toolCalls: must be an array/,
      },
      {
        name: "finite numeric cost",
        mutate: (receipt) => {
          setWireField(receipt, "costUsd", "free");
        },
        message: /\/costUsd: must be a non-negative finite number/,
      },
      {
        name: "optional sha256",
        mutate: (receipt) => {
          setWireField(fileChangeWireAt(receipt, 0), "beforeHash", "not-a-sha256");
        },
        message: /\/filesChanged\/0\/beforeHash: not a sha256 hex digest/,
      },
      {
        name: "optional string",
        mutate: (receipt) => {
          setWireField(sourceReadWireAt(receipt, 0), "rawRef", 1);
        },
        message: /\/sourceReads\/0\/rawRef: must be a string/,
      },
    ];

    for (const testCase of cases) {
      const receipt = receiptJsonFixture();
      testCase.mutate(receipt);

      expect(() => receiptFromJson(JSON.stringify(receipt)), testCase.name).toThrow(
        testCase.message,
      );
    }
  });

  it.each([
    {
      name: "oversized token id",
      mutate: (token: ApprovalTokenWire) => {
        token.tokenId = "A".repeat(27);
      },
      receiptMessage: /\/writes\/0\/approvalToken\/tokenId: ApprovalTokenId: ApprovalTokenId bytes/,
      ipcMessage:
        /approvalSubmitRequest\.approvalToken\/tokenId: ApprovalTokenId: ApprovalTokenId bytes/,
    },
    {
      name: "oversized assertion member",
      mutate: (token: ApprovalTokenWire) => {
        const signature = wireRecordField(token, "signature");
        setWireField(signature, "signature", "A".repeat(MAX_WEBAUTHN_ASSERTION_FIELD_BYTES + 1));
      },
      receiptMessage:
        /receipt writes\[0\]\.approvalToken: approvalToken\.signature\.signature bytes/,
      ipcMessage:
        /approvalSubmitRequest\.approvalToken\/signature\/signature: .*signature bytes exceeds budget: 16385 > 16384/,
    },
    {
      name: "malformed issuedTo",
      mutate: (token: ApprovalTokenWire) => {
        token.issuedTo = "invalid agent id";
      },
      receiptMessage: /\/writes\/0\/approvalToken\/issuedTo: not an AgentId/,
      ipcMessage: /approvalSubmitRequest\.approvalToken\/issuedTo: not an AgentId/,
    },
    {
      name: "overlong token lifetime",
      mutate: (token: ApprovalTokenWire) => {
        token.expiresAt = Number(token.notBefore) + MAX_APPROVAL_TOKEN_LIFETIME_MS + 1;
      },
      receiptMessage: /receipt writes\[0\]\.approvalToken: approval token lifetime ms/,
      ipcMessage: /approvalSubmitRequest\.approvalToken\/expiresAt: approval token lifetime ms/,
    },
  ])("receiptFromJson matches IPC approval-token rejection for $name", ({
    mutate,
    receiptMessage,
    ipcMessage,
  }) => {
    const receipt = receiptJsonFixture();
    const token = writeWireApprovalToken(receipt, 0);
    mutate(token);

    expect(() =>
      approvalSubmitRequestFromJson({
        receiptId: RECEIPT_ID,
        idempotencyKey: "approval-alignment-01",
        approvalToken: token,
      }),
    ).toThrow(ipcMessage);
    expect(() => receiptFromJson(JSON.stringify(receipt))).toThrow(receiptMessage);
  });

  it("rejects malformed approval-event issuedTo while decoding receipt JSON", () => {
    const receipt = receiptJsonFixture();
    const token = approvalWireSignedToken(approvalWireAt(receipt, 0)) as ApprovalTokenWire;
    token.issuedTo = "invalid agent id";

    expect(() => receiptFromJson(JSON.stringify(receipt))).toThrow(
      /\/approvals\/0\/signedToken\/issuedTo: not an AgentId/,
    );
  });

  it("never throws for unknown fuzz payloads", () => {
    fc.assert(
      fc.property(fuzzReceiptPayload(), (payload) => {
        const result = validateReceipt(payload);
        if (result.ok) {
          expect(isReceiptSnapshot(payload)).toBe(true);
          return;
        }
        expect(result.errors.length).toBeGreaterThan(0);
      }),
      { numRuns: 1000 },
    );
  });

  it("brands uppercase ULID receipt ids", () => {
    expect(() => asReceiptId("not-a-ulid")).toThrow();
    expect(asReceiptId(RECEIPT_ID)).toBe(RECEIPT_ID);
  });

  it("rejects non-uppercase-ULID-shaped ReceiptId and TaskId boundary values", () => {
    const invalidIds = [
      RECEIPT_ID.toLowerCase(),
      RECEIPT_ID.slice(0, 25),
      `${RECEIPT_ID}X`,
      `${RECEIPT_ID.slice(0, 25)}_`,
    ];

    for (const invalidId of invalidIds) {
      expect(() => asReceiptId(invalidId)).toThrow();
      expect(() => asTaskId(invalidId)).toThrow();
    }

    expect(asReceiptId(RECEIPT_ID)).toBe(RECEIPT_ID);
    expect(asTaskId(TASK_ID)).toBe(TASK_ID);
  });

  it("rejects forged FrozenArgs (instanceof prototype with mismatched hash)", () => {
    const fixture = validReceiptFixture();
    const firstToolCall = nonNull(fixture.toolCalls[0]);
    const forged = Object.create(FrozenArgs.prototype) as FrozenArgs;
    Object.assign(forged, {
      canonicalJson: '{"forged":true}',
      hash: "0".repeat(64),
    });
    const tampered: ReceiptSnapshot = {
      ...fixture,
      toolCalls: [{ ...firstToolCall, inputs: forged }],
    };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) => e.path.endsWith("/inputs/hash") && /does not match/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("rejects forged SanitizedString (instanceof prototype with bidi-laden value)", () => {
    const fixture = validReceiptFixture();
    const forged = Object.create(SanitizedString.prototype) as SanitizedString;
    Object.assign(forged, { value: "evil‮override" });
    const tampered: ReceiptSnapshot = { ...fixture, finalMessage: forged };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some((e) => e.path === "/finalMessage" && /sanitized/.test(e.message)),
      ).toBe(true);
    }
  });

  it("rejects malformed runtime FrozenArgs and SanitizedString boundary values", () => {
    const malformedFrozenArgs = Object.create(FrozenArgs.prototype) as FrozenArgs;
    Object.assign(malformedFrozenArgs, { canonicalJson: 1, hash: sha256Hex("hash") });
    const malformedFrozenArgsHash = Object.create(FrozenArgs.prototype) as FrozenArgs;
    Object.assign(malformedFrozenArgsHash, { canonicalJson: "{}", hash: "not-a-sha256" });
    const malformedSanitizedString = Object.create(SanitizedString.prototype) as SanitizedString;
    Object.assign(malformedSanitizedString, { value: 1 });

    const cases: readonly {
      readonly path: string;
      readonly input: ReceiptSnapshot | Record<string, unknown>;
      readonly message: RegExp;
    }[] = [
      {
        path: "/toolCalls/0/inputs",
        input: {
          ...validReceiptFixture(),
          toolCalls: [{ ...nonNull(validReceiptFixture().toolCalls[0]), inputs: "not-frozen" }],
        },
        message: /must be FrozenArgs/,
      },
      {
        path: "/toolCalls/0/inputs/canonicalJson",
        input: {
          ...validReceiptFixture(),
          toolCalls: [
            { ...nonNull(validReceiptFixture().toolCalls[0]), inputs: malformedFrozenArgs },
          ],
        },
        message: /must be a string/,
      },
      {
        path: "/toolCalls/0/inputs/hash",
        input: {
          ...validReceiptFixture(),
          toolCalls: [
            { ...nonNull(validReceiptFixture().toolCalls[0]), inputs: malformedFrozenArgsHash },
          ],
        },
        message: /sha256 hex digest/,
      },
      {
        path: "/finalMessage",
        input: { ...validReceiptFixture(), finalMessage: "not-sanitized" },
        message: /must be SanitizedString/,
      },
      {
        path: "/finalMessage/value",
        input: { ...validReceiptFixture(), finalMessage: malformedSanitizedString },
        message: /must be a string/,
      },
    ];

    for (const testCase of cases) {
      expectReceiptValidationError(testCase.input, testCase.path, testCase.message);
    }
  });

  it("rejects approval token bound to a different receipt id", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const signedToken = receiptCoSignToken(firstApproval.signedToken);
    const otherReceiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const wrongTokenApproval = {
      ...firstApproval,
      signedToken: {
        ...signedToken,
        claim: { ...signedToken.claim, receiptId: otherReceiptId },
        scope: { ...signedToken.scope, receiptId: otherReceiptId },
      },
    };
    const tampered: ReceiptSnapshot = { ...fixture, approvals: [wrongTokenApproval] };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path === "/approvals/0/signedToken/claim/receiptId" && /must match/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("rejects external write whose approval token does not bind the proposedDiff hash", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const otherDiff = FrozenArgs.freeze({ unrelated: "diff" });
    const wrongHashWrite = {
      ...firstWrite,
      approvalToken: {
        ...approvalToken,
        claim: { ...approvalToken.claim, frozenArgsHash: otherDiff.hash },
        scope: { ...approvalToken.scope, frozenArgsHash: otherDiff.hash },
      },
    };
    const tampered: ReceiptSnapshot = { ...fixture, writes: [wrongHashWrite] };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path === "/writes/0/approvalToken/claim/frozenArgsHash" &&
            /proposedDiff hash/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("rejects external write whose approval token is bound to a different receipt id", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const otherReceiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          approvalToken: {
            ...approvalToken,
            claim: { ...approvalToken.claim, receiptId: otherReceiptId },
            scope: { ...approvalToken.scope, receiptId: otherReceiptId },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/writes/0/approvalToken/claim/receiptId",
      /must match enclosing receipt id/,
    );
  });

  it("rejects forged proposedDiff hash using the locally re-derived hash", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const forgedHash = sha256Hex('{"different":2}');
    const forged = Object.create(FrozenArgs.prototype) as FrozenArgs;
    Object.assign(forged, {
      canonicalJson: '{"a":1}',
      hash: forgedHash,
    });
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          proposedDiff: forged,
          approvalToken: {
            ...approvalToken,
            claim: { ...approvalToken.claim, frozenArgsHash: forgedHash },
            scope: { ...approvalToken.scope, frozenArgsHash: forgedHash },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/writes/0/approvalToken/claim/frozenArgsHash",
      /proposedDiff hash/,
    );
  });

  it("preserves field-scoped errors for malformed forged proposedDiff JSON", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const forged = Object.create(FrozenArgs.prototype) as FrozenArgs;
    Object.assign(forged, {
      canonicalJson: "not valid json",
      hash: sha256Hex("forged-proposed-diff"),
    });
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [{ ...firstWrite, proposedDiff: forged }],
    };

    const result = validateReceipt(tampered);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.some((error) => error.path === "")).toBe(false);
      expect(
        result.errors.some((error) => error.path === "/writes/0/proposedDiff/canonicalJson"),
      ).toBe(true);
    }
  });

  it("rejects external write whose approval token writeId does not match the enclosing write", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const wrongWrite = {
      ...firstWrite,
      approvalToken: {
        ...approvalToken,
        claim: { ...approvalToken.claim, writeId: asWriteId("write_wrong") },
        scope: { ...approvalToken.scope, writeId: asWriteId("write_wrong") },
      },
    };
    const tampered: ReceiptSnapshot = { ...fixture, writes: [wrongWrite] };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path === "/writes/0/approvalToken/claim/writeId" &&
            /must match this write's writeId/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("allows receipt-scoped approval token without writeId on an external write", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const { writeId: _writeId, ...receiptScopedClaim } = approvalToken.claim;
    const { writeId: _scopeWriteId, ...receiptScopedScope } = approvalToken.scope;
    const receiptScopedWrite = {
      ...firstWrite,
      approvalToken: { ...approvalToken, claim: receiptScopedClaim, scope: receiptScopedScope },
    };
    const receiptScoped: ReceiptSnapshot = { ...fixture, writes: [receiptScopedWrite] };

    expect(validateReceipt(receiptScoped)).toEqual({ ok: true });
  });

  it("rejects approval token role that does not match the approval event role", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const signedToken = receiptCoSignToken(firstApproval.signedToken);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...signedToken,
            scope: { ...signedToken.scope, role: "host" },
          },
        },
      ],
    };

    expectReceiptValidationError(tampered, "/approvals/0/signedToken/scope/role", /approval role/);
  });

  it("rejects approval tokens that expire before they are valid", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...firstApproval.signedToken,
            expiresAt: asTimestampMs(firstApproval.signedToken.notBefore - 1),
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/approvals/0/signedToken/expiresAt",
      /strictly greater than notBefore/,
    );
  });

  it("rejects approval tokens whose lifetime exceeds the cap", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...firstApproval.signedToken,
            expiresAt: asTimestampMs(
              firstApproval.signedToken.notBefore + MAX_APPROVAL_TOKEN_LIFETIME_MS + 1,
            ),
          },
        },
      ],
    };

    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.some((error) => /approval token lifetime ms/.test(error.message))).toBe(
        true,
      );
    }
  });

  it("rejects tool calls that finish before they start", () => {
    const fixture = validReceiptFixture();
    const firstToolCall = nonNull(fixture.toolCalls[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      toolCalls: [
        {
          ...firstToolCall,
          finishedAt: new Date("2026-05-08T18:00:00.999Z"),
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/toolCalls/0/finishedAt",
      /finishedAt=2026-05-08T18:00:00.999Z startedAt=2026-05-08T18:00:01.000Z/,
    );
  });

  it("rejects receipts that finish before they start", () => {
    const fixture = validReceiptFixture();
    const tampered: ReceiptSnapshot = {
      ...fixture,
      finishedAt: new Date("2026-05-08T17:59:59.999Z"),
    };

    expectReceiptValidationError(
      tampered,
      "/finishedAt",
      /finishedAt=2026-05-08T17:59:59.999Z startedAt=2026-05-08T18:00:00.000Z/,
    );
  });

  it("rejects external writes approved before their token validity window", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          approvedAt: new Date(approvalToken.notBefore - 1),
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/writes/0/approvedAt",
      /at or after approvalToken\.notBefore/,
    );
  });

  it("allows external writes approved exactly at token notBefore", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = receiptCoSignToken(nonNull(firstWrite.approvalToken));
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          approvedAt: new Date(approvalToken.notBefore),
        },
      ],
    };

    expect(validateReceipt(tampered)).toEqual({ ok: true });
  });

  it("rejects unknown top-level keys", () => {
    const fixture = validReceiptFixture();
    const tampered = { ...fixture, shadow: { unsanitized: "...‮evil..." } };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.some((e) => e.path === "/shadow" && /not allowed/.test(e.message))).toBe(
        true,
      );
    }
  });

  it("rejects unknown nested keys (in toolCall)", () => {
    const fixture = validReceiptFixture();
    const firstToolCall = nonNull(fixture.toolCalls[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      toolCalls: [{ ...firstToolCall, evilField: "nope" } as never],
    };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) => e.path === "/toolCalls/0/evilField" && /not allowed/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("receiptFromJson throws on unknown top-level fields (no silent drop)", () => {
    const json = receiptToJson(validReceiptFixture());
    const parsed = JSON.parse(json) as Record<string, unknown>;
    const tampered = { ...parsed, shadow: "evil" };
    const tamperedJson = JSON.stringify(tampered);
    expect(() => receiptFromJson(tamperedJson)).toThrow(/shadow.*not allowed/);
  });

  it("receiptFromJson throws on unknown FrozenArgs envelope fields (no silent drop)", () => {
    // Without the FROZEN_ARGS_KEYS allowlist guard, a payload like
    // `{canonicalJson, hash, extra}` survived a round-trip — the codec
    // ignored `extra` and re-emitted only `{canonicalJson, hash}`. That
    // boundary was the only object in the receipt where an attacker could
    // smuggle unhashed shadow data through the wire shape.
    interface FrozenArgsWire {
      canonicalJson: string;
      hash: string;
    }
    interface ToolCallWire {
      inputs: FrozenArgsWire;
    }
    interface ReceiptWire {
      toolCalls: ToolCallWire[];
    }
    const json = receiptToJson(validReceiptFixture());
    const parsed = JSON.parse(json) as ReceiptWire;
    const firstToolCall = parsed.toolCalls[0];
    if (!firstToolCall) throw new Error("fixture must contain a tool call");
    const tampered = {
      ...firstToolCall,
      inputs: { ...firstToolCall.inputs, evilShadow: "smuggled" },
    };
    parsed.toolCalls[0] = tampered as ToolCallWire;
    const tamperedJson = JSON.stringify(parsed);
    expect(() => receiptFromJson(tamperedJson)).toThrow(/evilShadow.*not allowed/);
  });

  it("property: every receipt record allowlist accepts known keys and rejects unknown keys", () => {
    for (const boundary of RECORD_BOUNDARIES) {
      fc.assert(
        fc.property(unknownKeyArbitrary(boundary.keys), (unknownKey) => {
          const record = recordWithKnownKeys(boundary.keys);
          expect(() => assertKnownKeys(record, boundary.path, boundary.keys)).not.toThrow();

          Object.defineProperty(record, unknownKey, {
            value: "shadow",
            enumerable: true,
            configurable: true,
          });

          expect(() => assertKnownKeys(record, boundary.path, boundary.keys)).toThrow(
            /is not allowed/,
          );
        }),
        { numRuns: 50 },
      );
    }
  });

  it("rejects non-object runtime values at every nested receipt record boundary", () => {
    const cases: readonly {
      readonly path: string;
      readonly mutate: (receipt: Record<string, unknown>) => void;
    }[] = [
      {
        path: "/sourceReads/0",
        mutate: (receipt) => {
          setWireField(receipt, "sourceReads", ["not-an-object"]);
        },
      },
      {
        path: "/toolCalls/0",
        mutate: (receipt) => {
          setWireField(receipt, "toolCalls", ["not-an-object"]);
        },
      },
      {
        path: "/approvals/0",
        mutate: (receipt) => {
          setWireField(receipt, "approvals", ["not-an-object"]);
        },
      },
      {
        path: "/approvals/0/tokenVerdict",
        mutate: (receipt) => {
          const firstApproval = nonNull(validReceiptFixture().approvals[0]);
          setWireField(receipt, "approvals", [{ ...firstApproval, tokenVerdict: "not-an-object" }]);
        },
      },
      {
        path: "/approvals/0/signedToken",
        mutate: (receipt) => {
          const firstApproval = nonNull(validReceiptFixture().approvals[0]);
          setWireField(receipt, "approvals", [{ ...firstApproval, signedToken: "not-an-object" }]);
        },
      },
      {
        path: "/approvals/0/signedToken/claim",
        mutate: (receipt) => {
          const firstApproval = nonNull(validReceiptFixture().approvals[0]);
          setWireField(receipt, "approvals", [
            {
              ...firstApproval,
              signedToken: { ...firstApproval.signedToken, claim: "not-an-object" },
            },
          ]);
        },
      },
      {
        path: "/filesChanged/0",
        mutate: (receipt) => {
          setWireField(receipt, "filesChanged", ["not-an-object"]);
        },
      },
      {
        path: "/commits/0",
        mutate: (receipt) => {
          setWireField(receipt, "commits", ["not-an-object"]);
        },
      },
      {
        path: "/notebookWrites/0",
        mutate: (receipt) => {
          setWireField(receipt, "notebookWrites", ["not-an-object"]);
        },
      },
      {
        path: "/wikiWrites/0",
        mutate: (receipt) => {
          setWireField(receipt, "wikiWrites", ["not-an-object"]);
        },
      },
      {
        path: "/writes/0",
        mutate: (receipt) => {
          setWireField(receipt, "writes", ["not-an-object"]);
        },
      },
      {
        path: "/writes/0/failureMetadata",
        mutate: (receipt) => {
          const firstWrite = nonNull(validReceiptFixture().writes[0]);
          setWireField(receipt, "status", "error");
          setWireField(receipt, "writes", [
            { ...writeForResult(firstWrite, "partial"), failureMetadata: "not-an-object" },
          ]);
        },
      },
    ];

    for (const testCase of cases) {
      const receipt = { ...validReceiptFixture() } as unknown as Record<string, unknown>;
      testCase.mutate(receipt);

      expectReceiptValidationError(receipt, testCase.path, /must be an object/);
    }
  });

  it("receiptFromJson includes JSON pointer context for nested brand decoder failures", () => {
    interface FrozenArgsWire {
      hash: string;
    }
    interface ToolCallWire {
      inputs: FrozenArgsWire;
    }
    interface ReceiptWire {
      toolCalls: ToolCallWire[];
    }
    const parsed = JSON.parse(receiptToJson(validReceiptFixture())) as ReceiptWire;
    const firstToolCall = parsed.toolCalls[0];
    if (!firstToolCall) throw new Error("fixture must contain a tool call");
    firstToolCall.inputs.hash = "not-a-sha256";

    expect(() => receiptFromJson(JSON.stringify(parsed))).toThrow(
      /\/toolCalls\/0\/inputs\/hash: not a sha256 hex digest/,
    );
  });

  it("rejects ToolCallId/ApprovalId containing colons (LOCAL_ID_RE excludes ':')", () => {
    expect(() => asToolCallId("tool:01")).toThrow();
    expect(() => asApprovalId("approval:01")).toThrow();
  });

  it("length-caps receipt-local brand constructors at the byte budget", () => {
    const agentSlugAtCap = `a${"b".repeat(MAX_AGENT_SLUG_BYTES - 1)}`;
    expect(asAgentSlug(agentSlugAtCap)).toBe(agentSlugAtCap);
    expect(() => asAgentSlug(`${agentSlugAtCap}b`)).toThrow(/AgentSlug/);

    const toolCallIdAtCap = `t${"o".repeat(MAX_TOOL_CALL_ID_BYTES - 1)}`;
    expect(asToolCallId(toolCallIdAtCap)).toBe(toolCallIdAtCap);
    expect(() => asToolCallId(`${toolCallIdAtCap}o`)).toThrow(/ToolCallId/);

    const approvalIdAtCap = `a${"p".repeat(MAX_APPROVAL_ID_BYTES - 1)}`;
    expect(asApprovalId(approvalIdAtCap)).toBe(approvalIdAtCap);
    expect(() => asApprovalId(`${approvalIdAtCap}p`)).toThrow(/ApprovalId/);

    const writeIdAtCap = `w${"r".repeat(MAX_WRITE_ID_BYTES - 1)}`;
    expect(asWriteId(writeIdAtCap)).toBe(writeIdAtCap);
    expect(() => asWriteId(`${writeIdAtCap}r`)).toThrow(/WriteId/);
  });

  it("MAX_LOCAL_ID_BYTES is the upper bound for any LOCAL_ID_RE-derived brand cap", () => {
    // The shared cap exists so individual brands can be tightened independently
    // (e.g. tighter than 128) but never relaxed past 128 without an explicit
    // budget bump.
    expect(MAX_LOCAL_ID_BYTES).toBeGreaterThanOrEqual(MAX_TOOL_CALL_ID_BYTES);
    expect(MAX_LOCAL_ID_BYTES).toBeGreaterThanOrEqual(MAX_APPROVAL_ID_BYTES);
    expect(MAX_LOCAL_ID_BYTES).toBeGreaterThanOrEqual(MAX_WRITE_ID_BYTES);
  });

  it("rejects notebookWrites entries that claim the wiki store", () => {
    const fixture = validReceiptFixture();
    const firstNotebookWrite = nonNull(fixture.notebookWrites[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      notebookWrites: [{ ...firstNotebookWrite, store: "wiki" }],
    };

    expectReceiptValidationError(tampered, "/notebookWrites/0/store", /must be notebook/);
  });

  it("rejects wikiWrites entries that claim the notebook store", () => {
    const fixture = validReceiptFixture();
    const firstWikiWrite = nonNull(fixture.wikiWrites[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      wikiWrites: [{ ...firstWikiWrite, store: "notebook" }],
    };

    expectReceiptValidationError(tampered, "/wikiWrites/0/store", /must be wiki/);
  });

  it("rejects ok receipts whose approval evidence is rejected", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [{ ...firstApproval, decision: "reject" }],
    };

    expectReceiptValidationError(
      tampered,
      "/status",
      /rejected or error when approvals include a rejection/,
    );
  });

  it("rejects ok receipts whose tool-call evidence failed", () => {
    const fixture = validReceiptFixture();
    const firstToolCall = nonNull(fixture.toolCalls[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      toolCalls: [{ ...firstToolCall, status: "error" }],
    };

    expectReceiptValidationError(tampered, "/status", /must not be ok.*tool call failed/);
  });

  it("rejects ok receipts whose write evidence did not fully apply", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [writeForResult(firstWrite, "partial")],
    };

    expectReceiptValidationError(tampered, "/status", /must not be ok.*write did not apply/);
  });

  it.each([
    {
      name: "approval_pending with applied write evidence",
      receipt: () => ({ ...validReceiptFixture(), status: "approval_pending" }) as ReceiptSnapshot,
      path: "/status",
      message: /must not be approval_pending.*write applied/,
    },
    {
      name: "stalled with applied write evidence",
      receipt: () => ({ ...validReceiptFixture(), status: "stalled" }) as ReceiptSnapshot,
      path: "/status",
      message: /must not be stalled.*write applied/,
    },
    {
      name: "rejected without rejection evidence",
      receipt: () => ({ ...validReceiptFixture(), status: "rejected" }) as ReceiptSnapshot,
      path: "/status",
      message: /must include rejected approval or rejected write evidence/,
    },
    {
      name: "error without failure evidence",
      receipt: () => ({ ...validReceiptFixture(), status: "error" }) as ReceiptSnapshot,
      path: "/status",
      message: /must include failure evidence/,
    },
    {
      name: "rejected approval matching an applied write",
      receipt: () => {
        const fixture = validReceiptFixture();
        const firstApproval = nonNull(fixture.approvals[0]);
        return {
          ...fixture,
          status: "rejected",
          approvals: [{ ...firstApproval, decision: "reject" }],
        } as ReceiptSnapshot;
      },
      path: "/writes/0/result",
      message: /matching approval was rejected/,
    },
  ])("rejects receipt status/evidence contradiction: $name", ({ receipt, path, message }) => {
    expectReceiptValidationError(receipt(), path, message);
  });

  it("accepts rejected status when rejected write evidence is present", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const receipt: ReceiptSnapshot = {
      ...fixture,
      status: "rejected",
      writes: [writeForResult(firstWrite, "rejected")],
    };

    expect(validateReceipt(receipt)).toEqual({ ok: true });
  });

  it("ExternalWrite: validator rejects result='applied' with null appliedDiff (per-state invariant)", () => {
    interface ReceiptWire {
      writes: Array<Record<string, unknown>>;
    }
    const json = receiptToJson(validReceiptFixture());
    const parsed = JSON.parse(json) as ReceiptWire;
    const firstWrite = parsed.writes[0];
    if (!firstWrite) throw new Error("fixture must contain a write");
    parsed.writes[0] = { ...firstWrite, appliedDiff: null };
    const tampered = JSON.parse(receiptToJson(validReceiptFixture())) as ReceiptWire;
    tampered.writes[0] = parsed.writes[0];
    // Codec-level: throws with a clear message.
    expect(() => receiptFromJson(JSON.stringify(tampered))).toThrow(
      /appliedDiff.*null is invalid for state "applied"/,
    );
  });

  it("ExternalWrite: rejects invalid idempotency key shapes", () => {
    interface ExternalWriteWire extends Record<string, unknown> {
      idempotencyKey?: unknown;
    }
    interface ReceiptWire {
      writes: ExternalWriteWire[];
    }
    const invalidKeys = ["", "a".repeat(129), "has\ncontrol", "has/slash"];

    for (const idempotencyKey of invalidKeys) {
      const fixture = validReceiptFixture();
      const firstWrite = nonNull(fixture.writes[0]);
      const validationResult = validateReceipt({
        ...fixture,
        writes: [{ ...firstWrite, idempotencyKey }],
      });
      expect(validationResult.ok).toBe(false);
      if (!validationResult.ok) {
        expect(
          validationResult.errors.some(
            (e) => e.path === "/writes/0/idempotencyKey" && /A-Za-z0-9_/.test(e.message),
          ),
        ).toBe(true);
      }

      const wire = JSON.parse(receiptToJson(validReceiptFixture())) as ReceiptWire;
      const write = wire.writes[0];
      if (!write) throw new Error("fixture must contain a write");
      write.idempotencyKey = idempotencyKey;
      expect(() => receiptFromJson(JSON.stringify(wire))).toThrow(
        /\/writes\/0\/idempotencyKey: asIdempotencyKey/,
      );
    }
  });

  it("WriteResult discriminated union variants construct and round-trip", () => {
    for (const writeResult of WRITE_RESULT_VALUES) {
      const receipt = validReceiptFixture();
      const firstWrite = nonNull(receipt.writes[0]);
      const snapshot: ReceiptSnapshot = {
        ...receipt,
        status: receiptStatusForWriteResult(writeResult),
        writes: [writeForResult(firstWrite, writeResult)],
      };

      expect(validateReceipt(snapshot)).toEqual({ ok: true });
      expect(receiptFromJson(receiptToJson(snapshot))).toEqual(snapshot);
    }
  });

  it("ExternalWrite: validator rejects result='rejected' with non-null postWriteVerify", () => {
    interface ReceiptWire {
      writes: Array<Record<string, unknown>>;
    }
    const tampered = JSON.parse(receiptToJson(validReceiptFixture())) as ReceiptWire;
    const firstWrite = tampered.writes[0];
    if (!firstWrite) throw new Error("fixture must contain a write");
    // Switch result to rejected but leave postWriteVerify populated → invalid.
    tampered.writes[0] = { ...firstWrite, result: "rejected", appliedDiff: null };
    expect(() => receiptFromJson(JSON.stringify(tampered))).toThrow(
      /postWriteVerify.*must be null for state "rejected"/,
    );
  });

  it("ExternalWrite: validator accepts rejected failure metadata", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    if (firstWrite.result !== "applied") throw new Error("fixture write must be applied");
    const receipt: ReceiptSnapshot = {
      ...fixture,
      status: "error",
      writes: [
        {
          ...firstWrite,
          result: "rejected",
          appliedDiff: null,
          postWriteVerify: null,
          failureMetadata: { code: "policy_denied", retryable: false },
        },
      ],
    };

    expect(validateReceipt(receipt)).toEqual({ ok: true });
  });

  it("ExternalWrite: validator accepts partial retry guidance", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    if (firstWrite.result !== "applied") throw new Error("fixture write must be applied");
    const receipt: ReceiptSnapshot = {
      ...fixture,
      status: "error",
      writes: [
        {
          ...firstWrite,
          result: "partial",
          failureMetadata: { code: "rate_limited", retryable: true, retryAfterMs: 5000 },
        },
      ],
    };

    expect(validateReceipt(receipt)).toEqual({ ok: true });
  });

  it("ExternalWrite: codec preserves failure metadata", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    if (firstWrite.result !== "applied") throw new Error("fixture write must be applied");
    const receipt: ReceiptSnapshot = {
      ...fixture,
      status: "error",
      writes: [
        {
          ...firstWrite,
          result: "rollback",
          postWriteVerify: null,
          failureMetadata: {
            code: "downstream_unavailable",
            retryable: false,
            terminalReason: SanitizedString.fromUnknown("Downstream rejected the rollback check"),
          },
        },
      ],
    };

    expect(receiptFromJson(receiptToJson(receipt))).toEqual(receipt);
  });

  it("ExternalWrite: validator rejects unknown failure metadata keys", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const receipt: Record<string, unknown> = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          result: "partial",
          failureMetadata: { code: "rate_limited", retryable: true, extra: "nope" },
        },
      ],
    };

    const result = validateReceipt(receipt);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) => e.path === "/writes/0/failureMetadata/extra" && /not allowed/.test(e.message),
        ),
      ).toBe(true);
    }
  });
});

function expectReceiptValidationError(
  input: unknown,
  expectedPath: string,
  expectedMessage: RegExp,
): void {
  const result = validateReceipt(input);
  expect(result.ok).toBe(false);
  if (!result.ok) {
    expect(
      result.errors.some(
        (error) => error.path === expectedPath && expectedMessage.test(error.message),
      ),
    ).toBe(true);
  }
}

function nonNull<T>(value: T | null | undefined): T {
  if (value === null || value === undefined) {
    throw new Error("fixture missing required value");
  }
  return value;
}

function setWireField(record: Record<string, unknown>, key: string, value: unknown): void {
  record[key] = value;
}

function wireField(record: Record<string, unknown>, key: string): unknown {
  return record[key];
}

function approvalWireAt(receipt: Record<string, unknown>, index: number): Record<string, unknown> {
  return wireRecordArrayItem(receipt, "approvals", index);
}

function approvalWireSignedToken(approval: Record<string, unknown>): Record<string, unknown> {
  const signedToken = wireField(approval, "signedToken");
  if (!isWireRecord(signedToken)) {
    throw new Error("fixture approval missing signedToken");
  }
  return signedToken;
}

function writeWireAt(receipt: Record<string, unknown>, index: number): Record<string, unknown> {
  return wireRecordArrayItem(receipt, "writes", index);
}

function writeWireApprovalToken(
  receipt: Record<string, unknown>,
  index: number,
): ApprovalTokenWire {
  return wireRecordField(writeWireAt(receipt, index), "approvalToken") as ApprovalTokenWire;
}

function fileChangeWireAt(
  receipt: Record<string, unknown>,
  index: number,
): Record<string, unknown> {
  return wireRecordArrayItem(receipt, "filesChanged", index);
}

function sourceReadWireAt(
  receipt: Record<string, unknown>,
  index: number,
): Record<string, unknown> {
  return wireRecordArrayItem(receipt, "sourceReads", index);
}

function wireRecordArrayItem(
  record: Record<string, unknown>,
  key: string,
  index: number,
): Record<string, unknown> {
  const value = record[key];
  if (!Array.isArray(value)) {
    throw new Error(`fixture missing ${key}`);
  }
  const item = value[index];
  if (!isWireRecord(item)) {
    throw new Error(`fixture missing ${key}[${index}]`);
  }
  return item;
}

function wireRecordField(record: Record<string, unknown>, key: string): Record<string, unknown> {
  const value = record[key];
  if (!isWireRecord(value)) {
    throw new Error(`fixture missing ${key}`);
  }
  return value;
}

function isWireRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function receiptJsonFixture(): Record<string, unknown> & {
  id?: unknown;
  providerKind?: unknown;
  schemaVersion?: unknown;
  threadId?: unknown;
  finalMessage?: unknown;
  toolCalls: (Record<string, unknown> & { inputs?: unknown })[];
} {
  const receipt = JSON.parse(receiptToJson(validReceiptFixture())) as Record<string, unknown> & {
    id?: unknown;
    providerKind?: unknown;
    schemaVersion?: unknown;
    threadId?: unknown;
    finalMessage?: unknown;
    toolCalls: (Record<string, unknown> & { inputs?: unknown })[];
  };
  if (!Array.isArray(receipt.toolCalls)) {
    throw new Error("fixture missing toolCalls");
  }
  return receipt;
}

function validReceiptSnapshotArbitrary(): fc.Arbitrary<ReceiptSnapshot> {
  return fc
    .tuple(fc.constantFrom(...PROVIDER_KIND_VALUES), fc.constantFrom(...WRITE_RESULT_VALUES))
    .map(([providerKind, writeResult]) => {
      const receipt = validReceiptFixture();
      const firstWrite = nonNull(receipt.writes[0]);
      return {
        ...receipt,
        providerKind: asProviderKind(providerKind),
        status: receiptStatusForWriteResult(writeResult),
        writes: [writeForResult(firstWrite, writeResult)],
      };
    });
}

function writeForResult(base: ExternalWrite, result: WriteResult): ExternalWrite {
  const appliedDiff = base.appliedDiff ?? FrozenArgs.freeze({ result, field: "appliedDiff" });
  const postWriteVerify =
    base.postWriteVerify ?? FrozenArgs.freeze({ result, field: "postWriteVerify" });
  const common = {
    writeId: base.writeId,
    action: base.action,
    target: base.target,
    idempotencyKey: base.idempotencyKey,
    proposedDiff: base.proposedDiff,
    approvalToken: base.approvalToken,
    ...(base.approvedAt === undefined ? {} : { approvedAt: base.approvedAt }),
  };

  switch (result) {
    case "applied":
      return {
        ...common,
        result,
        appliedDiff,
        postWriteVerify,
      };
    case "rejected":
      return {
        ...common,
        result,
        appliedDiff: null,
        postWriteVerify: null,
        failureMetadata: { code: "policy_denied", retryable: false },
      };
    case "partial":
      return {
        ...common,
        result,
        appliedDiff,
        postWriteVerify: null,
        failureMetadata: { code: "verification_failed", retryable: true, retryAfterMs: 5000 },
      };
    case "rollback":
      return {
        ...common,
        result,
        appliedDiff,
        postWriteVerify: null,
        failureMetadata: { code: "rolled_back", retryable: false },
      };
  }
}

function receiptStatusForWriteResult(result: WriteResult): ReceiptStatus {
  return result === "applied" ? "ok" : "error";
}

function unknownKeyArbitrary(keys: ReadonlySet<string>): fc.Arbitrary<string> {
  return fc.string({ minLength: 1 }).filter((key) => !keys.has(key));
}

function recordWithKnownKeys(keys: ReadonlySet<string>): Record<string, unknown> {
  const record = Object.create(null) as Record<string, unknown>;
  for (const key of keys) {
    Object.defineProperty(record, key, {
      value: "known",
      enumerable: true,
      configurable: true,
    });
  }
  return record;
}

function receiptCoSignToken(token: SignedApprovalToken): ReceiptCoSignToken {
  if (token.claim.kind !== "receipt_co_sign" || token.scope.claimKind !== "receipt_co_sign") {
    throw new Error("fixture token must be receipt_co_sign");
  }
  return token as ReceiptCoSignToken;
}

function signedApprovalTokenFixture(input: {
  readonly receiptId: ReturnType<typeof asReceiptId>;
  readonly writeId?: ReturnType<typeof asWriteId> | undefined;
  readonly frozenArgsHash: Sha256Hex;
}): SignedApprovalToken {
  const claimId = asApprovalClaimId("claim_01");
  return {
    schemaVersion: 1,
    tokenId: asApprovalTokenId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
    claim: {
      schemaVersion: 1,
      claimId,
      kind: "receipt_co_sign",
      receiptId: input.receiptId,
      ...(input.writeId === undefined ? {} : { writeId: input.writeId }),
      frozenArgsHash: input.frozenArgsHash,
      riskClass: "high",
    },
    scope: {
      mode: "single_use",
      claimId,
      claimKind: "receipt_co_sign",
      role: "approver",
      maxUses: 1,
      receiptId: input.receiptId,
      ...(input.writeId === undefined ? {} : { writeId: input.writeId }),
      frozenArgsHash: input.frozenArgsHash,
    },
    notBefore: asTimestampMs(1_778_262_060_000),
    expiresAt: asTimestampMs(1_778_263_800_000),
    issuedTo: asAgentId("agent_alpha"),
    signature: {
      credentialId: "Y3JlZGVudGlhbC0wMQ",
      authenticatorData: "YXV0aGVudGljYXRvci1kYXRh",
      clientDataJson: "Y2xpZW50LWRhdGE",
      signature: "YXNzZXJ0aW9uLXNpZ25hdHVyZQ",
      userHandle: "dXNlci0wMQ",
    },
  };
}

function validReceiptFixture(): ReceiptSnapshot {
  const receiptId = asReceiptId(RECEIPT_ID);
  const taskId = asTaskId(TASK_ID);
  const writeId = asWriteId("write_01");
  const toolInputs = FrozenArgs.freeze({ action: "summarize", entityId: "contact:1234" });
  const proposedDiff = FrozenArgs.freeze({
    after: { amount: 1500, stage: "qualified" },
    before: { amount: 1000, stage: "lead" },
  });
  const appliedDiff = FrozenArgs.freeze({
    amount: { from: 1000, to: 1500 },
    stage: { from: "lead", to: "qualified" },
  });
  const postWriteVerify = FrozenArgs.freeze({ amount: 1500, stage: "qualified" });
  const approvalToken = signedApprovalTokenFixture({
    receiptId,
    writeId,
    frozenArgsHash: proposedDiff.hash,
  });

  return {
    id: receiptId,
    agentSlug: asAgentSlug("sam_agent"),
    taskId,
    triggerKind: "human_message",
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    finishedAt: new Date("2026-05-08T18:05:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("openai"),
    model: "gpt-5.2",
    promptHash: sha256Hex("prompt:v1"),
    toolManifest: sha256Hex("tool-manifest:v1"),
    toolCalls: [
      {
        toolId: asToolCallId("tool_01"),
        toolName: "hubspot.contacts.read",
        inputs: toolInputs,
        output: SanitizedString.fromUnknown("Fetched HubSpot contact #1234"),
        startedAt: new Date("2026-05-08T18:00:01.000Z"),
        finishedAt: new Date("2026-05-08T18:00:02.000Z"),
        status: "ok",
        error: SanitizedString.fromUnknown(""),
      },
    ],
    approvals: [
      {
        approvalId: asApprovalId("approval_01"),
        role: "approver",
        decision: "approve",
        signedToken: approvalToken,
        tokenVerdict: {
          status: "valid",
          verifiedAt: new Date("2026-05-08T18:01:00.500Z"),
        },
        decidedAt: new Date("2026-05-08T18:01:00.000Z"),
      },
    ],
    filesChanged: [
      {
        path: "docs/brief.md",
        mode: "modified",
        beforeHash: sha256Hex("docs/brief.md:before"),
        afterHash: sha256Hex("docs/brief.md:after"),
        linesAdded: 12,
        linesRemoved: 3,
      },
    ],
    commits: [
      {
        sha: "abc123def456",
        message: SanitizedString.fromUnknown("docs: update meeting brief"),
        author: "Fran",
        authorEmail: "fran@example.com",
        parentSha: "def456abc123",
        signed: true,
      },
    ],
    sourceReads: [
      {
        provider: "hubspot",
        entityType: "contact",
        entityId: "1234",
        fetchedAt: new Date("2026-05-08T18:00:01.000Z"),
        hash: sha256Hex("hubspot-contact-1234"),
        citation: "HubSpot contact #1234 fetched at 2026-05-08T18:00:01.000Z",
        rawRef: "hubspot://contacts/1234",
      },
    ],
    writes: [
      {
        writeId,
        action: "hubspot.deals.update",
        target: "deal:5678",
        idempotencyKey: asIdempotencyKey("receipt-01ARZ3NDEKTSV4RRFFQ69G5FAV-write-1"),
        proposedDiff,
        appliedDiff,
        approvalToken,
        approvedAt: new Date("2026-05-08T18:01:01.000Z"),
        result: "applied",
        postWriteVerify,
      },
    ],
    inputTokens: 1200,
    outputTokens: 345,
    cacheReadTokens: 50,
    cacheCreationTokens: 25,
    costUsd: 0.0425,
    finalMessage: SanitizedString.fromUnknown("Done. Updated the deal and wrote the brief."),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [
      {
        store: "notebook",
        slug: "meeting-briefs",
        hash: sha256Hex("notebook-write"),
        citation: "Notebook meeting-briefs updated",
      },
    ],
    wikiWrites: [
      {
        store: "wiki",
        slug: "accounts/acme",
        hash: sha256Hex("wiki-write"),
        citation: "Wiki account page updated",
      },
    ],
    worktreePath: "/Users/fd/src/nex/wuphf/.worktrees/task-01",
    gitHeadStart: "abc123def456",
    gitHeadEnd: "fed654cba321",
    schemaVersion: 1,
  };
}

function shuffledReceiptFixture(): ReceiptSnapshot {
  const receiptId = asReceiptId(RECEIPT_ID);
  const taskId = asTaskId(TASK_ID);
  const writeId = asWriteId("write_01");
  const toolInputs = FrozenArgs.freeze({ entityId: "contact:1234", action: "summarize" });
  const proposedDiff = FrozenArgs.freeze({
    before: { stage: "lead", amount: 1000 },
    after: { stage: "qualified", amount: 1500 },
  });
  const appliedDiff = FrozenArgs.freeze({
    stage: { to: "qualified", from: "lead" },
    amount: { to: 1500, from: 1000 },
  });
  const postWriteVerify = FrozenArgs.freeze({ stage: "qualified", amount: 1500 });
  const approvalToken = signedApprovalTokenFixture({
    receiptId,
    writeId,
    frozenArgsHash: proposedDiff.hash,
  });

  return {
    schemaVersion: 1,
    gitHeadEnd: "fed654cba321",
    gitHeadStart: "abc123def456",
    worktreePath: "/Users/fd/src/nex/wuphf/.worktrees/task-01",
    wikiWrites: [
      {
        citation: "Wiki account page updated",
        hash: sha256Hex("wiki-write"),
        slug: "accounts/acme",
        store: "wiki",
      },
    ],
    notebookWrites: [
      {
        citation: "Notebook meeting-briefs updated",
        hash: sha256Hex("notebook-write"),
        slug: "meeting-briefs",
        store: "notebook",
      },
    ],
    error: SanitizedString.fromUnknown(""),
    finalMessage: SanitizedString.fromUnknown("Done. Updated the deal and wrote the brief."),
    costUsd: 0.0425,
    cacheCreationTokens: 25,
    cacheReadTokens: 50,
    outputTokens: 345,
    inputTokens: 1200,
    writes: [
      {
        postWriteVerify,
        result: "applied",
        approvedAt: new Date("2026-05-08T18:01:01.000Z"),
        approvalToken,
        appliedDiff,
        proposedDiff,
        idempotencyKey: asIdempotencyKey("receipt-01ARZ3NDEKTSV4RRFFQ69G5FAV-write-1"),
        writeId,
        target: "deal:5678",
        action: "hubspot.deals.update",
      },
    ],
    sourceReads: [
      {
        rawRef: "hubspot://contacts/1234",
        citation: "HubSpot contact #1234 fetched at 2026-05-08T18:00:01.000Z",
        hash: sha256Hex("hubspot-contact-1234"),
        fetchedAt: new Date("2026-05-08T18:00:01.000Z"),
        entityId: "1234",
        entityType: "contact",
        provider: "hubspot",
      },
    ],
    commits: [
      {
        signed: true,
        parentSha: "def456abc123",
        authorEmail: "fran@example.com",
        author: "Fran",
        message: SanitizedString.fromUnknown("docs: update meeting brief"),
        sha: "abc123def456",
      },
    ],
    filesChanged: [
      {
        linesRemoved: 3,
        linesAdded: 12,
        afterHash: sha256Hex("docs/brief.md:after"),
        beforeHash: sha256Hex("docs/brief.md:before"),
        mode: "modified",
        path: "docs/brief.md",
      },
    ],
    approvals: [
      {
        decidedAt: new Date("2026-05-08T18:01:00.000Z"),
        tokenVerdict: {
          verifiedAt: new Date("2026-05-08T18:01:00.500Z"),
          status: "valid",
        },
        signedToken: approvalToken,
        decision: "approve",
        role: "approver",
        approvalId: asApprovalId("approval_01"),
      },
    ],
    toolCalls: [
      {
        error: SanitizedString.fromUnknown(""),
        status: "ok",
        finishedAt: new Date("2026-05-08T18:00:02.000Z"),
        startedAt: new Date("2026-05-08T18:00:01.000Z"),
        output: SanitizedString.fromUnknown("Fetched HubSpot contact #1234"),
        inputs: toolInputs,
        toolName: "hubspot.contacts.read",
        toolId: asToolCallId("tool_01"),
      },
    ],
    toolManifest: sha256Hex("tool-manifest:v1"),
    promptHash: sha256Hex("prompt:v1"),
    model: "gpt-5.2",
    providerKind: asProviderKind("openai"),
    status: "ok",
    finishedAt: new Date("2026-05-08T18:05:00.000Z"),
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    triggerKind: "human_message",
    taskId,
    agentSlug: asAgentSlug("sam_agent"),
    id: receiptId,
  };
}

function fuzzReceiptPayload(): fc.Arbitrary<unknown> {
  return fc.oneof(fc.anything(), fc.constant(validReceiptFixture()), corruptReceiptArbitrary());
}

function corruptReceiptArbitrary(): fc.Arbitrary<unknown> {
  return fc.constantFrom("id", "status", "inputTokens").map((field) => {
    const corruptValue = field === "id" ? "not-a-ulid" : field === "status" ? "done" : "many";
    return { ...validReceiptFixture(), [field]: corruptValue };
  });
}
