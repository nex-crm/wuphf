/**
 * Shared broker event stream.
 *
 * Every real-time feature (chat, wiki sections/writes, notebooks, entity
 * briefs, playbooks, PAM actions, broker presence) subscribes to the SAME
 * broker endpoint — `/events` — and filters by event type on the client.
 * Historically each feature opened its OWN `new EventSource(sseURL("/events"))`.
 * Each EventSource holds one HTTP/1.1 connection open for its whole lifetime,
 * and browsers cap concurrent connections per origin at ~6. A busy route (a
 * wiki article opens 8 subscribers) therefore saturates the pool with idle SSE
 * streams and STARVES every other request: the next `fetch` (e.g. the
 * create-page POST) queues behind the open streams and never sends, so the UI
 * hangs (the "Creating…" modal that never resolves).
 *
 * This module multiplexes all subscribers onto a SINGLE shared EventSource and
 * ref-counts them, so the whole app uses one connection for `/events` instead
 * of one-per-feature. The returned handle mimics the slice of the EventSource
 * API the callers use (`addEventListener`, `onerror`, `readyState`, the state
 * constants, and `close`); `close()` removes only that handle's listeners and
 * tears the shared connection down once the last subscriber leaves.
 */

import { sseURL } from "./client";

type AnyListener = (event: Event) => void;

export interface SharedEventStream {
  addEventListener(type: string, listener: AnyListener): void;
  removeEventListener(type: string, listener: AnyListener): void;
  onerror: ((event: Event) => void) | null;
  readonly readyState: number;
  readonly CONNECTING: number;
  readonly OPEN: number;
  readonly CLOSED: number;
  close(): void;
}

let shared: EventSource | null = null;
let refCount = 0;

function ensureSource(): EventSource | null {
  const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
  if (!ES) return null;
  if (!shared || shared.readyState === ES.CLOSED) {
    shared = new ES(sseURL("/events"));
  }
  return shared;
}

/**
 * Join the shared `/events` stream. Returns a per-subscriber handle, or null
 * when EventSource is unavailable (SSR / tests that stub SSE away) — callers
 * already guard for that. Always call `close()` on cleanup so the shared
 * connection is released when the last subscriber unmounts.
 */
export function openSharedEventStream(): SharedEventStream | null {
  const source = ensureSource();
  if (!source) return null;
  refCount += 1;

  // Track only this handle's listeners so close() detaches ours without
  // disturbing the other features sharing the connection.
  const own: Array<[string, EventListener]> = [];
  let errorListener: ((event: Event) => void) | null = null;
  let closed = false;

  return {
    addEventListener(type, listener) {
      source.addEventListener(type, listener as EventListener);
      own.push([type, listener as EventListener]);
    },
    removeEventListener(type, listener) {
      source.removeEventListener(type, listener as EventListener);
      const idx = own.findIndex(
        ([t, fn]) => t === type && fn === (listener as EventListener),
      );
      if (idx !== -1) own.splice(idx, 1);
    },
    get onerror() {
      return errorListener;
    },
    set onerror(fn) {
      if (errorListener) {
        source.removeEventListener("error", errorListener as EventListener);
        const idx = own.findIndex(
          ([t, f]) => t === "error" && f === (errorListener as EventListener),
        );
        if (idx !== -1) own.splice(idx, 1);
      }
      errorListener = fn;
      if (fn) {
        source.addEventListener("error", fn as EventListener);
        own.push(["error", fn as EventListener]);
      }
    },
    get readyState() {
      return source.readyState;
    },
    get CONNECTING() {
      return source.CONNECTING;
    },
    get OPEN() {
      return source.OPEN;
    },
    get CLOSED() {
      return source.CLOSED;
    },
    close() {
      if (closed) return;
      closed = true;
      for (const [type, fn] of own) source.removeEventListener(type, fn);
      own.length = 0;
      refCount = Math.max(0, refCount - 1);
      if (refCount === 0 && shared) {
        shared.close();
        shared = null;
      }
    },
  };
}
