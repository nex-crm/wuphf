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

// React attaches its fiber to a DOM node under a key whose suffix is a random
// number, so we can't hardcode it. These are the two historical prefixes
// (current React uses `__reactFiber$`; older builds used `__reactInternalInstance$`).
const FIBER_KEY_PREFIXES = ["__reactFiber$", "__reactInternalInstance$"];

interface DebugSource {
  fileName: string;
  lineNumber: number;
  columnNumber?: number;
}

interface ReactFiberLike {
  return?: ReactFiberLike | null;
  _debugSource?: DebugSource;
}

interface SelectPayload {
  file: string;
  line: number;
  col: number;
  tag: string;
  label: string;
}

/**
 * Read `import.meta.env.DEV` without depending on `vite/client` ambient types
 * (the scaffold tsconfig does not include them). Narrow through `unknown` so we
 * never introduce an `any`, and default to production (no-op) if the shape is
 * unexpected.
 */
function isDev(): boolean {
  const meta = import.meta as unknown as {
    env?: { DEV?: boolean };
  };
  return meta.env?.DEV === true;
}

function cap(value: string, max: number): string {
  return value.length > max ? value.slice(0, max) : value;
}

/** Find the React fiber attached to a DOM node, if any. */
function fiberForNode(node: Element): ReactFiberLike | null {
  const keys = Object.keys(node);
  for (const key of keys) {
    if (FIBER_KEY_PREFIXES.some((prefix) => key.startsWith(prefix))) {
      const value = (node as unknown as Record<string, unknown>)[key];
      return (value as ReactFiberLike) ?? null;
    }
  }
  return null;
}

/** Walk up the fiber tree until one carries a `_debugSource`. */
function debugSourceForNode(node: Element): DebugSource | null {
  let fiber = fiberForNode(node);
  let hops = 0;
  // Bound the walk: React trees are deep but a debug source is always within a
  // few hops of the clicked node. 200 is a generous ceiling against cycles.
  while (fiber && hops < 200) {
    if (
      fiber._debugSource &&
      typeof fiber._debugSource.fileName === "string"
    ) {
      return fiber._debugSource;
    }
    fiber = fiber.return ?? null;
    hops += 1;
  }
  return null;
}

/**
 * Turn an absolute Vite file path into a workspace-relative one: strip
 * everything up to and including the last `/src/`, else fall back to basename.
 */
function relativeFile(fileName: string): string {
  const marker = "/src/";
  const idx = fileName.lastIndexOf(marker);
  if (idx >= 0) return fileName.slice(idx + marker.length);
  const slash = fileName.lastIndexOf("/");
  return slash >= 0 ? fileName.slice(slash + 1) : fileName;
}

function selectPayloadFor(target: Element): SelectPayload | null {
  const source = debugSourceForNode(target);
  if (!source) return null;
  const tag = target.tagName.toLowerCase();
  const label = cap((target.textContent ?? "").trim().replace(/\s+/g, " "), LABEL_MAX);
  return {
    file: relativeFile(source.fileName),
    line: source.lineNumber,
    col: source.columnNumber ?? 0,
    tag,
    label,
  };
}

export function installInspector(): void {
  if (!isDev()) return;
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
