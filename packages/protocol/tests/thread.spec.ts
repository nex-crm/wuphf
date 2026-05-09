import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { FrozenArgs } from "../src/frozen-args.ts";
import {
  type AuditEventRecord,
  asIdempotencyKey,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  GENESIS_PREV_HASH,
  isSignerIdentity,
  isThreadId,
  isThreadSpecRevisionId,
  type JsonValue,
  lsnFromV1Number,
  MAX_SIGNER_IDENTITY_BYTES,
  MAX_THREAD_EXTERNAL_REF_BYTES,
  MAX_THREAD_EXTERNAL_REFS,
  MAX_THREAD_TASK_IDS,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  serializeAuditEventRecordForHash,
  sha256Hex,
  THREAD_STATUS_VALUES,
  type Thread,
  type ThreadCreatedAuditPayload,
  type ThreadExternalRefs,
  type ThreadSpecEditedAuditPayload,
  type ThreadSpecRevision,
  type ThreadStatus,
  type ThreadStatusChangedAuditPayload,
  threadAuditPayloadFromJsonValue,
  threadAuditPayloadToBytes,
  threadAuditPayloadToJsonValue,
  threadExternalRefsFromJsonValue,
  threadExternalRefsToJsonValue,
  threadFromJson,
  threadFromJsonValue,
  threadSpecContentHash,
  threadSpecRevisionFromJsonValue,
  threadSpecRevisionToJsonValue,
  threadToJson,
  threadToJsonValue,
  validateThread,
  validateThreadAuditPayloadForKind,
  validateThreadCommand,
  validateThreadCreatedAuditPayload,
  validateThreadExternalRefs,
  validateThreadForeignKeys,
  validateThreadReceiptIndex,
  validateThreadSpecEditedAuditPayload,
  validateThreadSpecRevision,
  validateThreadSpecRevisionChain,
  validateThreadStatusChangedAuditPayload,
  validateThreadStatusFold,
} from "../src/index.ts";
import { SanitizedString } from "../src/sanitized-string.ts";

const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const OTHER_THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const REVISION_1 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const REVISION_2 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA1");
const STALE_REVISION = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA2");
const SIGNER = asSignerIdentity("fran@example.com");
const CREATED_AT = new Date("2026-05-08T18:00:00.000Z");
const UPDATED_AT = new Date("2026-05-08T18:05:00.000Z");
const TEXT_DECODER = new TextDecoder();
const TEXT_ENCODER = new TextEncoder();

describe("thread protocol slice", () => {
  it("brands ThreadId, ThreadSpecRevisionId, and bounded SignerIdentity", () => {
    expect(asThreadId(THREAD_ID) as string).toBe(THREAD_ID);
    expect(asThreadSpecRevisionId(REVISION_1) as string).toBe(REVISION_1);
    expect(asSignerIdentity("a".repeat(MAX_SIGNER_IDENTITY_BYTES)) as string).toBe(
      "a".repeat(MAX_SIGNER_IDENTITY_BYTES),
    );
    expect(() => asSignerIdentity("")).toThrow(/non-empty/);
    expect(() => asSignerIdentity("a".repeat(MAX_SIGNER_IDENTITY_BYTES + 1))).toThrow(/budget/);
  });

  it("runtime guards mirror brand constructors", () => {
    expect(isThreadId(THREAD_ID)).toBe(true);
    expect(isThreadId("not-a-ulid")).toBe(false);
    expect(isThreadId(123)).toBe(false);

    expect(isThreadSpecRevisionId(REVISION_1)).toBe(true);
    expect(isThreadSpecRevisionId("not-a-ulid")).toBe(false);

    expect(isSignerIdentity(SIGNER)).toBe(true);
    expect(isSignerIdentity("")).toBe(false);
    expect(isSignerIdentity("a".repeat(MAX_SIGNER_IDENTITY_BYTES + 1))).toBe(false);
    expect(isSignerIdentity(42)).toBe(false);
  });

  it("THREAD_STATUS_VALUES exhausts the ThreadStatus union", () => {
    const coverage: Record<ThreadStatus, true> = {
      open: true,
      in_progress: true,
      needs_review: true,
      merged: true,
      closed: true,
    };
    expect([...THREAD_STATUS_VALUES].sort()).toStrictEqual(
      (Object.keys(coverage) as ThreadStatus[]).sort(),
    );
  });

  it("round-trips Thread through snake_case wire JSON and rejects unknown keys", () => {
    const thread = validThreadFixture();
    const json = threadToJson(thread);
    const parsed = JSON.parse(json) as Record<string, unknown> &
      Record<"external_refs" | "thread_id", unknown>;

    expect(parsed.thread_id).toBe(THREAD_ID);
    expect(parsed.external_refs).toEqual({
      entity_ids: ["issue:743"],
      source_urls: ["https://example.test/wuphf/743"],
    });
    expect(threadFromJson(json)).toEqual(thread);
    expect(() => threadFromJson(JSON.stringify({ ...parsed, shadow: true }))).toThrow(
      /thread\/shadow: is not allowed/,
    );
  });

  it("enforces receipt schemaVersion V1/V2 threadId semantics", () => {
    const v1: ReceiptSnapshot = validReceiptFixture();
    const v2: ReceiptSnapshot = { ...v1, schemaVersion: 2, threadId: THREAD_ID };

    expect(() =>
      receiptFromJson(JSON.stringify({ ...JSON.parse(receiptToJson(v1)), threadId: THREAD_ID })),
    ).toThrow(/\/threadId/);
    expect(receiptFromJson(receiptToJson(v2))).toEqual(v2);
    expect(receiptFromJson(receiptToJson({ ...v1, schemaVersion: 2 }))).toEqual({
      ...v1,
      schemaVersion: 2,
    });
  });

  it("property-checks receipt V1 rejects threadId and V2 preserves optional threadId", () => {
    fc.assert(
      fc.property(fc.boolean(), (includeThreadId) => {
        const base = validReceiptFixture();
        const v2: ReceiptSnapshot = includeThreadId
          ? { ...base, schemaVersion: 2, threadId: THREAD_ID }
          : { ...base, schemaVersion: 2 };
        expect(receiptFromJson(receiptToJson(v2))).toEqual(v2);

        const v1Wire = JSON.parse(receiptToJson(base)) as Record<string, unknown> & {
          threadId?: unknown;
        };
        v1Wire.threadId = THREAD_ID;
        expect(() => receiptFromJson(JSON.stringify(v1Wire))).toThrow(/\/threadId/);
      }),
      { numRuns: 25 },
    );
  });

  it("re-derives ThreadSpecRevision.contentHash from canonical content", () => {
    const spec = validSpecRevision();

    expect(validateThreadSpecRevision(spec)).toEqual({ ok: true });
    expect(validateThreadSpecRevision({ ...spec, contentHash: sha256Hex("forged") })).toEqual({
      ok: false,
      errors: [{ path: "/contentHash", message: "must match sha256(canonical(content))" }],
    });
    expect(
      validateThread({
        ...validThreadFixture(),
        spec: { ...spec, contentHash: sha256Hex("forged") },
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/spec/contentHash", message: "must match sha256(canonical(content))" }],
    });
  });

  it("property-checks ThreadSpecRevision.contentHash across canonical content", () => {
    fc.assert(
      fc.property(fc.string({ maxLength: 64 }), fc.integer({ min: 0, max: 10 }), (body, n) => {
        const content: JsonValue = { body, n };
        const spec = validSpecRevision({ content, contentHash: threadSpecContentHash(content) });

        expect(validateThreadSpecRevision(spec)).toEqual({ ok: true });
        expect(
          validateThreadSpecRevision({ ...spec, contentHash: sha256Hex(`${body}:${n}:bad`) }).ok,
        ).toBe(false);
      }),
      { numRuns: 50 },
    );
  });

  it("enforces thread_spec_edited baseRevisionId OCC sequence", () => {
    const first = validSpecEditedPayload({ revisionId: REVISION_1 });
    const second = validSpecEditedPayload({ baseRevisionId: REVISION_1, revisionId: REVISION_2 });
    const stale = validSpecEditedPayload({
      baseRevisionId: STALE_REVISION,
      revisionId: REVISION_2,
    });

    expect(validateThreadSpecRevisionChain([first, second])).toEqual({ ok: true });
    expect(validateThreadSpecRevisionChain([{ ...first, baseRevisionId: REVISION_1 }]).ok).toBe(
      false,
    );
    expect(
      validateThreadSpecRevisionChain([
        {
          ...first,
          contentHash: sha256Hex("forged"),
        },
      ]).ok,
    ).toBe(false);
    expect(validateThreadSpecRevisionChain([first, stale])).toEqual({
      ok: false,
      errors: [{ path: "/1/baseRevisionId", message: "must match prior revisionId" }],
    });
  });

  it("rejects self-referential and duplicate spec revisions in a chain", () => {
    const first = validSpecEditedPayload({ revisionId: REVISION_1 });
    const second = validSpecEditedPayload({
      baseRevisionId: REVISION_1,
      revisionId: REVISION_2,
    });
    const selfReferential = validSpecEditedPayload({
      baseRevisionId: REVISION_2,
      revisionId: REVISION_2,
    });
    const duplicateRevision = validSpecEditedPayload({
      baseRevisionId: REVISION_2,
      revisionId: REVISION_1,
    });

    expect(validateThreadSpecRevisionChain([first, selfReferential])).toEqual(
      expect.objectContaining({
        ok: false,
        errors: expect.arrayContaining([
          { path: "/1/baseRevisionId", message: "must not equal revisionId" },
        ]),
      }),
    );
    expect(validateThreadSpecRevisionChain([first, second, duplicateRevision])).toEqual(
      expect.objectContaining({
        ok: false,
        errors: expect.arrayContaining([
          { path: "/2/revisionId", message: "duplicate revisionId in chain" },
        ]),
      }),
    );
  });

  it("property-checks stale spec edit bases are rejected", () => {
    fc.assert(
      fc.property(fc.boolean(), (useStaleBase) => {
        const first = validSpecEditedPayload({ revisionId: REVISION_1 });
        const second = validSpecEditedPayload({
          baseRevisionId: useStaleBase ? STALE_REVISION : REVISION_1,
          revisionId: REVISION_2,
        });

        expect(validateThreadSpecRevisionChain([first, second]).ok).toBe(!useStaleBase);
      }),
      { numRuns: 25 },
    );
  });

  it("folds status changes and rejects transitions out of terminal statuses", () => {
    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: THREAD_ID },
        {
          kind: "thread_status_changed",
          threadId: THREAD_ID,
          fromStatus: "open",
          toStatus: "in_progress",
          changedBy: SIGNER,
          changedAt: UPDATED_AT,
        },
        {
          kind: "thread_status_changed",
          threadId: THREAD_ID,
          fromStatus: "in_progress",
          toStatus: "closed",
          changedBy: SIGNER,
          changedAt: UPDATED_AT,
        },
      ]),
    ).toEqual({ ok: true });

    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: THREAD_ID },
        {
          kind: "thread_status_changed",
          threadId: THREAD_ID,
          fromStatus: "open",
          toStatus: "closed",
          changedBy: SIGNER,
          changedAt: UPDATED_AT,
        },
        {
          kind: "thread_status_changed",
          threadId: THREAD_ID,
          fromStatus: "closed",
          toStatus: "in_progress",
          changedBy: SIGNER,
          changedAt: UPDATED_AT,
        },
      ]).ok,
    ).toBe(false);
  });

  it("property-checks terminal status_changed payloads are rejected", () => {
    fc.assert(
      fc.property(fc.constantFrom<ThreadStatus>("merged", "closed"), (fromStatus) => {
        const payload = {
          threadId: THREAD_ID,
          fromStatus,
          toStatus: "open" as const,
          changedBy: SIGNER,
          changedAt: UPDATED_AT,
        };

        expect(validateThreadStatusChangedAuditPayload(payload).ok).toBe(false);
      }),
      { numRuns: 10 },
    );
  });

  it("rejects every status transition outside the lifecycle table", () => {
    const allowedTransitions = new Set([
      "open->in_progress",
      "open->closed",
      "in_progress->needs_review",
      "in_progress->closed",
      "needs_review->merged",
      "needs_review->closed",
    ]);

    for (const fromStatus of THREAD_STATUS_VALUES) {
      for (const toStatus of THREAD_STATUS_VALUES) {
        if (allowedTransitions.has(`${fromStatus}->${toStatus}`)) continue;

        const transitionError = {
          path: "/toStatus",
          message: `transition not allowed from ${fromStatus}`,
        };
        expect(
          validateThreadStatusChangedAuditPayload(
            validStatusChangedPayload({ fromStatus, toStatus }),
          ),
        ).toEqual(
          expect.objectContaining({
            ok: false,
            errors: expect.arrayContaining([transitionError]),
          }),
        );
        expect(
          validateThreadCommand({
            ...validStatusChangeCommand(),
            fromStatus,
            toStatus,
          }),
        ).toEqual(
          expect.objectContaining({
            ok: false,
            errors: expect.arrayContaining([transitionError]),
          }),
        );
      }
    }
  });

  it("defines projection-supporting shapes for external refs and receipt task index", () => {
    const thread = validThreadFixture();

    expect(thread.externalRefs.sourceUrls).toEqual(["https://example.test/wuphf/743"]);
    expect(thread.taskIds).toEqual([asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW")]);
  });

  it("validates thread foreign-key references for edits, status changes, and receipts", () => {
    const receipt: ReceiptSnapshot = {
      ...validReceiptFixture(),
      schemaVersion: 2,
      threadId: THREAD_ID,
    };
    const badReceipt: ReceiptSnapshot = {
      ...validReceiptFixture(),
      schemaVersion: 2,
      threadId: OTHER_THREAD_ID,
    };

    expect(
      validateThreadForeignKeys({
        existingThreadIds: new Set([THREAD_ID]),
        specEdits: [validSpecEditedPayload()],
        statusChanges: [
          {
            threadId: THREAD_ID,
            fromStatus: "open",
            toStatus: "in_progress",
            changedBy: SIGNER,
            changedAt: UPDATED_AT,
          },
        ],
        receipts: [receipt],
      }),
    ).toEqual({ ok: true });

    expect(
      validateThreadForeignKeys({
        existingThreadIds: new Set([THREAD_ID]),
        specEdits: [],
        statusChanges: [],
        receipts: [badReceipt],
      }).ok,
    ).toBe(false);
  });

  it("property-checks foreign-key helper allows the per-office inbox thread", () => {
    fc.assert(
      fc.property(fc.boolean(), (useInboxThread) => {
        const receipt = {
          ...validReceiptFixture(),
          schemaVersion: 2,
          threadId: OTHER_THREAD_ID,
        } satisfies ReceiptSnapshot;

        const result = validateThreadForeignKeys({
          existingThreadIds: new Set([THREAD_ID]),
          specEdits: [],
          statusChanges: [],
          receipts: [receipt],
          ...(useInboxThread ? { inboxThreadId: OTHER_THREAD_ID } : {}),
        });

        expect(result.ok).toBe(useInboxThread);
      }),
      { numRuns: 25 },
    );
  });

  it("asserts Thread.taskIds equals unique receipt taskIds for that thread", () => {
    const receipt = validReceiptFixture();
    const thread = validThreadFixture();
    const indexedReceipt = {
      ...receipt,
      schemaVersion: 2,
      threadId: THREAD_ID,
    } satisfies ReceiptSnapshot;

    expect(validateThreadReceiptIndex(thread, [indexedReceipt, indexedReceipt])).toEqual({
      ok: true,
    });
    expect(validateThreadReceiptIndex({ ...thread, taskIds: [] }, [indexedReceipt]).ok).toBe(false);
  });

  it("property-checks receipt/thread index detects missing projected task IDs", () => {
    fc.assert(
      fc.property(fc.boolean(), (includeTaskId) => {
        const thread = includeTaskId
          ? validThreadFixture()
          : { ...validThreadFixture(), taskIds: [] };
        const receipt = {
          ...validReceiptFixture(),
          schemaVersion: 2,
          threadId: THREAD_ID,
        } satisfies ReceiptSnapshot;

        expect(validateThreadReceiptIndex(thread, [receipt]).ok).toBe(includeTaskId);
      }),
      { numRuns: 25 },
    );
  });

  it("requires idempotencyKey on every thread command shape", () => {
    const create = {
      kind: "thread.create",
      idempotencyKey: asIdempotencyKey("thread-create-1"),
      threadId: THREAD_ID,
      title: "Thread protocol",
      createdBy: SIGNER,
      createdAt: CREATED_AT,
      externalRefs: validExternalRefs(),
      content: validSpecContent(),
    };

    expect(validateThreadCommand(create)).toEqual({ ok: true });
    expect(validateThreadCommand({ ...create, idempotencyKey: undefined }).ok).toBe(false);
  });

  it("property-checks thread command kinds all reject missing idempotencyKey", () => {
    fc.assert(
      fc.property(
        fc.constantFrom("thread.create", "thread.spec.edit", "thread.status.change"),
        (kind) => {
          const command =
            kind === "thread.create"
              ? {
                  kind,
                  threadId: THREAD_ID,
                  title: "Thread protocol",
                  createdBy: SIGNER,
                  createdAt: CREATED_AT,
                  externalRefs: validExternalRefs(),
                  content: validSpecContent(),
                }
              : kind === "thread.spec.edit"
                ? {
                    kind,
                    threadId: THREAD_ID,
                    revisionId: REVISION_1,
                    content: validSpecContent(),
                    contentHash: threadSpecContentHash(validSpecContent()),
                    authoredBy: SIGNER,
                    authoredAt: CREATED_AT,
                  }
                : {
                    kind,
                    threadId: THREAD_ID,
                    fromStatus: "open" as const,
                    toStatus: "in_progress" as const,
                    changedBy: SIGNER,
                    changedAt: CREATED_AT,
                  };

          expect(validateThreadCommand(command).ok).toBe(false);
        },
      ),
      { numRuns: 15 },
    );
  });

  it("validates thread audit payloads with full spec content", () => {
    const payload = validSpecEditedPayload();

    expect(validateThreadSpecEditedAuditPayload(payload)).toEqual({ ok: true });
    expect(
      validateThreadSpecEditedAuditPayload({ ...payload, contentHash: sha256Hex("forged") }).ok,
    ).toBe(false);
  });

  it("canonicalizes no-base spec edits as a missing baseRevisionId", () => {
    const payload = validSpecEditedPayload();
    const canonicalBytes = threadAuditPayloadToBytes("thread_spec_edited", payload);
    const canonicalJson = JSON.parse(TEXT_DECODER.decode(canonicalBytes)) as Record<
      string,
      unknown
    >;
    const explicitNull = {
      ...payload,
      baseRevisionId: null,
    };

    expect(Object.hasOwn(canonicalJson, "baseRevisionId")).toBe(false);
    expect(validateThreadSpecEditedAuditPayload(explicitNull).ok).toBe(false);
    expect(() =>
      threadAuditPayloadFromJsonValue("thread_spec_edited", {
        ...canonicalJson,
        baseRevisionId: null,
      }),
    ).toThrow(/baseRevisionId/);
    expect(
      TEXT_DECODER.decode(
        threadAuditPayloadToBytes(
          "thread_spec_edited",
          threadAuditPayloadFromJsonValue("thread_spec_edited", canonicalJson),
        ),
      ),
    ).toBe(TEXT_DECODER.decode(canonicalBytes));
  });

  it("covers strict thread wire codec failures and terminal closedAt decoding", () => {
    const thread = validThreadFixture();
    const wire = threadToJsonValue(thread);

    expect(
      threadFromJsonValue({ ...wire, status: "closed", closed_at: UPDATED_AT.toISOString() }),
    ).toEqual({
      ...thread,
      status: "closed",
      closedAt: UPDATED_AT,
    });
    expect(() => threadToJson({ ...thread, title: "" })).toThrow(/title/);
    expect(() => threadFromJsonValue({ ...wire, thread_id: "not-a-ulid" })).toThrow(
      /thread\.thread_id/,
    );
    expect(() => threadFromJsonValue({ ...wire, title: undefined })).toThrow(/title/);
    expect(() => threadFromJsonValue({ ...wire, title: 7 })).toThrow(/title/);
    expect(() => threadFromJsonValue({ ...wire, status: "mergedish" })).toThrow(/thread.status/);
    expect(() => threadFromJsonValue({ ...wire, created_at: "not-an-instant" })).toThrow(
      /created_at/,
    );
    expect(() => threadFromJsonValue({ ...wire, closed_at: 7 })).toThrow(/closed_at/);
    expect(() => threadFromJsonValue({ ...wire, closed_at: "2026-02-31T00:00:00.000Z" })).toThrow(
      /valid ISO 8601 instant/,
    );
    expect(() => threadFromJsonValue({ ...wire, task_ids: "bad" })).toThrow(/task_ids/);
    expect(() => threadFromJsonValue({ ...wire, task_ids: [123] })).toThrow(/task_ids\.0/);
    expect(() =>
      threadFromJsonValue({
        ...wire,
        spec: threadSpecRevisionToJsonValue(validSpecRevision({ threadId: OTHER_THREAD_ID })),
      }),
    ).toThrow(/\/spec\/threadId/);
  });

  it("covers spec revision and external ref wire codecs at object boundaries", () => {
    const specWithBase = validSpecRevision({ baseRevisionId: REVISION_1, revisionId: REVISION_2 });
    const specWire = threadSpecRevisionToJsonValue(specWithBase);
    const specWireWithoutBase: Record<string, unknown> = { ...specWire };
    const refsWire = threadExternalRefsToJsonValue(validExternalRefs());
    Reflect.deleteProperty(specWireWithoutBase, "base_revision_id");

    expect(threadSpecRevisionFromJsonValue(specWire)).toEqual(specWithBase);
    expect(threadSpecRevisionFromJsonValue(specWireWithoutBase)).toEqual({
      ...specWithBase,
      baseRevisionId: undefined,
    });
    expect(threadExternalRefsFromJsonValue(refsWire)).toEqual(validExternalRefs());
    expect(() => threadSpecRevisionFromJsonValue({ ...specWire, base_revision_id: 12 })).toThrow(
      /base_revision_id/,
    );
    expect(() =>
      threadSpecRevisionFromJsonValue({ ...specWire, content_hash: sha256Hex("forged") }),
    ).toThrow(/contentHash/);
    expect(() => threadSpecRevisionFromJsonValue({ ...specWire, content: BigInt(1) })).toThrow(
      /content/,
    );
    expect(() => threadExternalRefsFromJsonValue({ ...refsWire, shadow: true })).toThrow(/shadow/);
    expect(() => threadExternalRefsFromJsonValue({ ...refsWire, source_urls: [1] })).toThrow(
      /source_urls\.0/,
    );
    expect(() =>
      threadExternalRefsFromJsonValue({ ...refsWire, source_urls: ["dup", "dup"] }),
    ).toThrow(/sourceUrls/);
    expect(validateThreadExternalRefs({ sourceUrls: "bad", entityIds: [] }).ok).toBe(false);
    expect(
      validateThreadExternalRefs({
        sourceUrls: ["", "dup", "dup", "a".repeat(MAX_THREAD_EXTERNAL_REF_BYTES + 1)],
        entityIds: Array.from({ length: MAX_THREAD_EXTERNAL_REFS + 1 }, (_, i) => `entity:${i}`),
      }).ok,
    ).toBe(false);
  });

  it("covers thread audit payload codecs for created, edited, and status-changed bodies", () => {
    const created = validCreatedPayload();
    const edited = validSpecEditedPayload({ baseRevisionId: REVISION_1, revisionId: REVISION_2 });
    const status = validStatusChangedPayload();
    const createdJson = threadAuditPayloadToJsonValue("thread_created", created);
    const editedJson = threadAuditPayloadToJsonValue("thread_spec_edited", edited);
    const statusJson = threadAuditPayloadToJsonValue("thread_status_changed", status);

    expect(threadAuditPayloadFromJsonValue("thread_created", createdJson)).toEqual(created);
    expect(threadAuditPayloadFromJsonValue("thread_spec_edited", editedJson)).toEqual(edited);
    expect(threadAuditPayloadFromJsonValue("thread_status_changed", statusJson)).toEqual(status);
    expect(threadAuditPayloadToBytes("thread_created", created).length).toBeGreaterThan(0);
    expect(threadAuditPayloadToBytes("thread_spec_edited", edited).length).toBeGreaterThan(0);
    expect(threadAuditPayloadToBytes("thread_status_changed", status).length).toBeGreaterThan(0);
    expect(validateThreadCreatedAuditPayload(created)).toEqual({ ok: true });
    expect(validateThreadAuditPayloadForKind("thread_created", created)).toEqual({ ok: true });
    expect(validateThreadAuditPayloadForKind("thread_spec_edited", edited)).toEqual({ ok: true });
    expect(validateThreadAuditPayloadForKind("thread_status_changed", status)).toEqual({
      ok: true,
    });
    const bogusKind = "bogus" as unknown as "thread_status_changed";
    expect(() => validateThreadAuditPayloadForKind(bogusKind, status)).toThrow(
      /unknown ThreadAuditEventKind: bogus/,
    );
    expect(() => threadAuditPayloadFromJsonValue(bogusKind, statusJson)).toThrow(
      /unknown ThreadAuditEventKind: bogus/,
    );
    expect(() => threadAuditPayloadToJsonValue(bogusKind, status)).toThrow(
      /unknown ThreadAuditEventKind: bogus/,
    );
    expect(() => threadAuditPayloadToBytes(bogusKind, status)).toThrow(
      /unknown ThreadAuditEventKind: bogus/,
    );
    expect(validateThreadCreatedAuditPayload(7).ok).toBe(false);
    expect(validateThreadSpecEditedAuditPayload(7).ok).toBe(false);
    expect(validateThreadStatusChangedAuditPayload(7).ok).toBe(false);
    expect(
      threadAuditPayloadFromJsonValue("thread_spec_edited", {
        ...threadAuditPayloadToJsonValue("thread_spec_edited", validSpecEditedPayload()),
      }),
    ).toEqual(validSpecEditedPayload());
    expect(() =>
      threadAuditPayloadToBytes("thread_spec_edited", {
        ...edited,
        contentHash: sha256Hex("forged"),
      }),
    ).toThrow(/contentHash/);
    expect(() =>
      threadAuditPayloadFromJsonValue("thread_created", { ...createdJson, extra: true }),
    ).toThrow(/extra/);
    expect(() =>
      threadAuditPayloadFromJsonValue("thread_created", {
        ...createdJson,
        externalRefs: { sourceUrls: ["dup", "dup"], entityIds: [] },
      }),
    ).toThrow(/threadCreatedAuditPayload\/externalRefs/);
    expect(() =>
      threadAuditPayloadFromJsonValue("thread_status_changed", {
        ...statusJson,
        fromStatus: "closed",
      }),
    ).toThrow(/fromStatus/);
  });

  it("rejects non-canonical thread audit payload bytes before hashing", () => {
    const payload = validCreatedPayload();
    const payloadJson = threadAuditPayloadToJsonValue("thread_created", payload);
    const canonicalRecord: AuditEventRecord = {
      seqNo: lsnFromV1Number(0),
      timestamp: CREATED_AT,
      prevHash: GENESIS_PREV_HASH,
      eventHash: sha256Hex("thread-created-record"),
      payload: {
        kind: "thread_created",
        body: threadAuditPayloadToBytes("thread_created", payload),
      },
    };
    const canonicalBytes = serializeAuditEventRecordForHash(canonicalRecord);

    expect(() => serializeAuditEventRecordForHash(canonicalRecord)).not.toThrow();
    expect(() =>
      serializeAuditEventRecordForHash({
        ...canonicalRecord,
        payload: {
          ...canonicalRecord.payload,
          body: TEXT_ENCODER.encode(JSON.stringify(payloadJson, null, 2)),
        },
      }),
    ).toThrow(/payload\.body must be canonical JSON for thread_created/);
    expect(() =>
      serializeAuditEventRecordForHash({
        ...canonicalRecord,
        payload: {
          ...canonicalRecord.payload,
          body: TEXT_ENCODER.encode('{"__proto__":1}'),
        },
      }),
    ).toThrow(/payload\.body invalid canonical JSON for thread_created/);
    expect(() =>
      serializeAuditEventRecordForHash({
        ...canonicalRecord,
        payload: {
          ...canonicalRecord.payload,
          body: TEXT_ENCODER.encode('{"createdAt":"2026-05-08T18:00:00.000Z"}'),
        },
      }),
    ).toThrow(/payload\.body invalid for thread_created/);
    expect(serializeAuditEventRecordForHash(canonicalRecord)).toStrictEqual(canonicalBytes);
  });

  it("covers thread command validators across all command kinds", () => {
    const create = validCreateCommand();
    const edit = validSpecEditCommand();
    const status = validStatusChangeCommand();

    expect(validateThreadCommand(7)).toEqual({
      ok: false,
      errors: [{ path: "", message: "must be an object" }],
    });
    expect(validateThreadCommand({ kind: "thread.delete" }).ok).toBe(false);
    expect(validateThreadCommand({ ...create, shadow: true }).ok).toBe(false);
    expect(validateThreadCommand({ ...create, idempotencyKey: "bad key" }).ok).toBe(false);
    expect(validateThreadCommand(edit)).toEqual({ ok: true });
    expect(validateThreadCommand({ ...edit, contentHash: sha256Hex("forged") }).ok).toBe(false);
    expect(validateThreadCommand(status)).toEqual({ ok: true });
    expect(validateThreadCommand({ ...status, fromStatus: "merged" }).ok).toBe(false);
  });

  it("covers status fold failure modes and allowed needs-review terminal path", () => {
    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: "bad" as unknown as typeof THREAD_ID },
      ]).ok,
    ).toBe(false);
    expect(
      validateThreadStatusFold([
        {
          kind: "thread_created",
          threadId: THREAD_ID,
          status: "closed" as unknown as "open",
        },
      ]).ok,
    ).toBe(false);
    expect(
      validateThreadStatusFold([{ ...validStatusChangedPayload(), kind: "thread_status_changed" }])
        .ok,
    ).toBe(false);
    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: THREAD_ID },
        {
          ...validStatusChangedPayload({ fromStatus: "in_progress", toStatus: "closed" }),
          kind: "thread_status_changed",
        },
      ]).ok,
    ).toBe(false);
    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: THREAD_ID },
        {
          ...validStatusChangedPayload({ toStatus: "needs_review" }),
          kind: "thread_status_changed",
        },
      ]).ok,
    ).toBe(false);
    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: THREAD_ID },
        {
          ...validStatusChangedPayload({ fromStatus: "open", toStatus: "in_progress" }),
          kind: "thread_status_changed",
        },
        {
          ...validStatusChangedPayload({ fromStatus: "in_progress", toStatus: "needs_review" }),
          kind: "thread_status_changed",
        },
        {
          ...validStatusChangedPayload({ fromStatus: "needs_review", toStatus: "merged" }),
          kind: "thread_status_changed",
        },
      ]),
    ).toEqual({ ok: true });
    expect(
      validateThreadStatusFold([
        { kind: "thread_created", threadId: THREAD_ID },
        {
          ...validStatusChangedPayload({ fromStatus: "open", toStatus: "closed" }),
          kind: "thread_status_changed",
        },
        {
          ...validStatusChangedPayload({ fromStatus: "open", toStatus: "in_progress" }),
          kind: "thread_status_changed",
        },
      ]).ok,
    ).toBe(false);
    const nonStatusEvent = {
      ...validStatusChangedPayload({ fromStatus: "open", toStatus: "in_progress" }),
      kind: "thread_spec_edited",
    } as unknown as Parameters<typeof validateThreadStatusFold>[0][number];
    expect(
      validateThreadStatusFold([{ kind: "thread_created", threadId: THREAD_ID }, nonStatusEvent]),
    ).toEqual({
      ok: false,
      errors: [
        {
          path: "/1/kind",
          message: "unexpected event kind in status fold: thread_spec_edited",
        },
      ],
    });
  });

  it("covers nested validators for malformed thread objects and projection helpers", () => {
    const thread = validThreadFixture();
    const sparseTaskIds = Array(MAX_THREAD_TASK_IDS + 1) as unknown as Thread["taskIds"];

    expect(validateThread(42)).toEqual({
      ok: false,
      errors: [{ path: "", message: "must be an object" }],
    });
    expect(validateThread({ ...thread, unknown: true }).ok).toBe(false);
    expect(validateThread({ ...thread, status: "bogus" }).ok).toBe(false);
    expect(validateThread({ ...thread, spec: "bad" }).ok).toBe(false);
    expect(
      validateThreadSpecRevision({
        ...validSpecRevision(),
        revisionId: "bad" as unknown as ThreadSpecRevision["revisionId"],
      }).ok,
    ).toBe(false);
    expect(
      validateThreadSpecRevision({
        ...validSpecRevision(),
        content: BigInt(1) as unknown as JsonValue,
      }).ok,
    ).toBe(false);
    expect(validateThread({ ...thread, externalRefs: 7 }).ok).toBe(false);
    expect(validateThread({ ...thread, taskIds: "bad" }).ok).toBe(false);
    expect(validateThread({ ...thread, taskIds: sparseTaskIds }).ok).toBe(false);
    expect(validateThread({ ...thread, taskIds: [thread.taskIds[0], thread.taskIds[0]] }).ok).toBe(
      false,
    );
    expect(validateThread({ ...thread, createdBy: "" }).ok).toBe(false);
    expect(validateThread({ ...thread, createdAt: new Date("invalid") }).ok).toBe(false);
    expect(validateThread({ ...thread, closedAt: UPDATED_AT }).ok).toBe(false);
    expect(
      validateThread({
        ...thread,
        status: "closed",
        closedAt: UPDATED_AT,
      }),
    ).toEqual({ ok: true });
    expect(
      validateThreadReceiptIndex(
        {
          ...thread,
          taskIds: [thread.taskIds[0], thread.taskIds[0]].filter(
            (taskId): taskId is Thread["taskIds"][number] => taskId !== undefined,
          ),
        },
        [],
      ).ok,
    ).toBe(false);
    expect(
      validateThreadForeignKeys({
        existingThreadIds: new Set([THREAD_ID]),
        specEdits: [validSpecEditedPayload({ threadId: OTHER_THREAD_ID })],
        statusChanges: [validStatusChangedPayload({ threadId: OTHER_THREAD_ID })],
        receipts: [validReceiptFixture()],
      }).ok,
    ).toBe(false);
  });
});

function validExternalRefs(): ThreadExternalRefs {
  return {
    sourceUrls: ["https://example.test/wuphf/743"],
    entityIds: ["issue:743"],
  };
}

function validSpecContent(): JsonValue {
  return {
    body: "Implement the thread protocol slice",
    checklist: ["receipt v2", "audit events", "stream invalidation"],
  };
}

function validSpecRevision(overrides: Partial<ThreadSpecRevision> = {}): ThreadSpecRevision {
  const content = overrides.content ?? validSpecContent();
  return {
    revisionId: REVISION_1,
    threadId: THREAD_ID,
    content,
    contentHash: threadSpecContentHash(content),
    authoredBy: SIGNER,
    authoredAt: CREATED_AT,
    ...overrides,
  };
}

function validThreadFixture(): Thread {
  return {
    id: THREAD_ID,
    title: "Thread protocol",
    status: "open",
    spec: validSpecRevision(),
    externalRefs: validExternalRefs(),
    taskIds: [asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW")],
    createdBy: SIGNER,
    createdAt: CREATED_AT,
    updatedAt: UPDATED_AT,
  };
}

function validSpecEditedPayload(
  overrides: Partial<ThreadSpecEditedAuditPayload> = {},
): ThreadSpecEditedAuditPayload {
  const content = overrides.content ?? validSpecContent();
  return {
    threadId: THREAD_ID,
    revisionId: REVISION_1,
    content,
    contentHash: threadSpecContentHash(content),
    authoredBy: SIGNER,
    authoredAt: CREATED_AT,
    ...overrides,
  };
}

function validCreatedPayload(
  overrides: Partial<ThreadCreatedAuditPayload> = {},
): ThreadCreatedAuditPayload {
  return {
    threadId: THREAD_ID,
    title: "Thread protocol",
    createdBy: SIGNER,
    createdAt: CREATED_AT,
    externalRefs: validExternalRefs(),
    ...overrides,
  };
}

function validStatusChangedPayload(
  overrides: Partial<ThreadStatusChangedAuditPayload> = {},
): ThreadStatusChangedAuditPayload {
  return {
    threadId: THREAD_ID,
    fromStatus: "open",
    toStatus: "in_progress",
    changedBy: SIGNER,
    changedAt: UPDATED_AT,
    ...overrides,
  };
}

function validCreateCommand(): Record<string, unknown> {
  return {
    kind: "thread.create",
    idempotencyKey: asIdempotencyKey("thread-create-command-1"),
    threadId: THREAD_ID,
    title: "Thread protocol",
    createdBy: SIGNER,
    createdAt: CREATED_AT,
    externalRefs: validExternalRefs(),
    content: validSpecContent(),
  };
}

function validSpecEditCommand(): Record<string, unknown> {
  const content = validSpecContent();
  return {
    kind: "thread.spec.edit",
    idempotencyKey: asIdempotencyKey("thread-spec-edit-command-1"),
    threadId: THREAD_ID,
    revisionId: REVISION_2,
    baseRevisionId: REVISION_1,
    content,
    contentHash: threadSpecContentHash(content),
    authoredBy: SIGNER,
    authoredAt: UPDATED_AT,
  };
}

function validStatusChangeCommand(): Record<string, unknown> {
  return {
    kind: "thread.status.change",
    idempotencyKey: asIdempotencyKey("thread-status-change-command-1"),
    threadId: THREAD_ID,
    fromStatus: "open",
    toStatus: "in_progress",
    changedBy: SIGNER,
    changedAt: UPDATED_AT,
  };
}

function validReceiptFixture(): ReceiptSnapshot {
  const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
  const taskId = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
  const writeId = "write_01";
  const proposedDiff = FrozenArgs.freeze({ before: "a", after: "b" });
  return {
    id: receiptId,
    agentSlug: "sam_agent" as ReceiptSnapshot["agentSlug"],
    taskId,
    triggerKind: "human_message",
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    startedAt: CREATED_AT,
    finishedAt: UPDATED_AT,
    status: "ok",
    providerKind: "openai" as ReceiptSnapshot["providerKind"],
    model: "gpt-5.2",
    promptHash: sha256Hex("prompt:v1"),
    toolManifest: sha256Hex("tool-manifest:v1"),
    toolCalls: [],
    approvals: [],
    filesChanged: [],
    commits: [],
    sourceReads: [],
    writes: [
      {
        writeId: writeId as ReceiptSnapshot["writes"][number]["writeId"],
        action: "write",
        target: "target",
        idempotencyKey: asIdempotencyKey("receipt-write-1"),
        proposedDiff,
        appliedDiff: proposedDiff,
        approvalToken: null,
        approvedAt: UPDATED_AT,
        result: "applied",
        postWriteVerify: proposedDiff,
      },
    ],
    inputTokens: 1,
    outputTokens: 1,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    costUsd: 0,
    finalMessage: SanitizedString.fromUnknown("done"),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [],
    wikiWrites: [],
    schemaVersion: 1,
  };
}
