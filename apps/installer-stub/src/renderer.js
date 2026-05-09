const button = document.querySelector("#check-updates");
const installButton = document.querySelector("#install-update");
const installerTitle = document.querySelector("#installer-title");
const buildChannel = document.querySelector("#build-channel");
const updateState = document.querySelector("#update-state");
const updater = window.wuphfUpdater;

let currentState = "idle";
let actionInFlight = false;

function setUpdateState(state) {
  currentState = state.state;

  if (updateState) {
    if (state.state === "checking") {
      updateState.textContent = "Auto-update: checking...";
    } else if (state.state === "available") {
      updateState.textContent = `Auto-update: update available v${state.version}`;
    } else if (state.state === "downloading") {
      const percent = Number.isFinite(state.percent) ? ` ${Math.round(state.percent)}%` : "";
      updateState.textContent = `Auto-update: downloading${percent}`;
    } else if (state.state === "downloaded") {
      updateState.textContent = `Auto-update: downloaded v${state.version}; restart required`;
    } else if (state.state === "cancelled") {
      updateState.textContent = state.version
        ? `Auto-update: download cancelled for v${state.version}`
        : "Auto-update: download cancelled";
    } else if (state.state === "up-to-date") {
      updateState.textContent = "Auto-update: up to date";
    } else if (state.state === "error") {
      updateState.textContent = `Auto-update: error: ${state.message}`;
    } else {
      updateState.textContent = "Auto-update: idle";
    }
  }

  if (button) {
    button.disabled = state.state === "checking" || state.state === "downloading";
    button.hidden = state.state === "downloaded";
    if (state.state === "available" || state.state === "cancelled") {
      button.textContent = "Download update";
    } else {
      button.textContent = "Check for updates";
    }
  }

  if (installButton) {
    installButton.hidden = state.state !== "downloaded";
  }
}

if (!updater) {
  setUpdateState({ state: "error", message: "Updater bridge unavailable" });
} else {
  button?.addEventListener("click", async () => {
    if (actionInFlight) {
      return;
    }

    actionInFlight = true;
    try {
      if (currentState === "downloaded") {
        await updater.installUpdateAndRestart();
        return;
      }

      if (currentState === "available" || currentState === "cancelled") {
        await updater.downloadUpdate();
        return;
      }

      setUpdateState({ state: "checking" });
      await updater.checkForUpdates();
    } catch (error) {
      setUpdateState({ state: "error", message: error.message });
    } finally {
      actionInFlight = false;
    }
  });

  installButton?.addEventListener("click", async () => {
    if (actionInFlight) {
      return;
    }

    actionInFlight = true;
    try {
      await updater.installUpdateAndRestart();
    } catch (error) {
      setUpdateState({ state: "error", message: error.message });
    } finally {
      actionInFlight = false;
    }
  });

  updater.onUpdateState(setUpdateState);

  updater
    .getInstallerVersion()
    .then((metadata) => {
      if (installerTitle) {
        installerTitle.textContent = `WUPHF installer-stub v${metadata.version}`;
      }

      if (buildChannel) {
        buildChannel.textContent = `Channel: ${metadata.channel}`;
      }
    })
    .catch((error) => {
      setUpdateState({ state: "error", message: error.message });
    });
}
