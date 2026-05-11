import "./styles.css";

import type {
  BrokerStatus,
  GetBrokerStatusResponse,
  GetPlatformResponse,
} from "../shared/api-contract.ts";
import { parseBootstrap } from "./bootstrap.ts";

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
  } finally {
    probeInFlight = false;
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

// This remains in the entry point because it mutates renderer module state,
// schedules bootstrap probes, and rerenders the status pane.
export async function refreshBrokerStatus(): Promise<void> {
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
