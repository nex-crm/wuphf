import { EventEmitter } from "node:events";
import type { IncomingMessage, ServerResponse } from "node:http";
import { Readable } from "node:stream";

import {
  type ApprovalRequest,
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  lsnFromV1Number,
  type ThreadExternalRefs,
  threadSpecContentHash,
} from "@wuphf/protocol";
import type BetterSqlite3 from "better-sqlite3";
import { describe, expect, it } from "vitest";

import { openDatabase, runMigrations } from "../../src/event-log/index.ts";
import {
  InvalidListCursorError,
  ReceiptStoreBusyError,
  ReceiptStoreFullError,
  ReceiptStoreUnavailableError,
} from "../../src/receipt-store.ts";
import {
  ThreadCommandValidationError,
  ThreadConflictError,
  ThreadIdempotencyConflictError,
  ThreadNotFoundError,
} from "../../src/threads/appender.ts";
import type { ThreadStateRow, ThreadStateStore } from "../../src/threads/projections.ts";
import type { ThreadReceiptIndexStore } from "../../src/threads/receipt-index.ts";
import { handleThreadRoute, type ThreadRouteDeps } from "../../src/threads/routes.ts";
import type { BrokerLogger } from "../../src/types.ts";

const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FA0");
const REVISION_ID = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FB0");
const NEXT_REVISION_ID = asThreadSpecRevisionId("01CRZ3NDEKTSV4RRFFQ69G5FC0");
const SIGNER = asSignerIdentity("broker");
const CREATED_AT = new Date("2026-05-18T09:00:00.000Z");
const CONTENT = { goal: "unit route coverage" };
const EMPTY_EXTERNAL_REFS: ThreadExternalRefs = Object.freeze({
  sourceUrls: Object.freeze([]),
  entityIds: Object.freeze([]),
});

describe("handleThreadRoute unit error paths", () => {
  it("returns precise method and not-found responses for thread routes", async () => {
    const fix = deps();
    try {
      await expectRoute("/not-thread", "GET", fix, 0, false);
      await expectRoute("/api/v1/threads", "PUT", fix, 405);
      await expectRoute("/api/v1/threads/replay-check", "POST", fix, 405);
      await expectRoute(`/api/v1/threads/${THREAD_ID}`, "POST", fix, 405);
      await expectRoute(`/api/v1/threads/${THREAD_ID}/pinned-approvals`, "POST", fix, 405);
      await expectRoute(`/api/v1/threads/${THREAD_ID}/spec`, "GET", fix, 405);
      await expectRoute(`/api/v1/threads/${THREAD_ID}/status`, "GET", fix, 405);
      await expectRoute(`/api/v1/threads/${THREAD_ID}/unknown`, "GET", fix, 404);
      await expectRoute("/api/v1/threads/%E0%A4%A", "GET", fix, 404);
      await expectRoute(
        `/api/v1/threads/${THREAD_ID}/pinned-approvals`,
        "GET",
        {
          ...fix,
          state: stateStore({ getById: () => null }),
        },
        404,
      );
    } finally {
      fix.db.close();
    }
  });

  it("maps route parse and idempotency failures to structured 4xx responses", async () => {
    const fix = deps();
    try {
      await expectRoute("/api/v1/threads", "POST", fix, 415);
      await expectRoute("/api/v1/threads", "POST", fix, 400, true, "{");
      await expectRoute(
        "/api/v1/threads",
        "POST",
        fix,
        400,
        true,
        JSON.stringify({ ...createBody(), idempotencyKey: "" }),
        { error: "invalid_thread_command" },
      );
      await expectRoute(
        "/api/v1/threads",
        "POST",
        fix,
        400,
        true,
        JSON.stringify({ ...createBody(), idempotencyKey: "bad-key" }),
        { error: "idempotency_key_invalid" },
      );
      await expectRoute(
        "/api/v1/threads",
        "POST",
        fix,
        400,
        true,
        JSON.stringify({
          ...createBody(),
          idempotencyKey: "cmd_thread.status.change_01CRZ3NDEKTSV4RRFFQ69G5FC0",
        }),
        { error: "invalid_thread_command" },
      );
      await expectRoute(
        `/api/v1/threads/${THREAD_ID}/spec`,
        "PATCH",
        fix,
        400,
        true,
        JSON.stringify({ ...specBody(), idempotencyKey: "" }),
        { error: "invalid_thread_command" },
      );
      await expectRoute(
        `/api/v1/threads/${THREAD_ID}/status`,
        "PATCH",
        fix,
        400,
        true,
        JSON.stringify({ ...statusBody(), idempotencyKey: "bad-key" }),
        { error: "idempotency_key_invalid" },
      );
      await expectRoute(
        `/api/v1/threads/${THREAD_ID}/spec`,
        "PATCH",
        fix,
        413,
        true,
        JSON.stringify({ ...specBody(), content: "x".repeat(512 * 1_024) }),
        { error: "body_too_large" },
      );
    } finally {
      fix.db.close();
    }
  });

  it("maps appender and projection failures without leaking implementation details", async () => {
    const cases: readonly {
      readonly name: string;
      readonly path: string;
      readonly body: string;
      readonly error: Error;
      readonly status: number;
      readonly routeError: string;
    }[] = [
      {
        name: "validation",
        path: `/api/v1/threads/${THREAD_ID}/spec`,
        body: JSON.stringify(specBody()),
        error: new ThreadCommandValidationError([{ path: "/content", message: "bad" }]),
        status: 400,
        routeError: "invalid_thread_command",
      },
      {
        name: "missing thread",
        path: `/api/v1/threads/${THREAD_ID}/spec`,
        body: JSON.stringify(specBody()),
        error: new ThreadNotFoundError("missing thread"),
        status: 404,
        routeError: "not_found",
      },
      {
        name: "conflict",
        path: `/api/v1/threads/${THREAD_ID}/spec`,
        body: JSON.stringify(specBody()),
        error: new ThreadConflictError("revision_exists"),
        status: 409,
        routeError: "revision_exists",
      },
      {
        name: "idempotency conflict",
        path: "/api/v1/threads",
        body: JSON.stringify(createBody()),
        error: new ThreadIdempotencyConflictError(),
        status: 409,
        routeError: "idempotency_key_conflict",
      },
    ];
    for (const testCase of cases) {
      const fix = deps({ appenderError: testCase.error });
      try {
        const response = await callRoute(testCase.path, "PATCH", fix, true, testCase.body);
        if (testCase.path === "/api/v1/threads") {
          const createResponse = await callRoute(testCase.path, "POST", fix, true, testCase.body);
          expect(createResponse.statusCode, testCase.name).toBe(testCase.status);
          expect(createResponse.json()).toMatchObject({ error: testCase.routeError });
        } else {
          expect(response.statusCode, testCase.name).toBe(testCase.status);
          expect(response.json()).toMatchObject({ error: testCase.routeError });
        }
      } finally {
        fix.db.close();
      }
    }
  });

  it("maps read-side storage failures for list, get, pinned approvals, and replay-check", async () => {
    const listBusy = deps({ stateError: new ReceiptStoreBusyError("busy") });
    try {
      await expectRoute("/api/v1/threads", "GET", listBusy, 503, false, undefined, {
        error: "store_busy",
      });
    } finally {
      listBusy.db.close();
    }

    const invalidReceiptIndex = deps({
      receiptIndexError: new InvalidListCursorError(),
    });
    try {
      await expectRoute(
        `/api/v1/threads/${THREAD_ID}`,
        "GET",
        invalidReceiptIndex,
        500,
        false,
        undefined,
        {
          error: "receipt_index_invalid",
        },
      );
    } finally {
      invalidReceiptIndex.db.close();
    }

    const pinnedUnavailable = deps({
      approvalsError: new ReceiptStoreUnavailableError("unavailable"),
    });
    try {
      await expectRoute(
        `/api/v1/threads/${THREAD_ID}/pinned-approvals`,
        "GET",
        pinnedUnavailable,
        503,
        false,
        undefined,
        { error: "storage_error" },
      );
    } finally {
      pinnedUnavailable.db.close();
    }

    const replayFullBase = deps();
    const replayFull: ThreadRouteDeps = {
      ...replayFullBase,
      db: {
        prepare: () => {
          throw new ReceiptStoreFullError("full");
        },
      } as unknown as BetterSqlite3.Database,
    };
    try {
      await expectRoute("/api/v1/threads/replay-check", "GET", replayFull, 507, false, undefined, {
        error: "store_full",
      });
    } finally {
      replayFullBase.db.close();
    }
  });

  it("throws when a folded thread row fails protocol validation", async () => {
    const fix = deps({
      row: {
        ...threadRow(),
        title: "",
      },
    });
    try {
      await expect(callRoute("/api/v1/threads", "GET", fix)).rejects.toThrow(
        /thread projection failed validation/,
      );
    } finally {
      fix.db.close();
    }
  });
});

function createBody(): Readonly<Record<string, unknown>> {
  return {
    title: "Unit route thread",
    specContent: CONTENT,
    idempotencyKey: REVISION_ID,
  };
}

function specBody(): Readonly<Record<string, unknown>> {
  return {
    baseRevisionId: REVISION_ID,
    baseContentHash: threadSpecContentHash(CONTENT),
    content: { goal: "unit route coverage", version: 2 },
    idempotencyKey: NEXT_REVISION_ID,
  };
}

function statusBody(): Readonly<Record<string, unknown>> {
  return {
    fromStatus: "open",
    toStatus: "closed",
    idempotencyKey: NEXT_REVISION_ID,
  };
}

async function expectRoute(
  path: string,
  method: string,
  fix: ThreadRouteDeps,
  statusCode: number,
  json = false,
  body?: string,
  expectedBody?: Readonly<Record<string, unknown>>,
): Promise<void> {
  const response = await callRoute(path, method, fix, json, body);
  expect(response.handled).toBe(statusCode !== 0);
  if (statusCode !== 0) {
    expect(response.statusCode).toBe(statusCode);
  }
  if (expectedBody !== undefined) {
    expect(response.json()).toMatchObject(expectedBody);
  }
}

async function callRoute(
  path: string,
  method: string,
  fix: ThreadRouteDeps,
  json = false,
  body?: string,
): Promise<FakeResponse> {
  const req = new FakeRequest(method, path, body, json).asRequest();
  const res = new FakeResponse();
  const handled = await handleThreadRoute(req, res.asResponse(), path, fix);
  res.handled = handled;
  return res;
}

function deps(
  overrides: {
    readonly row?: ThreadStateRow;
    readonly appenderError?: Error;
    readonly stateError?: Error;
    readonly receiptIndexError?: Error;
    readonly approvalsError?: Error;
  } = {},
): ThreadRouteDeps {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const row = overrides.row ?? threadRow();
  return {
    db,
    appender: {
      appendCreateIdempotent(args) {
        if (overrides.appenderError !== undefined) throw overrides.appenderError;
        const applied = {
          threadId: THREAD_ID,
          headLsn: lsnFromV1Number(2),
          revisionId: REVISION_ID,
          contentHash: threadSpecContentHash(CONTENT),
          streamKind: "thread.created" as const,
        };
        const rendered = args.render(applied);
        return {
          replayed: false,
          statusCode: rendered.statusCode,
          payload: rendered.payload,
          applied,
        };
      },
      appendSpecEditIdempotent(args) {
        if (overrides.appenderError !== undefined) throw overrides.appenderError;
        const applied = {
          threadId: THREAD_ID,
          headLsn: lsnFromV1Number(3),
          revisionId: NEXT_REVISION_ID,
          contentHash: threadSpecContentHash({ goal: "unit route coverage", version: 2 }),
          streamKind: "thread.updated" as const,
        };
        const rendered = args.render(applied);
        return {
          replayed: false,
          statusCode: rendered.statusCode,
          payload: rendered.payload,
          applied,
        };
      },
      appendStatusChangeIdempotent(args) {
        if (overrides.appenderError !== undefined) throw overrides.appenderError;
        const applied = {
          threadId: THREAD_ID,
          headLsn: lsnFromV1Number(4),
          revisionId: NEXT_REVISION_ID,
          contentHash: threadSpecContentHash(CONTENT),
          streamKind: "thread.updated" as const,
        };
        const rendered = args.render(applied);
        return {
          replayed: false,
          statusCode: rendered.statusCode,
          payload: rendered.payload,
          applied,
        };
      },
    },
    state: stateStore({
      row,
      ...(overrides.stateError === undefined ? {} : { error: overrides.stateError }),
    }),
    receiptIndex: receiptIndexStore(overrides.receiptIndexError),
    approvals: approvalQuery(overrides.approvalsError),
    logger: LOGGER,
    nowMs: () => CREATED_AT.getTime(),
    emitThreadEvent: () => undefined,
  };
}

function stateStore(
  args: {
    readonly row?: ThreadStateRow;
    readonly error?: Error;
    readonly getById?: (threadId: typeof THREAD_ID) => ThreadStateRow | null;
  } = {},
): ThreadStateStore {
  const row = args.row ?? threadRow();
  return {
    applyEvent: () => undefined,
    rebuildFromLog: () => undefined,
    getById(threadId) {
      if (args.error !== undefined) throw args.error;
      if (args.getById !== undefined) return args.getById(threadId);
      return threadId === THREAD_ID ? row : null;
    },
    hasSpecRevision: () => false,
    list() {
      if (args.error !== undefined) throw args.error;
      return [row];
    },
  };
}

function receiptIndexStore(error?: Error): ThreadReceiptIndexStore {
  const maybeThrow = (): void => {
    if (error !== undefined) throw error;
  };
  return {
    applyEvent: () => undefined,
    clear: () => undefined,
    rebuildFromLog: () => undefined,
    list: () => ({ items: [], nextCursor: null }),
    refsForThread: () => {
      maybeThrow();
      return { receiptIds: [], taskIds: [] };
    },
    latestForThread: () => {
      maybeThrow();
      return null;
    },
  };
}

function approvalQuery(error?: Error): ThreadRouteDeps["approvals"] {
  const request = {
    id: "01DRZ3NDEKTSV4RRFFQ69G5FD0",
    claim: {
      schemaVersion: 1,
      claimId: "claim_unit_route",
      kind: "receipt_co_sign",
      receiptId: "01ERZ3NDEKTSV4RRFFQ69G5FE0",
      frozenArgsHash: threadSpecContentHash({ frozen: true }),
      riskClass: "critical",
    },
    scope: {
      mode: "single_use",
      claimId: "claim_unit_route",
      claimKind: "receipt_co_sign",
      role: "approver",
      maxUses: 1,
      receiptId: "01ERZ3NDEKTSV4RRFFQ69G5FE0",
      frozenArgsHash: threadSpecContentHash({ frozen: true }),
    },
    riskClass: "critical",
    threadId: THREAD_ID,
    receiptId: "01ERZ3NDEKTSV4RRFFQ69G5FE0",
    requestedBy: SIGNER,
    requestedAt: CREATED_AT,
    status: "rejected",
    decision: {
      decision: "reject",
      decidedBy: SIGNER,
      decidedAt: CREATED_AT,
    },
    schemaVersion: 1,
  } as ApprovalRequest;
  return {
    countPendingByThread() {
      if (error !== undefined) throw error;
      return 1;
    },
    listPendingByThread() {
      if (error !== undefined) throw error;
      return [{ approval: request, headLsn: lsnFromV1Number(5) }];
    },
    latestHeadLsnByThread() {
      if (error !== undefined) throw error;
      return lsnFromV1Number(5);
    },
  };
}

function threadRow(): ThreadStateRow {
  return {
    id: THREAD_ID,
    title: "Unit route thread",
    status: "open",
    headLsn: lsnFromV1Number(2),
    createdBy: SIGNER,
    createdAt: CREATED_AT,
    updatedAt: CREATED_AT,
    spec: {
      threadId: THREAD_ID,
      revisionId: REVISION_ID,
      content: CONTENT,
      contentHash: threadSpecContentHash(CONTENT),
      authoredBy: SIGNER,
      authoredAt: CREATED_AT,
    },
    externalRefs: EMPTY_EXTERNAL_REFS,
  };
}

class FakeRequest extends Readable {
  readonly headers: Record<string, string>;
  readonly method: string;
  readonly url: string;
  private sent = false;

  constructor(
    method: string,
    url: string,
    private readonly body?: string,
    json = false,
  ) {
    super();
    this.method = method;
    this.url = url;
    this.headers = json ? { "content-type": "application/json" } : {};
  }

  override _read(): void {
    if (this.sent) return;
    this.sent = true;
    if (this.body !== undefined) {
      this.push(Buffer.from(this.body, "utf8"));
    }
    this.push(null);
  }

  asRequest(): IncomingMessage {
    return this as unknown as IncomingMessage;
  }
}

class FakeResponse extends EventEmitter {
  statusCode = 0;
  handled = false;
  writableEnded = false;
  destroyed = false;
  writableLength = 0;
  readonly chunks: Buffer[] = [];
  readonly headers = new Map<string, string>();

  writeHead(statusCode: number, headers: Readonly<Record<string, string>> = {}): this {
    this.statusCode = statusCode;
    for (const [key, value] of Object.entries(headers)) {
      this.headers.set(key.toLowerCase(), value);
    }
    return this;
  }

  setHeader(name: string, value: string): this {
    this.headers.set(name.toLowerCase(), value);
    return this;
  }

  write(chunk: string | Buffer): boolean {
    this.chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk, "utf8"));
    return true;
  }

  end(chunk?: string | Buffer): this {
    if (chunk !== undefined) this.write(chunk);
    this.writableEnded = true;
    return this;
  }

  asResponse(): ServerResponse {
    return this as unknown as ServerResponse;
  }

  text(): string {
    return Buffer.concat(this.chunks).toString("utf8");
  }

  json(): unknown {
    return JSON.parse(this.text()) as unknown;
  }
}

const LOGGER: BrokerLogger = {
  info: () => undefined,
  warn: () => undefined,
  error: () => undefined,
};
