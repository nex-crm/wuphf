import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { FrozenArgs } from "../src/frozen-args.ts";
import {
  asAgentSlug,
  asApprovalId,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asToolCallId,
  asWriteId,
  isReceiptSnapshot,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  validateReceipt,
} from "../src/receipt.ts";
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
      /expiresAt=2026-05-08T18:00:59.000Z issuedAt=2026-05-08T18:01:00.000Z/,
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
        idempotencyKey: "receipt-01ARZ3NDEKTSV4RRFFQ69G5FAV-write-1",
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
        idempotencyKey: "receipt-01ARZ3NDEKTSV4RRFFQ69G5FAV-write-1",
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
