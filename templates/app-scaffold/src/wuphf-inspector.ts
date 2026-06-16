/// <reference types="vite/client" />
/**
 * wuphf-inspector — DEV-ONLY in-app runtime that powers the host's
 * "select to edit" and runtime-error surfacing in the live preview.
 *
 * It is guarded on `import.meta.env.DEV`, so the production single-file build
 * (where this is imported but never installs) tree-shakes the body away — the
 * sealed bundle ships none of this.
 *
 * Wire contract (must match web/src/components/apps/CustomAppFrame.tsx):
 *   host -> app : { source: "wuphf-host", type: "wuphf-select-mode", enabled }
 *   app  -> host : { source: "wuphf-app", type: "wuphf-select",
 *                    file, line, col, tag, label }
 *   app  -> host : { source: "wuphf-app", type: "wuphf-error", message, stack }
 *
 * These app->host messages carry DISPLAY DATA ONLY. They never trigger a broker
 * call or any network request on the host — they only surface text to host
 * React state. All app-supplied strings are length-capped here at the source
 * (and re-capped by the host) and are rendered by the host as text, never HTML.
 */

const HOST_SOURCE = "wuphf-host";
const APP_SOURCE = "wuphf-app";

// Length caps applied at the source. The host re-applies its own caps as the
// trust boundary, but capping here keeps the postMessage payloads small.
const LABEL_MAX = 120;
const ERROR_MAX = 600;

// The source location is read from a DOM attribute the dev-only Babel plugin in
// vite.config.ts stamps on every host JSX element: data-wuphf-source="rel:line:col".
// This replaces walking React fibers for `_debugSource`, which React 19 removed —
// a real DOM attribute is version-proof and survives a click on any descendant.
const SOURCE_ATTR = "data-wuphf-source";

interface SourceLoc {
  file: string;
  line: number;
  col: number;
}

interface SelectPayload extends SourceLoc {
  tag: string;
  label: string;
}

// Accessed directly (not via an alias) so Vite statically replaces it with
// `false` in the production build, letting Rollup tree-shake this whole module
// out of the sealed single-file bundle. The `vite/client` reference above types
// `import.meta.env` without an `any`.
const DEV = import.meta.env.DEV;

function cap(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) : value;
}

/**
 * Resolve a clicked node to its JSX source location by reading the nearest
 * ancestor's stamped `data-wuphf-source="rel:line:col"`. `closest` covers the
 * common case of clicking a text run inside a stamped element.
 */
function sourceForNode(node: Element): SourceLoc | null {
  const el = node.closest(`[${SOURCE_ATTR}]`);
  const raw = el?.getAttribute(SOURCE_ATTR);
  if (!raw) return null;
  // "relative/path:line:col" — anchor the two trailing numeric groups so a path
  // is parsed correctly even if it (unexpectedly) contained a colon.
  const m = raw.match(/^(.*):(\d+):(\d+)$/);
  if (!m) return null;
  return { file: m[1], line: Number(m[2]), col: Number(m[3]) };
}

function selectPayloadFor(target: Element): SelectPayload | null {
  const source = sourceForNode(target);
  if (!source) return null;
  const tag = target.tagName.toLowerCase();
  const label = cap(
    (target.textContent ?? "").trim().replace(/\s+/g, " "),
    LABEL_MAX,
  );
  return { file: source.file, line: source.line, col: source.col, tag, label };
}

export function installInspector(): void {
  if (!DEV) return;
  // Guard against double-install (StrictMode / HMR can re-run module init).
  const flag = "__wuphfInspectorInstalled";
  const w = window as unknown as Record<string, unknown>;
  if (w[flag]) return;
  w[flag] = true;

  let selectMode = false;
  let hovered: HTMLElement | null = null;
  let prevOutline = "";
  let prevCursor = "";

  function clearHover(): void {
    if (hovered) {
      hovered.style.outline = prevOutline;
      hovered.style.cursor = prevCursor;
      hovered = null;
    }
  }

  function setHover(el: HTMLElement): void {
    if (hovered === el) return;
    clearHover();
    hovered = el;
    prevOutline = el.style.outline;
    prevCursor = el.style.cursor;
    // A thin accent outline + crosshair: enough to show what will be selected
    // without a full overlay-rectangle system.
    el.style.outline = "2px solid #4c8dff";
    el.style.cursor = "crosshair";
  }

  function onPointerMove(event: Event): void {
    if (!selectMode) return;
    try {
      const target = event.target;
      if (target instanceof HTMLElement) setHover(target);
    } catch {
      // Never throw out of a listener.
    }
  }

  function onClickCapture(event: MouseEvent): void {
    if (!selectMode) return;
    try {
      // Intercept the click so the app under inspection doesn't act on it.
      event.preventDefault();
      event.stopPropagation();
      const target = event.target;
      const payload =
        target instanceof Element ? selectPayloadFor(target) : null;
      if (payload) {
        window.parent.postMessage(
          { source: APP_SOURCE, type: "wuphf-select", ...payload },
          // Target origin "*" is fine from the app side: the host validates the
          // sender by window identity, not origin, and this is display-only data.
          "*",
        );
      }
    } catch {
      // Best-effort: React internals are not a stable contract.
    } finally {
      // One-shot: a select turns the mode off locally and clears the outline.
      selectMode = false;
      clearHover();
    }
  }

  function onHostMessage(event: MessageEvent): void {
    try {
      const data = event.data as
        | { source?: string; type?: string; enabled?: boolean }
        | null;
      if (
        !data ||
        data.source !== HOST_SOURCE ||
        data.type !== "wuphf-select-mode"
      ) {
        return;
      }
      selectMode = data.enabled === true;
      if (!selectMode) clearHover();
    } catch {
      // Never throw out of a listener.
    }
  }

  let lastErrorAt = 0;
  function reportError(message: string, stack: string): void {
    // Throttle: a render loop could fire onerror in a tight loop. One report
    // per 500ms is enough to surface the banner without flooding postMessage.
    const now = Date.now();
    if (now - lastErrorAt < 500) return;
    lastErrorAt = now;
    try {
      window.parent.postMessage(
        {
          source: APP_SOURCE,
          type: "wuphf-error",
          message: cap(message, ERROR_MAX),
          stack: cap(stack, ERROR_MAX),
        },
        "*",
      );
    } catch {
      // Surfacing the error must itself never throw.
    }
  }

  window.addEventListener("message", onHostMessage);
  window.addEventListener("pointermove", onPointerMove, true);
  window.addEventListener("click", onClickCapture, true);

  window.addEventListener("error", (event: ErrorEvent) => {
    const message = event.message || "Script error";
    const stack = event.error instanceof Error ? (event.error.stack ?? "") : "";
    reportError(message, stack);
  });

  window.addEventListener(
    "unhandledrejection",
    (event: PromiseRejectionEvent) => {
      const reason = event.reason;
      const message =
        reason instanceof Error
          ? reason.message
          : typeof reason === "string"
            ? reason
            : "Unhandled promise rejection";
      const stack = reason instanceof Error ? (reason.stack ?? "") : "";
      reportError(message, stack);
    },
  );
}

// Self-install on load: vite.config.ts injects this module as a static external
// `<script type="module" src=…>` (dev only), so loading it IS the install. A
// static module script is part of the document's module graph — the browser
// loads it reliably, unlike a fire-and-forget dynamic import that can lose the
// race at first paint. Idempotent (the guard inside) + dev-gated + tree-shaken
// from the sealed build, where this module is never imported.
installInspector();
