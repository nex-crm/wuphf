import fc from "fast-check";
import { describe, expect, it, vi } from "vitest";
import {
  MAX_FROZEN_ARGS_BYTES,
  MAX_RECEIPT_BYTES,
  MAX_SANITIZED_STRING_BYTES,
  MAX_TOOL_CALLS_PER_RECEIPT,
} from "../src/budgets.ts";
import { FrozenArgs } from "../src/frozen-args.ts";
import {
  asAgentSlug,
  asApprovalId,
  asIdempotencyKey,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asToolCallId,
  asWriteId,
  type ExternalWrite,
  isReceiptSnapshot,
  PROVIDER_KIND_VALUES,
  type ReceiptSnapshot,
  type ReceiptStatus,
  receiptFromJson,
  receiptToJson,
  validateReceipt,
  type WriteResult,
} from "../src/receipt.ts";
import { WRITE_RESULT_VALUES } from "../src/receipt-literals.ts";
import { assertKnownKeys } from "../src/receipt-utils.ts";
import {
  APPROVAL_CLAIMS_KEYS,
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
import { sha256Hex } from "../src/sha256.ts";

const RECEIPT_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";

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
  { path: "/approvals/0/signedToken/claims", keys: APPROVAL_CLAIMS_KEYS },
  { path: "/approvals/0/signedToken", keys: SIGNED_APPROVAL_TOKEN_KEYS },
] as const satisfies readonly { path: string; keys: ReadonlySet<string> }[];

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

  it("accepts schemaVersion 1 as the only v1 wire schema until a migration codec ships", () => {
    const receipt = validReceiptFixture();

    expect(validateReceipt(receipt)).toEqual({ ok: true });
    expect(receiptFromJson(receiptToJson(receipt))).toEqual(receipt);
  });

  it("rejects schemaVersion 0 because v1 has no backward migration codec", () => {
    const runtimeReceipt: Record<string, unknown> = { ...validReceiptFixture(), schemaVersion: 0 };
    expectReceiptValidationError(runtimeReceipt, "/schemaVersion", /must be 1/);

    const wireReceipt = receiptJsonFixture();
    wireReceipt.schemaVersion = 0;
    expect(() => receiptFromJson(JSON.stringify(wireReceipt))).toThrow(
      /\/schemaVersion: must be 1/,
    );
  });

  it("rejects schemaVersion 2 because future schemas require a migration codec first", () => {
    const runtimeReceipt: Record<string, unknown> = { ...validReceiptFixture(), schemaVersion: 2 };
    expectReceiptValidationError(runtimeReceipt, "/schemaVersion", /must be 1/);

    const wireReceipt = receiptJsonFixture();
    wireReceipt.schemaVersion = 2;
    expect(() => receiptFromJson(JSON.stringify(wireReceipt))).toThrow(
      /\/schemaVersion: must be 1/,
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
        name: "signed token base64",
        mutate: (receipt) => {
          const approval = approvalWireAt(receipt, 0);
          setWireField(approvalWireSignedToken(approval), "signature", "");
        },
        message: /\/approvals\/0\/signedToken\/signature: must be a non-empty base64 string/,
      },
      {
        name: "high-risk assertion",
        mutate: (receipt) => {
          const approval = approvalWireAt(receipt, 0);
          setWireField(approvalWireClaims(approval), "webauthnAssertion", "");
        },
        message:
          /\/approvals\/0\/signedToken\/claims\/webauthnAssertion: must be a non-empty string/,
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
    const otherReceiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const wrongTokenApproval = {
      ...firstApproval,
      signedToken: {
        ...firstApproval.signedToken,
        claims: { ...firstApproval.signedToken.claims, receiptId: otherReceiptId },
      },
    };
    const tampered: ReceiptSnapshot = { ...fixture, approvals: [wrongTokenApproval] };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path === "/approvals/0/signedToken/claims/receiptId" && /must match/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("rejects external write whose approval token does not bind the proposedDiff hash", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = nonNull(firstWrite.approvalToken);
    const otherDiff = FrozenArgs.freeze({ unrelated: "diff" });
    const wrongHashWrite = {
      ...firstWrite,
      approvalToken: {
        ...approvalToken,
        claims: { ...approvalToken.claims, frozenArgsHash: otherDiff.hash },
      },
    };
    const tampered: ReceiptSnapshot = { ...fixture, writes: [wrongHashWrite] };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path === "/writes/0/approvalToken/claims/frozenArgsHash" &&
            /proposedDiff hash/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("rejects external write whose approval token is bound to a different receipt id", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = nonNull(firstWrite.approvalToken);
    const otherReceiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          approvalToken: {
            ...approvalToken,
            claims: { ...approvalToken.claims, receiptId: otherReceiptId },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/writes/0/approvalToken/claims/receiptId",
      /must match enclosing receipt id/,
    );
  });

  it("rejects forged proposedDiff hash using the locally re-derived hash", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = nonNull(firstWrite.approvalToken);
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
            claims: { ...approvalToken.claims, frozenArgsHash: forgedHash },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/writes/0/approvalToken/claims/frozenArgsHash",
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
    const approvalToken = nonNull(firstWrite.approvalToken);
    const wrongWrite = {
      ...firstWrite,
      approvalToken: {
        ...approvalToken,
        claims: { ...approvalToken.claims, writeId: asWriteId("write_wrong") },
      },
    };
    const tampered: ReceiptSnapshot = { ...fixture, writes: [wrongWrite] };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path === "/writes/0/approvalToken/claims/writeId" &&
            /must match this write's writeId/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("allows receipt-scoped approval token without writeId on an external write", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = nonNull(firstWrite.approvalToken);
    const { writeId: _writeId, ...receiptScopedClaims } = approvalToken.claims;
    const receiptScopedWrite = {
      ...firstWrite,
      approvalToken: { ...approvalToken, claims: receiptScopedClaims },
    };
    const receiptScoped: ReceiptSnapshot = { ...fixture, writes: [receiptScopedWrite] };

    expect(validateReceipt(receiptScoped)).toEqual({ ok: true });
  });

  it("rejects empty webauthnAssertion when riskClass is high", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...firstApproval.signedToken,
            claims: { ...firstApproval.signedToken.claims, webauthnAssertion: "" },
          },
        },
      ],
    };
    const result = validateReceipt(tampered);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(
        result.errors.some(
          (e) =>
            e.path.endsWith("/webauthnAssertion") && /non-empty.*high\/critical/.test(e.message),
        ),
      ).toBe(true);
    }
  });

  it("rejects empty webauthnAssertion when riskClass is critical", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...firstApproval.signedToken,
            claims: {
              ...firstApproval.signedToken.claims,
              riskClass: "critical",
              webauthnAssertion: "",
            },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/approvals/0/signedToken/claims/webauthnAssertion",
      /non-empty.*high\/critical/,
    );
  });

  it("rejects approval claims that expire before they are issued", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...firstApproval.signedToken,
            claims: {
              ...firstApproval.signedToken.claims,
              expiresAt: new Date("2026-05-08T18:00:59.000Z"),
            },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/approvals/0/signedToken/claims/expiresAt",
      /must be after issuedAt/,
    );
  });

  it("rejects approval claims that expire exactly when they are issued because expiry is strict-after", () => {
    const fixture = validReceiptFixture();
    const firstApproval = nonNull(fixture.approvals[0]);
    const issuedAt = firstApproval.signedToken.claims.issuedAt;
    const tampered: ReceiptSnapshot = {
      ...fixture,
      approvals: [
        {
          ...firstApproval,
          signedToken: {
            ...firstApproval.signedToken,
            claims: {
              ...firstApproval.signedToken.claims,
              expiresAt: issuedAt,
            },
          },
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/approvals/0/signedToken/claims/expiresAt",
      /must be after issuedAt/,
    );
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

  it("rejects external writes approved before their token was issued", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          approvedAt: new Date("2026-05-08T18:00:59.000Z"),
        },
      ],
    };

    expectReceiptValidationError(
      tampered,
      "/writes/0/approvedAt",
      /approvedAt=2026-05-08T18:00:59.000Z issuedAt=2026-05-08T18:01:00.000Z/,
    );
  });

  it("rejects external writes approved exactly when issued because approval is strict-after", () => {
    const fixture = validReceiptFixture();
    const firstWrite = nonNull(fixture.writes[0]);
    const approvalToken = nonNull(firstWrite.approvalToken);
    const tampered: ReceiptSnapshot = {
      ...fixture,
      writes: [
        {
          ...firstWrite,
          approvedAt: approvalToken.claims.issuedAt,
        },
      ],
    };

    expectReceiptValidationError(tampered, "/writes/0/approvedAt", /must be after issuedAt/);
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
        path: "/approvals/0/signedToken/claims",
        mutate: (receipt) => {
          const firstApproval = nonNull(validReceiptFixture().approvals[0]);
          setWireField(receipt, "approvals", [
            {
              ...firstApproval,
              signedToken: { ...firstApproval.signedToken, claims: "not-an-object" },
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

function approvalWireClaims(approval: Record<string, unknown>): Record<string, unknown> {
  const claims = wireField(approvalWireSignedToken(approval), "claims");
  if (!isWireRecord(claims)) {
    throw new Error("fixture approval missing claims");
  }
  return claims;
}

function writeWireAt(receipt: Record<string, unknown>, index: number): Record<string, unknown> {
  return wireRecordArrayItem(receipt, "writes", index);
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

function isWireRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function receiptJsonFixture(): Record<string, unknown> & {
  id?: unknown;
  providerKind?: unknown;
  schemaVersion?: unknown;
  finalMessage?: unknown;
  toolCalls: (Record<string, unknown> & { inputs?: unknown })[];
} {
  const receipt = JSON.parse(receiptToJson(validReceiptFixture())) as Record<string, unknown> & {
    id?: unknown;
    providerKind?: unknown;
    schemaVersion?: unknown;
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
  const approvalToken = {
    claims: {
      signerIdentity: "fran@example.com",
      role: "approver" as const,
      receiptId,
      writeId,
      frozenArgsHash: proposedDiff.hash,
      riskClass: "high" as const,
      issuedAt: new Date("2026-05-08T18:01:00.000Z"),
      expiresAt: new Date("2026-05-08T18:30:00.000Z"),
      webauthnAssertion: "webauthn-assertion",
    },
    algorithm: "ed25519" as const,
    signerKeyId: "key_ed25519_01",
    signature: "YXBwcm92YWwtdG9rZW4tc2lnbmF0dXJl",
  };

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
  const approvalToken = {
    signature: "YXBwcm92YWwtdG9rZW4tc2lnbmF0dXJl",
    signerKeyId: "key_ed25519_01",
    algorithm: "ed25519" as const,
    claims: {
      webauthnAssertion: "webauthn-assertion",
      expiresAt: new Date("2026-05-08T18:30:00.000Z"),
      issuedAt: new Date("2026-05-08T18:01:00.000Z"),
      riskClass: "high" as const,
      frozenArgsHash: proposedDiff.hash,
      writeId,
      receiptId,
      role: "approver" as const,
      signerIdentity: "fran@example.com",
    },
  };

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
