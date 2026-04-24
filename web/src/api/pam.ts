/**
 * Pam the Archivist — client API.
 *
 * Backend lives in internal/team/pam.go + internal/team/broker_pam.go. Pam is
 * spawned in her own sub-process per action; this module just triggers jobs
 * and subscribes to her progress.
 */

import { get, post, sseURL } from "./client";

// Open string union on PamActionId is intentional: the canonical id today is
// `enrich_article`, but new actions added in pam_actions.go should flow
// through the UI without a web-side type patch. `(string & {})` keeps
// literal-autocomplete for the known id while allowing any string at runtime.
export type PamActionId = "enrich_article" | (string & {});

export interface PamActionDescriptor {
  id: PamActionId;
  label: string;
}

export interface PamActionStartedEvent {
  job_id: number;
  action: PamActionId;
  article_path: string;
  request_by: string;
  started_at: string;
}

export interface PamActionDoneEvent {
  job_id: number;
  action: PamActionId;
  article_path: string;
  commit_sha: string;
  finished_at: string;
}

export interface PamActionFailedEvent {
  job_id: number;
  action: PamActionId;
  article_path: string;
  error: string;
  failed_at: string;
}

export type PamActionEvent =
  | ({ kind: "started" } & PamActionStartedEvent)
  | ({ kind: "done" } & PamActionDoneEvent)
  | ({ kind: "failed" } & PamActionFailedEvent);

/**
 * Fetches Pam's action registry so the UI renders the desk menu in the same
 * order and with the same labels as the server defines. Keeps the menu
 * extensible — adding a new entry in pam_actions.go surfaces it in the UI
 * automatically.
 */
export function listPamActions() {
  return get<{ actions: PamActionDescriptor[] }>("/pam/actions");
}

/**
 * Triggers a Pam action on an article. Returns the job id so callers can
 * correlate subsequent SSE events from subscribePamEvents.
 */
export function triggerPamAction(action: PamActionId, articlePath: string) {
  return post<{ job_id: number; queued_at: string }>("/pam/action", {
    action,
    path: articlePath,
  });
}

type EventKind = "started" | "done" | "failed";

// parsePamEvent validates the shape of an SSE payload before the component
// trusts it. The backend is the source of truth for the wire format, but a
// broken intermediate (proxy, misrouted event, etc.) should never crash the
// UI — we warn and drop the event.
function parsePamEvent(kind: EventKind, raw: string): PamActionEvent | null {
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    console.warn("pam: malformed event (JSON)", raw);
    return null;
  }
  if (!data || typeof data !== "object") {
    console.warn("pam: malformed event (not object)", raw);
    return null;
  }
  const obj = data as Record<string, unknown>;
  if (typeof obj.job_id !== "number" || !Number.isFinite(obj.job_id)) {
    console.warn("pam: malformed event (job_id)", raw);
    return null;
  }
  if (
    (kind === "started" || kind === "done") &&
    typeof obj.action !== "string"
  ) {
    console.warn("pam: malformed event (action)", raw);
    return null;
  }
  if (kind === "failed" && typeof obj.error !== "string") {
    console.warn("pam: malformed event (error)", raw);
    return null;
  }
  if (kind === "started") {
    return { kind: "started", ...(data as PamActionStartedEvent) };
  }
  if (kind === "done") {
    return { kind: "done", ...(data as PamActionDoneEvent) };
  }
  return { kind: "failed", ...(data as PamActionFailedEvent) };
}

/**
 * Subscribes to Pam's progress events on /events. Mirrors subscribeEditLog
 * in api/wiki.ts. Returns an unsubscribe function.
 *
 * Connection-loss handling: when the underlying EventSource terminally
 * closes we synthesise a `failed` event so the UI can surface the outage
 * without needing a separate callback. job_id is NaN because the id of the
 * in-flight action is owned by the caller (Pam.tsx) — the component filters
 * by its own activeJobId, and treats a NaN id as "connection lost" if it
 * matters in future callers. We chose this over a separate onConnectionLost
 * callback to keep the subscribe signature simple and match subscribeEditLog.
 */
export function subscribePamEvents(
  handler: (evt: PamActionEvent) => void,
): () => void {
  let closed = false;
  let source: EventSource | null = null;
  let onStarted: ((ev: MessageEvent) => void) | null = null;
  let onDone: ((ev: MessageEvent) => void) | null = null;
  let onFailed: ((ev: MessageEvent) => void) | null = null;
  let onError: ((ev: Event) => void) | null = null;

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource;
    if (!ES)
      return () => {
        closed = true;
      };
    source = new ES(sseURL("/events"));

    const dispatch = (kind: EventKind, ev: MessageEvent) => {
      if (closed) return;
      const parsed = parsePamEvent(kind, ev.data);
      if (parsed) handler(parsed);
    };
    onStarted = (ev: MessageEvent) => dispatch("started", ev);
    onDone = (ev: MessageEvent) => dispatch("done", ev);
    onFailed = (ev: MessageEvent) => dispatch("failed", ev);
    onError = () => {
      if (closed) return;
      // EventSource auto-reconnects on transient errors. Only surface a
      // failure to the caller when the connection is terminally closed.
      if (source && source.readyState === ES.CLOSED) {
        closed = true;
        console.warn("pam: SSE connection lost");
        handler({
          kind: "failed",
          job_id: Number.NaN,
          action: "",
          article_path: "",
          error: "connection lost",
          failed_at: new Date().toISOString(),
        });
      }
    };

    source.addEventListener("pam:action_started", onStarted as EventListener);
    source.addEventListener("pam:action_done", onDone as EventListener);
    source.addEventListener("pam:action_failed", onFailed as EventListener);
    source.addEventListener("error", onError as EventListener);
  } catch {
    source = null;
  }

  return () => {
    closed = true;
    if (source) {
      if (onStarted)
        source.removeEventListener(
          "pam:action_started",
          onStarted as EventListener,
        );
      if (onDone)
        source.removeEventListener("pam:action_done", onDone as EventListener);
      if (onFailed)
        source.removeEventListener(
          "pam:action_failed",
          onFailed as EventListener,
        );
      if (onError)
        source.removeEventListener("error", onError as EventListener);
      source.close();
    }
  };
}
