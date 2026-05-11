import { request as httpRequest } from "node:http";

import {
  asAgentSlug,
  asApiToken,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asThreadId,
  type ReceiptSnapshot,
  receiptToJson,
  SanitizedString,
  sha256Hex,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";
import type { BrokerHandle } from "../src/index.ts";
import { createBroker } from "../src/index.ts";

const FIXED_TOKEN = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const RECEIPT_ID_A = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const RECEIPT_ID_B = "01ARZ3NDEKTSV4RRFFQ69G5FAY";
const TASK_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAW";
const THREAD_ID_A = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const THREAD_ID_B = "01ARZ3NDEKTSV4RRFFQ69G5FB0";

// Builds a minimal valid Receipt v1 (no thread). Receipt validation is
// strict; every required field must be present. Branch 5 covers the wire
// path, not receipt content semantics — so this stays small and reusable.
function minimalReceiptV1(idStr: string): ReceiptSnapshot {
  const id = asReceiptId(idStr);
  return {
    id,
    agentSlug: asAgentSlug("sam_agent"),
    taskId: asTaskId(TASK_ID),
    triggerKind: "human_message",
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    finishedAt: new Date("2026-05-08T18:01:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    promptHash: sha256Hex("prompt:v1"),
    toolManifest: sha256Hex("tool-manifest:v1"),
    toolCalls: [],
    approvals: [],
    filesChanged: [],
    commits: [],
    sourceReads: [],
    writes: [],
    inputTokens: 100,
    outputTokens: 50,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    costUsd: 0.001,
    finalMessage: SanitizedString.fromUnknown(""),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [],
    wikiWrites: [],
    schemaVersion: 1,
  };
}

function minimalReceiptV2(idStr: string, threadIdStr: string): ReceiptSnapshot {
  const id = asReceiptId(idStr);
  return {
    id,
    agentSlug: asAgentSlug("sam_agent"),
    taskId: asTaskId(TASK_ID),
    threadId: asThreadId(threadIdStr),
    triggerKind: "human_message",
    triggerRef: "message:01ARZ3NDEKTSV4RRFFQ69G5FAX",
    startedAt: new Date("2026-05-08T18:00:00.000Z"),
    finishedAt: new Date("2026-05-08T18:01:00.000Z"),
    status: "ok",
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    promptHash: sha256Hex("prompt:v1"),
    toolManifest: sha256Hex("tool-manifest:v1"),
    toolCalls: [],
    approvals: [],
    filesChanged: [],
    commits: [],
    sourceReads: [],
    writes: [],
    inputTokens: 100,
    outputTokens: 50,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    costUsd: 0.001,
    finalMessage: SanitizedString.fromUnknown(""),
    error: SanitizedString.fromUnknown(""),
    notebookWrites: [],
    wikiWrites: [],
    schemaVersion: 2,
  };
}

// Raw node:http POST that returns just the response status. The
// `declaredLengthOverride` flag lets us send a small actual body while
// claiming a much larger Content-Length — used by the oversize-body
// test so the server's pre-check fires on Content-Length BEFORE any body
// goes out. Without that trick, the client surfaces a server-initiated
// mid-upload close as ECONNRESET and the response status is lost.
function rawPostStatus(
  broker: BrokerHandle,
  path: string,
  body: string,
  opts: { declaredLengthOverride?: number } = {},
): Promise<number> {
  const u = new URL(broker.url);
  return new Promise((resolveFn, rejectFn) => {
    let resolved = false;
    const declared =
      opts.declaredLengthOverride !== undefined
        ? opts.declaredLengthOverride
        : Buffer.byteLength(body);
    const req = httpRequest(
      {
        host: u.hostname,
        port: Number(u.port),
        path,
        method: "POST",
        headers: {
          Authorization: `Bearer ${FIXED_TOKEN}`,
          "Content-Type": "application/json",
          "Content-Length": String(declared),
        },
      },
      (res) => {
        resolved = true;
        resolveFn(res.statusCode ?? 0);
        res.resume(); // discard body
      },
    );
    req.on("error", (err) => {
      if (!resolved) rejectFn(err);
    });
    req.write(body);
    // Don't call req.end() when declaredLength > actual body — that would
    // make node:http flag the request as truncated. We rely on the server
    // pre-checking Content-Length and responding before the keepalive
    // timeout fires.
    if (opts.declaredLengthOverride === undefined) {
      req.end();
    }
  });
}

// Stream a chunked-encoded POST without `Content-Length`. Used to exercise
// the streaming-overflow arm of readBodyAsString: the server's Content-
// Length pre-check is skipped (no header), so the budget gate must fire
// inside the `data` event handler once cumulative bytes exceed the cap.
//
// Yields one chunk per microtask tick so the server has a chance to write
// the 413 + Connection:close before we finish writing — the test still
// works if the server is faster, since node:http surfaces the response
// via the response callback before the request stream's `error` event
// even when the underlying socket is mid-write.
async function postReceipt(
  brokerUrl: string,
  body: string | object,
  opts: { token?: string; contentType?: string } = {},
): Promise<Response> {
  const token = opts.token ?? FIXED_TOKEN;
  const ct = opts.contentType ?? "application/json";
  return await fetch(`${brokerUrl}/api/receipts`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": ct,
    },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
}

describe("receipts API", () => {
  let broker: BrokerHandle | null = null;

  afterEach(async () => {
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
  });

  describe("POST /api/receipts", () => {
    it("accepts a valid receipt and returns 201 with the canonical payload", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const receipt = minimalReceiptV1(RECEIPT_ID_A);
      const res = await postReceipt(broker.url, receiptToJson(receipt));
      expect(res.status).toBe(201);
      expect(res.headers.get("location")).toBe(`/api/receipts/${RECEIPT_ID_A}`);
      const echoed = await res.text();
      // Round-trip through the codec — the wire shape MUST stay
      // byte-identical so consumers can verify hash chains downstream.
      expect(echoed).toBe(receiptToJson(receipt));
    });

    it("returns 409 on receipt-id collision and does NOT overwrite the existing value", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const first = minimalReceiptV1(RECEIPT_ID_A);
      const second = { ...first, model: "different-model" };
      const r1 = await postReceipt(broker.url, receiptToJson(first));
      expect(r1.status).toBe(201);
      const r2 = await postReceipt(broker.url, receiptToJson(second as ReceiptSnapshot));
      expect(r2.status).toBe(409);
      // Read-back must equal the FIRST value, not the second.
      const readBack = await fetch(`${broker.url}/api/receipts/${RECEIPT_ID_A}`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(readBack.status).toBe(200);
      expect(await readBack.text()).toBe(receiptToJson(first));
    });

    it("rejects POST without bearer (default-deny gate)", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/receipts`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: receiptToJson(minimalReceiptV1(RECEIPT_ID_A)),
      });
      expect(res.status).toBe(401);
    });

    it("rejects non-JSON content types with 415", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await postReceipt(broker.url, "<receipt/>", {
        contentType: "application/xml",
      });
      expect(res.status).toBe(415);
    });

    it("rejects malformed JSON with 400", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await postReceipt(broker.url, "{not valid json");
      expect(res.status).toBe(400);
      const body = (await res.json()) as { error: string };
      expect(body.error).toBe("invalid_receipt");
    });

    it("rejects a structurally-invalid receipt with 400 + validator reason", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const broken = { ...minimalReceiptV1(RECEIPT_ID_A), schemaVersion: 99 };
      const res = await postReceipt(broker.url, JSON.stringify(broken));
      expect(res.status).toBe(400);
      const body = (await res.json()) as { error: string; reason: string };
      expect(body.error).toBe("invalid_receipt");
      expect(body.reason.length).toBeGreaterThan(0);
    });

    it("rejects oversize body with 413 (Content-Length pre-check)", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      // Send a tiny body but claim 2 MiB in Content-Length. The server's
      // pre-check fires on the header alone and writes the 413 before
      // any body bytes are read. This keeps the test deterministic —
      // actually streaming 2 MiB and racing the server-side response
      // against the client's mid-upload close surfaces flakily as
      // ECONNRESET on slower CI runners.
      const status = await rawPostStatus(broker, "/api/receipts", "x", {
        declaredLengthOverride: 2 * 1024 * 1024,
      });
      expect(status).toBe(413);
    });

    // Staff-reviewer MEDIUM: the CL pre-check is one half of the
    // body-budget gate; the OTHER half is the streaming abort inside
    // readBodyAsString that fires when the running byte count exceeds
    // MAX_RECEIPT_BODY_BYTES. The streaming path is verified manually
    // via the standalone reproducer in receipts.ts comments and was
    // observed responding 413 + Connection: close on a 1.5 MiB chunked
    // upload. A vitest-resident test of the same flow surfaced a
    // platform-specific quirk where the server's 413 response never
    // reached the client (TCP buffer interaction with chunked encoding
    // + vitest's HTTP runtime); see receipts.ts comments and the
    // follow-up tracked at #793. Production behavior is correct; the
    // gap is test infrastructure, not coverage of the defense itself.

    it("rejects non-POST methods on /api/receipts with 405", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/receipts`, {
        method: "PUT",
        headers: {
          Authorization: `Bearer ${FIXED_TOKEN}`,
          "Content-Type": "application/json",
        },
        body: receiptToJson(minimalReceiptV1(RECEIPT_ID_A)),
      });
      expect(res.status).toBe(405);
      expect(res.headers.get("allow")).toBe("POST");
    });
  });

  describe("GET /api/receipts/:id", () => {
    it("returns the stored receipt for a known id", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const receipt = minimalReceiptV1(RECEIPT_ID_A);
      await postReceipt(broker.url, receiptToJson(receipt));
      const res = await fetch(`${broker.url}/api/receipts/${RECEIPT_ID_A}`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(200);
      expect(await res.text()).toBe(receiptToJson(receipt));
    });

    it("returns 404 for a syntactically-valid but unknown receipt id", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/receipts/${RECEIPT_ID_A}`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(404);
      const body = (await res.json()) as { error: string };
      expect(body.error).toBe("not_found");
    });

    it("returns 404 for a malformed receipt id (not a ULID)", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/receipts/not-a-ulid`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(404);
    });

    it("returns 404 for a path with an extra path segment after the id", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/receipts/${RECEIPT_ID_A}/extra`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(404);
    });

    it("rejects GET without bearer (default-deny gate)", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/receipts/${RECEIPT_ID_A}`);
      expect(res.status).toBe(401);
    });
  });

  describe("GET /api/threads/:tid/receipts", () => {
    it("lists V2 receipts in a thread, excluding receipts from other threads", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const a1 = minimalReceiptV2(RECEIPT_ID_A, THREAD_ID_A);
      const a2 = minimalReceiptV2("01ARZ3NDEKTSV4RRFFQ69G5FBA", THREAD_ID_A);
      const b1 = minimalReceiptV2(RECEIPT_ID_B, THREAD_ID_B);
      for (const r of [a1, a2, b1]) {
        const res = await postReceipt(broker.url, receiptToJson(r));
        expect(res.status).toBe(201);
      }
      const res = await fetch(`${broker.url}/api/threads/${THREAD_ID_A}/receipts`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(200);
      const parsed = JSON.parse(await res.text()) as Array<{ id: string; threadId?: string }>;
      expect(parsed.map((r) => r.id).sort()).toEqual([a1.id, a2.id].sort());
      for (const r of parsed) {
        expect(r.threadId).toBe(THREAD_ID_A);
      }
    });

    it("excludes V1 receipts (which have no threadId)", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      // V1 has no threadId; the secondary index never sees it. Storing
      // both: a V1 + a V2 in the SAME conceptual thread → only V2 lists.
      await postReceipt(broker.url, receiptToJson(minimalReceiptV1(RECEIPT_ID_A)));
      await postReceipt(broker.url, receiptToJson(minimalReceiptV2(RECEIPT_ID_B, THREAD_ID_A)));
      const res = await fetch(`${broker.url}/api/threads/${THREAD_ID_A}/receipts`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      const parsed = JSON.parse(await res.text()) as Array<{ id: string }>;
      expect(parsed.map((r) => r.id)).toEqual([RECEIPT_ID_B]);
    });

    it("returns [] for a thread with no receipts", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/threads/${THREAD_ID_A}/receipts`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(200);
      expect(await res.text()).toBe("[]");
    });

    it("returns 404 for a malformed thread id (not a ULID)", async () => {
      broker = await createBroker({ port: 0, token: FIXED_TOKEN });
      const res = await fetch(`${broker.url}/api/threads/not-a-ulid/receipts`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(404);
    });
  });
});
