const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("wuphfUpdater", {
  getInstallerVersion: () => ipcRenderer.invoke("get-installer-version"),
  checkForUpdates: () => ipcRenderer.invoke("check-for-updates"),
  downloadUpdate: () => ipcRenderer.invoke("download-update"),
  installUpdateAndRestart: () => ipcRenderer.invoke("install-update-and-restart"),
  onUpdateState: (callback) => {
    ipcRenderer.on("update-state", (_event, state) => callback(state));
  },
});
