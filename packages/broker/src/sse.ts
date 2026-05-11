// SSE (Server-Sent Events) stream owner.
//
// Branch-4 scope: the `/api/events` endpoint accepts authenticated subscribers
// and emits a single named `ready` event so the renderer can prove the
// loopback contract end-to-end. Real event payloads (`receipt.created`,
// `approval.requested`, etc.) come in later branches; the framing — named
// events with JSON data and `id` — is locked here so subsequent branches
// only add event kinds, not transport mechanics.
//
// Wire format (one event):
//
//   id: <ulid-or-monotonic-id>
//   event: ready
//   data: {"emittedAt":"2026-05-10T12:00:00.000Z"}
//   <blank line>
//
// Heartbeat: a `: keepalive` comment every `KEEPALIVE_MS` so intermediaries
// (none on loopback today, but Electron-shipped extensions could add one)
// do not drop idle streams.

import type { ServerResponse } from "node:http";

const KEEPALIVE_MS = 30_000;

export interface SseSession {
  close(): void;
}

export interface SseSessionOptions {
  /**
   * Override the keepalive interval. Tests pass a small value to assert the
   * keepalive fires; the listener uses the default in production.
   */
  readonly keepaliveMs?: number;
  /**
   * Override the `Date` used for the `ready` event's `emittedAt`. The
   * production caller does not pass this; tests inject a frozen instant.
   */
  readonly nowIso?: string;
  /**
   * Override the event id source. Production uses a monotonic counter
   * (`broker_<seq>`); tests pass a fixed value to assert framing.
   */
  readonly idForReady?: string;
}

export function startSseSession(res: ServerResponse, opts: SseSessionOptions = {}): SseSession {
  res.statusCode = 200;
  res.setHeader("Content-Type", "text/event-stream; charset=utf-8");
  res.setHeader("Cache-Control", "no-store");
  res.setHeader("Connection", "keep-alive");
  res.setHeader("X-Accel-Buffering", "no");
  // Flush headers so the renderer's `EventSource.onopen` fires before the
  // first event is written. Without this, Node buffers the headers until the
  // first chunk lands and the renderer sees the open + first event together.
  res.flushHeaders();

  const emittedAt = opts.nowIso ?? new Date().toISOString();
  const id = opts.idForReady ?? "broker_ready_0";
  res.write(formatEvent({ id, event: "ready", data: { emittedAt } }));

  const keepaliveMs = opts.keepaliveMs ?? KEEPALIVE_MS;
  const keepalive = setInterval(() => {
    if (res.writableEnded) return;
    try {
      res.write(": keepalive\n\n");
    } catch {
      // `writableEnded` is false while the response is *destroying* (after a
      // socket error but before `'close'` fires). A `res.write` here throws
      // ERR_STREAM_DESTROYED synchronously; tear the interval down rather
      // than letting it become an unhandled exception every keepalive tick.
      clearInterval(keepalive);
    }
  }, keepaliveMs);
  // `unref` so a hanging keepalive does not keep the broker process alive
  // after `BrokerHandle.stop` closes the listener.
  keepalive.unref();

  let closed = false;
  const close = (): void => {
    if (closed) return;
    closed = true;
    clearInterval(keepalive);
    if (!res.writableEnded) {
      res.end();
    }
  };

  res.on("close", close);
  res.on("error", close);

  return { close };
}

interface SseEvent {
  readonly id: string;
  readonly event: string;
  readonly data: Readonly<Record<string, unknown>>;
}

function formatEvent(event: SseEvent): string {
  return `id: ${event.id}\nevent: ${event.event}\ndata: ${JSON.stringify(event.data)}\n\n`;
}
