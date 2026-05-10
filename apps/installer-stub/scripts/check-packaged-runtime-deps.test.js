#!/usr/bin/env node
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const scriptPath = path.resolve(__dirname, "check-packaged-runtime-deps.js");
const asarApi = require("@electron/asar");

function runCheck(distDir) {
  return spawnSync(process.execPath, [scriptPath, distDir], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
}

async function makeFakeDist(rootDir, layout) {
  // layout: { kind: 'mac' | 'linux' | 'win', deps: ['electron-updater', ...], unpackedDeps: [...] }
  const distDir = path.join(rootDir, "dist");
  fs.mkdirSync(distDir, { recursive: true });

  let resourcesDir;
  switch (layout.kind) {
    case "mac": {
      const macDir = path.join(distDir, "mac-universal");
      const appDir = path.join(macDir, "WUPHF (installer stub).app");
      resourcesDir = path.join(appDir, "Contents", "Resources");
      fs.mkdirSync(resourcesDir, { recursive: true });
      break;
    }
    case "linux": {
      resourcesDir = path.join(distDir, "linux-unpacked", "resources");
      fs.mkdirSync(resourcesDir, { recursive: true });
      break;
    }
    case "win": {
      resourcesDir = path.join(distDir, "win-unpacked", "resources");
      fs.mkdirSync(resourcesDir, { recursive: true });
      break;
    }
    default:
      throw new Error(`unknown layout kind: ${layout.kind}`);
  }

  // Build a tiny fake asar containing the requested deps.
  if (Array.isArray(layout.deps)) {
    const stagingDir = path.join(rootDir, "staging");
    for (const dep of layout.deps) {
      const depDir = path.join(stagingDir, "node_modules", dep);
      fs.mkdirSync(depDir, { recursive: true });
      fs.writeFileSync(
        path.join(depDir, "package.json"),
        JSON.stringify({ name: dep, version: "0.0.0" }),
      );
    }
    fs.writeFileSync(path.join(stagingDir, "package.json"), JSON.stringify({ name: "stub" }));
    await asarApi.createPackage(stagingDir, path.join(resourcesDir, "app.asar"));
  }

  // Create unpacked deps if requested.
  if (Array.isArray(layout.unpackedDeps)) {
    for (const dep of layout.unpackedDeps) {
      const depDir = path.join(resourcesDir, "app.asar.unpacked", "node_modules", dep);
      fs.mkdirSync(depDir, { recursive: true });
      fs.writeFileSync(
        path.join(depDir, "package.json"),
        JSON.stringify({ name: dep, version: "0.0.0" }),
      );
    }
  }

  return distDir;
}

async function withTempRoot(fn) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "packaged-deps-test-"));
  try {
    await fn(tmp);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

async function testPassesWhenAllAllowlistedDepsAreInAsar() {
  await withTempRoot(async (tmp) => {
    const distDir = await makeFakeDist(tmp, { kind: "mac", deps: ["electron-updater"] });
    const result = runCheck(distDir);
    assert.equal(
      result.status,
      0,
      `expected exit 0, got ${result.status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
    );
    assert.match(result.stdout, /packaged-runtime-deps OK/);
    assert.match(result.stdout, /electron-updater/);
  });
}

async function testPassesWhenDepIsInUnpackedRatherThanAsar() {
  await withTempRoot(async (tmp) => {
    const distDir = await makeFakeDist(tmp, {
      kind: "linux",
      deps: ["some-other-pkg"],
      unpackedDeps: ["electron-updater"],
    });
    const result = runCheck(distDir);
    assert.equal(
      result.status,
      0,
      `expected exit 0, got ${result.status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
    );
  });
}

async function testFailsWhenAllowlistedDepIsMissing() {
  await withTempRoot(async (tmp) => {
    const distDir = await makeFakeDist(tmp, { kind: "win", deps: ["something-else"] });
    const result = runCheck(distDir);
    assert.equal(result.status, 1, "expected exit 1 for missing dep");
    assert.match(result.stderr, /Missing runtime dependencies in packaged app/);
    assert.match(result.stderr, /electron-updater/);
  });
}

async function testFailsWhenNoBundleIsPresent() {
  await withTempRoot(async (tmp) => {
    const distDir = path.join(tmp, "dist");
    fs.mkdirSync(distDir, { recursive: true });
    // No mac/win/linux subdirectory at all
    const result = runCheck(distDir);
    assert.equal(result.status, 2, "expected exit 2 for missing bundle");
    assert.match(result.stderr, /could not locate Electron resources\/ directory/);
  });
}

function testFailsWhenDistDirDoesNotExist() {
  const missing = path.join(os.tmpdir(), `does-not-exist-${Date.now()}-${process.pid}`);
  const result = runCheck(missing);
  assert.equal(result.status, 2);
  assert.match(result.stderr, /dist directory does not exist/);
}

async function main() {
  await testPassesWhenAllAllowlistedDepsAreInAsar();
  await testPassesWhenDepIsInUnpackedRatherThanAsar();
  await testFailsWhenAllowlistedDepIsMissing();
  await testFailsWhenNoBundleIsPresent();
  testFailsWhenDistDirDoesNotExist();
  console.log("check-packaged-runtime-deps self-test OK");
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
