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
 *   host -> app  : { source: "wuphf-host", id, ok, data? , error? }
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

export function getTasks(): Promise<{ tasks: OfficeTask[] }> {
  return callBroker<{ tasks: OfficeTask[] }>("/tasks");
}
