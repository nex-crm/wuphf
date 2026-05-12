#!/usr/bin/env node
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { createPackage } from "@electron/asar";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const scriptPath = path.resolve(__dirname, "check-packaged-runtime-deps.mjs");

function runCheck(distDir, env = {}) {
  return spawnSync(process.execPath, [scriptPath, distDir], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, ...env },
  });
}

// Lay down a fake installer-stub workspace with its own package.json and
// node_modules. The script reads from these via the test seams:
//   WUPHF_PACKAGED_DEPS_PACKAGE_JSON
//   WUPHF_PACKAGED_DEPS_INSTALLER_STUB_ROOT
function makeFakeInstallerStub(rootDir, options) {
  const stubDir = path.join(rootDir, "stub");
  fs.mkdirSync(stubDir, { recursive: true });

  const packageJson = {
    name: "@wuphf/installer-stub",
    private: true,
    ...(options.dependencies ? { dependencies: options.dependencies } : {}),
    ...(options.allowlist === undefined
      ? {}
      : { wuphfRuntimeDependenciesAllowlist: options.allowlist }),
  };
  fs.writeFileSync(path.join(stubDir, "package.json"), JSON.stringify(packageJson));

  // For each transitive map entry, write a fake package.json under
  // node_modules/<name>/ that declares its own `dependencies`. The
  // closure walker reads these to expand allowlist roots into a full
  // runtime set.
  if (options.transitive) {
    for (const [name, deps] of Object.entries(options.transitive)) {
      const depDir = path.join(stubDir, "node_modules", name);
      fs.mkdirSync(depDir, { recursive: true });
      fs.writeFileSync(
        path.join(depDir, "package.json"),
        JSON.stringify({
          name,
          version: "0.0.0",
          ...(Object.keys(deps).length > 0 ? { dependencies: deps } : {}),
        }),
      );
    }
  }

  return {
    stubRoot: stubDir,
    packageJson: path.join(stubDir, "package.json"),
  };
}

async function makeFakeBundle(rootDir, layout) {
  // layout: { kind, deps, unpackedDeps, distName? }
  // Multi-bundle support: pass distName to override the default subdir
  // so a single distDir can host multiple bundles.
  const distDir = path.join(rootDir, "dist");
  fs.mkdirSync(distDir, { recursive: true });

  let resourcesDir;
  switch (layout.kind) {
    case "mac": {
      const macDir = path.join(distDir, layout.distName ?? "mac-universal");
      const appDir = path.join(macDir, "WUPHF (installer stub).app");
      resourcesDir = path.join(appDir, "Contents", "Resources");
      fs.mkdirSync(resourcesDir, { recursive: true });
      break;
    }
    case "linux": {
      resourcesDir = path.join(distDir, layout.distName ?? "linux-unpacked", "resources");
      fs.mkdirSync(resourcesDir, { recursive: true });
      break;
    }
    case "win": {
      resourcesDir = path.join(distDir, layout.distName ?? "win-unpacked", "resources");
      fs.mkdirSync(resourcesDir, { recursive: true });
      break;
    }
    default:
      throw new Error(`unknown layout kind: ${layout.kind}`);
  }

  if (Array.isArray(layout.deps)) {
    const stagingDir = path.join(rootDir, `staging-${layout.kind}-${layout.distName ?? "default"}`);
    for (const dep of layout.deps) {
      const depDir = path.join(stagingDir, "node_modules", dep);
      fs.mkdirSync(depDir, { recursive: true });
      fs.writeFileSync(
        path.join(depDir, "package.json"),
        JSON.stringify({ name: dep, version: "0.0.0" }),
      );
    }
    fs.writeFileSync(path.join(stagingDir, "package.json"), JSON.stringify({ name: "stub" }));
    await createPackage(stagingDir, path.join(resourcesDir, "app.asar"));
  }

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

function envFor(stub) {
  return {
    WUPHF_PACKAGED_DEPS_INSTALLER_STUB_ROOT: stub.stubRoot,
    WUPHF_PACKAGED_DEPS_PACKAGE_JSON: stub.packageJson,
  };
}

async function testPassesWhenAllAllowlistedDepsAreInAsar() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: ["electron-updater"],
      transitive: { "electron-updater": {} },
    });
    const distDir = await makeFakeBundle(tmp, { kind: "mac", deps: ["electron-updater"] });
    const result = runCheck(distDir, envFor(stub));
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
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: ["electron-updater"],
      transitive: { "electron-updater": {} },
    });
    const distDir = await makeFakeBundle(tmp, {
      kind: "linux",
      deps: ["some-other-pkg"],
      unpackedDeps: ["electron-updater"],
    });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(
      result.status,
      0,
      `expected exit 0, got ${result.status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
    );
  });
}

async function testFailsWhenAllowlistedDepIsMissing() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: ["electron-updater"],
      transitive: { "electron-updater": {} },
    });
    const distDir = await makeFakeBundle(tmp, { kind: "win", deps: ["something-else"] });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(result.status, 1, "expected exit 1 for missing dep");
    assert.match(result.stderr, /Missing runtime dependencies in packaged app/);
    assert.match(result.stderr, /electron-updater/);
  });
}

// NEW: transitive closure check — even if the root dep is bundled, a
// missing transitive (e.g. fs-extra under electron-updater) must fail.
async function testFailsWhenTransitiveDepIsMissing() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: ["electron-updater"],
      transitive: {
        "electron-updater": { "fs-extra": "11.0.0" },
        "fs-extra": {},
      },
    });
    // Bundle the root but NOT the transitive — exact post-fix-of-fix
    // failure mode the security/sre/electron lenses converged on.
    const distDir = await makeFakeBundle(tmp, { kind: "mac", deps: ["electron-updater"] });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(
      result.status,
      1,
      `expected exit 1 for missing transitive\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
    );
    assert.match(result.stderr, /Missing runtime dependencies/);
    assert.match(result.stderr, /fs-extra/);
  });
}

// NEW: multi-bundle support — verify EVERY discovered bundle, not just
// the first. A stale dist with one good bundle would otherwise mask a
// missing dep in a sibling bundle.
async function testFailsWhenOneOfMultipleBundlesIsMissingDep() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: ["electron-updater"],
      transitive: { "electron-updater": {} },
    });
    // Bundle A (linux-unpacked): has electron-updater
    await makeFakeBundle(tmp, { kind: "linux", deps: ["electron-updater"] });
    // Bundle B (linux-arm64-unpacked): MISSING electron-updater
    await makeFakeBundle(tmp, {
      kind: "linux",
      deps: ["something-else"],
      distName: "linux-arm64-unpacked",
    });
    const result = runCheck(path.join(tmp, "dist"), envFor(stub));
    assert.equal(
      result.status,
      1,
      `expected exit 1 when one bundle is missing dep\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
    );
    assert.match(result.stderr, /linux-arm64-unpacked/);
    assert.match(result.stderr, /electron-updater/);
  });
}

// NEW: fail-closed when allowlist is missing while deps are non-empty.
async function testFailsWhenAllowlistMissingWithDeps() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      // allowlist intentionally omitted
      transitive: { "electron-updater": {} },
    });
    const distDir = await makeFakeBundle(tmp, { kind: "mac", deps: ["electron-updater"] });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(result.status, 2, "expected exit 2 for missing allowlist");
    assert.match(result.stderr, /missing or not an array/);
  });
}

// NEW: fail-closed when allowlist is empty array but deps are non-empty.
async function testFailsWhenAllowlistEmptyWithDeps() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: [],
      transitive: { "electron-updater": {} },
    });
    const distDir = await makeFakeBundle(tmp, { kind: "mac", deps: ["electron-updater"] });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(result.status, 2, "expected exit 2 for empty allowlist with deps");
    assert.match(result.stderr, /is empty but dependencies block is non-empty/);
  });
}

async function testPassesWhenAllowlistEmptyAndDepsEmpty() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, { allowlist: [] });
    const distDir = path.join(tmp, "dist");
    fs.mkdirSync(distDir, { recursive: true });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(result.status, 0, "expected exit 0 for empty allowlist + empty deps");
    assert.match(result.stdout, /empty allowlist \+ empty dependencies/);
  });
}

async function testFailsWhenNoBundleIsPresent() {
  await withTempRoot(async (tmp) => {
    const stub = makeFakeInstallerStub(tmp, {
      dependencies: { "electron-updater": "6.3.9" },
      allowlist: ["electron-updater"],
      transitive: { "electron-updater": {} },
    });
    const distDir = path.join(tmp, "dist");
    fs.mkdirSync(distDir, { recursive: true });
    const result = runCheck(distDir, envFor(stub));
    assert.equal(result.status, 2, "expected exit 2 for missing bundle");
    assert.match(result.stderr, /could not locate any Electron resources\/ directory/);
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
  await testFailsWhenTransitiveDepIsMissing();
  await testFailsWhenOneOfMultipleBundlesIsMissingDep();
  await testFailsWhenAllowlistMissingWithDeps();
  await testFailsWhenAllowlistEmptyWithDeps();
  await testPassesWhenAllowlistEmptyAndDepsEmpty();
  await testFailsWhenNoBundleIsPresent();
  testFailsWhenDistDirDoesNotExist();
  console.log("check-packaged-runtime-deps self-test OK");
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
