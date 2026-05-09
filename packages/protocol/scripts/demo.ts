// @wuphf/protocol — manually-runnable demo
//
// Usage:
//   bun run packages/protocol/scripts/demo.ts
//
// Walks through adversarial scenarios. Each prints:
//   • what we tried to do
//   • what the moat MUST do (expected behavior)
//   • what the moat actually did (so a human can spot a regression)
//
// This is the manual companion to the test suite — same invariants, but
// laid out as a narrative so a reviewer doesn't have to read fast-check
// arbitraries to be convinced the moat fires on the right inputs.

import {
  type AuditEventRecord,
  apiBootstrapFromJson,
  apiBootstrapToJson,
  approvalClaimsToSigningBytes,
  approvalSubmitRequestFromJson,
  asAgentSlug,
  asApiToken,
  asApprovalId,
  asIdempotencyKey,
  asMerkleRootHex,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  asToolCallId,
  asWriteId,
  canonicalJSON,
  computeAuditEventHash,
  FrozenArgs,
  GENESIS_PREV_HASH,
  INITIAL_VERIFIER_STATE,
  isAllowedLoopbackHost,
  isLoopbackRemoteAddress,
  isStreamEventKind,
  isWsFrameType,
  lsnFromV1Number,
  MAX_APPROVAL_SIGNATURE_BYTES,
  MAX_AUDIT_CHAIN_BATCH_SIZE,
  MAX_TOOL_CALLS_PER_RECEIPT,
  MAX_WEBAUTHN_ASSERTION_BYTES,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  SanitizedString,
  STREAM_EVENT_KIND_VALUES,
  serializeAuditEventRecordForHash,
  sha256Hex,
  type ThreadSpecEditedAuditPayload,
  threadAuditPayloadToBytes,
  threadFromJson,
  threadSpecContentHash,
  threadToJson,
  validateApprovalSubmitRequest,
  validateMerkleRootRecord,
  validateReceiptBudget,
  validateThreadSpecEditedAuditPayload,
  validateThreadSpecRevisionChain,
  validateThreadStatusFold,
  verifyChain,
  verifyChainIncremental,
  WS_FRAME_TYPE_VALUES,
} from "../src/index.ts";

const ANSI = {
  reset: "\x1b[0m",
  dim: "\x1b[2m",
  bold: "\x1b[1m",
  green: "\x1b[32m",
  red: "\x1b[31m",
  yellow: "\x1b[33m",
  cyan: "\x1b[36m",
};

let passed = 0;
let failed = 0;
const textDecoder = new TextDecoder();

function header(num: number, title: string): void {
  console.log("");
  console.log(`${ANSI.bold}${ANSI.cyan}── Scenario ${num}: ${title}${ANSI.reset}`);
}

function expectThrows(fn: () => unknown, expectedFragment: RegExp): void {
  try {
    const result = fn();
    console.log(`  ${ANSI.red}FAIL${ANSI.reset} expected throw, got: ${JSON.stringify(result)}`);
    failed++;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (expectedFragment.test(msg)) {
      console.log(
        `  ${ANSI.green}PASS${ANSI.reset} threw: ${ANSI.dim}${msg.slice(0, 100)}${ANSI.reset}`,
      );
      passed++;
    } else {
      console.log(
        `  ${ANSI.red}FAIL${ANSI.reset} threw wrong message: ${msg}\n` +
          `       expected to match: ${expectedFragment}`,
      );
      failed++;
    }
  }
}

function expectEqual<T>(label: string, actual: T, expected: T): void {
  const ok = JSON.stringify(actual) === JSON.stringify(expected);
  if (ok) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${JSON.stringify(actual)}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label}\n` +
        `       expected: ${JSON.stringify(expected)}\n` +
        `       actual:   ${JSON.stringify(actual)}`,
    );
    failed++;
  }
}

function nonNull<T>(value: T | null | undefined, label: string): T {
  if (value === null || value === undefined) {
    throw new Error(`demo fixture missing required value: ${label}`);
  }
  return value;
}

function expectChainResult(
  label: string,
  actual: ReturnType<typeof verifyChain>,
  expectedCode: string | "ok",
): void {
  const actualCode = actual.ok ? "ok" : actual.code;
  if (actualCode === expectedCode) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${actualCode}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label} expected ${expectedCode}, got ${actualCode}`,
    );
    failed++;
  }
}

function expectIncrementalResult(
  label: string,
  actual: ReturnType<typeof verifyChainIncremental>,
  expectedCode: string | "ok",
): void {
  const actualCode = actual.ok ? "ok" : actual.code;
  if (actualCode === expectedCode) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${actualCode}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label} expected ${expectedCode}, got ${actualCode}`,
    );
    failed++;
  }
}

function expectBudgetFailure(
  label: string,
  actual: ReturnType<typeof validateReceiptBudget>,
  expectedFragment: RegExp,
): void {
  if (!actual.ok && expectedFragment.test(actual.reason)) {
    console.log(
      `  ${ANSI.green}PASS${ANSI.reset} ${label} = ${ANSI.dim}${actual.reason}${ANSI.reset}`,
    );
    passed++;
  } else {
    console.log(
      `  ${ANSI.red}FAIL${ANSI.reset} ${label} expected reason matching ${expectedFragment}`,
    );
    failed++;
  }
}

console.log(`${ANSI.bold}@wuphf/protocol — moat demo${ANSI.reset}`);
console.log(`${ANSI.dim}Each scenario: input → expected behavior → actual.${ANSI.reset}`);

// ──────────────────────────────────────────────────────────────────────────
header(1, "FrozenArgs is content-addressed, key-order-independent");
// ──────────────────────────────────────────────────────────────────────────
const argsA = FrozenArgs.freeze({ b: 2, a: 1 });
const argsB = FrozenArgs.freeze({ a: 1, b: 2 });
expectEqual("hash equality across key order", argsA.hash === argsB.hash, true);
expectEqual("canonical JSON matches", argsA.canonicalJson, '{"a":1,"b":2}');

// ──────────────────────────────────────────────────────────────────────────
header(2, "SanitizedString strips weaponized invisible code points");
// ──────────────────────────────────────────────────────────────────────────
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

// ──────────────────────────────────────────────────────────────────────────
header(3, "SanitizedString rejects untrusted graph BEFORE side-effects fire");
// ──────────────────────────────────────────────────────────────────────────
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

// ──────────────────────────────────────────────────────────────────────────
header(4, "Canonical JSON rejects prototype pollution attempts");
// ──────────────────────────────────────────────────────────────────────────
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

// ──────────────────────────────────────────────────────────────────────────
header(5, "Audit chain catches tampering with a typed failure code");
// ──────────────────────────────────────────────────────────────────────────
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
expectChainResult("chain with deleted record at seq 1", verifyChain(sparseChain), "missing_record");

// ──────────────────────────────────────────────────────────────────────────
header(6, "Audit chain verifies incrementally without retaining the full log");
// ──────────────────────────────────────────────────────────────────────────
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

// ──────────────────────────────────────────────────────────────────────────
header(7, "Receipt budgets reject runaway task fanout");
// ──────────────────────────────────────────────────────────────────────────
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

// ──────────────────────────────────────────────────────────────────────────
header(8, "FrozenArgs JSON envelope rejects smuggled siblings");
// ──────────────────────────────────────────────────────────────────────────
const validReceiptJson = receiptToJson(buildValidReceipt());
const parsed = JSON.parse(validReceiptJson) as {
  toolCalls: Array<{ inputs: Record<string, unknown> }>;
};
const firstToolCall = nonNull(parsed.toolCalls[0], "parsed.toolCalls[0]");
firstToolCall.inputs = { ...firstToolCall.inputs, evilShadow: "smuggled" };
expectThrows(() => receiptFromJson(JSON.stringify(parsed)), /evilShadow.*not allowed/);

// ──────────────────────────────────────────────────────────────────────────
header(9, "ExternalWrite per-state invariants");
// ──────────────────────────────────────────────────────────────────────────
const tamperedApplied = JSON.parse(receiptToJson(buildValidReceipt())) as {
  writes: Array<Record<string, unknown>>;
};
const firstWrite = nonNull(tamperedApplied.writes[0], "tamperedApplied.writes[0]");
firstWrite.appliedDiff = null;
expectThrows(
  () => receiptFromJson(JSON.stringify(tamperedApplied)),
  /appliedDiff.*null is invalid for state "applied"/,
);

// ──────────────────────────────────────────────────────────────────────────
header(10, "Approval token writeId binding catches cross-write authorization");
// ──────────────────────────────────────────────────────────────────────────
const writeIdMismatch = JSON.parse(receiptToJson(buildValidReceipt())) as {
  writes: Array<Record<string, unknown>>;
};
const writeWithToken = nonNull(writeIdMismatch.writes[0], "writeIdMismatch.writes[0]");
const tokenWithWrongWriteId = JSON.parse(JSON.stringify(writeWithToken.approvalToken)) as {
  claims: Record<string, unknown>;
};
tokenWithWrongWriteId.claims.writeId = "write_wrong_target";
writeWithToken.approvalToken = tokenWithWrongWriteId;
expectThrows(() => receiptFromJson(JSON.stringify(writeIdMismatch)), /writeId.*must match/);

// ──────────────────────────────────────────────────────────────────────────
header(11, "ApprovalSubmitRequest cross-field validator (IPC layer)");
// ──────────────────────────────────────────────────────────────────────────
const validReceipt = buildValidReceipt();
const validToken = nonNull(validReceipt.approvals[0], "validReceipt.approvals[0]").signedToken;
const goodReq = {
  receiptId: validReceipt.id,
  approvalToken: validToken,
  idempotencyKey: asIdempotencyKey("submit-01"),
};
expectEqual("matched receiptId/claims", validateApprovalSubmitRequest(goodReq), { ok: true });

const badReq = {
  ...goodReq,
  receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAY"),
};
expectEqual("mismatched receiptId/claims", validateApprovalSubmitRequest(badReq), {
  ok: false,
  reason: "receiptId must match approvalToken.claims.receiptId",
});

const emptyKeyReq = { ...goodReq, idempotencyKey: "" };
expectEqual("empty idempotencyKey rejected", validateApprovalSubmitRequest(emptyKeyReq), {
  ok: false,
  reason: "idempotencyKey must match /^[A-Za-z0-9_-]{1,128}$/",
});

const missingAlgorithmReq = {
  ...goodReq,
  approvalToken: {
    ...validToken,
    algorithm: undefined,
  },
};
expectEqual(
  "missing token algorithm rejected",
  validateApprovalSubmitRequest(missingAlgorithmReq),
  {
    ok: false,
    reason: "approvalToken.algorithm is required",
  },
);

// ──────────────────────────────────────────────────────────────────────────
header(12, "EventLsn safe-integer bound (writer ⟷ verifier round-trip)");
// ──────────────────────────────────────────────────────────────────────────
expectThrows(() => lsnFromV1Number(Number.MAX_SAFE_INTEGER + 1), /non-negative safe integer/);
expectThrows(() => lsnFromV1Number(-1), /non-negative safe integer/);
expectEqual(
  "MAX_SAFE_INTEGER accepted",
  lsnFromV1Number(Number.MAX_SAFE_INTEGER),
  `v1:${Number.MAX_SAFE_INTEGER}`,
);

// ──────────────────────────────────────────────────────────────────────────
header(13, "Bonus: golden eventHash literal (cross-language wire contract)");
// ──────────────────────────────────────────────────────────────────────────
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
  `  ${ANSI.dim}A Go/Rust/Python verifier implementing the wire contract MUST produce the same hash.${ANSI.reset}`,
);
console.log(
  `  ${ANSI.dim}Run testdata/verifier-reference.go to verify cross-language portability.${ANSI.reset}`,
);

// ──────────────────────────────────────────────────────────────────────────
header(14, "FrozenArgs.fromCanonical rehydrates canonical JSON");
// ──────────────────────────────────────────────────────────────────────────
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

// ──────────────────────────────────────────────────────────────────────────
header(15, "ApiBootstrap codec: snake_case wire ↔ camelCase TS");
// ──────────────────────────────────────────────────────────────────────────
// Wire JSON is snake_case (v0 contract: `{ token, broker_url }`); the TS
// runtime surface is camelCase, enforced by `style/useNamingConvention`.
// The codec functions are the only place those two shapes meet.
const bootstrapWire = { token: "tok-bootstrap-demo-abc", broker_url: "http://127.0.0.1:54321" };
const bootstrapTs = apiBootstrapFromJson(bootstrapWire);
expectEqual("decoded brokerUrl (camelCase)", bootstrapTs.brokerUrl, "http://127.0.0.1:54321");
expectEqual("round-trip back to wire shape", apiBootstrapToJson(bootstrapTs), {
  token: "tok-bootstrap-demo-abc",
  broker_url: "http://127.0.0.1:54321",
});
expectThrows(
  () =>
    apiBootstrapFromJson({ token: "tok-bootstrap-demo-abc", brokerUrl: "http://127.0.0.1:54321" }),
  /broker_url|brokerUrl/,
);

// ──────────────────────────────────────────────────────────────────────────
header(16, "Loopback gate rejects DNS-rebinding probes");
// ──────────────────────────────────────────────────────────────────────────
expectEqual("Host Localhost:3000 allowed", isAllowedLoopbackHost("Localhost:3000"), true);
expectEqual("Host localhost.evil.com rejected", isAllowedLoopbackHost("localhost.evil.com"), false);
expectEqual(
  "Remote IPv4-mapped loopback allowed",
  isLoopbackRemoteAddress("::ffff:127.0.0.1"),
  true,
);
expectEqual("Remote with port rejected", isLoopbackRemoteAddress("127.0.0.1:1234"), false);

// ──────────────────────────────────────────────────────────────────────────
header(17, "IPC wire codecs pin approval signing bytes and runtime guards");
// ──────────────────────────────────────────────────────────────────────────
const signingBytes = textDecoder.decode(approvalClaimsToSigningBytes(validToken.claims));
expectEqual(
  "approval signing bytes include ISO issuedAt",
  signingBytes.includes('"issuedAt":"2026-05-08T18:00:00.000Z"'),
  true,
);
const approvalSubmitWire = {
  receiptId: validReceipt.id,
  idempotencyKey: "submit-01",
  approvalToken: {
    ...validToken,
    claims: {
      ...validToken.claims,
      issuedAt: validToken.claims.issuedAt.toISOString(),
      expiresAt: validToken.claims.expiresAt.toISOString(),
    },
  },
};
const decodedSubmit = approvalSubmitRequestFromJson(approvalSubmitWire);
expectEqual(
  "approvalSubmitRequestFromJson parses Date claims",
  decodedSubmit.approvalToken.claims.issuedAt instanceof Date,
  true,
);
expectEqual("decoded submit request validates", validateApprovalSubmitRequest(decodedSubmit), {
  ok: true,
});
expectEqual(
  "oversized signature rejected before regex",
  validateApprovalSubmitRequest({
    ...goodReq,
    approvalToken: { ...validToken, signature: "A".repeat(MAX_APPROVAL_SIGNATURE_BYTES + 1) },
  }),
  { ok: false, reason: "approvalToken.signature exceeds MAX_APPROVAL_SIGNATURE_BYTES" },
);
expectEqual(
  "oversized WebAuthn assertion rejected",
  validateApprovalSubmitRequest({
    ...goodReq,
    approvalToken: {
      ...validToken,
      claims: {
        ...validToken.claims,
        riskClass: "high",
        webauthnAssertion: "x".repeat(MAX_WEBAUTHN_ASSERTION_BYTES + 1),
      },
    },
  }),
  {
    ok: false,
    reason: "approvalToken.claims.webauthnAssertion exceeds MAX_WEBAUTHN_ASSERTION_BYTES",
  },
);
expectEqual(
  "stream event guard accepts tuple values",
  STREAM_EVENT_KIND_VALUES.every(isStreamEventKind),
  true,
);
expectEqual(
  "stream event guard rejects unknown value",
  isStreamEventKind("receipt.deleted"),
  false,
);
expectEqual("WS frame guard accepts tuple values", WS_FRAME_TYPE_VALUES.every(isWsFrameType), true);
expectEqual("WS frame guard rejects unknown value", isWsFrameType("close"), false);

const threadId = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const revision1 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const revision2 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA1");
const staleRevision = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA2");
const threadSigner = asSignerIdentity("fran@example.com");
const specContent = {
  body: "Implement the thread protocol slice",
  checklist: ["receipt v2", "audit events", "stream invalidation"],
};
const thread = {
  id: threadId,
  title: "Thread protocol",
  status: "open" as const,
  spec: {
    revisionId: revision1,
    threadId,
    baseRevisionId: null,
    content: specContent,
    contentHash: threadSpecContentHash(specContent),
    authoredBy: threadSigner,
    authoredAt: new Date("2026-05-08T18:00:00.000Z"),
  },
  externalRefs: {
    sourceUrls: ["https://example.test/wuphf/743"],
    entityIds: ["issue:743"],
  },
  taskIds: [validReceipt.taskId],
  createdBy: threadSigner,
  createdAt: new Date("2026-05-08T18:00:00.000Z"),
  updatedAt: new Date("2026-05-08T18:01:00.000Z"),
};

// ──────────────────────────────────────────────────────────────────────────
header(18, "Thread create + spec edit + status change round-trip through canonical JSON");
// ──────────────────────────────────────────────────────────────────────────
expectEqual("threadFromJson(threadToJson(thread))", threadFromJson(threadToJson(thread)), thread);
expectEqual(
  "thread_created audit body is canonical JSON bytes",
  textDecoder
    .decode(
      threadAuditPayloadToBytes("thread_created", {
        threadId,
        title: "Thread protocol",
        createdBy: threadSigner,
        createdAt: thread.createdAt,
        externalRefs: thread.externalRefs,
      }),
    )
    .includes('"threadId"'),
  true,
);

// ──────────────────────────────────────────────────────────────────────────
header(19, "Thread spec OCC accepts the prior revision and rejects stale-base 409 shape");
// ──────────────────────────────────────────────────────────────────────────
const firstEdit: ThreadSpecEditedAuditPayload = {
  threadId,
  revisionId: revision1,
  baseRevisionId: null,
  content: specContent,
  contentHash: threadSpecContentHash(specContent),
  authoredBy: threadSigner,
  authoredAt: new Date("2026-05-08T18:00:00.000Z"),
};
const secondEdit: ThreadSpecEditedAuditPayload = {
  ...firstEdit,
  revisionId: revision2,
  baseRevisionId: revision1,
};
const staleEdit: ThreadSpecEditedAuditPayload = {
  ...firstEdit,
  revisionId: revision2,
  baseRevisionId: staleRevision,
};
expectEqual("OCC happy path", validateThreadSpecRevisionChain([firstEdit, secondEdit]), {
  ok: true,
});
expectEqual(
  "OCC stale-base rejected",
  validateThreadSpecRevisionChain([firstEdit, staleEdit]).ok,
  false,
);

// ──────────────────────────────────────────────────────────────────────────
header(20, "Thread status fold and terminal-write-once invariant");
// ──────────────────────────────────────────────────────────────────────────
expectEqual(
  "status fold happy path",
  validateThreadStatusFold([
    { kind: "thread_created", threadId },
    {
      kind: "thread_status_changed",
      threadId,
      fromStatus: "open",
      toStatus: "in_progress",
      changedBy: threadSigner,
      changedAt: new Date("2026-05-08T18:02:00.000Z"),
    },
    {
      kind: "thread_status_changed",
      threadId,
      fromStatus: "in_progress",
      toStatus: "closed",
      changedBy: threadSigner,
      changedAt: new Date("2026-05-08T18:03:00.000Z"),
    },
  ]),
  { ok: true },
);
expectEqual(
  "terminal transition rejected",
  validateThreadStatusFold([
    { kind: "thread_created", threadId },
    {
      kind: "thread_status_changed",
      threadId,
      fromStatus: "open",
      toStatus: "closed",
      changedBy: threadSigner,
      changedAt: new Date("2026-05-08T18:02:00.000Z"),
    },
    {
      kind: "thread_status_changed",
      threadId,
      fromStatus: "closed",
      toStatus: "in_progress",
      changedBy: threadSigner,
      changedAt: new Date("2026-05-08T18:03:00.000Z"),
    },
  ]).ok,
  false,
);

// ──────────────────────────────────────────────────────────────────────────
header(21, "ReceiptSnapshotV1 rejects threadId presence with path /threadId");
// ──────────────────────────────────────────────────────────────────────────
const v1WithThreadId = JSON.parse(receiptToJson(validReceipt)) as Record<string, unknown>;
v1WithThreadId.threadId = threadId;
expectThrows(() => receiptFromJson(JSON.stringify(v1WithThreadId)), /\/threadId/);

// ──────────────────────────────────────────────────────────────────────────
header(22, "ReceiptSnapshotV2 round-trips with and without threadId");
// ──────────────────────────────────────────────────────────────────────────
const v2WithThread: ReceiptSnapshot = { ...validReceipt, schemaVersion: 2, threadId };
const v2WithoutThread: ReceiptSnapshot = { ...validReceipt, schemaVersion: 2 };
expectEqual(
  "v2 with threadId canonical bytes",
  receiptToJson(receiptFromJson(receiptToJson(v2WithThread))),
  receiptToJson(v2WithThread),
);
expectEqual(
  "v2 without threadId canonical bytes",
  receiptToJson(receiptFromJson(receiptToJson(v2WithoutThread))),
  receiptToJson(v2WithoutThread),
);

// ──────────────────────────────────────────────────────────────────────────
header(23, "thread_spec_edited validator catches forged contentHash");
// ──────────────────────────────────────────────────────────────────────────
expectEqual(
  "forged contentHash rejected",
  validateThreadSpecEditedAuditPayload({ ...firstEdit, contentHash: sha256Hex("forged") }).ok,
  false,
);

// ──────────────────────────────────────────────────────────────────────────
header(24, "Thread stream event tuple guards stay invalidation-only");
// ──────────────────────────────────────────────────────────────────────────
expectEqual("thread.created accepted", isStreamEventKind("thread.created"), true);
expectEqual("thread.updated accepted", isStreamEventKind("thread.updated"), true);
expectEqual(
  "thread.pinned_approvals.changed accepted",
  isStreamEventKind("thread.pinned_approvals.changed"),
  true,
);

// ──────────────────────────────────────────────────────────────────────────
console.log("");
console.log(`${ANSI.bold}─────────────────────────────────────${ANSI.reset}`);
const summaryColor = failed === 0 ? ANSI.green : ANSI.red;
console.log(`${ANSI.bold}${summaryColor}${passed} passed, ${failed} failed${ANSI.reset}`);
process.exit(failed === 0 ? 0 : 1);

// ──────────────────────────────────────────────────────────────────────────
function buildValidReceipt(): ReceiptSnapshot {
  const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
  const writeId = asWriteId("write_01");
  const proposedDiff = FrozenArgs.freeze({ amount: { from: 1000, to: 1500 } });
  const claims = {
    signerIdentity: "fd@example.com",
    role: "approver" as const,
    receiptId,
    writeId,
    frozenArgsHash: proposedDiff.hash,
    riskClass: "high" as const,
    issuedAt: new Date("2026-05-08T18:00:00.000Z"),
    expiresAt: new Date("2026-05-08T18:30:00.000Z"),
    webauthnAssertion: "webauthn-attestation-blob",
  };
  const signedToken = {
    claims,
    algorithm: "ed25519" as const,
    signerKeyId: "key-01",
    // Demo only — real broker would produce a real Ed25519 detached signature.
    signature: "ZmFrZS1zaWduYXR1cmUtZm9yLWRlbW8tcHVycG9zZXM=",
  };
  return {
    id: receiptId,
    agentSlug: asAgentSlug("sam_agent"),
    taskId: asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW"),
    triggerKind: "human_message",
    triggerRef: "msg:01ARZ3NDEKTSV4RRFFQ69G5FAX",
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
        toolName: "hubspot.deals.update",
        inputs: FrozenArgs.freeze({ deal: "5678", action: "advance_stage" }),
        output: SanitizedString.fromUnknown("Stage advanced to qualified"),
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
        signedToken,
        tokenVerdict: { status: "valid", verifiedAt: new Date("2026-05-08T18:01:00.000Z") },
        decidedAt: new Date("2026-05-08T18:01:00.000Z"),
      },
    ],
    filesChanged: [],
    commits: [],
    sourceReads: [],
    writes: [
      {
        writeId,
        action: "hubspot.deals.update",
        target: "deal:5678",
        idempotencyKey: asIdempotencyKey("write-01"),
        proposedDiff,
        appliedDiff: FrozenArgs.freeze({ stage: { from: "lead", to: "qualified" } }),
        approvalToken: signedToken,
        approvedAt: new Date("2026-05-08T18:01:01.000Z"),
        result: "applied",
        postWriteVerify: FrozenArgs.freeze({ stage: "qualified" }),
      },
    ],
    inputTokens: 1200,
    outputTokens: 345,
    cacheReadTokens: 50,
    cacheCreationTokens: 25,
    costUsd: 0.0425,
    finalMessage: SanitizedString.fromUnknown("Done."),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [],
    wikiWrites: [],
    schemaVersion: 1,
  };
}

// Defeat unused-import warnings for symbols the demo references via dynamic
// scenarios above (kept here so the import block stays a complete map of
// what the package exposes for adversarial play).
void canonicalJSON;
void asApiToken;
void asMerkleRootHex;
void serializeAuditEventRecordForHash;
void validateMerkleRootRecord;
