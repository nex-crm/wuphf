const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const appRoot = path.resolve(__dirname, "..");

function setDefaultCache(name, directory) {
  if (process.env[name]) {
    return;
  }

  fs.mkdirSync(directory, { recursive: true });
  process.env[name] = directory;
}

setDefaultCache("ELECTRON_CACHE", path.join(appRoot, ".cache", "electron"));
setDefaultCache("ELECTRON_BUILDER_CACHE", path.join(appRoot, ".cache", "electron-builder"));

const builderCli = require.resolve("electron-builder/cli");
const nodeBinary = process.env.NODE_BINARY || "node";
const result = spawnSync(nodeBinary, [builderCli, ...process.argv.slice(2)], {
  env: process.env,
  stdio: "inherit",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 1);
