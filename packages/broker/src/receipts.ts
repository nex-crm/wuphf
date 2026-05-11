// /api/receipts route handlers.
//
// Wire contract (mirrors @wuphf/protocol#receiptToJson/receiptFromJson):
//   POST /api/receipts          — body: receipt JSON. 201 + body on insert,
//                                 409 if `receipt.id` already exists, 400
//                                 on parse/validation error, 413 on
//                                 oversize body, 415 on wrong content-type.
//   GET  /api/receipts/:id      — 200 + body on hit, 404 on miss / bad id.
//   GET  /api/threads/:tid/receipts — list-scope-by-thread. 200 + JSON
//                                 array, 404 on bad thread id shape.
//
// Bearer auth is enforced at the listener level (default-deny gate on
// `/api/*`); these handlers assume the caller has already passed
// `authorize()` and only handle shape/storage concerns.

import type { IncomingMessage, ServerResponse } from "node:http";

import {
  asReceiptId,
  asThreadId,
  type ReceiptId,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  type ThreadId,
} from "@wuphf/protocol";

import { type ReceiptStore, ReceiptStoreFullError } from "./receipt-store.ts";
import type { BrokerLogger } from "./types.ts";

// 1 MiB body budget. This is the broker's wire-layer pre-parse cap and is
// INTENTIONALLY stricter than the protocol-level receipt budget (see
// `@wuphf/protocol/src/budgets.ts:MAX_RECEIPT_BYTES`, 10 MiB) — the
// protocol cap is the global semantic ceiling for a single receipt across
// all transports (file dumps, IPC, future event logs); the broker cap is
// a coarse network-layer pre-parse abort so a malicious or runaway HTTP
// producer can't stream gigabytes into V8's parser before the boundary
// validator gets a chance to weigh in. A receipt larger than 1 MiB but
// smaller than the protocol cap is rejected at this layer with 413; raise
// this value if a future use case needs to submit larger receipts over
// HTTP, but do NOT raise it past the protocol cap.
const MAX_RECEIPT_BODY_BYTES = 1_048_576;

// Hard ceiling for thread-scoped list responses. Without a limit a single
// thread can accumulate enough receipts to make `GET /api/threads/:tid/
// receipts` a memory-pressure path (the response is materialized as one
// JSON-array string before send). Cursor-aware pagination handling on
// this route lands in a follow-up commit in branch 6 (along with `Link:
// rel="next"`); until then the route requests the first page from the
// store at the maximum allowed limit, preserving the branch-5 wire shape.
const MAX_THREAD_LIST_RECEIPTS = 1_000;

interface ReceiptRouteDeps {
  readonly receiptStore: ReceiptStore;
  readonly logger: BrokerLogger;
}

export async function handleReceiptCreate(
  req: IncomingMessage,
  res: ServerResponse,
  deps: ReceiptRouteDeps,
): Promise<void> {
  if (req.method !== "POST") {
    deps.logger.warn("receipt_post_rejected", { reason: "method_not_allowed" });
    res.writeHead(405, { Allow: "POST", "Content-Type": "text/plain" });
    res.end("method_not_allowed");
    return;
  }

  // Be strict about Content-Type: callers that send `text/plain` with a
  // JSON-looking body are likely confused about the wire shape (e.g., a
  // curl-from-script that forgot `-H content-type`). Reject early so the
  // failure mode is "415, your client is wrong" not "400, my parser is
  // wrong". Match exactly `application/json` (optional whitespace and
  // optional charset/structured-suffix parameters) — a bare prefix check
  // would accept `application/jsonp` or `application/json-bogus`.
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !isJsonMediaType(contentType)) {
    deps.logger.warn("receipt_post_rejected", { reason: "unsupported_media_type" });
    res.writeHead(415, { "Content-Type": "text/plain" });
    res.end("unsupported_media_type");
    return;
  }

  // Fast-path: trust an honest Content-Length over MAX_RECEIPT_BODY_BYTES
  // and reject without ever opening the body stream. Cheap protection
  // against producers that announce their oversize intent up front. We
  // cap the logged size at MAX_RECEIPT_BODY_BYTES + 1 to keep the log
  // bounded — a client claiming Content-Length: 9_999_999_999 should not
  // become a 13-digit log entry.
  const contentLength = req.headers["content-length"];
  if (typeof contentLength === "string") {
    const parsed = Number(contentLength);
    if (Number.isFinite(parsed) && parsed > MAX_RECEIPT_BODY_BYTES) {
      deps.logger.warn("receipt_post_rejected", {
        reason: "body_too_large_declared",
        payloadBytes: Math.min(parsed, MAX_RECEIPT_BODY_BYTES + 1),
      });
      writePayloadTooLarge(res);
      return;
    }
  }

  let body: string;
  try {
    body = await readBodyAsString(req, MAX_RECEIPT_BODY_BYTES, res);
  } catch (err) {
    const reason = err instanceof Error ? err.message : "body_read_error";
    if (reason === "body_too_large") {
      // The 413 has already been written by readBodyAsString — it owns the
      // response close-handshake so the client receives the status before
      // the connection terminates. payloadBytes is capped at the body
      // limit + 1 (we paused the stream at the overflow byte).
      deps.logger.warn("receipt_post_rejected", {
        reason: "body_too_large_streamed",
        payloadBytes: MAX_RECEIPT_BODY_BYTES + 1,
      });
      return;
    }
    deps.logger.warn("receipt_post_rejected", { reason: "bad_body" });
    if (!res.writableEnded) {
      res.writeHead(400, { "Content-Type": "text/plain" });
      res.end("bad_body");
    }
    return;
  }

  // receiptFromJson runs the full validator (boundary budget, frozen-args
  // canonicalization, shape, branded ids). Any failure is a 400 with the
  // validator's own message — protocol-grade errors are structured and
  // safe to surface (no internal state leaks).
  let receipt: ReceiptSnapshot;
  try {
    receipt = receiptFromJson(body);
  } catch (err) {
    const reason = err instanceof Error ? err.message : "validation_failed";
    deps.logger.warn("receipt_post_rejected", { reason: "invalid_receipt" });
    writeJsonResponse(res, 400, JSON.stringify({ error: "invalid_receipt", reason }));
    return;
  }

  let result: { readonly existed: boolean };
  try {
    result = await deps.receiptStore.put(receipt);
  } catch (err) {
    if (err instanceof ReceiptStoreFullError) {
      deps.logger.warn("receipt_post_rejected", { reason: "store_full" });
      writeJsonResponse(res, 507, JSON.stringify({ error: "store_full" }));
      return;
    }
    throw err;
  }
  if (result.existed) {
    deps.logger.info("receipt_put_conflict", { receiptId: receipt.id });
    writeJsonResponse(res, 409, JSON.stringify({ error: "receipt_id_exists", id: receipt.id }));
    return;
  }

  deps.logger.info("receipt_put_ok", { receiptId: receipt.id });
  writeJsonResponse(res, 201, receiptToJson(receipt), {
    Location: `/api/receipts/${encodeURIComponent(receipt.id)}`,
  });
}

export async function handleReceiptGet(
  pathname: string,
  res: ServerResponse,
  deps: { readonly receiptStore: ReceiptStore },
): Promise<void> {
  const idSegment = pathname.slice("/api/receipts/".length);
  if (idSegment.length === 0 || idSegment.includes("/")) {
    notFoundJson(res);
    return;
  }

  let decoded: string;
  try {
    decoded = decodeURIComponent(idSegment);
  } catch {
    notFoundJson(res);
    return;
  }

  let id: ReceiptId;
  try {
    id = asReceiptId(decoded);
  } catch {
    notFoundJson(res);
    return;
  }

  const receipt = await deps.receiptStore.get(id);
  if (receipt === null) {
    notFoundJson(res);
    return;
  }
  writeJsonResponse(res, 200, receiptToJson(receipt));
}

export async function handleThreadReceiptsList(
  pathname: string,
  res: ServerResponse,
  deps: { readonly receiptStore: ReceiptStore },
): Promise<void> {
  // /api/threads/:tid/receipts — extract :tid and require an exact match.
  const prefix = "/api/threads/";
  const suffix = "/receipts";
  if (!pathname.startsWith(prefix) || !pathname.endsWith(suffix)) {
    notFoundJson(res);
    return;
  }
  const tidSegment = pathname.slice(prefix.length, pathname.length - suffix.length);
  if (tidSegment.length === 0 || tidSegment.includes("/")) {
    notFoundJson(res);
    return;
  }

  let decoded: string;
  try {
    decoded = decodeURIComponent(tidSegment);
  } catch {
    notFoundJson(res);
    return;
  }

  let threadId: ThreadId;
  try {
    threadId = asThreadId(decoded);
  } catch {
    notFoundJson(res);
    return;
  }

  const page = await deps.receiptStore.list({ threadId, limit: MAX_THREAD_LIST_RECEIPTS });
  // Branch-5 wire shape is a bare JSON array. The cursor-aware route
  // signature (`?cursor=&limit=`, `Link: rel="next"` header) lands in a
  // follow-up commit on this branch — until then we always request the
  // first page at the max limit so behavior is identical to before.
  const body = `[${page.items.map((r) => receiptToJson(r)).join(",")}]`;
  writeJsonResponse(res, 200, body);
}

function notFoundJson(res: ServerResponse): void {
  writeJsonResponse(res, 404, JSON.stringify({ error: "not_found" }));
}

// Shared JSON response helper. Sets `Content-Type: application/json;
// charset=utf-8`, `Cache-Control: no-store`, and a byte-accurate
// `Content-Length` for every receipt-route JSON reply. Matches the
// convention established by `/api-token` and `/api/health` so future
// route authors have one pattern to copy, not two.
function writeJsonResponse(
  res: ServerResponse,
  status: number,
  body: string,
  extraHeaders: Record<string, string> = {},
): void {
  const headers: Record<string, string> = {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
    ...extraHeaders,
  };
  res.writeHead(status, headers);
  res.end(body);
}

// Exact `application/json` media-type match (per RFC 7231 §3.1.1.5).
// Accepts `application/json`, optional whitespace, and optional
// `; charset=...` or `; <param>=...` tails. Rejects `application/jsonp`,
// `application/json-foo`, and other prefix collisions.
function isJsonMediaType(value: string): boolean {
  const semi = value.indexOf(";");
  const head = (semi === -1 ? value : value.slice(0, semi)).trim().toLowerCase();
  return head === "application/json";
}

// Stream the request body into a string, aborting once the cumulative
// byte count exceeds `maxBytes`. On overflow we WRITE the 413 first (so
// the client receives the status before the connection closes), set
// `Connection: close` so HTTP/1.1 knows not to reuse the socket, then
// end the response — which terminates the upstream half cleanly. We do
// NOT call `req.destroy()`: tearing down the socket mid-write loses the
// 413 in transit, surfacing as a network-level error on the client
// instead of a clean status code.
function readBodyAsString(
  req: IncomingMessage,
  maxBytes: number,
  res: ServerResponse,
): Promise<string> {
  return new Promise((resolveFn, rejectFn) => {
    let receivedBytes = 0;
    const chunks: Buffer[] = [];
    let settled = false;
    const finish = (run: () => void): void => {
      if (settled) return;
      settled = true;
      run();
    };
    req.on("data", (chunk: Buffer) => {
      receivedBytes += chunk.length;
      if (receivedBytes > maxBytes) {
        // Stop accumulating; further chunks are discarded.
        req.pause();
        if (!res.writableEnded) {
          writePayloadTooLarge(res);
        }
        finish(() => rejectFn(new Error("body_too_large")));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => {
      finish(() => resolveFn(Buffer.concat(chunks).toString("utf8")));
    });
    req.on("error", (err) => {
      finish(() => rejectFn(err));
    });
    req.on("close", () => {
      finish(() => rejectFn(new Error("body_read_aborted")));
    });
  });
}

function writePayloadTooLarge(res: ServerResponse): void {
  res.writeHead(413, {
    "Content-Type": "text/plain",
    Connection: "close",
  });
  res.end("body_too_large");
}
