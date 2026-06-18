/**
 * wuphf-bridge — the ONLY way a WUPHF App reaches workspace data.
 *
 * The app runs inside a hardened sandbox (opaque origin, CSP connect-src 'none'),
 * so fetch/XHR/WebSocket are blocked. Data flows through window.parent via
 * postMessage to the WUPHF host, which services a small READ-ONLY allowlist of
 * broker GETs using the signed-in user's own session. Never call fetch() — it
 * will be blocked. Use callBroker() instead.
 *
 * Wire contract (must match web/src/components/apps/CustomAppFrame.tsx):
 *   app  -> host : { source: "wuphf-app",  type: "broker", id, method:"GET", path }
 *   app  -> host : { source: "wuphf-app",  type: "action", id, action:"create_task",
 *                    payload:{ title, details } }    // the one safe write
 *   host -> app  : { source: "wuphf-host", id, ok, data? , error? }
 *
 * Reads go through callBroker() (GET allowlist). The single write is
 * createTask(): the host shows the human a confirmation, then creates a normal
 * office task on their behalf — use it for "kick off a follow-up" buttons.
 *
 * Dev-only inspector messages (see ./wuphf-inspector.ts) ride the same channel
 * but are display-only — they NEVER reach this broker path or any network:
 *   host -> app  : { source: "wuphf-host", type: "wuphf-select-mode", enabled }
 *   app  -> host : { source: "wuphf-app",  type: "wuphf-select", file, line, col, tag, label }
 *   app  -> host : { source: "wuphf-app",  type: "wuphf-error", message, stack }
 */

interface PendingResolver {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
}

const pending = new Map<number, PendingResolver>();
let nextId = 1;
let listening = false;

function ensureListener(): void {
  if (listening) return;
  listening = true;
  window.addEventListener("message", (event: MessageEvent) => {
    const data = event.data as
      | { source?: string; id?: number; ok?: boolean; data?: unknown; error?: string }
      | null;
    if (!data || data.source !== "wuphf-host" || typeof data.id !== "number") {
      return;
    }
    const resolver = pending.get(data.id);
    if (!resolver) return;
    pending.delete(data.id);
    if (data.ok) {
      resolver.resolve(data.data);
    } else {
      resolver.reject(new Error(data.error ?? "broker request failed"));
    }
  });
}

/**
 * callBroker issues a read-only GET against the WUPHF broker through the host
 * bridge. `path` must be one of the allowlisted prefixes (see the host):
 *   /apps  /apps/gmail  /tasks  /office-members  /channels  /requests
 *   /wiki/list  /wiki/catalog  /wiki/read  /wiki/tree
 */
export function callBroker<T = unknown>(path: string): Promise<T> {
  ensureListener();
  const id = nextId++;
  return new Promise<T>((resolve, reject) => {
    pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
    window.parent.postMessage(
      { source: "wuphf-app", type: "broker", id, method: "GET", path },
      "*",
    );
    // Fail loudly rather than hang forever if the host never replies.
    window.setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        reject(new Error("broker request timed out"));
      }
    }, 15_000);
  });
}

// ── Typed convenience wrappers for the common reads ─────────────────────────

export interface OfficeMember {
  slug: string;
  name: string;
  role?: string;
}

export interface OfficeTask {
  id: string;
  title: string;
  owner?: string;
  status?: string;
  lifecycle_state?: string;
}

export function getOfficeMembers(): Promise<{ members: OfficeMember[] }> {
  return callBroker<{ members: OfficeMember[] }>("/office-members");
}

/**
 * All current office tasks across every channel (not just the default one).
 * `all_channels=true` is what the WUPHF task list itself uses; without it you
 * only get the "general" channel, which is usually empty.
 */
export function getTasks(): Promise<{ tasks: OfficeTask[] }> {
  return callBroker<{ tasks: OfficeTask[] }>(
    "/tasks?all_channels=true&viewer_slug=human",
  );
}

// ── Read-only Gmail (metadata + snippet only) ───────────────────────────────

/**
 * One Gmail message, SANITIZED by the host: metadata + a short snippet only.
 * There is no full body, no MIME headers, and no attachments — by design, this
 * is a read-only, minimal view of the operator's mail. Use it to build inbox
 * digests, "what needs a reply" lists, etc.
 */
export interface EmailItem {
  /** Gmail message id. */
  id: string;
  /** Gmail thread id (groups a conversation). */
  threadId: string;
  /** Sender email address, e.g. "noreply@getsentry.com". */
  from: string;
  /** Sender display name, e.g. "Sentry" (may be empty). */
  fromName: string;
  subject: string;
  /** Short preview snippet — NOT the full body. */
  snippet: string;
  /** ISO-8601 / RFC3339 timestamp, e.g. "2026-06-18T11:49:42Z". */
  date: string;
  /** True when the message still carries Gmail's UNREAD label. */
  unread: boolean;
  /** Gmail label names, e.g. ["UNREAD","IMPORTANT","INBOX"]. */
  labels: string[];
}

/**
 * getEmails reads the operator's most recent Gmail messages through the host
 * bridge. READ-ONLY: it can only ever read, never send or modify. Each message
 * is metadata + snippet only (no full body or attachments).
 *
 * `connected` is false when Gmail is not connected (or is temporarily
 * unavailable); render a connect-state in that case rather than an error. When
 * connected, `emails` holds up to `limit` recent messages (default 25, capped
 * at 50 by the host).
 */
export function getEmails(opts?: {
  limit?: number;
}): Promise<{ connected: boolean; emails: EmailItem[] }> {
  const limit = opts?.limit;
  const path =
    typeof limit === "number" && Number.isFinite(limit) && limit > 0
      ? `/apps/gmail/recent?limit=${Math.floor(limit)}`
      : "/apps/gmail/recent";
  return callBroker<{ connected: boolean; emails: EmailItem[] }>(path);
}

// ── The one safe write: create a follow-up task ─────────────────────────────

export interface CreateTaskInput {
  /** Short imperative title, e.g. "Follow up on the Acme renewal". Required. */
  title: string;
  /** Optional longer brief for the agents. */
  details?: string;
}

/**
 * createTask asks WUPHF to spin up a new office task. The host shows the human a
 * confirmation first (this is a state-changing action), then creates a normal
 * task on their behalf — the agents pick it up like any other. The app only
 * supplies a title + details; it can't set owner, type, or anything privileged.
 * Resolves with the new task id once the human confirms; rejects if they cancel
 * (or after a timeout). Wire it to a button — never fire it on load.
 */
export function createTask(
  input: CreateTaskInput,
): Promise<{ id: string; title: string }> {
  ensureListener();
  const id = nextId++;
  return new Promise<{ id: string; title: string }>((resolve, reject) => {
    pending.set(id, {
      resolve: resolve as (v: unknown) => void,
      reject,
    });
    window.parent.postMessage(
      {
        source: "wuphf-app",
        type: "action",
        id,
        action: "create_task",
        payload: { title: input.title, details: input.details ?? "" },
      },
      "*",
    );
    // Longer than a read: the human has to accept the confirmation dialog.
    window.setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        reject(new Error("create task cancelled or timed out"));
      }
    }, 60_000);
  });
}
