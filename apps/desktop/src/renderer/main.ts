import "./styles.css";

import type {
  BrokerStatus,
  GetBrokerStatusResponse,
  GetPlatformResponse,
} from "../shared/api-contract.ts";

interface RendererState {
  version: string;
  platform: GetPlatformResponse | null;
  broker: GetBrokerStatusResponse | null;
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

const openRepoButton = document.createElement("button");
openRepoButton.type = "button";
openRepoButton.textContent = "Open repo on GitHub";
openRepoButton.addEventListener("click", () => {
  void api.openExternal({ url: "https://github.com/nex-crm/wuphf" });
});

const errorLine = document.createElement("p");
errorLine.className = "error-line";

pane.append(title, brokerLine, platformLine, openRepoButton, errorLine);
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
}

async function refreshBrokerStatus(): Promise<void> {
  try {
    state.broker = await api.getBrokerStatus();
    state.error = null;
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

  errorLine.textContent = state.error ?? "";
}

function formatBrokerStatus(status: BrokerStatus): string {
  if (status === "alive") {
    return "alive \u2713";
  }
  return status;
}
