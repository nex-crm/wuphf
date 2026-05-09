const path = require("node:path");
const { app, BrowserWindow } = require("electron");

function createWindow() {
  const window = new BrowserWindow({
    width: 520,
    height: 360,
    resizable: false,
    backgroundColor: "#101214",
    title: "WUPHF (installer stub)",
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  window.loadFile(path.join(__dirname, "index.html"));
}

app.whenReady().then(() => {
  app.setName("WUPHF (installer stub)");
  createWindow();
});

app.on("all-windows-closed", () => {
  app.quit();
});
