import { useEffect, useMemo, useRef } from "react";

import { get } from "../../api/client";

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
 *     user's own session (the app never holds a token). Writes are rejected.
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
  "/apps",
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

interface BrokerBridgeMessage {
  source: typeof APP_SOURCE;
  type: "broker";
  id: string | number;
  method?: string;
  path?: string;
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

interface CustomAppFrameProps {
  title: string;
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
    function reply(
      target: Window,
      id: string | number,
      payload: { ok: boolean; data?: unknown; error?: string },
    ): void {
      target.postMessage({ source: HOST_SOURCE, id, ...payload }, replyOrigin);
    }

    async function handle(
      message: BrokerBridgeMessage,
      target: Window,
    ): Promise<void> {
      const method = String(message.method ?? "GET").toUpperCase();
      const path = String(message.path ?? "");
      if (method !== "GET") {
        reply(target, message.id, {
          ok: false,
          error: "Apps can only read data (GET) in this version.",
        });
        return;
      }
      if (!isAllowedGetPath(path)) {
        reply(target, message.id, {
          ok: false,
          error: `Path not permitted for apps: ${path}`,
        });
        return;
      }
      try {
        const data = await get(path);
        reply(target, message.id, { ok: true, data });
      } catch (err) {
        reply(target, message.id, { ok: false, error: errorMessage(err) });
      }
    }

    function onMessage(event: MessageEvent): void {
      const frame = iframeRef.current;
      // Identity by window reference, not origin: a sandboxed opaque-origin
      // frame reports origin "null", so origin checks are useless here.
      if (!frame || event.source !== frame.contentWindow) return;
      const data = event.data as {
        source?: string;
        type?: string;
        id?: string | number;
      } | null;
      if (!data || data.source !== APP_SOURCE) return;
      // Dev-only inspector messages: DISPLAY DATA ONLY. They surface to host
      // React state via callbacks and MUST NOT touch the broker. Validate/cap
      // then return early — they never fall through to broker handling.
      if (data.type === "wuphf-select") {
        const sel = parseSelectPayload(data);
        if (sel) onSelectRef.current?.(sel);
        return;
      }
      if (data.type === "wuphf-error") {
        onAppErrorRef.current?.(parseErrorPayload(data));
        return;
      }
      if (data.type !== "broker") return;
      if (data.id === undefined || event.source === null) return;
      void handle(data as BrokerBridgeMessage, event.source as Window);
    }

    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [devUrl]);

  // Push the select-mode toggle into the dev frame. Posted to the SAME pinned
  // dev origin the bridge uses (never "*"), so only the known proxy frame can
  // receive it. Re-posts on selectMode change and once the frame is ready
  // (onLoad). Only meaningful in dev (no inspector in the sealed bundle); a
  // no-op when there is no frame/contentWindow.
  useEffect(() => {
    if (!devUrl) return;
    let replyOrigin: string;
    try {
      replyOrigin = new URL(devUrl).origin;
    } catch {
      return;
    }
    function post(): void {
      iframeRef.current?.contentWindow?.postMessage(
        {
          source: HOST_SOURCE,
          type: "wuphf-select-mode",
          enabled: !!selectMode,
        },
        replyOrigin,
      );
    }
    post();
    const frame = iframeRef.current;
    frame?.addEventListener("load", post);
    return () => frame?.removeEventListener("load", post);
  }, [selectMode, devUrl]);

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
