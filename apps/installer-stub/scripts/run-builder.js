const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const appRoot = path.resolve(__dirname, "..");
const workspaceRoot = path.resolve(appRoot, "..", "..");

function setDefaultCache(env, name, directory) {
  if (env[name]) {
    return;
  }
  fs.mkdirSync(directory, { recursive: true });
  env[name] = directory;
}

// macOS-only: bun's content-addressable store occasionally drops 7zip-bin's
// `7za` binary without the +x bit, which kills electron-builder's DMG step
// with EACCES. Fix it once at startup; works for both bun and npm layouts.
function ensureBundledToolExecutables() {
  if (process.platform !== "darwin") {
    return;
  }
  for (const searchRoot of [appRoot, workspaceRoot]) {
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
const nodeBinary = process.env.NODE_BINARY || process.execPath;
const releaseMode = process.env.WUPHF_RELEASE_MODE || "pr";

if (!["pr", "production"].includes(releaseMode)) {
  console.error(`WUPHF_RELEASE_MODE must be 'pr' or 'production', got '${releaseMode}'`);
  process.exit(1);
}

if (process.versions.bun && !process.env.NODE_BINARY) {
  console.error(
    "run-builder.js must run under real Node so electron-builder's CLI is executed by Node. Run via `node scripts/run-builder.js` after setup-node.",
  );
  process.exit(1);
}

const builderArgs = process.argv.slice(2);
if (releaseMode !== "production") {
  builderArgs.push("--config.mac.notarize=false");
}

// CR-CI-1: electron-builder reads `npm_execpath` to locate the package
// manager for its production-dep install / native-rebuild step. When
// setup-bun is in the env (CI), `npm_execpath` points at bun's native
// binary; electron-builder then spawns `node $npm_execpath ...` and Node
// chokes parsing the binary. Scrubbing bun/npm lifecycle vars removes the
// leak. `npmRebuild: false` in electron-builder.yml also short-circuits the
// rebuild path; check-invariants.sh enforces installer-stub has no
// production deps so that's safe.
function scrubbedEnv() {
  const env = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (/^(npm_|BUN_)/.test(key)) {
      continue;
    }
    env[key] = value;
  }
  setDefaultCache(env, "ELECTRON_CACHE", path.join(appRoot, ".cache", "electron"));
  setDefaultCache(env, "ELECTRON_BUILDER_CACHE", path.join(appRoot, ".cache", "electron-builder"));
  // Belt-and-suspenders: app-builder-bin honors CUSTOM_APP_BUILDER_PATH
  // natively (see its index.js), so when the bun nested-store layout makes
  // its self-resolution fragile, we hand it the resolved binary path.
  // app-builder-bin is a transitive dep of electron-builder, not a direct
  // dep of this package — resolve it from the electron-builder install tree.
  const pkgDir = path.dirname(
    require.resolve("app-builder-bin", { paths: [path.dirname(builderCli)] }),
  );
  const archSuffix = process.arch === "x64" ? "amd64" : process.arch;
  const binPath =
    process.platform === "darwin"
      ? path.join(pkgDir, "mac", `app-builder_${archSuffix}`)
      : process.platform === "win32"
        ? path.join(pkgDir, "win", process.arch, "app-builder.exe")
        : path.join(pkgDir, "linux", process.arch, "app-builder");
  if (fs.existsSync(binPath)) {
    env.CUSTOM_APP_BUILDER_PATH = binPath;
  }
  return env;
}

const result = spawnSync(nodeBinary, [builderCli, ...builderArgs], {
  env: scrubbedEnv(),
  stdio: "inherit",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 1);
