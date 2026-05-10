#!/usr/bin/env node
// Asserts every name in `wuphfRuntimeDependenciesAllowlist` is actually
// bundled into the packaged Electron app — either inside `app.asar` or
// under `app.asar.unpacked/`.
//
// Why this exists: issue #771 — `electron-updater` was wired into
// `src/main.js` but only declared in `devDependencies`, so electron-builder
// pruned it out of the asar and the packaged app crashed on launch with
// "Cannot find module 'electron-updater'". CI never caught it because the
// existing build steps only assert the .dmg/.exe/.AppImage exist on disk,
// not that they contain a working app.
//
// Usage:
//   # 1. Build with `--dir` first so the unpacked dist exists.
//   #    e.g. `node scripts/run-builder.js --linux --dir`
//   # 2. Then point this script at the dist directory:
//   node scripts/check-packaged-runtime-deps.js apps/installer-stub/dist
//
// Exit codes:
//   0 = all allowlisted deps found in either app.asar or app.asar.unpacked/
//   1 = a dep is missing (the bug)
//   2 = the dist directory does not contain a recognizable app bundle

const fs = require("node:fs");
const path = require("node:path");

// @electron/asar ships a Node API used here directly so we don't depend on
// a `node_modules/.bin/asar` shim that bun lays out differently from npm.
const asarApi = require("@electron/asar");

function main() {
  const distArg = process.argv[2];
  if (!distArg) {
    console.error("usage: check-packaged-runtime-deps.js <dist-dir>");
    process.exit(2);
  }

  const distDir = path.resolve(distArg);
  if (!fs.existsSync(distDir)) {
    console.error(`dist directory does not exist: ${distDir}`);
    process.exit(2);
  }

  const packageJson = require(path.resolve(__dirname, "..", "package.json"));
  const allowlist = packageJson.wuphfRuntimeDependenciesAllowlist;
  if (!Array.isArray(allowlist) || allowlist.length === 0) {
    console.error("wuphfRuntimeDependenciesAllowlist is empty or missing — nothing to check");
    process.exit(0);
  }

  const resourcesDir = locateResourcesDir(distDir);
  if (resourcesDir === null) {
    console.error(
      `could not locate Electron resources/ directory under ${distDir}; ` +
        "supported layouts: macOS .app/Contents/Resources, Linux *-unpacked/resources, Windows win-unpacked/resources",
    );
    process.exit(2);
  }

  const asarPath = path.join(resourcesDir, "app.asar");
  const unpackedDir = path.join(resourcesDir, "app.asar.unpacked");
  const asarExists = fs.existsSync(asarPath);
  const unpackedExists = fs.existsSync(unpackedDir);

  if (!asarExists && !unpackedExists) {
    console.error(
      `neither app.asar nor app.asar.unpacked/ found under ${resourcesDir}; ` +
        "did electron-builder skip packaging?",
    );
    process.exit(2);
  }

  const asarEntries = asarExists ? listAsarEntries(asarPath) : new Set();

  const missing = [];
  for (const depName of allowlist) {
    const asarKey = `node_modules/${depName}/package.json`;
    const unpackedPath = path.join(unpackedDir, "node_modules", depName, "package.json");
    const inAsar = asarEntries.has(asarKey);
    const inUnpacked = unpackedExists && fs.existsSync(unpackedPath);
    if (!inAsar && !inUnpacked) {
      missing.push(depName);
    }
  }

  if (missing.length > 0) {
    console.error(
      "Missing runtime dependencies in packaged app:\n  - " +
        missing.join("\n  - ") +
        "\n\n" +
        "Each name appears in package.json's wuphfRuntimeDependenciesAllowlist " +
        "but was NOT found in either app.asar or app.asar.unpacked/. The packaged " +
        "app will fail at module-load time. Make sure the dep is declared in " +
        "`dependencies` (not `devDependencies`) and that bun.lock is up to date.",
    );
    process.exit(1);
  }

  console.log(
    `packaged-runtime-deps OK (${allowlist.length} allowlisted, all bundled): ${allowlist.join(", ")}`,
  );
}

function locateResourcesDir(distDir) {
  const candidates = [];

  // Walk one level deep looking for known electron-builder output shapes.
  for (const entry of fs.readdirSync(distDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) {
      continue;
    }
    const childPath = path.join(distDir, entry.name);

    // macOS: dist/mac{,-arm64,-universal}/<ProductName>.app/Contents/Resources
    if (/^mac(-.*)?$/.test(entry.name)) {
      for (const inner of fs.readdirSync(childPath, { withFileTypes: true })) {
        if (inner.isDirectory() && inner.name.endsWith(".app")) {
          candidates.push(path.join(childPath, inner.name, "Contents", "Resources"));
        }
      }
      continue;
    }

    // Windows: dist/win-unpacked/resources, dist/win-arm64-unpacked/resources
    if (/^win.*-unpacked$/.test(entry.name)) {
      candidates.push(path.join(childPath, "resources"));
      continue;
    }

    // Linux: dist/linux-unpacked/resources, dist/linux-arm64-unpacked/resources
    if (/^linux.*-unpacked$/.test(entry.name)) {
      candidates.push(path.join(childPath, "resources"));
    }
  }

  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return null;
}

function listAsarEntries(asarPath) {
  // @electron/asar.listPackage returns one entry per file/directory inside
  // the asar with a leading slash, e.g. "/node_modules/electron-updater/package.json".
  // Strip the leading slash so callers can use a forward-slash relative key.
  const lines = asarApi.listPackage(asarPath, { isPack: false });
  const entries = new Set();
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.length === 0) {
      continue;
    }
    entries.add(trimmed.replace(/^\/+/, ""));
  }
  return entries;
}

main();
