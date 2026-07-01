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
 *   app  -> host : { source: "wuphf-app",  type: "db", id, op, table?, columns?, rows?, key? }
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
    const data = event.data as {
      source?: string;
      id?: number;
      ok?: boolean;
      data?: unknown;
      error?: string;
    } | null;
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
    window.parent.postMessage({ source: "wuphf-app", id, ...fields }, "*");
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
 *   - read failed        -> { connected:true, error }   (e.g. upstream error)
 *   - mutating          -> { connected:true, status:"needs_approval", request_id }
 *                          (NOT executed; a human must approve the raised card)
 *   - not connected     -> { connected:false, error }
 *
 * Always check `error` and `connected` before using `result`; `result` is only
 * present on a successful read.
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
 *
 * REQUEST ONLY WHAT YOU NEED. Pass params that bound the result (a limit, a
 * lean/metadata flag, a query). The broker errors on an oversized upstream read
 * rather than truncating it — it does not trim your payload for you, so an
 * over-fetch comes back as an error, not silently-shrunk data.
 *
 * FAILURE HANDLING. Business outcomes resolve (not-connected, needs-approval,
 * and even a read that failed upstream — the latter as `{ error }`). A transport
 * / host failure REJECTS the promise. ALWAYS await this in a try/catch (or
 * `.catch`) AND inspect `result.error` / `result.connected`; never assume
 * `result.result` is present. The host reply is untrusted — do not run an
 * unguarded `JSON.parse` on it.
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
 * object is returned; otherwise plain text is returned.
 *
 * FAILURE HANDLING. Expected product states resolve as `{ error }`:
 * `"ai_unavailable"` (no provider configured) or `"not_json"` (json:true but the
 * model returned prose — `text` carries it). A transport / host failure REJECTS
 * the promise. ALWAYS await this in a try/catch (or `.catch`) AND check
 * `result.error` before using `result.object` / `result.text`; render a fallback
 * rather than crashing. Never run an unguarded `JSON.parse` on the reply.
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

// ── db: the app's OWN backing database (persisted, typed data model) ─────────

/**
 * A typed column in an app table. `type` is a hint the Data tab renders; values
 * are stored as-is. Use "string" | "number" | "boolean" | "date" | "string[]".
 */
export interface DBColumn {
  name: string;
  type: "string" | "number" | "boolean" | "date" | "string[]";
}

/** One table in the app's database: its name, typed columns, and rows. */
export interface DBTable {
  name: string;
  columns: DBColumn[];
  rows: Record<string, unknown>[];
}

/**
 * db is the app's REAL backing store — persisted server-side, per app. This is
 * how an app OWNS its data model instead of recomputing it on every mount:
 *
 *   1. On first load, derive your model ONCE from the source reads (getEmails,
 *      getTasks, callIntegration, …) and persist it: defineTable() then upsert().
 *   2. Render from db.query()/db.all() — the Data tab reads the very same tables,
 *      so what the app shows and what the Data tab shows are one source of truth.
 *   3. On later mounts, read the DB first; only re-derive when it is empty or you
 *      explicitly refresh. Do NOT recompute from the integration on every mount.
 *
 * Writes are gated + bounded server-side (table/column/row limits); every call
 * REJECTS on a transport/host failure, so await in try/catch. `upsert` with a
 * `key` column replaces the row whose key matches (primary-key dedup); without a
 * key it appends. A table must be defined (defineTable) before you upsert to it.
 */
export const db = {
  /** Create a table or replace its columns (existing rows are preserved). */
  defineTable(name: string, columns: DBColumn[]): Promise<{ table: DBTable }> {
    return postToHost<{ table: DBTable }>(
      { type: "db", op: "define", table: name, columns },
      15_000,
    );
  },
  /** Append rows, or replace by `key` (primary-key dedup) when key is given. */
  upsert(
    name: string,
    rows: Record<string, unknown>[],
    key?: string,
  ): Promise<{ table: DBTable }> {
    return postToHost<{ table: DBTable }>(
      { type: "db", op: "upsert", table: name, rows, key },
      15_000,
    );
  },
  /** Read one table by name. */
  query(name: string): Promise<{ table: DBTable }> {
    return postToHost<{ table: DBTable }>(
      { type: "db", op: "query", table: name },
      15_000,
    );
  },
  /** Read every table in the app's database. */
  all(): Promise<{ tables: DBTable[] }> {
    return postToHost<{ tables: DBTable[] }>({ type: "db", op: "all" }, 15_000);
  },
  /** Empty a table's rows, keeping its column definition. */
  clear(name: string): Promise<{ table: DBTable }> {
    return postToHost<{ table: DBTable }>(
      { type: "db", op: "clear", table: name },
      15_000,
    );
  },
};

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

/** The outcome of getEmails(): a state the app renders, never a thrown error. */
export interface GetEmailsResult {
  /** False when Gmail is not connected — render a connect-state, not an error. */
  connected: boolean;
  /** Up to `limit` recent messages (metadata + snippet only). */
  emails: EmailItem[];
  /**
   * Set when the read failed (bridge error, or the broker returned an error
   * state). Render this as an error state; `emails` is empty. `connected` may
   * still be true (the integration is connected but the read failed).
   */
  error?: string;
}

/**
 * getEmails reads the operator's most recent Gmail messages. It is a THIN
 * WRAPPER over callIntegration('gmail', 'GMAIL_FETCH_EMAILS', …) — kept as a
 * worked example of the generic Bridge v2 pattern, so nothing hard-depends on a
 * bespoke endpoint. READ-ONLY: GMAIL_FETCH_EMAILS is a read, so the broker
 * executes it and returns the result (no approval card).
 *
 * REQUESTS A LEAN PAYLOAD. It passes `verbose:false` (drop full message bodies)
 * and `include_payload:false` (drop raw MIME headers) so the response is
 * kilobytes, not megabytes — at limit 25 the body shrinks ~25x (≈430 KB → ≈18
 * KB) while every EmailItem field (subject, from, snippet, date, labels, unread)
 * stays intact. The app uses only the snippet, never the full body, so fetching
 * full bodies would just be discarded bandwidth. Don't re-fetch full bodies.
 *
 * NEVER THROWS on a normal error. `connected` is false when Gmail is not
 * connected (render a connect-state). `error` is set when the read itself failed
 * (render an error state); in both cases `emails` is empty. Build your own reads
 * the same way: pick a READ action id, bound its params, and handle failures.
 */
export function getEmails(opts?: { limit?: number }): Promise<GetEmailsResult> {
  const limit =
    typeof opts?.limit === "number" &&
    Number.isFinite(opts.limit) &&
    opts.limit > 0
      ? Math.floor(opts.limit)
      : 25;
  return callIntegration("gmail", "GMAIL_FETCH_EMAILS", {
    max_results: limit,
    // Lean payload: metadata + snippet only. See the doc comment above.
    verbose: false,
    include_payload: false,
  })
    .then((res) => {
      if (res.error) {
        return { connected: res.connected, emails: [], error: res.error };
      }
      return { connected: res.connected, emails: mapGmailMessages(res.result) };
    })
    .catch((err: unknown) => ({
      connected: false,
      emails: [],
      error: err instanceof Error ? err.message : "Could not read Gmail.",
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

// ── File download: save app-generated data to disk ──────────────────────────

export interface DownloadInput {
  /**
   * File name, e.g. "leads.csv". The host takes only the basename and strips
   * path separators + control / illegal characters, so keep it simple.
   */
  filename: string;
  /**
   * The file contents. UTF-8 text by default (CSV, JSON, Markdown, …). For
   * BINARY data, base64-encode it and pass encoding:"base64". The host caps the
   * total size.
   */
  content: string;
  /** MIME type hint, e.g. "text/csv". Defaults to "application/octet-stream". */
  mime?: string;
  /** "utf-8" (default) or "base64" for binary content. */
  encoding?: "utf-8" | "base64";
}

/**
 * download saves app-generated data to the user's disk. A raw `<a download>` or
 * programmatic anchor click does NOT work here — the app runs in an opaque-origin
 * sandbox, so the browser ignores the download and the click becomes a blocked
 * navigation. Instead the app hands the bytes to the host, which performs the
 * actual download from its own trusted context. The app never reaches the
 * filesystem or the network directly; this keeps the sandbox fully intact.
 *
 * Wire it to a button (an "Export CSV" click), never fire it on load. Resolves
 * once the host has started the download; rejects if the payload is invalid
 * (empty/oversized), the rate limit is hit, or after a timeout.
 *
 * Example — export rows to CSV:
 *   const csv = "name,score\n" + rows.map(r => `${r.name},${r.score}`).join("\n");
 *   await download({ filename: "leads.csv", content: csv, mime: "text/csv" });
 */
export function download(input: DownloadInput): Promise<void> {
  return postToHost<void>(
    {
      type: "download",
      filename: input.filename,
      content: input.content,
      mime: input.mime,
      encoding: input.encoding === "base64" ? "base64" : "utf-8",
    },
    20_000,
  );
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
