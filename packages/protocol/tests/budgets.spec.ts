import { describe, expect, it } from "vitest";
import {
  type ApprovalClaims,
  asAgentSlug,
  asApprovalId,
  asIdempotencyKey,
  asProviderKind,
  asReceiptId,
  assertWithinBudget,
  asTaskId,
  asToolCallId,
  asWriteId,
  type CommitRef,
  type ExternalWrite,
  type FileChange,
  FrozenArgs,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  MAX_FROZEN_ARGS_BYTES,
  MAX_RECEIPT_APPROVALS,
  MAX_RECEIPT_BYTES,
  MAX_RECEIPT_COMMITS,
  MAX_RECEIPT_FILES_CHANGED,
  MAX_RECEIPT_NOTEBOOK_WRITES,
  MAX_RECEIPT_SOURCE_READS,
  MAX_RECEIPT_WIKI_WRITES,
  MAX_RECEIPT_WRITES,
  MAX_SANITIZED_STRING_BYTES,
  MAX_TOOL_CALLS_PER_RECEIPT,
  type MemoryWriteRef,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  SanitizedString,
  type SignedApprovalToken,
  type SourceRead,
  sha256Hex,
  type ToolCall,
  validateApprovalTokenLifetime,
  validateFrozenArgsBudget,
  validateReceiptBudget,
  validateSanitizedStringBudget,
} from "../src/index.ts";

describe("resource budgets", () => {
  it("assertWithinBudget accepts the edge and rejects one over", () => {
    expect(() => assertWithinBudget(1024, 1024, "test budget")).not.toThrow();
    expect(() => assertWithinBudget(1025, 1024, "test budget")).toThrow(
      /test budget exceeds budget: 1025 > 1024/,
    );
  });

  it("bounds serialized receipt bytes at the edge", () => {
    expect(validateReceiptBudget(receiptWithSerializedBytes(MAX_RECEIPT_BYTES))).toEqual({
      ok: true,
    });

    const result = validateReceiptBudget(receiptWithSerializedBytes(MAX_RECEIPT_BYTES + 1));

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toMatch(/receipt serialized bytes.*exceeds budget/);
    }
  });

  it("receipt codecs reject oversized raw and typed receipts before semantic validation", () => {
    expect(() => receiptFromJson("x".repeat(MAX_RECEIPT_BYTES + 1))).toThrow(
      /receipt serialized bytes.*exceeds budget/,
    );
    expect(() => receiptToJson(receiptWithSerializedBytes(MAX_RECEIPT_BYTES + 1))).toThrow(
      /receipt serialized bytes.*exceeds budget/,
    );
  });

  it("bounds tool calls per receipt at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        toolCalls: repeat(itemToolCall(), MAX_TOOL_CALLS_PER_RECEIPT),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      toolCalls: repeat(itemToolCall(), MAX_TOOL_CALLS_PER_RECEIPT + 1),
    });

    expectBudgetRejection(result, /toolCalls.*1025 > 1024/);
  });

  it("bounds FrozenArgs canonical JSON bytes at the edge", () => {
    expect(validateFrozenArgsBudget(forgedFrozenArgs("x".repeat(MAX_FROZEN_ARGS_BYTES)))).toEqual({
      ok: true,
    });

    const result = validateFrozenArgsBudget(
      forgedFrozenArgs("x".repeat(MAX_FROZEN_ARGS_BYTES + 1)),
    );

    expectBudgetRejection(result, /FrozenArgs canonicalJson bytes.*1048577 > 1048576/);
  });

  it("bounds SanitizedString UTF-8 bytes at the edge", () => {
    expect(
      validateSanitizedStringBudget(forgedSanitizedString("x".repeat(MAX_SANITIZED_STRING_BYTES))),
    ).toEqual({ ok: true });

    const result = validateSanitizedStringBudget(
      forgedSanitizedString("x".repeat(MAX_SANITIZED_STRING_BYTES + 1)),
    );

    expectBudgetRejection(result, /SanitizedString value bytes.*1048577 > 1048576/);
  });

  it("bounds approval token lifetime at the edge", () => {
    const issuedAt = new Date("2026-05-08T18:00:00.000Z");
    expect(
      validateApprovalTokenLifetime({
        ...approvalClaimsFixture(),
        issuedAt,
        expiresAt: new Date(issuedAt.getTime() + MAX_APPROVAL_TOKEN_LIFETIME_MS),
      }),
    ).toEqual({ ok: true });

    const result = validateApprovalTokenLifetime({
      ...approvalClaimsFixture(),
      issuedAt,
      expiresAt: new Date(issuedAt.getTime() + MAX_APPROVAL_TOKEN_LIFETIME_MS + 1),
    });

    expectBudgetRejection(result, /approval token lifetime ms.*1800001 > 1800000/);
  });

  it("bounds filesChanged at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        filesChanged: repeat(itemFileChange(), MAX_RECEIPT_FILES_CHANGED),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      filesChanged: repeat(itemFileChange(), MAX_RECEIPT_FILES_CHANGED + 1),
    });

    expectBudgetRejection(result, /filesChanged.*10001 > 10000/);
  });

  it("bounds commits at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        commits: repeat(itemCommit(), MAX_RECEIPT_COMMITS),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      commits: repeat(itemCommit(), MAX_RECEIPT_COMMITS + 1),
    });

    expectBudgetRejection(result, /commits.*1025 > 1024/);
  });

  it("bounds external writes at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        writes: repeat(itemWrite(), MAX_RECEIPT_WRITES),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      writes: repeat(itemWrite(), MAX_RECEIPT_WRITES + 1),
    });

    expectBudgetRejection(result, /writes.*257 > 256/);
  });

  it("bounds approvals at the edge", () => {
    const approval = validReceiptFixture().approvals[0];
    if (approval === undefined) throw new Error("fixture must contain an approval");
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        approvals: repeat(approval, MAX_RECEIPT_APPROVALS),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      approvals: repeat(approval, MAX_RECEIPT_APPROVALS + 1),
    });

    expectBudgetRejection(result, /approvals.*65 > 64/);
  });

  it("bounds source reads at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        sourceReads: repeat(itemSourceRead(), MAX_RECEIPT_SOURCE_READS),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      sourceReads: repeat(itemSourceRead(), MAX_RECEIPT_SOURCE_READS + 1),
    });

    expectBudgetRejection(result, /sourceReads.*10001 > 10000/);
  });

  it("bounds notebook write refs at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        notebookWrites: repeat(itemMemoryWrite("notebook"), MAX_RECEIPT_NOTEBOOK_WRITES),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      notebookWrites: repeat(itemMemoryWrite("notebook"), MAX_RECEIPT_NOTEBOOK_WRITES + 1),
    });

    expectBudgetRejection(result, /notebookWrites.*10001 > 10000/);
  });

  it("bounds wiki write refs at the edge", () => {
    expect(
      validateReceiptBudget({
        ...validReceiptFixture(),
        wikiWrites: repeat(itemMemoryWrite("wiki"), MAX_RECEIPT_WIKI_WRITES),
      }),
    ).toEqual({ ok: true });

    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      wikiWrites: repeat(itemMemoryWrite("wiki"), MAX_RECEIPT_WIKI_WRITES + 1),
    });

    expectBudgetRejection(result, /wikiWrites.*10001 > 10000/);
  });
});

function expectBudgetRejection(
  result: { ok: true } | { ok: false; reason: string },
  expectedReason: RegExp,
): void {
  expect(result.ok).toBe(false);
  if (!result.ok) {
    expect(result.reason).toMatch(expectedReason);
  }
}

function repeat<T>(item: T, length: number): readonly T[] {
  return Array.from({ length }, () => item);
}

function receiptWithSerializedBytes(targetBytes: number): ReceiptSnapshot {
  const base = { ...validReceiptFixture(), triggerRef: "" };
  const baseBytes = JSON.stringify(base).length;
  const fillerLength = targetBytes - baseBytes;
  if (fillerLength < 0) {
    throw new Error(`fixture base receipt exceeds target bytes: ${baseBytes} > ${targetBytes}`);
  }
  const receipt = { ...base, triggerRef: "x".repeat(fillerLength) };
  const actualBytes = JSON.stringify(receipt).length;
  if (actualBytes !== targetBytes) {
    throw new Error(`fixture byte mismatch: expected ${targetBytes}, got ${actualBytes}`);
  }
  return receipt;
}

function forgedFrozenArgs(canonicalJson: string): FrozenArgs {
  const frozen = Object.create(FrozenArgs.prototype) as FrozenArgs;
  Object.assign(frozen, { canonicalJson, hash: sha256Hex(canonicalJson) });
  return frozen;
}

function forgedSanitizedString(value: string): SanitizedString {
  const sanitized = Object.create(SanitizedString.prototype) as SanitizedString;
  Object.assign(sanitized, { value });
  return sanitized;
}

function validReceiptFixture(): ReceiptSnapshot {
  return {
    id: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
    agentSlug: asAgentSlug("sam_agent"),
    taskId: asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW"),
    triggerKind: "human_message",
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    finishedAt: new Date("2026-05-08T18:05:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("openai"),
    model: "gpt-5.2",
    promptHash: sha256Hex("prompt:v1"),
    toolManifest: sha256Hex("tool-manifest:v1"),
    toolCalls: [itemToolCall()],
    approvals: [
      {
        approvalId: asApprovalId("approval_01"),
        role: "approver",
        decision: "approve",
        signedToken: signedApprovalTokenFixture(),
        tokenVerdict: { status: "valid", verifiedAt: new Date("2026-05-08T18:01:00.000Z") },
        decidedAt: new Date("2026-05-08T18:01:00.000Z"),
      },
    ],
    filesChanged: [itemFileChange()],
    commits: [itemCommit()],
    sourceReads: [itemSourceRead()],
    writes: [itemWrite()],
    inputTokens: 1200,
    outputTokens: 345,
    cacheReadTokens: 50,
    cacheCreationTokens: 25,
    costUsd: 0.0425,
    finalMessage: SanitizedString.fromUnknown("Done."),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [itemMemoryWrite("notebook")],
    wikiWrites: [itemMemoryWrite("wiki")],
    schemaVersion: 1,
  };
}

function itemToolCall(): ToolCall {
  return {
    toolId: asToolCallId("tool_01"),
    toolName: "hubspot.deals.update",
    inputs: FrozenArgs.freeze({ deal: "5678", action: "advance_stage" }),
    output: SanitizedString.fromUnknown("Stage advanced to qualified"),
    startedAt: new Date("2026-05-08T18:00:01.000Z"),
    finishedAt: new Date("2026-05-08T18:00:02.000Z"),
    status: "ok",
    error: SanitizedString.fromUnknown(""),
  };
}

function itemFileChange(): FileChange {
  return {
    path: "docs/brief.md",
    mode: "modified",
    beforeHash: sha256Hex("docs/brief.md:before"),
    afterHash: sha256Hex("docs/brief.md:after"),
    linesAdded: 12,
    linesRemoved: 3,
  };
}

function itemCommit(): CommitRef {
  return {
    sha: "abc123def456",
    message: SanitizedString.fromUnknown("docs: update meeting brief"),
    author: "Fran",
    authorEmail: "fran@example.com",
    parentSha: "def456abc123",
    signed: true,
  };
}

function itemSourceRead(): SourceRead {
  return {
    provider: "hubspot",
    entityType: "contact",
    entityId: "1234",
    fetchedAt: new Date("2026-05-08T18:00:01.000Z"),
    hash: sha256Hex("hubspot-contact-1234"),
    citation: "HubSpot contact #1234 fetched at 2026-05-08T18:00:01.000Z",
    rawRef: "hubspot://contacts/1234",
  };
}

function itemMemoryWrite(store: "notebook" | "wiki"): MemoryWriteRef {
  return {
    store,
    slug: store === "notebook" ? "meeting-briefs" : "accounts/acme",
    hash: sha256Hex(`${store}-write`),
    citation: `${store} write citation`,
  };
}

function itemWrite(): ExternalWrite {
  const token = signedApprovalTokenFixture();
  return {
    writeId: asWriteId("write_01"),
    action: "hubspot.deals.update",
    target: "deal:5678",
    idempotencyKey: asIdempotencyKey("write-01"),
    proposedDiff: FrozenArgs.freeze({ amount: { from: 1000, to: 1500 } }),
    appliedDiff: FrozenArgs.freeze({ stage: { from: "lead", to: "qualified" } }),
    approvalToken: token,
    approvedAt: new Date("2026-05-08T18:01:01.000Z"),
    result: "applied",
    postWriteVerify: FrozenArgs.freeze({ stage: "qualified" }),
  };
}

function signedApprovalTokenFixture(): SignedApprovalToken {
  return {
    claims: approvalClaimsFixture(),
    algorithm: "ed25519",
    signerKeyId: "key-01",
    signature: "ZmFrZS1zaWduYXR1cmUtZm9yLWRlbW8tcHVycG9zZXM=",
  };
}

function approvalClaimsFixture(): ApprovalClaims {
  return {
    signerIdentity: "fd@example.com",
    role: "approver",
    receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
    writeId: asWriteId("write_01"),
    frozenArgsHash: FrozenArgs.freeze({ amount: { from: 1000, to: 1500 } }).hash,
    riskClass: "high",
    issuedAt: new Date("2026-05-08T18:00:00.000Z"),
    expiresAt: new Date("2026-05-08T18:30:00.000Z"),
    webauthnAssertion: "webauthn-attestation-blob",
  };
}
