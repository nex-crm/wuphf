import { type RefObject, useEffect, useMemo, useRef } from "react";

import { get, post } from "../../api/client";
import { confirm } from "../ui/ConfirmDialog";
import { showNotice } from "../ui/Toast";

/**
 * CustomAppFrame renders an agent-generated internal tool inside a hardened
 * sandbox and brokers its data access.
 *
 * Security model (the iframe is the real boundary, not the write-time HTML
 * validator):
 *   - sandbox="allow-scripts" ONLY — no allow-same-origin, so the app runs at an
 *     opaque origin with no access to cookies, localStorage, or the parent DOM;
 *     no allow-forms / allow-popups / allow-top-navigation.
 *   - An injected CSP with connect-src 'none' blocks ALL network from inside the
 *     frame (fetch/XHR/WebSocket), so a generated app cannot exfiltrate data,
 *     even if its own bundle tried to. default-src 'none' + inline script/style
 *     only — everything must be self-contained.
 *   - The ONLY way out is window.parent.postMessage to this component, which
 *     services a small READ-ONLY allowlist of broker GETs using the signed-in
 *     user's own session (the app never holds a token), PLUS a single narrow
 *     write — `create_task` — that the host fully parameterizes and gates behind
 *     a human confirmation. Arbitrary writes (any other path/method) are rejected.
 *
 * ─────────────────────────── BRIDGE v2: WIDENED SURFACE ─────────────────────
 * SECURITY — needs a security-reviewer sign-off before shipping broadly.
 * Two new sanctioned shapes widen what an app can reach. Both are HOST-VALIDATED
 * here (the app is hostile by assumption) and re-validated server-side:
 *
 *   - "integration" → POST /apps/integrations/call {platform, action, params}.
 *     The HOST forwards; the BROKER decides read-vs-mutate via the same
 *     deterministic verb table the agent gate uses. A read returns the user's
 *     own data into their own sandboxed app (ok). A MUTATING action is NEVER
 *     executed by this path — the broker raises the human ExternalActionApproval
 *     card and returns {status:"needs_approval", request_id}. The app cannot
 *     smuggle a write: read-only classification is enforced server-side, not
 *     trusted from the app, and only a human click can execute a mutation.
 *
 *   - "ai" → POST /apps/ai {prompt, input?, json?}. A bounded one-shot LLM
 *     completion over data the app ALREADY fetched through this bridge. It is
 *     not a network escape hatch (the frame still has connect-src 'none'); the
 *     broker bounds prompt + input size so it cannot become an exfil/cost
 *     channel. Read-only reasoning, never a tool loop.
 *
 * Neither new shape touches the broker GET allowlist or the create_task write —
 * they are distinct POST endpoints with their own server-side gates.
 */

const APP_CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline'",
  "style-src 'unsafe-inline'",
  "img-src data: blob:",
  "font-src data:",
  "media-src data: blob:",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "frame-src 'none'",
].join("; ");

// Read-only broker paths an app may request through the bridge. Prefix match on
// the path (query string ignored). Deliberately small: live office data an
// internal tool would display. Mutations are NOT exposed in this version.
const ALLOWED_GET_PREFIXES: readonly string[] = [
  // Bridge v2: the connected-integrations catalog (listIntegrations). NOTE the
  // bare "/apps" prefix is deliberately NOT here — it would let an app read any
  // other app's full HTML source via GET /apps/<id> (security review L2). Apps
  // reach their own data through the dedicated bridge endpoints, not /apps/<id>.
  "/apps/integrations/catalog",
  "/tasks",
  "/office-members",
  "/channels",
  "/requests",
  "/wiki/list",
  "/wiki/catalog",
  "/wiki/read",
  "/wiki/tree",
];

const HOST_SOURCE = "wuphf-host";
const APP_SOURCE = "wuphf-app";

// Host-side caps on app-supplied display strings. The app caps at its source
// too, but the host is the trust boundary: re-cap before anything is stored in
// host React state or shown to the operator.
const SELECT_LABEL_MAX = 120;
const SELECT_FILE_MAX = 256;
const SELECT_TAG_MAX = 32;
const APP_ERROR_MAX = 600;
// Caps on the one safe write an app may request (create_task). The app supplies
// only a title + details; the host parameterizes every other field.
const ACTION_TITLE_MAX = 200;
const ACTION_DETAILS_MAX = 4000;

// Bridge v2 host-side caps. The broker re-enforces the real bounds; these are a
// first, cheap line so an obviously-abusive message never leaves the host.
const INTEGRATION_PLATFORM_MAX = 64;
const INTEGRATION_ACTION_MAX = 128;
// Serialized params / ai payload caps. Mirror the broker's bounds loosely; the
// broker is the authority. JSON.stringify length is a byte-ish proxy.
const INTEGRATION_PARAMS_MAX = 16 * 1024;
const AI_PROMPT_MAX = 8 * 1024;
const AI_INPUT_MAX = 200 * 1024;

interface BrokerBridgeMessage {
  source: typeof APP_SOURCE;
  type: "broker";
  id: string | number;
  method?: string;
  path?: string;
}

interface AppActionMessage {
  source: typeof APP_SOURCE;
  type: "action";
  id: string | number;
  action?: string;
  payload?: unknown;
}

/** Bridge v2: an app's generic integration call. */
interface AppIntegrationMessage {
  source: typeof APP_SOURCE;
  type: "integration";
  id: string | number;
  platform?: unknown;
  action?: unknown;
  params?: unknown;
}

/** Bridge v2: an app's one-shot ai() reasoning call. */
interface AppAIMessage {
  source: typeof APP_SOURCE;
  type: "ai";
  id: string | number;
  prompt?: unknown;
  input?: unknown;
  json?: unknown;
}

/** Validated, host-trusted shape of an integration call. */
export interface IntegrationCallArgs {
  platform: string;
  action: string;
  params?: Record<string, unknown>;
}

/** Validated, host-trusted shape of an ai() call. */
export interface AICallArgs {
  prompt: string;
  input?: unknown;
  json: boolean;
}

/**
 * Display-only payload from a dev-preview "select to edit" click. Carries the
 * clicked element's source location + a short label so the host can prefill the
 * App Builder edit dialog. It NEVER triggers a broker call or network request.
 */
export interface AppSelectPayload {
  file: string;
  line: number;
  col: number;
  tag: string;
  label: string;
}

/** Display-only runtime error surfaced from inside the app (dev preview). */
export interface AppErrorPayload {
  message: string;
  stack: string;
}

function capString(value: unknown, max: number): string {
  const s = typeof value === "string" ? value : "";
  return s.length > max ? s.slice(0, max) : s;
}

function capInt(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? Math.floor(value)
    : 0;
}

/**
 * Validate + cap an inbound "wuphf-select" payload into the host's trusted
 * shape. Pure so it can be unit-tested without a real iframe. Returns null when
 * the message isn't a usable selection (no file).
 */
export function parseSelectPayload(data: unknown): AppSelectPayload | null {
  if (!data || typeof data !== "object") return null;
  const d = data as Record<string, unknown>;
  const file = capString(d.file, SELECT_FILE_MAX);
  if (!file) return null;
  return {
    file,
    line: capInt(d.line),
    col: capInt(d.col),
    tag: capString(d.tag, SELECT_TAG_MAX),
    label: capString(d.label, SELECT_LABEL_MAX),
  };
}

/** Validate + cap an inbound "wuphf-error" payload. Pure for unit tests. */
export function parseErrorPayload(data: unknown): AppErrorPayload {
  if (!data || typeof data !== "object") {
    return { message: "", stack: "" };
  }
  const d = data as Record<string, unknown>;
  return {
    message: capString(d.message, APP_ERROR_MAX),
    stack: capString(d.stack, APP_ERROR_MAX),
  };
}

/**
 * Validate + normalize an inbound "integration" message into the host's
 * trusted IntegrationCallArgs. Pure so it can be unit-tested without a frame.
 * Returns null when the message is unusable (missing platform/action, params
 * not a plain object, or over the size cap). The HOST is the trust boundary:
 * the broker still re-validates and re-classifies read-vs-mutate.
 */
export function parseIntegrationArgs(
  data: unknown,
): IntegrationCallArgs | null {
  if (!data || typeof data !== "object") return null;
  const d = data as Record<string, unknown>;
  const platform =
    typeof d.platform === "string"
      ? d.platform.trim().slice(0, INTEGRATION_PLATFORM_MAX)
      : "";
  const action =
    typeof d.action === "string"
      ? d.action.trim().slice(0, INTEGRATION_ACTION_MAX)
      : "";
  if (!(platform && action)) return null;
  let params: Record<string, unknown> | undefined;
  if (d.params !== undefined && d.params !== null) {
    // Must be a plain object (not an array, not a primitive) — the broker
    // expects a params map.
    if (
      typeof d.params !== "object" ||
      Array.isArray(d.params) ||
      JSON.stringify(d.params).length > INTEGRATION_PARAMS_MAX
    ) {
      return null;
    }
    params = d.params as Record<string, unknown>;
  }
  return params ? { platform, action, params } : { platform, action };
}

/**
 * Validate + normalize an inbound "ai" message into AICallArgs. Pure for unit
 * tests. Returns null when prompt is missing/empty or prompt/input exceeds the
 * size cap. The broker re-enforces the real bounds and timeout.
 */
export function parseAIArgs(data: unknown): AICallArgs | null {
  if (!data || typeof data !== "object") return null;
  const d = data as Record<string, unknown>;
  const prompt = typeof d.prompt === "string" ? d.prompt.trim() : "";
  if (!prompt || prompt.length > AI_PROMPT_MAX) return null;
  if (d.input !== undefined && d.input !== null) {
    let serialized: string;
    try {
      serialized = JSON.stringify(d.input);
    } catch {
      return null;
    }
    if (serialized.length > AI_INPUT_MAX) return null;
  }
  const json = d.json === true;
  return d.input === undefined || d.input === null
    ? { prompt, json }
    : { prompt, input: d.input, json };
}

export function isAllowedGetPath(path: string): boolean {
  if (!path.startsWith("/")) return false;
  const [raw] = path.split("?");
  // Normalize away ./ and ../ segments BEFORE the allowlist check. The browser
  // resolves "/tasks/../config" to "/config" before the request leaves, so a
  // prefix check on the raw string must not be fooled into allowing it. Also
  // reject anything that changes the origin (e.g. "//evil/tasks" is a
  // protocol-relative URL whose host would become "evil").
  let clean: string;
  try {
    const url = new URL(raw, "http://localhost");
    if (url.origin !== "http://localhost") return false;
    clean = url.pathname;
  } catch {
    return false;
  }
  return ALLOWED_GET_PREFIXES.some(
    (prefix) => clean === prefix || clean.startsWith(`${prefix}/`),
  );
}

export function withAppCsp(html: string): string {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${APP_CSP}">`;
  // Strip HTML comments before locating the injection point: a crafted
  // `<!-- <head> -->` must not shadow the real <head> and leave the document
  // with no CSP (which would re-enable network exfiltration). Comments are
  // inert, so a genuine app renders identically. Inject the meta as the FIRST
  // child of <head> so the CSP precedes any in-head or in-body script in
  // document order.
  const doc = html.replace(/<!--[\s\S]*?-->/g, "");
  if (/<head[^>]*>/i.test(doc)) {
    return doc.replace(/<head[^>]*>/i, (match) => `${match}${meta}`);
  }
  if (/<html[^>]*>/i.test(doc)) {
    return doc.replace(
      /<html[^>]*>/i,
      (match) => `${match}<head>${meta}</head>`,
    );
  }
  return `<!doctype html><html><head>${meta}</head><body>${doc}</body></html>`;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : "request failed";
}

/**
 * Service one broker bridge request: enforce GET-only + the read-only path
 * allowlist, then reply to the frame at the pinned origin. Module-scoped so the
 * component/listener stays simple; all the security checks live here.
 */
async function serviceBrokerGet(
  message: BrokerBridgeMessage,
  target: Window,
  replyOrigin: string,
): Promise<void> {
  const reply = (payload: {
    ok: boolean;
    data?: unknown;
    error?: string;
  }): void => {
    target.postMessage(
      { source: HOST_SOURCE, id: message.id, ...payload },
      replyOrigin,
    );
  };
  const method = String(message.method ?? "GET").toUpperCase();
  const path = String(message.path ?? "");
  if (method !== "GET") {
    reply({
      ok: false,
      error: "Apps can only read data (GET) in this version.",
    });
    return;
  }
  if (!isAllowedGetPath(path)) {
    reply({ ok: false, error: `Path not permitted for apps: ${path}` });
    return;
  }
  try {
    const data = await get(appBrokerPath(path));
    reply({ ok: true, data });
  } catch (err) {
    reply({ ok: false, error: errorMessage(err) });
  }
}

// appBrokerPath upgrades a bare `/tasks` to the whole office task list. A plain
// `/tasks` is channel-scoped and returns only the (usually empty) "general"
// channel and excludes done tasks, but an app virtually always wants EVERY task
// — including completed work, which is the point of a "what we did" digest — and
// an agent often rewrites the bridge's getTasks() down to a bare `/tasks`,
// dropping the query. Upgrading here (host-side) makes apps see real data
// regardless of how their bridge phrased the call. An explicit query (a specific
// channel) is left as-is.
export function appBrokerPath(path: string): string {
  return path === "/tasks"
    ? "/tasks?all_channels=true&include_done=true&viewer_slug=human"
    : path;
}

// Only one create_task confirmation may be pending at a time. A hostile app
// that loops createTask() on load would otherwise drip confirmations and train
// the human to reflexively accept (confirm-fatigue). While one is awaiting the
// human, further requests are refused; the lock frees on accept or after a
// safety window (the human ignored/cancelled the dialog).
let createTaskPending = false;
const CREATE_TASK_LOCK_MS = 65_000;

/**
 * Service the one safe write apps may request: create_task. The app supplies a
 * title + details ONLY; the host fixes every other field (a top-level office
 * issue, created by the signed-in human) so an app can never mint a
 * privileged/owned task or mutate anything else. It is gated behind a human
 * confirmation — a state-changing action the human may not have explicitly
 * authorized (the app could fire on load), so we show it before it happens
 * rather than surprise them. The created task is visible in the normal feed.
 */
function serviceCreateTask(
  message: AppActionMessage,
  target: Window,
  replyOrigin: string,
): void {
  const reply = (payload: {
    ok: boolean;
    data?: unknown;
    error?: string;
  }): void => {
    target.postMessage(
      { source: HOST_SOURCE, id: message.id, ...payload },
      replyOrigin,
    );
  };
  if (message.action !== "create_task") {
    reply({ ok: false, error: `Unsupported app action: ${message.action}` });
    return;
  }
  const input =
    message.payload && typeof message.payload === "object"
      ? (message.payload as Record<string, unknown>)
      : {};
  const title = capString(input.title, ACTION_TITLE_MAX).trim();
  if (!title) {
    reply({ ok: false, error: "A task needs a title." });
    return;
  }
  if (createTaskPending) {
    reply({
      ok: false,
      error:
        "A task is already awaiting your confirmation — finish that first.",
    });
    return;
  }
  const details = capString(input.details, ACTION_DETAILS_MAX);
  createTaskPending = true;
  const release = window.setTimeout(() => {
    createTaskPending = false;
  }, CREATE_TASK_LOCK_MS);
  confirm({
    message: `Create a task for the team?\n\n“${title}”`,
    confirmLabel: "Create task",
    onConfirm: async () => {
      window.clearTimeout(release);
      createTaskPending = false;
      try {
        const res = await post<{ task?: { id?: string } }>("/tasks", {
          action: "create",
          channel: "general",
          title,
          details,
          created_by: "human",
          task_type: "issue",
        });
        const id = res?.task?.id ?? "";
        showNotice(`Created task${id ? ` ${id}` : ""}: ${title}`, "success");
        reply({ ok: true, data: { id, title } });
      } catch (err) {
        showNotice("Could not create the task.", "error");
        reply({ ok: false, error: errorMessage(err) });
      }
    },
  });
}

/**
 * Service a Bridge v2 "integration" call: validate the {platform, action,
 * params} shape, forward to POST /apps/integrations/call, and reply to the
 * frame. The host does NOT decide read-vs-mutate — the broker does, server-side,
 * and a mutating action returns {status:"needs_approval"} instead of executing.
 * The host's job is to gate the message shape and never let the app reach the
 * broker except through this one sanctioned endpoint.
 */
async function serviceIntegrationCall(
  message: AppIntegrationMessage,
  target: Window,
  replyOrigin: string,
  appId?: string,
): Promise<void> {
  const reply = (payload: {
    ok: boolean;
    data?: unknown;
    error?: string;
  }): void => {
    target.postMessage(
      { source: HOST_SOURCE, id: message.id, ...payload },
      replyOrigin,
    );
  };
  const args = parseIntegrationArgs(message);
  if (!args) {
    reply({
      ok: false,
      error:
        "An integration call needs a platform + action; params must be a small object.",
    });
    return;
  }
  try {
    // app_id is HOST-supplied (the sealed iframe never sees it), so the broker
    // budgets integration reads per-app. Omitted when unknown → server falls back
    // to the actor bucket.
    const data = await post("/apps/integrations/call", {
      platform: args.platform,
      action: args.action,
      params: args.params ?? {},
      ...(appId ? { app_id: appId } : {}),
    });
    reply({ ok: true, data });
  } catch (err) {
    reply({ ok: false, error: errorMessage(err) });
  }
}

/**
 * Service a Bridge v2 "ai" call: validate {prompt, input, json}, forward to
 * POST /apps/ai, and reply. The broker runs a BOUNDED one-shot completion over
 * data the app already holds. Not a network escape hatch — the frame still has
 * connect-src 'none'; this is read-only reasoning routed through the host.
 */
async function serviceAICall(
  message: AppAIMessage,
  target: Window,
  replyOrigin: string,
  appId?: string,
): Promise<void> {
  const reply = (payload: {
    ok: boolean;
    data?: unknown;
    error?: string;
  }): void => {
    target.postMessage(
      { source: HOST_SOURCE, id: message.id, ...payload },
      replyOrigin,
    );
  };
  const args = parseAIArgs(message);
  if (!args) {
    reply({
      ok: false,
      error:
        "ai() needs a non-empty prompt (and prompt/input within size limits).",
    });
    return;
  }
  try {
    // app_id is HOST-supplied so the broker budgets ai() per-app (a misbehaving
    // app can't burn the whole workspace's LLM budget). Omitted when unknown.
    const data = await post("/apps/ai", {
      prompt: args.prompt,
      input: args.input,
      json: args.json,
      ...(appId ? { app_id: appId } : {}),
    });
    reply({ ok: true, data });
  } catch (err) {
    reply({ ok: false, error: errorMessage(err) });
  }
}

type SelectHandlerRef = {
  current: ((sel: AppSelectPayload) => void) | undefined;
};
type ErrorHandlerRef = {
  current: ((err: AppErrorPayload) => void) | undefined;
};

/**
 * Route one inbound app→host message. Module-scoped (shallow nesting) so the
 * security-relevant branching stays flat and readable. Order is the contract:
 * identity check first, then the display-only inspector types (which return
 * early and NEVER reach the broker), then the broker GET path.
 */
export function routeInboundMessage(
  event: MessageEvent,
  frame: HTMLIFrameElement | null,
  replyOrigin: string,
  onSelectRef: SelectHandlerRef,
  onAppErrorRef: ErrorHandlerRef,
  appId?: string,
): void {
  // Identity by window reference, not origin: a sandboxed opaque-origin frame
  // reports origin "null", so origin checks are useless here.
  if (!frame || event.source !== frame.contentWindow) return;
  const data = event.data as {
    source?: string;
    type?: string;
    id?: string | number;
  } | null;
  if (!data || data.source !== APP_SOURCE) return;
  // Dev-only inspector messages: DISPLAY DATA ONLY. They surface to host React
  // state via callbacks and MUST NOT touch the broker. Validate/cap then return
  // early — they never fall through to broker handling.
  if (data.type === "wuphf-select") {
    const sel = parseSelectPayload(data);
    if (sel) onSelectRef.current?.(sel);
    return;
  }
  if (data.type === "wuphf-error") {
    onAppErrorRef.current?.(parseErrorPayload(data));
    return;
  }
  // Every remaining type is a request that needs a reply: it MUST carry an id
  // and a real sender window. Gate that once here so each handler can assume it.
  if (data.id === undefined || event.source === null) return;
  routeAppRequest(data, event.source as Window, replyOrigin, appId);
}

/**
 * routeAppRequest dispatches the reply-bearing app→host requests AFTER the
 * identity + display-only checks in routeInboundMessage have passed. Split out
 * so the security-ordering function above stays flat; the contract is unchanged
 * (inspector types still return before anything here runs). Each branch forwards
 * exactly one sanctioned shape and nothing else.
 */
function routeAppRequest(
  data: { type?: string; id?: string | number },
  source: Window,
  replyOrigin: string,
  appId?: string,
): void {
  switch (data.type) {
    // The one safe office write: a human-confirmed, host-parameterized task.
    case "action":
      serviceCreateTask(data as AppActionMessage, source, replyOrigin);
      return;
    // Bridge v2: a generic integration call. The host forwards the validated
    // shape; the broker decides read-vs-mutate and gates mutations behind the
    // human approval card. Reads return the user's own data; the app never
    // executes a write without a human click.
    case "integration":
      void serviceIntegrationCall(
        data as AppIntegrationMessage,
        source,
        replyOrigin,
        appId,
      );
      return;
    // Bridge v2: a bounded one-shot ai() completion over data the app holds.
    case "ai":
      void serviceAICall(data as AppAIMessage, source, replyOrigin, appId);
      return;
    case "broker":
      void serviceBrokerGet(data as BrokerBridgeMessage, source, replyOrigin);
      return;
    default:
      return;
  }
}

/**
 * useAppBridge wires the host side of the postMessage bridge: it services
 * read-only broker GETs and routes the dev-only inspector messages
 * (`wuphf-select` / `wuphf-error`) to display-only callbacks. Keyed on devUrl
 * only — the callback refs let new parent closures flow through without
 * re-subscribing, so the identity-by-window listener stays stable.
 */
function useAppBridge(
  iframeRef: RefObject<HTMLIFrameElement | null>,
  devUrl: string | undefined,
  onSelectRef: SelectHandlerRef,
  onAppErrorRef: ErrorHandlerRef,
  appId: string | undefined,
): void {
  useEffect(() => {
    // In DEV mode the frame is a real, known origin (the proxy), so pin replies
    // to it — the office data must not reach a frame that navigated to a
    // different origin. In SEALED mode the frame is an opaque origin ("null")
    // and "*" is the only option (and is safe: no allow-same-origin, no nested
    // browsing contexts).
    let replyOrigin = "*";
    if (devUrl) {
      try {
        replyOrigin = new URL(devUrl).origin;
      } catch {
        replyOrigin = "*";
      }
    }

    const onMessage = (event: MessageEvent): void =>
      routeInboundMessage(
        event,
        iframeRef.current,
        replyOrigin,
        onSelectRef,
        onAppErrorRef,
        appId,
      );

    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [devUrl, iframeRef, onSelectRef, onAppErrorRef, appId]);
}

/**
 * useSelectModeSync pushes the select-mode toggle into the dev frame. Posted to
 * the SAME pinned dev origin the bridge uses (never "*"), so only the known
 * proxy frame can receive it. Re-posts on selectMode change and once the frame
 * is ready (onLoad). Only meaningful in dev; a no-op when there is no frame.
 */
function useSelectModeSync(
  iframeRef: RefObject<HTMLIFrameElement | null>,
  devUrl: string | undefined,
  selectMode: boolean | undefined,
): void {
  useEffect(() => {
    if (!devUrl) return;
    let replyOrigin: string;
    try {
      replyOrigin = new URL(devUrl).origin;
    } catch {
      return;
    }
    function postToggle(): void {
      iframeRef.current?.contentWindow?.postMessage(
        {
          source: HOST_SOURCE,
          type: "wuphf-select-mode",
          enabled: !!selectMode,
        },
        replyOrigin,
      );
    }
    postToggle();
    const frame = iframeRef.current;
    frame?.addEventListener("load", postToggle);
    return () => frame?.removeEventListener("load", postToggle);
  }, [selectMode, devUrl, iframeRef]);
}

interface CustomAppFrameProps {
  title: string;
  /**
   * The owning app's id. Forwarded (host-side) on ai() / integration bridge
   * calls so the broker meters them PER-APP. Optional: when absent the broker
   * falls back to an actor-scoped budget.
   */
  appId?: string;
  /** Sealed mode: the published single-file bundle, rendered via srcDoc. */
  html?: string;
  /**
   * Dev (live) mode: the broker dev-server proxy origin. When set the frame
   * loads it directly with HMR instead of the sealed bundle. The proxy injects
   * the security CSP (a generated vite.config can't strip it) and serves a
   * DISTINCT 127.0.0.1 origin, so allow-same-origin grants the app only its OWN
   * origin — never the parent app's session. The postMessage bridge below works
   * in both modes (identity is by window reference, not origin).
   */
  devUrl?: string;
  /**
   * Dev-only: when true the in-app inspector outlines elements and intercepts
   * the next click to report its source location via `onSelect`. The host posts
   * the toggle to the frame using the pinned dev origin.
   */
  selectMode?: boolean;
  /** Dev-only: a "select to edit" click resolved to a source location. */
  onSelect?: (sel: AppSelectPayload) => void;
  /** Dev-only: a runtime error/unhandled rejection fired inside the app. */
  onAppError?: (err: AppErrorPayload) => void;
}

export function CustomAppFrame({
  html,
  title,
  appId,
  devUrl,
  selectMode,
  onSelect,
  onAppError,
}: CustomAppFrameProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  // Hold the latest callbacks in refs so the message listener effect (keyed on
  // devUrl only) need not re-subscribe when a parent passes new closures — the
  // broker listener identity stays stable, preserving existing behavior.
  const onSelectRef = useRef(onSelect);
  const onAppErrorRef = useRef(onAppError);
  onSelectRef.current = onSelect;
  onAppErrorRef.current = onAppError;
  const srcDoc = useMemo(
    () => (html !== undefined ? withAppCsp(html) : undefined),
    [html],
  );

  useAppBridge(iframeRef, devUrl, onSelectRef, onAppErrorRef, appId);
  useSelectModeSync(iframeRef, devUrl, selectMode);

  if (devUrl) {
    return (
      <iframe
        ref={iframeRef}
        className="custom-app-frame"
        title={title}
        sandbox="allow-scripts allow-same-origin"
        src={devUrl}
      />
    );
  }

  return (
    <iframe
      ref={iframeRef}
      className="custom-app-frame"
      title={title}
      sandbox="allow-scripts"
      srcDoc={srcDoc}
    />
  );
}
