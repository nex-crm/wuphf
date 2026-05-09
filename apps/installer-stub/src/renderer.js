const button = document.querySelector("#check-updates");
const installButton = document.querySelector("#install-update");
const installerTitle = document.querySelector("#installer-title");
const buildChannel = document.querySelector("#build-channel");
const updateState = document.querySelector("#update-state");

let currentState = "idle";

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
    if (state.state === "available" || state.state === "cancelled") {
      button.textContent = "Download update";
    } else if (state.state === "downloaded") {
      button.textContent = "Restart and install";
    } else {
      button.textContent = "Check for updates";
    }
  }

  if (installButton) {
    installButton.hidden = state.state !== "downloaded";
  }
}

button?.addEventListener("click", async () => {
  try {
    if (currentState === "downloaded") {
      await window.wuphfUpdater.installUpdateAndRestart();
      return;
    }

    if (currentState === "available" || currentState === "cancelled") {
      await window.wuphfUpdater.downloadUpdate();
      return;
    }

    setUpdateState({ state: "checking" });
    await window.wuphfUpdater.checkForUpdates();
  } catch (error) {
    setUpdateState({ state: "error", message: error.message });
  }
});

installButton?.addEventListener("click", async () => {
  try {
    await window.wuphfUpdater.installUpdateAndRestart();
  } catch (error) {
    setUpdateState({ state: "error", message: error.message });
  }
});

window.wuphfUpdater.onUpdateState(setUpdateState);

window.wuphfUpdater
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
