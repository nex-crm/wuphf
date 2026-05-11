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

import type { ReceiptStore } from "./receipt-store.ts";
import type { BrokerLogger } from "./types.ts";

// 1 MiB body budget. Receipt boundary-budget validation (see
// @wuphf/protocol#validateReceiptBudget) caps individual receipt size well
// below this; the limit here is a coarse pre-parse abort so a malicious or
// runaway producer can't stream gigabytes into V8's parser. If a future
// receipt schema needs more, raise this value here AND review the
// protocol-level boundary budget.
const MAX_RECEIPT_BODY_BYTES = 1_048_576;

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
  // wrong".
  const contentType = req.headers["content-type"];
  if (
    typeof contentType !== "string" ||
    !contentType.toLowerCase().startsWith("application/json")
  ) {
    deps.logger.warn("receipt_post_rejected", { reason: "unsupported_media_type" });
    res.writeHead(415, { "Content-Type": "text/plain" });
    res.end("unsupported_media_type");
    return;
  }

  // Fast-path: trust an honest Content-Length over MAX_RECEIPT_BODY_BYTES
  // and reject without ever opening the body stream. Cheap protection
  // against producers that announce their oversize intent up front.
  const contentLength = req.headers["content-length"];
  if (typeof contentLength === "string") {
    const parsed = Number(contentLength);
    if (Number.isFinite(parsed) && parsed > MAX_RECEIPT_BODY_BYTES) {
      deps.logger.warn("receipt_post_rejected", { reason: "body_too_large_declared" });
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
      // the connection terminates.
      deps.logger.warn("receipt_post_rejected", { reason: "body_too_large_streamed" });
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
    res.writeHead(400, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "invalid_receipt", reason }));
    return;
  }

  const result = await deps.receiptStore.put(receipt);
  if (result.existed) {
    deps.logger.info("receipt_put_conflict", { receiptId: receipt.id });
    res.writeHead(409, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "receipt_id_exists", id: receipt.id }));
    return;
  }

  deps.logger.info("receipt_put_ok", { receiptId: receipt.id });
  res.writeHead(201, {
    "Content-Type": "application/json",
    Location: `/api/receipts/${encodeURIComponent(receipt.id)}`,
  });
  res.end(receiptToJson(receipt));
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
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(receiptToJson(receipt));
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

  const list = await deps.receiptStore.list({ threadId });
  res.writeHead(200, { "Content-Type": "application/json" });
  // Serialize each via the codec so the wire shape is identical to the
  // single-receipt GET response. Concatenate as a JSON array.
  res.end(`[${list.map((r) => receiptToJson(r)).join(",")}]`);
}

function notFoundJson(res: ServerResponse): void {
  res.writeHead(404, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: "not_found" }));
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
