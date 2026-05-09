const path = require("node:path");
const { app, BrowserWindow, ipcMain } = require("electron");
const { autoUpdater } = require("electron-updater");
const packageMetadata = require("../package.json");

autoUpdater.autoDownload = false;

let mainWindow;

function sendUpdateState(state) {
  if (!mainWindow || mainWindow.isDestroyed() || mainWindow.webContents.isDestroyed()) {
    return;
  }

  mainWindow.webContents.send("update-state", state);
}

function getInstallerVersion() {
  return {
    version: app.getVersion(),
    channel: process.env.WUPHF_BUILD_CHANNEL || packageMetadata.wuphfBuildChannel || "dev",
  };
}

autoUpdater.on("checking-for-update", () => {
  sendUpdateState({ state: "checking" });
});

autoUpdater.on("update-available", (info) => {
  sendUpdateState({ state: "available", version: info.version });
});

autoUpdater.on("update-not-available", () => {
  sendUpdateState({ state: "up-to-date" });
});

autoUpdater.on("download-progress", (progress) => {
  sendUpdateState({ state: "downloading", percent: progress.percent });
});

autoUpdater.on("update-downloaded", (info) => {
  sendUpdateState({ state: "downloaded", version: info.version });
});

autoUpdater.on("update-cancelled", (info) => {
  sendUpdateState({ state: "cancelled", version: info?.version });
});

autoUpdater.on("error", (error) => {
  sendUpdateState({ state: "error", message: error.message });
});

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 520,
    height: 360,
    resizable: false,
    backgroundColor: "#101214",
    title: "WUPHF (installer stub)",
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      preload: path.join(__dirname, "preload.js"),
    },
  });

  mainWindow.on("closed", () => {
    mainWindow = undefined;
  });

  mainWindow.loadFile(path.join(__dirname, "index.html"));
}

ipcMain.handle("get-installer-version", () => getInstallerVersion());
ipcMain.handle("check-for-updates", () => autoUpdater.checkForUpdates());
ipcMain.handle("download-update", () => autoUpdater.downloadUpdate());
ipcMain.handle("install-update-and-restart", () => autoUpdater.quitAndInstall());

app.whenReady().then(() => {
  app.setName("WUPHF (installer stub)");
  createWindow();
});

app.on("all-windows-closed", () => {
  app.quit();
});
