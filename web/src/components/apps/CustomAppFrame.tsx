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

interface BrokerBridgeMessage {
  source: typeof APP_SOURCE;
  type: "broker";
  id: string | number;
  method?: string;
  path?: string;
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
  html: string;
  title: string;
}

export function CustomAppFrame({ html, title }: CustomAppFrameProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const srcDoc = useMemo(() => withAppCsp(html), [html]);

  useEffect(() => {
    function reply(
      target: Window,
      id: string | number,
      payload: { ok: boolean; data?: unknown; error?: string },
    ): void {
      // The frame is an opaque origin (no allow-same-origin), so we cannot
      // target a concrete origin; "*" is acceptable because the payload is the
      // user's own office data and the sandbox forbids nested browsing contexts.
      target.postMessage({ source: HOST_SOURCE, id, ...payload }, "*");
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
      const data = event.data as Partial<BrokerBridgeMessage> | null;
      if (!data || data.source !== APP_SOURCE || data.type !== "broker") return;
      if (data.id === undefined || event.source === null) return;
      void handle(data as BrokerBridgeMessage, event.source as Window);
    }

    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

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
