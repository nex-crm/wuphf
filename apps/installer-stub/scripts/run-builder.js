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

function ensureBundledToolExecutables() {
  if (process.platform !== "darwin") {
    return;
  }

  const searchRoots = [appRoot, path.resolve(appRoot, "..", "..")];

  for (const searchRoot of searchRoots) {
    const bunModules = path.join(searchRoot, "node_modules", ".bun");
    if (!fs.existsSync(bunModules)) {
      continue;
    }

    for (const entry of fs.readdirSync(bunModules)) {
      if (!entry.startsWith("7zip-bin@")) {
        continue;
      }

      for (const arch of ["arm64", "x64"]) {
        const sevenZip = path.join(
          bunModules,
          entry,
          "node_modules",
          "7zip-bin",
          "mac",
          arch,
          "7za",
        );
        if (fs.existsSync(sevenZip)) {
          try {
            fs.chmodSync(sevenZip, 0o755);
          } catch {
            spawnSync("chmod", ["755", sevenZip], { stdio: "ignore" });
          }
        }
      }
    }
  }
}

ensureBundledToolExecutables();

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
