// SSE (Server-Sent Events) stream owner.
//
// Branch-4 scope: the `/api/events` endpoint accepts authenticated subscribers
// and emits a single named `ready` event so the renderer can prove the
// loopback contract end-to-end. Real event payloads (`receipt.created`,
// `approval.requested`, etc.) come in later branches; the framing — named
// events with JSON data and `id` — is locked here so subsequent branches
// only add event kinds, not transport mechanics. Thread events are
// invalidation-only: clients MUST refetch on `ready`, reconnect, and every
// thread event. Last-Event-ID log backfill is intentionally deferred.
//
// Wire format (one event):
//
//   id: <ready-id-or-committed-event-lsn>
//   event: ready
//   data: {"emittedAt":"2026-05-10T12:00:00.000Z"}
//   <blank line>
//
// Heartbeat: a `: keepalive` comment every `KEEPALIVE_MS` so intermediaries
// (none on loopback today, but Electron-shipped extensions could add one)
// do not drop idle streams.

import type { ServerResponse } from "node:http";

import {
  type EventLsn,
  type ThreadId,
  type ThreadStreamEvent,
  validateThreadStreamEvent,
} from "@wuphf/protocol";

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
   * Override the ready event id source. Thread event ids are committed LSNs.
   */
  readonly idForReady?: string;
  readonly onClose?: () => void;
}

export interface ThreadSseEmitArgs {
  readonly kind: "thread.created" | "thread.updated";
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
}

export interface SseHub {
  startSession(res: ServerResponse, opts?: SseSessionOptions): SseSession;
  emitThreadEvent(event: ThreadSseEmitArgs): void;
  closeAll(): void;
}

export function createSseHub(): SseHub {
  const sessions = new Set<ServerResponse>();
  return {
    startSession(res: ServerResponse, opts: SseSessionOptions = {}): SseSession {
      sessions.add(res);
      return startSseSession(res, {
        ...opts,
        onClose: () => {
          sessions.delete(res);
          opts.onClose?.();
        },
      });
    },
    emitThreadEvent(input: ThreadSseEmitArgs): void {
      const event: ThreadStreamEvent = {
        id: input.headLsn,
        kind: input.kind,
        emittedAt: new Date().toISOString(),
        payload: { threadId: input.threadId, headLsn: input.headLsn },
      };
      const validation = validateThreadStreamEvent(event);
      if (!validation.ok) {
        throw new Error(
          `invalid thread SSE event: ${validation.errors
            .map((error) => `${error.path}: ${error.message}`)
            .join("; ")}`,
        );
      }
      const frame = formatEvent({ id: event.id, event: event.kind, data: event });
      for (const session of sessions) {
        if (session.writableEnded) continue;
        try {
          session.write(frame);
        } catch {
          sessions.delete(session);
        }
      }
    },
    closeAll(): void {
      for (const session of sessions) {
        if (!session.writableEnded) {
          session.end();
        }
      }
      sessions.clear();
    },
  };
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
    opts.onClose?.();
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
  readonly data: unknown;
}

function formatEvent(event: SseEvent): string {
  return `id: ${event.id}\nevent: ${event.event}\ndata: ${JSON.stringify(event.data)}\n\n`;
}
