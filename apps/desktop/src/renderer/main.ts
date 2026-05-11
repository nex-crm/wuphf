import "./styles.css";

import type {
  BrokerStatus,
  GetBrokerStatusResponse,
  GetPlatformResponse,
} from "../shared/api-contract.ts";

interface BootstrapResult {
  readonly tokenLength: number;
  readonly brokerUrl: string;
  readonly healthOk: boolean;
}

interface RendererState {
  version: string;
  platform: GetPlatformResponse | null;
  broker: GetBrokerStatusResponse | null;
  bootstrap: BootstrapResult | null;
  error: string | null;
}

const api = window.wuphf;
const appRoot = document.querySelector<HTMLDivElement>("#app");

if (appRoot === null) {
  throw new Error("Missing #app mount point");
}

const state: RendererState = {
  version: "",
  platform: null,
  broker: null,
  bootstrap: null,
  error: null,
};

const pane = document.createElement("main");
pane.className = "status-pane";

const title = document.createElement("h1");
title.textContent = "WUPHF v1 desktop shell";

const brokerLine = document.createElement("p");
brokerLine.className = "status-line";

const platformLine = document.createElement("p");
platformLine.className = "status-line";

const bootstrapLine = document.createElement("p");
bootstrapLine.className = "status-line";

const openRepoButton = document.createElement("button");
openRepoButton.type = "button";
openRepoButton.textContent = "Open repo on GitHub";
openRepoButton.addEventListener("click", () => {
  void api.openExternal({ url: "https://github.com/nex-crm/wuphf" });
});

const errorLine = document.createElement("p");
errorLine.className = "error-line";

pane.append(title, brokerLine, platformLine, bootstrapLine, openRepoButton, errorLine);
appRoot.append(pane);

void initialize();
setInterval(() => {
  void refreshBrokerStatus();
}, 1_000);

async function initialize(): Promise<void> {
  try {
    const [version, platform, broker] = await Promise.all([
      api.getAppVersion(),
      api.getPlatform(),
      api.getBrokerStatus(),
    ]);
    state.version = version.version;
    state.platform = platform;
    state.broker = broker;
    document.title = `WUPHF ${state.version} desktop shell`;
  } catch (error) {
    state.error = error instanceof Error ? error.message : "Failed to initialize desktop shell";
  }
  render();
  // Branch-4 proof-of-life: hit the broker bootstrap + health endpoints over
  // loopback HTTP. In packaged mode the renderer is loaded from the broker
  // URL so `/api-token` is same-origin; in dev (electron-vite) we discover
  // the URL via `getBrokerStatus()` and fetch cross-origin.
  void runBrokerBootstrapProbe();
}

let probeInFlight = false;

async function runBrokerBootstrapProbe(): Promise<void> {
  if (probeInFlight) return;
  probeInFlight = true;
  try {
    const requestUrl = await resolveBrokerOrigin();
    const tokenRes = await fetch(`${requestUrl}/api-token`);
    if (!tokenRes.ok) throw new Error(`api-token ${String(tokenRes.status)}`);
    const tokenJson: unknown = await tokenRes.json();
    const bootstrap = parseBootstrap(tokenJson);
    const healthRes = await fetch(`${bootstrap.brokerUrl}/api/health`, {
      headers: { Authorization: `Bearer ${bootstrap.token}` },
    });
    state.bootstrap = {
      tokenLength: bootstrap.token.length,
      brokerUrl: bootstrap.brokerUrl,
      healthOk: healthRes.ok,
    };
  } catch (error) {
    state.bootstrap = null;
    state.error = error instanceof Error ? error.message : "Broker probe failed";
  }
  render();
}

async function resolveBrokerOrigin(): Promise<string> {
  // Same-origin loopback: when the bundle was loaded from the broker,
  // window.location.origin IS the broker. Confirm by matching the
  // supervisor's snapshot URL exactly — host prefix alone is not enough
  // because the dev server can also bind 127.0.0.1 (different port). In
  // that configuration `window.location.origin` would resolve to the dev
  // server, and the renderer would try to fetch `/api-token` from Vite.
  const status = state.broker ?? (await api.getBrokerStatus());
  if (status.brokerUrl !== null && status.brokerUrl.length > 0) {
    if (originsMatch(window.location.origin, status.brokerUrl)) {
      return window.location.origin;
    }
    return status.brokerUrl;
  }
  throw new Error("broker not ready");
}

// Bare origin comparison: strip trailing slashes / paths so
// `http://127.0.0.1:1234` matches `http://127.0.0.1:1234/`. Failures fall
// to `false`, which routes the caller to the supervisor-reported URL —
// the correct conservative choice when we can't prove same-origin.
function originsMatch(a: string, b: string): boolean {
  try {
    return new URL(a).origin === new URL(b).origin;
  } catch {
    return false;
  }
}

interface ParsedBootstrap {
  readonly token: string;
  readonly brokerUrl: string;
}

// Hand-mirrors the acceptance rules from @wuphf/protocol#apiBootstrapFromJson,
// #asApiToken, and #assertApiBootstrapBrokerUrl without importing the protocol
// package into the renderer bundle; protocol depends on `node:crypto` and is
// sized for the broker subprocess, not the browser-context renderer.
//
// API_TOKEN_RE: copy of `API_TOKEN_RE` in protocol. Base64url alphabet
// only — bounded length, no `+`/`/`/`.`/`~` (those don't round-trip through
// `?token=` query strings unchanged).
const API_TOKEN_RE = /^[A-Za-z0-9_-]{16,512}$/;

function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

// Copy of protocol's requiredStringField descriptor guard. Keep this inline:
// importing @wuphf/protocol would pull Node-only crypto dependencies into the
// sandboxed renderer.
function requiredBootstrapStringField(
  record: Readonly<Record<string, unknown>>,
  key: "token" | "broker_url",
  path: string,
): string {
  if (!hasOwn(record, key)) {
    throw new Error(`${path}: is required`);
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    throw new Error(`${path}: must be a data property`);
  }
  if (typeof descriptor.value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return descriptor.value;
}

export function parseBootstrap(value: unknown): ParsedBootstrap {
  if (typeof value !== "object" || value === null) {
    throw new Error("api-token response is not an object");
  }
  const record = value as Readonly<Record<string, unknown>>;
  for (const key of Object.keys(record)) {
    if (key !== "token" && key !== "broker_url") {
      throw new Error(`api-token response has unknown key: ${key}`);
    }
  }
  const token = requiredBootstrapStringField(record, "token", "api-token response: token");
  const brokerUrl = requiredBootstrapStringField(
    record,
    "broker_url",
    "api-token response: broker_url",
  );
  if (!API_TOKEN_RE.test(token)) {
    throw new Error("api-token response: token does not match the API token shape");
  }
  if (brokerUrl.length === 0) {
    throw new Error("api-token response: broker_url must be a non-empty string");
  }
  let parsed: URL;
  try {
    parsed = new URL(brokerUrl);
  } catch {
    throw new Error("api-token response: broker_url is not a valid URL");
  }
  if (parsed.protocol !== "http:") {
    throw new Error("api-token response: broker_url must use http://");
  }
  if (parsed.port === "") {
    throw new Error("api-token response: broker_url must include an explicit port");
  }
  const portNumber = Number(parsed.port);
  if (!Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535) {
    throw new Error("api-token response: broker_url port must be 1..65535");
  }
  if (!isLoopbackHostname(parsed.hostname)) {
    throw new Error("api-token response: broker_url host must be loopback");
  }
  // Shape lock: BrokerUrl IS the broker origin in bare canonical form (no
  // trailing slash). Downstream code does `${bootstrap.brokerUrl}/api/health`
  // — a trailing-slash form would produce `http://h:p//api/health` (double
  // slash). Raw-vs-origin equality also rejects percent-encoded dot segments
  // that URL normalizes to `/`. Mirror the protocol codec's
  // assertApiBootstrapBrokerUrl byte-for-byte.
  if (
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    brokerUrl !== parsed.origin
  ) {
    throw new Error(
      "api-token response: broker_url must be a bare loopback origin with no trailing slash, userinfo, path, query, or fragment",
    );
  }
  return { token, brokerUrl };
}

// `URL.hostname` returns bracketed IPv6 (`[::1]` for `http://[::1]:1234`),
// while the bare-form check protocol does on the SAME parsed value sees
// the bracketed form. Accept both bracketed and bare loopback IPv6 so
// renderer parity holds across the v0 IPv6 path the protocol codec
// supports.
function isLoopbackHostname(hostname: string): boolean {
  if (hostname === "127.0.0.1" || hostname === "localhost") return true;
  if (hostname === "::1" || hostname === "[::1]") return true;
  return false;
}

async function refreshBrokerStatus(): Promise<void> {
  try {
    state.broker = await api.getBrokerStatus();
    state.error = null;
    // Compare the supervisor's reported brokerUrl against the URL the
    // CURRENT bootstrap is bound to (not the previous-poll value). This
    // catches the restart sequence `url1 → null → url2`: in the brief
    // window where the supervisor has cleared brokerUrl during restart,
    // a previous-poll-based diff would observe `null → url2` and conclude
    // "no change since last poll", missing the rebootstrap. Tying to the
    // cached bootstrap's brokerUrl instead means: any time the live
    // supervisor URL diverges from what our token was issued against, we
    // rebootstrap.
    //
    // The cachedBootstrapUrl-null path is the "recovery" case: initial
    // probe failed (transient loopback hiccup, broker still finishing
    // its socket bind, etc.) so bootstrap is null while the supervisor
    // reports a healthy URL. Keep that arm enabled so we retry — without
    // it, a single transient failure traps the renderer at
    // "Bootstrap: pending" forever. The probeInFlight guard below
    // prevents the 1Hz poll from piling up concurrent retries while a
    // probe is still running.
    const nextBrokerUrl = state.broker.brokerUrl;
    const cachedBootstrapUrl = state.bootstrap?.brokerUrl ?? null;
    if (
      nextBrokerUrl !== null &&
      nextBrokerUrl.length > 0 &&
      cachedBootstrapUrl !== nextBrokerUrl &&
      !probeInFlight
    ) {
      state.bootstrap = null;
      void runBrokerBootstrapProbe();
    }
  } catch (error) {
    state.error = error instanceof Error ? error.message : "Failed to refresh broker status";
  }
  render();
}

function render(): void {
  const brokerStatus = state.broker?.status ?? "unknown";
  brokerLine.textContent = `Broker: ${formatBrokerStatus(brokerStatus)}`;

  const platform = state.platform;
  platformLine.textContent =
    platform === null ? "Platform: unknown" : `Platform: ${platform.platform} / ${platform.arch}`;

  if (state.bootstrap === null) {
    bootstrapLine.textContent = "Bootstrap: pending";
  } else {
    const probe = state.bootstrap;
    const healthMark = probe.healthOk ? "✓" : "✗";
    bootstrapLine.textContent = `Bootstrap: token=${String(probe.tokenLength)}ch · health ${healthMark} · ${probe.brokerUrl}`;
  }

  errorLine.textContent = state.error ?? "";
}

function formatBrokerStatus(status: BrokerStatus): string {
  if (status === "alive") {
    return "alive \u2713";
  }
  return status;
}
