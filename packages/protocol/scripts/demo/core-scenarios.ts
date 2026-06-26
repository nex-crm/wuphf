import {
  type AuditEventRecord,
  asIdempotencyKey,
  asReceiptId,
  canonicalJSON,
  computeAuditEventHash,
  FrozenArgs,
  GENESIS_PREV_HASH,
  INITIAL_VERIFIER_STATE,
  lsnFromV1Number,
  MAX_AUDIT_CHAIN_BATCH_SIZE,
  MAX_TOOL_CALLS_PER_RECEIPT,
  receiptFromJson,
  receiptToJson,
  SanitizedString,
  validateApprovalSubmitRequest,
  validateReceiptBudget,
  verifyChain,
  verifyChainIncremental,
} from "../../src/index.ts";
import { buildValidReceipt } from "./fixtures.ts";
import {
  expectBudgetFailure,
  expectChainResult,
  expectEqual,
  expectIncrementalResult,
  expectThrows,
  header,
  nonNull,
} from "./harness.ts";

function buildChain(n: number): AuditEventRecord[] {
  const out: AuditEventRecord[] = [];
  let prev = GENESIS_PREV_HASH;
  for (let i = 0; i < n; i++) {
    const partial: AuditEventRecord = {
      seqNo: lsnFromV1Number(i),
      timestamp: new Date(2026, 4, 8, 0, 0, i),
      prevHash: prev,
      eventHash: GENESIS_PREV_HASH,
      payload: { kind: "receipt_created", body: new TextEncoder().encode(`event-${i}`) },
    };
    const eventHash = computeAuditEventHash(partial);
    out.push({ ...partial, eventHash });
    prev = eventHash;
  }
  return out;
}

export function runCoreScenarios(): void {
  // ────────────────────────────────────────────────────────────────────────
  header(1, "FrozenArgs is content-addressed, key-order-independent");
  // ────────────────────────────────────────────────────────────────────────
  const argsA = FrozenArgs.freeze({ b: 2, a: 1 });
  const argsB = FrozenArgs.freeze({ a: 1, b: 2 });
  expectEqual("hash equality across key order", argsA.hash === argsB.hash, true);
  expectEqual("canonical JSON matches", argsA.canonicalJson, '{"a":1,"b":2}');

  // ────────────────────────────────────────────────────────────────────────
  header(2, "SanitizedString strips weaponized invisible code points");
  // ────────────────────────────────────────────────────────────────────────
  expectEqual(
    "U+180E (Mongolian Vowel Separator) stripped",
    SanitizedString.fromUnknown("ad᠎min").value,
    "admin",
  );
  expectEqual("U+2060 (Word Joiner) stripped", SanitizedString.fromUnknown("ev⁠il").value, "evil");
  expectEqual(
    "U+202E (Right-to-Left Override) stripped",
    SanitizedString.fromUnknown("evil‮txt.exe").value,
    "eviltxt.exe",
  );
  expectEqual(
    "U+FB01 (ﬁ ligature) NFKC-normalizes to 'fi'",
    SanitizedString.fromUnknown("ﬁle").value,
    "file",
  );

  // ────────────────────────────────────────────────────────────────────────
  header(3, "SanitizedString rejects untrusted graph BEFORE side-effects fire");
  // ────────────────────────────────────────────────────────────────────────
  let getterFired = false;
  const adversarialInput = {
    get tricky() {
      getterFired = true;
      return "should-never-be-read";
    },
  };
  expectThrows(
    () => SanitizedString.fromUnknown(adversarialInput),
    /accessor|getter|toJSON|non-plain/i,
  );
  expectEqual("getter side-effect did NOT fire", getterFired, false);
  expectThrows(
    () => SanitizedString.fromUnknown({ toJSON: () => "spoofed" }),
    /toJSON|accessor|non-plain/i,
  );
  expectThrows(
    () => SanitizedString.fromUnknown(new Map([["k", "v"]])),
    /Map|non-plain|class instance/i,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(4, "Canonical JSON rejects prototype pollution attempts");
  // ────────────────────────────────────────────────────────────────────────
  expectThrows(
    () => SanitizedString.fromUnknown(JSON.parse('{"__proto__":{"polluted":true},"ok":1}')),
    /__proto__|forbidden|prototype/,
  );
  expectThrows(
    () => canonicalJSON(JSON.parse('{"__proto__":{"polluted":true},"ok":1}')),
    /canonicalJSON: forbidden key "__proto__"/,
  );
  expectThrows(
    () => SanitizedString.fromUnknown(JSON.parse('{"constructor":{"polluted":true}}')),
    /constructor|forbidden|prototype/,
  );
  expectThrows(
    () => FrozenArgs.freeze(JSON.parse('{"prototype":{"polluted":true}}')),
    /canonicalJSON: forbidden key "prototype"/,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(5, "Audit chain catches tampering with a typed failure code");
  // ────────────────────────────────────────────────────────────────────────
  expectChainResult("clean 5-record chain", verifyChain(buildChain(5)), "ok");

  const tamperedChain = buildChain(5);
  const recordToTamper = nonNull(tamperedChain[2], "tamperedChain[2]");
  tamperedChain[2] = {
    ...recordToTamper,
    payload: { kind: "receipt_finalized", body: new TextEncoder().encode("malicious") },
  };
  expectChainResult(
    "chain with tampered payload at seq 2",
    verifyChain(tamperedChain),
    "event_hash_mismatch",
  );

  const unknownKindRecord = nonNull(buildChain(1)[0], "unknownKindRecord");
  expectChainResult(
    "chain with unknown payload kind",
    verifyChain([
      {
        ...unknownKindRecord,
        payload: {
          ...unknownKindRecord.payload,
          kind: "made_up_kind" as AuditEventRecord["payload"]["kind"],
        },
      },
    ]),
    "serialization_threw",
  );

  const oversizedBodyRecord = nonNull(buildChain(1)[0], "oversizedBodyRecord");
  expectChainResult(
    "chain with oversized audit event body",
    verifyChain([
      {
        ...oversizedBodyRecord,
        payload: {
          ...oversizedBodyRecord.payload,
          body: new Uint8Array(1 * 1024 * 1024 + 1),
        },
      },
    ]),
    "serialization_threw",
  );

  const sparseChain = buildChain(3);
  Reflect.deleteProperty(sparseChain, "1");
  expectChainResult(
    "chain with deleted record at seq 1",
    verifyChain(sparseChain),
    "missing_record",
  );

  // ────────────────────────────────────────────────────────────────────────
  header(6, "Audit chain verifies incrementally without retaining the full log");
  // ────────────────────────────────────────────────────────────────────────
  const incrementalChain = buildChain(12);
  const firstIncrementalBatch = verifyChainIncremental(
    INITIAL_VERIFIER_STATE,
    incrementalChain.slice(0, 5),
  );
  expectIncrementalResult("first 5-record batch", firstIncrementalBatch, "ok");
  if (firstIncrementalBatch.ok) {
    const secondIncrementalBatch = verifyChainIncremental(
      firstIncrementalBatch.state,
      incrementalChain.slice(5),
    );
    expectIncrementalResult("remaining 7-record batch", secondIncrementalBatch, "ok");
    if (secondIncrementalBatch.ok) {
      const allAtOnce = verifyChain(incrementalChain);
      expectEqual(
        "incremental last hash matches verifyChain",
        allAtOnce.ok && !allAtOnce.empty
          ? secondIncrementalBatch.state.expectedPrev === allAtOnce.lastEventHash
          : false,
        true,
      );
    }
  }
  expectIncrementalResult(
    "oversized verifier batch",
    verifyChainIncremental(
      INITIAL_VERIFIER_STATE,
      new Array<AuditEventRecord>(MAX_AUDIT_CHAIN_BATCH_SIZE + 1),
    ),
    "batch_too_large",
  );

  // ────────────────────────────────────────────────────────────────────────
  header(7, "Receipt budgets reject runaway task fanout");
  // ────────────────────────────────────────────────────────────────────────
  const budgetFixture = buildValidReceipt();
  const runawayToolCall = nonNull(budgetFixture.toolCalls[0], "budgetFixture.toolCalls[0]");
  expectEqual("normal receipt budget", validateReceiptBudget(budgetFixture), { ok: true });
  expectBudgetFailure(
    "toolCalls length cap",
    validateReceiptBudget({
      ...budgetFixture,
      toolCalls: Array.from({ length: MAX_TOOL_CALLS_PER_RECEIPT + 1 }, () => runawayToolCall),
    }),
    /toolCalls.*exceeds budget/,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(8, "FrozenArgs JSON envelope rejects smuggled siblings");
  // ────────────────────────────────────────────────────────────────────────
  const validReceiptJson = receiptToJson(buildValidReceipt());
  const parsed = JSON.parse(validReceiptJson) as {
    toolCalls: Array<{ inputs: Record<string, unknown> }>;
  };
  const firstToolCall = nonNull(parsed.toolCalls[0], "parsed.toolCalls[0]");
  firstToolCall.inputs = { ...firstToolCall.inputs, evilShadow: "smuggled" };
  expectThrows(() => receiptFromJson(JSON.stringify(parsed)), /evilShadow.*not allowed/);

  // ────────────────────────────────────────────────────────────────────────
  header(9, "ExternalWrite per-state invariants");
  // ────────────────────────────────────────────────────────────────────────
  const tamperedApplied = JSON.parse(receiptToJson(buildValidReceipt())) as {
    writes: Array<Record<string, unknown>>;
  };
  const firstWrite = nonNull(tamperedApplied.writes[0], "tamperedApplied.writes[0]");
  firstWrite.appliedDiff = null;
  expectThrows(
    () => receiptFromJson(JSON.stringify(tamperedApplied)),
    /appliedDiff.*null is invalid for state "applied"/,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(10, "Approval token writeId binding catches cross-write authorization");
  // ────────────────────────────────────────────────────────────────────────
  const writeIdMismatch = JSON.parse(receiptToJson(buildValidReceipt())) as {
    writes: Array<Record<string, unknown>>;
  };
  const writeWithToken = nonNull(writeIdMismatch.writes[0], "writeIdMismatch.writes[0]");
  const tokenWithWrongWriteId = JSON.parse(JSON.stringify(writeWithToken.approvalToken)) as {
    claim: Record<string, unknown>;
  };
  tokenWithWrongWriteId.claim.writeId = "write_wrong_target";
  writeWithToken.approvalToken = tokenWithWrongWriteId;
  expectThrows(() => receiptFromJson(JSON.stringify(writeIdMismatch)), /writeId.*must match/);

  // ────────────────────────────────────────────────────────────────────────
  header(11, "ApprovalSubmitRequest cross-field validator (IPC layer)");
  // ────────────────────────────────────────────────────────────────────────
  const validReceipt = buildValidReceipt();
  const validToken = nonNull(validReceipt.approvals[0], "validReceipt.approvals[0]").signedToken;
  const goodReq = {
    receiptId: validReceipt.id,
    approvalToken: validToken,
    idempotencyKey: asIdempotencyKey("submit-01"),
  };
  expectEqual("matched receiptId/claim", validateApprovalSubmitRequest(goodReq), { ok: true });

  const badReq = {
    ...goodReq,
    receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY"),
  };
  expectEqual("mismatched receiptId/claim", validateApprovalSubmitRequest(badReq), {
    ok: false,
    reason: "receiptId must match approvalToken.claim.receiptId",
  });

  const emptyKeyReq = { ...goodReq, idempotencyKey: "" };
  expectEqual("empty idempotencyKey rejected", validateApprovalSubmitRequest(emptyKeyReq), {
    ok: false,
    reason: "idempotencyKey must match /^[A-Za-z0-9_-]{1,128}$/",
  });

  const missingTokenIdReq = {
    ...goodReq,
    approvalToken: {
      ...validToken,
      tokenId: undefined,
    },
  };
  expectEqual("missing tokenId rejected", validateApprovalSubmitRequest(missingTokenIdReq), {
    ok: false,
    reason: "approvalToken/tokenId: is required",
  });

  // ────────────────────────────────────────────────────────────────────────
  header(12, "EventLsn safe-integer bound (writer ⟷ verifier round-trip)");
  // ────────────────────────────────────────────────────────────────────────
  expectThrows(() => lsnFromV1Number(Number.MAX_SAFE_INTEGER + 1), /non-negative safe integer/);
  expectThrows(() => lsnFromV1Number(-1), /non-negative safe integer/);
  expectEqual(
    "MAX_SAFE_INTEGER accepted",
    lsnFromV1Number(Number.MAX_SAFE_INTEGER),
    `v1:${Number.MAX_SAFE_INTEGER}`,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(13, "Bonus: golden eventHash literal (cross-language wire contract)");
  // ────────────────────────────────────────────────────────────────────────
  const goldenRecord: AuditEventRecord = {
    seqNo: lsnFromV1Number(0),
    timestamp: new Date("2026-05-08T00:00:00.000Z"),
    prevHash: GENESIS_PREV_HASH,
    eventHash: GENESIS_PREV_HASH,
    payload: { kind: "boot_marker", body: new TextEncoder().encode("boot") },
  };
  expectEqual(
    "boot_marker eventHash matches golden vector",
    computeAuditEventHash(goldenRecord),
    "e27134d1b1641fb13747d9fac78aecc90d9d1385d04bfeea4a8a596fdb6101bb",
  );
  console.log(
    "  \x1b[2mA Go/Rust/Python verifier implementing the wire contract MUST produce the same hash.\x1b[0m",
  );
  console.log(
    "  \x1b[2mRun testdata/verifier-reference.go to verify cross-language portability.\x1b[0m",
  );

  // ────────────────────────────────────────────────────────────────────────
  header(14, "FrozenArgs.fromCanonical rehydrates canonical JSON");
  // ────────────────────────────────────────────────────────────────────────
  const rehydratedArgs = FrozenArgs.fromCanonical(argsA.canonicalJson);
  expectEqual(
    "fromCanonical preserves canonical JSON",
    rehydratedArgs.canonicalJson,
    argsA.canonicalJson,
  );
  expectEqual("fromCanonical preserves hash", rehydratedArgs.hash, argsA.hash);
  expectThrows(
    () => FrozenArgs.fromCanonical('{"b":2,"a":1}'),
    /FrozenArgs\.fromCanonical: input is not canonical-form/,
  );
}

export function runFrozenNfkcScenario(): void {
  // ────────────────────────────────────────────────────────────────────────
  header(32, "SanitizedString normalizes NFKC against frozen Unicode 15.1 tables");
  // ────────────────────────────────────────────────────────────────────────
  // NFKC normalization is version-pinned (src/nfkc.ts -> frozen tables), not
  // the host runtime's String.prototype.normalize, so signed bytes stay stable
  // across runtimes with different Unicode versions.
  expectEqual(
    "decomposed 'A + combining ring' composes to 'Å'",
    SanitizedString.fromUnknown("Å").value,
    "Å",
  );
  expectEqual(
    "precomposed 'Å' is byte-identical to the decomposed form",
    SanitizedString.fromUnknown("Å").value,
    SanitizedString.fromUnknown("Å").value,
  );
  expectEqual(
    "recursive: U+1E09 (c+cedilla+acute) round-trips through frozen NFKC",
    SanitizedString.fromUnknown("ḉ").value,
    "ḉ",
  );
  expectEqual(
    "U+A7F1 stays U+A7F1 — frozen at Unicode 15.1, not folded to 'S' (17.0)",
    SanitizedString.fromUnknown("x꟱y").value,
    "x꟱y",
  );
}
