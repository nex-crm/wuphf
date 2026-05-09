import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  type ApprovalClaims,
  type AuditEventRecord,
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
  computeAuditEventHash,
  type ExternalWrite,
  type FileChange,
  FrozenArgs,
  GENESIS_PREV_HASH,
  INITIAL_VERIFIER_STATE,
  lsnFromV1Number,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  MAX_AUDIT_CHAIN_BATCH_SIZE,
  MAX_AUDIT_EVENT_BODY_BYTES,
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
  validateAuditEventBodyBudget,
  validateFrozenArgsBudget,
  validateReceipt,
  validateReceiptBudget,
  validateSanitizedStringBudget,
  verifyChainIncremental,
} from "../src/index.ts";

describe("resource budgets", () => {
  it("assertWithinBudget accepts the edge and rejects one over", () => {
    expect(() => assertWithinBudget(1024, 1024, "test budget")).not.toThrow();
    expect(() => assertWithinBudget(1025, 1024, "test budget")).toThrow(
      /test budget exceeds budget: 1025 > 1024/,
    );
  });

  it("assertWithinBudget rejects invalid budget parameters", () => {
    expect(() => assertWithinBudget(1, Number.NaN, "test budget")).toThrow(
      /test budget budget must be a non-negative finite number/,
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

  it("rejects receipt string payloads that exceed the serialized budget floor", () => {
    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      triggerRef: "x".repeat(MAX_RECEIPT_BYTES + 1),
    });

    expectBudgetRejection(result, /receipt string payload bytes.*10485761 > 10485760/);
  });

  it("rejects cyclic receipt payloads without walking forever", () => {
    const cyclic = { ...validReceiptFixture() } as ReceiptSnapshot & { self?: unknown };
    cyclic.self = cyclic;

    const result = validateReceiptBudget(cyclic);

    expect(result).toEqual({
      ok: false,
      reason: "receipt serialized bytes: receipt contains a cycle",
    });
  });

  it("reports non-JSON receipt payloads as serialization failures", () => {
    const result = validateReceiptBudget({
      ...validReceiptFixture(),
      inputTokens: 1n,
    } as unknown as ReceiptSnapshot);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toMatch(/receipt serialized bytes:.*BigInt/);
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

  it("validateReceipt returns the budget reason before field errors", () => {
    const result = validateReceipt({
      ...receiptWithSerializedBytes(MAX_RECEIPT_BYTES + 1),
      status: "not-a-status",
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors).toHaveLength(1);
      expect(result.errors[0]).toEqual({
        path: "",
        message: expect.stringMatching(/receipt serialized bytes.*exceeds budget/),
      });
    }
  });

  it("documents that receipt budget validation assumes plain-data inputs", () => {
    // Hostile wire input goes through receiptFromJson first; validateReceiptBudget
    // is scoped to typed receipts or JSON.parse output without accessors/toJSON.
    let toJsonGetterInvoked = false;
    const hostileReceipt = attachHostileToJson({ ...validReceiptFixture() }, () => {
      toJsonGetterInvoked = true;
    });
    const plainDataReceipt = JSON.parse(receiptToJson(validReceiptFixture())) as ReceiptSnapshot;

    expect(Object.getOwnPropertyDescriptor(hostileReceipt, "toJSON")?.enumerable).toBe(true);
    expect(validateReceiptBudget(plainDataReceipt)).toEqual({ ok: true });
    expect(toJsonGetterInvoked).toBe(false);
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

  it("counts multibyte UTF-8 strings against byte budgets", () => {
    expect(
      validateSanitizedStringBudget(
        forgedSanitizedString("\u00a2".repeat(MAX_SANITIZED_STRING_BYTES / 2)),
      ),
    ).toEqual({ ok: true });
    expectBudgetRejection(
      validateSanitizedStringBudget(
        forgedSanitizedString(`${"\u00a2".repeat(MAX_SANITIZED_STRING_BYTES / 2)}x`),
      ),
      /SanitizedString value bytes.*1048577 > 1048576/,
    );

    expect(
      validateSanitizedStringBudget(
        forgedSanitizedString("\ud83d\ude00".repeat(MAX_SANITIZED_STRING_BYTES / 4)),
      ),
    ).toEqual({ ok: true });
    expectBudgetRejection(
      validateSanitizedStringBudget(
        forgedSanitizedString(`${"\ud83d\ude00".repeat(MAX_SANITIZED_STRING_BYTES / 4)}x`),
      ),
      /SanitizedString value bytes.*1048577 > 1048576/,
    );

    expect(validateSanitizedStringBudget(forgedSanitizedString("\ud800"))).toEqual({ ok: true });
  });

  it("bounds audit event body bytes at the edge", () => {
    expect(validateAuditEventBodyBudget(new Uint8Array(MAX_AUDIT_EVENT_BODY_BYTES))).toEqual({
      ok: true,
    });

    const result = validateAuditEventBodyBudget(new Uint8Array(MAX_AUDIT_EVENT_BODY_BYTES + 1));

    expectBudgetRejection(result, /MAX_AUDIT_EVENT_BODY_BYTES.*1048577 > 1048576/);
  });

  it("bounds audit chain batches at the edge", () => {
    const atCap = auditChainOfLength(MAX_AUDIT_CHAIN_BATCH_SIZE);
    const atCapResult = verifyChainIncremental(INITIAL_VERIFIER_STATE, atCap);

    expect(atCapResult.ok).toBe(true);

    const firstRecord = atCap[0];
    if (firstRecord === undefined) throw new Error("audit batch fixture must be non-empty");
    const overCapResult = verifyChainIncremental(INITIAL_VERIFIER_STATE, [...atCap, firstRecord]);

    expect(overCapResult.ok).toBe(false);
    if (!overCapResult.ok) {
      expect(overCapResult.reason).toBe(
        `batch too large: ${MAX_AUDIT_CHAIN_BATCH_SIZE + 1} > ${MAX_AUDIT_CHAIN_BATCH_SIZE}`,
      );
    }
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

  it("does not reject zero or negative approval token lifetimes — that is the per-field validator's job", () => {
    // The lower bound (`expiresAt > issuedAt`) is enforced at the proper
    // per-field path by `validateApprovalClaims` (receipt-validator.ts) and
    // `validateApprovalClaimsShape` (ipc.ts), where it produces a path-
    // anchored error like `/approvals/N/signedToken/claims/expiresAt: must
    // be after issuedAt`. The budget validator here owns only the upper
    // bound (the 30-minute cap). Duplicating the lower bound would
    // short-circuit the per-field error path with a less actionable
    // top-level message.
    const issuedAt = new Date("2026-05-08T18:00:00.000Z");
    expect(
      validateApprovalTokenLifetime({
        ...approvalClaimsFixture(),
        issuedAt,
        expiresAt: issuedAt,
      }),
    ).toEqual({ ok: true });
    expect(
      validateApprovalTokenLifetime({
        ...approvalClaimsFixture(),
        issuedAt,
        expiresAt: new Date(issuedAt.getTime() - 1),
      }),
    ).toEqual({ ok: true });
  });

  it("rejects approval token lifetimes with invalid dates", () => {
    const result = validateApprovalTokenLifetime({
      ...approvalClaimsFixture(),
      expiresAt: new Date(Number.NaN),
    });

    expect(result).toEqual({ ok: false, reason: "approval token lifetime must be finite" });
  });

  it("direct budget helpers do not invoke hostile toJSON getters", () => {
    fc.assert(
      fc.property(
        fc.string({ maxLength: 256 }),
        fc.uint8Array({ minLength: 0, maxLength: 256 }),
        (text, bodyBytes) => {
          let toJsonGetterInvoked = false;
          const markToJsonGetterInvoked = (): void => {
            toJsonGetterInvoked = true;
          };
          const issuedAt = new Date("2026-05-08T18:00:00.000Z");
          const expiresAt = new Date("2026-05-08T18:30:00.000Z");

          expect(
            validateFrozenArgsBudget(
              attachHostileToJson(forgedFrozenArgs(text), markToJsonGetterInvoked),
            ),
          ).toEqual({ ok: true });
          expect(
            validateSanitizedStringBudget(
              attachHostileToJson(forgedSanitizedString(text), markToJsonGetterInvoked),
            ),
          ).toEqual({ ok: true });
          expect(
            validateAuditEventBodyBudget(
              attachHostileToJson(new Uint8Array(bodyBytes), markToJsonGetterInvoked),
            ),
          ).toEqual({ ok: true });
          expect(
            validateApprovalTokenLifetime(
              attachHostileToJson(
                { ...approvalClaimsFixture(), issuedAt, expiresAt },
                markToJsonGetterInvoked,
              ),
            ),
          ).toEqual({ ok: true });
          expect(toJsonGetterInvoked).toBe(false);
        },
      ),
    );
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

function attachHostileToJson<T extends object>(value: T, onInvoke: () => void): T {
  Object.defineProperty(value, "toJSON", {
    configurable: true,
    enumerable: true,
    get(): () => unknown {
      onInvoke();
      return () => ({});
    },
  });
  return value;
}

function auditChainOfLength(length: number): AuditEventRecord[] {
  const records: AuditEventRecord[] = [];
  let prevHash = GENESIS_PREV_HASH;

  for (let i = 0; i < length; i++) {
    const partial: AuditEventRecord = {
      seqNo: lsnFromV1Number(i),
      timestamp: new Date("2026-05-08T18:00:00.000Z"),
      prevHash,
      eventHash: GENESIS_PREV_HASH,
      payload: {
        kind: "receipt_created",
        body: new Uint8Array(),
      },
    };
    const record = { ...partial, eventHash: computeAuditEventHash(partial) };
    records.push(record);
    prevHash = record.eventHash;
  }

  return records;
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
