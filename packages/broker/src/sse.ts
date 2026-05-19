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
export const DEFAULT_MAX_SSE_SESSIONS = 64;
export const DEFAULT_MAX_SSE_WRITABLE_BYTES = 1024 * 1024;

export interface SseSession {
  close(): void;
}

export interface SseEvent {
  readonly id: string;
  readonly event: string;
  readonly data: unknown;
}

export interface ThreadSseEmitArgs {
  readonly kind: "thread.created" | "thread.updated" | "thread.pinned_approvals.changed";
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
}

export interface SseHub {
  startSession(res: ServerResponse, opts?: SseSessionOptions): SseSession;
  emit(event: SseEvent): void;
  emitThreadEvent(event: ThreadSseEmitArgs): void;
  closeAll(): void;
}

export interface SseHubOptions {
  readonly maxSessions?: number;
  readonly maxWritableLength?: number;
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
  readonly maxWritableLength?: number;
}

interface ManagedSseSession extends SseSession {
  readonly active: boolean;
  write(frame: string): boolean;
  drop(): void;
}

export function createSseHub(opts: SseHubOptions = {}): SseHub {
  const maxSessions = opts.maxSessions ?? DEFAULT_MAX_SSE_SESSIONS;
  const maxWritableLength = opts.maxWritableLength ?? DEFAULT_MAX_SSE_WRITABLE_BYTES;
  const clients = new Map<ServerResponse, ManagedSseSession>();
  const emit = (event: SseEvent): void => {
    const frame = formatEvent(event);
    for (const [res, session] of clients) {
      if (!session.active) {
        clients.delete(res);
        continue;
      }
      if (!session.write(frame)) {
        clients.delete(res);
      }
    }
  };

  return {
    startSession(res: ServerResponse, opts: SseSessionOptions = {}): SseSession {
      if (clients.size >= maxSessions) {
        rejectSseSession(res);
        return { close: () => undefined };
      }
      const session = startManagedSseSession(res, {
        ...opts,
        maxWritableLength: opts.maxWritableLength ?? maxWritableLength,
      });
      if (!session.active) return { close: () => undefined };
      clients.set(res, session);
      const remove = (): void => {
        clients.delete(res);
      };
      res.on("close", remove);
      res.on("error", remove);
      return {
        close(): void {
          remove();
          res.off("close", remove);
          res.off("error", remove);
          session.close();
        },
      };
    },
    emit,
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
      emit({ id: String(event.id), event: event.kind, data: event });
    },
    closeAll(): void {
      for (const session of clients.values()) {
        session.close();
      }
      clients.clear();
    },
  };
}

export function startSseSession(res: ServerResponse, opts: SseSessionOptions = {}): SseSession {
  return startManagedSseSession(res, opts);
}

function startManagedSseSession(
  res: ServerResponse,
  opts: SseSessionOptions = {},
): ManagedSseSession {
  const maxWritableLength = opts.maxWritableLength ?? DEFAULT_MAX_SSE_WRITABLE_BYTES;
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

  let closed = false;
  let keepalive: ReturnType<typeof setInterval> | null = null;
  const detach = (): void => {
    res.off("close", peerClosed);
    res.off("error", peerClosed);
  };
  const finish = (mode: "graceful" | "drop" | "peer"): void => {
    if (closed) return;
    closed = true;
    if (keepalive !== null) clearInterval(keepalive);
    detach();
    if (mode === "drop") {
      if (!res.destroyed) res.destroy();
      return;
    }
    if (mode === "graceful" && !res.writableEnded && !res.destroyed) {
      res.end();
    }
  };
  const close = (): void => {
    finish("graceful");
  };
  const drop = (): void => {
    finish("drop");
  };
  const peerClosed = (): void => {
    finish("peer");
  };
  const write = (frame: string): boolean => {
    if (closed || shouldDropForBackpressure(res, maxWritableLength)) {
      drop();
      return false;
    }
    try {
      if (!res.write(frame)) {
        drop();
        return false;
      }
    } catch {
      drop();
      return false;
    }
    if (shouldDropForBackpressure(res, maxWritableLength)) {
      drop();
      return false;
    }
    return true;
  };

  res.on("close", peerClosed);
  res.on("error", peerClosed);
  write(formatEvent({ id, event: "ready", data: { emittedAt } }));
  if (!closed) {
    keepalive = setInterval(() => {
      write(": keepalive\n\n");
    }, opts.keepaliveMs ?? KEEPALIVE_MS);
    // `unref` so a hanging keepalive does not keep the broker process alive
    // after `BrokerHandle.stop` closes the listener.
    keepalive.unref();
  }

  return {
    get active(): boolean {
      return !closed;
    },
    write,
    close,
    drop,
  };
}

function formatEvent(event: SseEvent): string {
  return `id: ${event.id}\nevent: ${event.event}\ndata: ${JSON.stringify(event.data)}\n\n`;
}

function shouldDropForBackpressure(res: ServerResponse, maxWritableLength: number): boolean {
  return res.writableEnded || res.destroyed || res.writableLength > maxWritableLength;
}

function rejectSseSession(res: ServerResponse): void {
  const body = "sse_session_limit\n";
  res.writeHead(503, {
    "Content-Type": "text/plain; charset=utf-8",
    "Cache-Control": "no-store",
    "Retry-After": "1",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
  });
  res.end(body);
}
