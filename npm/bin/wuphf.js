#!/usr/bin/env node
"use strict";

// Thin shim that spawns the native wuphf binary.
//
// Two responsibilities beyond a plain `spawn`:
//
//   1. Lazy download if postinstall was skipped (common with
//      `npm install --ignore-scripts` and with some `npx` cache behaviors).
//
//   2. Self-heal when npm's published `latest` has moved past the installed
//      version. `npm install -g` does NOT auto-upgrade, so a user who
//      installed weeks ago runs their old binary forever without this
//      check. We consult the npm registry (24h cache), and if a newer
//      release exists, we transparently serve it from an out-of-tree
//      version-keyed cache. The cached binary is verified against the
//      release's checksums.txt via the same path postinstall uses — there
//      is no path that runs an unverified binary.
//
// Escape hatches:
//   WUPHF_BINARY=/path/to/wuphf         — use a specific binary.
//   WUPHF_SKIP_VERSION_CHECK=1          — never query npm, always run the
//                                         locally-installed binary.

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { spawn } = require("node:child_process");
const { downloadBinary, packageVersion } = require("../scripts/download-binary");
const { getLatestVersion, compareVersions } = require("../scripts/version-check");

const installedBinary = path.join(__dirname, "wuphf");

function cachedBinaryPath(version) {
  return path.join(
    os.homedir(),
    ".wuphf",
    "cache",
    "binaries",
    `wuphf-${version}`,
  );
}

async function resolveInstalledBinary() {
  if (fs.existsSync(installedBinary)) return installedBinary;
  return downloadBinary();
}

async function ensureBinary() {
  if (process.env.WUPHF_BINARY && fs.existsSync(process.env.WUPHF_BINARY)) {
    return process.env.WUPHF_BINARY;
  }

  const installed = await resolveInstalledBinary();
  if (process.env.WUPHF_SKIP_VERSION_CHECK === "1") return installed;

  const installedVersion = packageVersion();
  const latestVersion = await getLatestVersion();
  if (!latestVersion) return installed;
  if (compareVersions(latestVersion, installedVersion) <= 0) return installed;

  // npm has a newer release than what's installed. Serve the cached newer
  // binary, downloading it once if absent. Integrity-verified via the same
  // checksums.txt path as postinstall — a failure anywhere in that chain
  // falls back to the installed binary rather than running something
  // unverified or crashing the command.
  const cachedPath = cachedBinaryPath(latestVersion);
  if (!fs.existsSync(cachedPath)) {
    try {
      await downloadBinary({
        version: latestVersion,
        targetPath: cachedPath,
      });
    } catch (err) {
      process.stderr.write(
        `wuphf: self-heal download of v${latestVersion} failed: ${err.message}\n` +
          `wuphf: running installed v${installedVersion}. ` +
          `Run \`npm install -g wuphf@latest\` to upgrade.\n`,
      );
      return installed;
    }
  }

  process.stderr.write(
    `wuphf: serving cached v${latestVersion} (installed is v${installedVersion}). ` +
      `Run \`npm install -g wuphf@latest\` to upgrade permanently, ` +
      `or set WUPHF_SKIP_VERSION_CHECK=1 to disable this check.\n`,
  );
  return cachedPath;
}

function run(resolvedPath) {
  const child = spawn(resolvedPath, process.argv.slice(2), {
    stdio: "inherit",
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
    } else {
      process.exit(code ?? 0);
    }
  });
  child.on("error", (err) => {
    process.stderr.write(`wuphf: failed to launch binary: ${err.message}\n`);
    process.exit(1);
  });
}

ensureBinary()
  .then(run)
  .catch((err) => {
    process.stderr.write(`wuphf: ${err.message}\n`);
    process.exit(1);
  });
