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

function appBuilderBinaryPathForPackageRoot(packageRoot) {
  return process.platform === "darwin"
    ? path.join(
        packageRoot,
        "mac",
        `app-builder_${process.arch === "x64" ? "amd64" : process.arch}`,
      )
    : process.platform === "win32"
      ? path.join(packageRoot, "win", process.arch, "app-builder.exe")
      : path.join(packageRoot, "linux", process.arch, "app-builder");
}

function appBuilderBinaryPath() {
  const packagePath = require.resolve("app-builder-bin/package.json", {
    paths: [path.dirname(builderCli)],
  });
  const packageRoot = path.dirname(fs.realpathSync(packagePath));
  const binaryPath = appBuilderBinaryPathForPackageRoot(packageRoot);

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`app-builder binary not found at ${binaryPath}`);
  }

  return fs.realpathSync(binaryPath);
}

function ensureWorkspaceAppBuilderBinary() {
  const sourcePath = appBuilderBinaryPath();
  const workspaceRoot = path.resolve(appRoot, "..", "..");
  const expectedPath = appBuilderBinaryPathForPackageRoot(
    path.join(workspaceRoot, "node_modules", "app-builder-bin"),
  );

  if (path.resolve(sourcePath) !== path.resolve(expectedPath)) {
    fs.mkdirSync(path.dirname(expectedPath), { recursive: true });
    fs.copyFileSync(sourcePath, expectedPath);
    if (process.platform !== "win32") {
      fs.chmodSync(expectedPath, 0o755);
    }
  }

  return sourcePath;
}

function electronBuilderEnv() {
  const env = {};

  for (const [key, value] of Object.entries(process.env)) {
    if (/^(npm_|BUN_)/.test(key)) {
      continue;
    }

    env[key] = value;
  }

  env.CUSTOM_APP_BUILDER_PATH ||= ensureWorkspaceAppBuilderBinary();

  return env;
}

const nodeBinary = process.env.NODE_BINARY || process.execPath;
const releaseMode = process.env.WUPHF_RELEASE_MODE || "pr";

if (!["pr", "production"].includes(releaseMode)) {
  console.error(`WUPHF_RELEASE_MODE must be 'pr' or 'production', got '${releaseMode}'`);
  process.exit(1);
}

const builderArgs = process.argv.slice(2);
if (releaseMode !== "production") {
  builderArgs.push("--config.mac.notarize=false");
}

if (process.versions.bun && !process.env.NODE_BINARY) {
  console.error(
    "run-builder.js must run under real Node so electron-builder's CLI is executed by Node. Run via `node scripts/run-builder.js` after setup-node.",
  );
  process.exit(1);
}

// CR-CI-1: electron-builder reads npm_execpath to locate npm for production
// dependency installs. When setup-bun leaves npm_execpath pointing at Bun's
// native binary, electron-builder runs `node $npm_execpath` and Node fails to
// parse the binary. Scrub Bun/npm lifecycle env before spawning the builder.
// Also pass CUSTOM_APP_BUILDER_PATH from electron-builder's real dependency
// tree so builder-util does not depend on Bun's root node_modules symlink shape
// in CI. The same helper materializes the binary at the root node_modules path
// builder-util falls back to when Bun 1.1 exposes a shallow app-builder-bin link.
const result = spawnSync(nodeBinary, [builderCli, ...builderArgs], {
  env: electronBuilderEnv(),
  stdio: "inherit",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 1);
