const button = document.querySelector("#check-updates");
const installButton = document.querySelector("#install-update");
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
    button.textContent = state.state === "available" ? "Download update" : "Check for updates";
  }

  if (installButton) {
    installButton.hidden = state.state !== "downloaded";
  }
}

button?.addEventListener("click", async () => {
  try {
    if (currentState === "available") {
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
