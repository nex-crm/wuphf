// Single source of truth for the contextBridge allowlist.
//
// Imported by:
//   - src/preload/preload.ts            (to wire each verb to ipcRenderer.invoke)
//   - src/main/ipc/register-handlers.ts (to assert every handler matches a verb)
//   - src/renderer/types/window.d.ts    (to type window.wuphf at the renderer)
//   - tests/preload-allowlist.spec.ts   (to assert exposed surface == this list)
//
// Adding a verb requires (per AGENTS.md rule 4):
//   1. A new entry in IPC_ALLOWLIST below with `kind: "os-verb"` and a
//      one-line "why this is an OS verb, not app data" justification.
//   2. A handler in src/main/ipc/<verb>.ts that validates its payload.
//   3. A test in tests/preload-allowlist.spec.ts.
//   4. A docs update in docs/modules/preload.md.
//
// Removing a verb is a breaking change; coordinate with renderer code first.

/**
 * Every IPC channel name lives in this enum. The literal string values are
 * the wire format; renaming changes the wire and requires migrating both
 * sides in the same PR.
 */
export const IpcChannel = {
  OpenExternal: "wuphf:open-external",
  ShowItemInFolder: "wuphf:show-item-in-folder",
  GetAppVersion: "wuphf:get-app-version",
  GetPlatform: "wuphf:get-platform",
  GetBrokerStatus: "wuphf:get-broker-status",
} as const;

export type IpcChannelName = (typeof IpcChannel)[keyof typeof IpcChannel];

/**
 * The enumerated set of allowed channel names. Used by the IPC allowlist
 * grep gate (scripts/check-ipc-allowlist.sh) — every `ipcMain.handle(...)`
 * call's first argument must be one of these literals.
 */
export const IPC_CHANNEL_VALUES = Object.values(IpcChannel) as readonly IpcChannelName[];

/**
 * Why this is an OS verb (and not app data) for each entry. Reviewers
 * grep this map to enforce AGENTS.md rule 3 — adding a new entry is a
 * load-bearing decision and reviewers reject any "why" line that smells
 * like app state.
 */
export const IPC_ALLOWLIST_RATIONALE: Record<IpcChannelName, string> = {
  [IpcChannel.OpenExternal]:
    "Hands a URL to the OS default browser. No app data crosses the boundary.",
  [IpcChannel.ShowItemInFolder]:
    "Reveals a path in the OS file manager. Path is supplied by the renderer; main does not return file contents.",
  [IpcChannel.GetAppVersion]:
    "Returns Electron `app.getVersion()` — the binary's own version string. Not user data.",
  [IpcChannel.GetPlatform]:
    "Returns process.platform + process.arch. Static OS facts, not user data.",
  [IpcChannel.GetBrokerStatus]:
    "Returns liveness state of the broker utility process: 'starting' | 'alive' | 'unresponsive' | 'dead' | 'unknown'. NOT broker data — only its lifecycle state.",
};

/**
 * Payload contracts. Each verb's request/response is typed here; runtime
 * guards in src/main/ipc/<verb>.ts enforce the request shape at the
 * boundary (per AGENTS.md rule 5).
 */

export interface OpenExternalRequest {
  readonly url: string;
}

export interface OkResponse {
  readonly ok: true;
}

export interface ErrResponse {
  readonly ok: false;
  readonly error: string;
}

export function okResponse(): OkResponse {
  return { ok: true };
}

export function errResponse(error: string): ErrResponse {
  return { ok: false, error };
}

export type OpenExternalResponse = OkResponse | ErrResponse;

export interface ShowItemInFolderRequest {
  readonly path: string;
}
export type ShowItemInFolderResponse = OkResponse | ErrResponse;

declare const emptyPayloadBrand: unique symbol;
export type EmptyPayload = { readonly [emptyPayloadBrand]: never };
export const EMPTY_PAYLOAD = Object.freeze({}) as EmptyPayload;

export type GetAppVersionRequest = EmptyPayload;
export interface GetAppVersionResponse {
  readonly version: string;
}

export type GetPlatformRequest = EmptyPayload;

export type DesktopPlatform =
  | "aix"
  | "android"
  | "darwin"
  | "freebsd"
  | "haiku"
  | "linux"
  | "openbsd"
  | "sunos"
  | "win32"
  | "cygwin"
  | "netbsd";

export interface GetPlatformResponse {
  readonly platform: DesktopPlatform;
  readonly arch: string;
}

export type BrokerStatus = "starting" | "alive" | "unresponsive" | "dead" | "unknown";

export interface BrokerSnapshot {
  readonly status: BrokerStatus;
  readonly pid: number | null;
  readonly restartCount: number;
  /**
   * Loopback URL the broker bound (e.g. `http://127.0.0.1:54321`) once the
   * broker reports `{ ready }`. `null` until the listener is up — including
   * across restarts where the supervisor re-spawns the broker process.
   * Renderer code reads this only as a discovery hint when running outside
   * the broker's same-origin context (i.e. dev mode); in packaged mode the
   * window is loaded from this URL and `window.location.origin` is the
   * authoritative source.
   *
   * Typed as plain `string | null` on the IPC contract surface because the
   * renderer tsconfig excludes node types — importing the
   * `@wuphf/protocol#BrokerUrl` brand here transitively requires `node:*`
   * resolution that the renderer cannot satisfy. The supervisor still
   * validates via `@wuphf/protocol#isBrokerUrl` at the utilityProcess
   * boundary and carries the brand internally; the contextBridge serialises
   * to plain string regardless.
   */
  readonly brokerUrl: string | null;
}

export type GetBrokerStatusRequest = EmptyPayload;
export type GetBrokerStatusResponse = BrokerSnapshot;

/**
 * The contextBridge surface as it appears on `window.wuphf`. Renderer
 * code imports this type and never reaches for `electron`, `ipcRenderer`,
 * or any Node global.
 */
export interface WuphfDesktopApi {
  readonly openExternal: (request: OpenExternalRequest) => Promise<OpenExternalResponse>;
  readonly showItemInFolder: (
    request: ShowItemInFolderRequest,
  ) => Promise<ShowItemInFolderResponse>;
  readonly getAppVersion: () => Promise<GetAppVersionResponse>;
  readonly getPlatform: () => Promise<GetPlatformResponse>;
  readonly getBrokerStatus: () => Promise<GetBrokerStatusResponse>;
}

/**
 * The single global key the contextBridge installs. Anything else under
 * `window.*` from this package is a bug.
 */
export const WUPHF_GLOBAL_KEY = "wuphf" as const;
