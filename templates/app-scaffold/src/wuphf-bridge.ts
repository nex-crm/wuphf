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
 *   app  -> host : { source: "wuphf-app",  type: "integration", id, platform, action, params }
 *   app  -> host : { source: "wuphf-app",  type: "ai", id, prompt, input?, json? }
 *   host -> app  : { source: "wuphf-host", id, ok, data? , error? }
 *
 * Reads go through callBroker() (GET allowlist). The single write is
 * createTask(): the host shows the human a confirmation, then creates a normal
 * office task on their behalf — use it for "kick off a follow-up" buttons.
 *
 * Bridge v2 adds two GENERIC capabilities so an app can be integration-backed
 * and AI-powered without a bespoke broker endpoint per feature:
 *   - callIntegration(platform, action, params): run any CONNECTED integration's
 *     action. The broker classifies it: a READ executes and returns the result;
 *     a MUTATING action is never executed here — the broker raises a human
 *     approval card and returns { status:"needs_approval", request_id }. The app
 *     cannot smuggle a write; read-only is enforced server-side.
 *   - ai(prompt, input?, opts): a bounded one-shot LLM completion over data the
 *     app already fetched through this bridge. Read-only reasoning, not network.
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
 *   /apps  /tasks  /office-members  /channels  /requests
 *   /wiki/list  /wiki/catalog  /wiki/read  /wiki/tree
 * For integration data (Gmail, Slack, …) use callIntegration(), not callBroker.
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

/**
 * postToHost is the shared request/reply primitive for the non-GET bridge
 * message types (action / integration / ai). It mints a request id, posts the
 * message, and resolves when the host replies with a matching id. `fields`
 * carries the type-specific payload (everything except source/id).
 */
function postToHost<T = unknown>(
  fields: Record<string, unknown>,
  timeoutMs: number,
): Promise<T> {
  ensureListener();
  const id = nextId++;
  return new Promise<T>((resolve, reject) => {
    pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
    window.parent.postMessage(
      { source: "wuphf-app", id, ...fields },
      "*",
    );
    window.setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        reject(new Error("host request timed out"));
      }
    }, timeoutMs);
  });
}

// ── Generic integration access (Bridge v2) ──────────────────────────────────

/**
 * The business outcome of a callIntegration() call. The broker classifies the
 * action server-side:
 *   - read, connected   -> { connected:true, status:"ok", result }
 *   - mutating          -> { connected:true, status:"needs_approval", request_id }
 *                          (NOT executed; a human must approve the raised card)
 *   - not connected     -> { connected:false }
 */
export interface IntegrationCallResult {
  connected: boolean;
  status?: "ok" | "needs_approval";
  request_id?: string;
  read_only: boolean;
  /** Present on a successful READ. The raw upstream action response. */
  result?: unknown;
  error?: string;
}

/**
 * callIntegration runs a CONNECTED integration's action through the host
 * bridge. The broker — never this app — decides read-vs-mutate. A read returns
 * the result; a mutating action is NOT executed and instead raises a human
 * approval card (status:"needs_approval"). Use read actions (GET/LIST/SEARCH/
 * FETCH) freely; expect a mutating call to come back needing approval.
 */
export function callIntegration<T = unknown>(
  platform: string,
  action: string,
  params?: Record<string, unknown>,
): Promise<IntegrationCallResult & { result?: T }> {
  return postToHost<IntegrationCallResult & { result?: T }>(
    { type: "integration", platform, action, params: params ?? {} },
    20_000,
  );
}

/** One connected integration plus its available READ action ids. */
export interface ConnectedIntegration {
  platform: string;
  name: string;
  logo_url?: string;
  read_actions: string[];
}

/**
 * listIntegrations returns the connected platforms and their available READ
 * actions, so the app knows what it can call without execute-time guesswork.
 */
export function listIntegrations(): Promise<{
  connected: ConnectedIntegration[];
}> {
  return callBroker<{ connected: ConnectedIntegration[] }>(
    "/apps/integrations/catalog",
  );
}

// ── ai(): one-shot LLM reasoning over data the app already holds ─────────────

/**
 * ai runs a BOUNDED one-shot completion using the workspace's own configured
 * LLM provider, over the prompt plus optional `input` data the app already
 * fetched through this bridge. It is read-only reasoning — never a tool loop,
 * never a network call (the sandbox blocks network). Bound your input: the
 * broker caps prompt + input size.
 *
 * With opts.json the model is asked for a single JSON value and the parsed
 * object is returned; otherwise plain text is returned. If the provider is not
 * available the result is `{ error: "ai_unavailable" }` — render a fallback.
 */
export interface AIResult<T = unknown> {
  /** Plain-text answer (or, under json:true, the prose if it wasn't valid JSON). */
  text?: string;
  /** Parsed JSON value when opts.json was set and the model returned JSON. */
  object?: T;
  /** "ai_unavailable" (no provider) or "not_json" (json asked, prose returned). */
  error?: string;
}

export function ai<T = unknown>(
  prompt: string,
  input?: unknown,
  opts?: { json?: boolean },
): Promise<AIResult<T>> {
  return postToHost<AIResult<T>>(
    { type: "ai", prompt, input, json: opts?.json === true },
    65_000,
  );
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
 * getEmails reads the operator's most recent Gmail messages. It is a THIN
 * WRAPPER over callIntegration('gmail', 'GMAIL_FETCH_EMAILS', …) — kept as a
 * worked example of the generic Bridge v2 pattern, so nothing hard-depends on a
 * bespoke endpoint. READ-ONLY: GMAIL_FETCH_EMAILS is a read, so the broker
 * executes it and returns the result (no approval card).
 *
 * `connected` is false when Gmail is not connected; render a connect-state in
 * that case rather than an error. When connected, `emails` holds up to `limit`
 * recent messages (default 25). Build your own reads the same way: pick a READ
 * action id and call callIntegration directly.
 */
export function getEmails(opts?: {
  limit?: number;
}): Promise<{ connected: boolean; emails: EmailItem[] }> {
  const limit =
    typeof opts?.limit === "number" &&
    Number.isFinite(opts.limit) &&
    opts.limit > 0
      ? Math.floor(opts.limit)
      : 25;
  return callIntegration("gmail", "GMAIL_FETCH_EMAILS", {
    max_results: limit,
  }).then((res) => ({
    connected: res.connected,
    emails: mapGmailMessages(res.result),
  }));
}

/**
 * mapGmailMessages adapts the raw GMAIL_FETCH_EMAILS response into EmailItem[].
 * The generic bridge returns the upstream action result verbatim, so the app
 * shapes it. Defensive: tolerates a missing/oddly-shaped envelope.
 */
function mapGmailMessages(result: unknown): EmailItem[] {
  const envelope = result as
    | { data?: { messages?: unknown[] } }
    | null
    | undefined;
  const messages = envelope?.data?.messages;
  if (!Array.isArray(messages)) return [];
  return messages.map((raw) => {
    const m = (raw ?? {}) as Record<string, unknown>;
    const labels = Array.isArray(m.labelIds)
      ? (m.labelIds as unknown[]).map(String)
      : [];
    const preview = (m.preview ?? {}) as Record<string, unknown>;
    return {
      id: String(m.messageId ?? ""),
      threadId: String(m.threadId ?? ""),
      from: String(m.sender ?? ""),
      fromName: "",
      subject: String(m.subject ?? ""),
      snippet: String(preview.body ?? ""),
      date: String(m.messageTimestamp ?? ""),
      unread: labels.includes("UNREAD"),
      labels,
    };
  });
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
