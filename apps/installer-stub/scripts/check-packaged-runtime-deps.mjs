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
//   node scripts/check-packaged-runtime-deps.mjs apps/installer-stub/dist
//
// Exit codes:
//   0 = all allowlisted deps found in either app.asar or app.asar.unpacked/
//   1 = a dep is missing (the bug)
//   2 = the dist directory does not contain a recognizable app bundle

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// @electron/asar v4 is ESM-only; we import the API directly so we don't
// depend on a `node_modules/.bin/asar` shim that bun lays out differently
// from npm.
import { listPackage } from "@electron/asar";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function main() {
  const distArg = process.argv[2];
  if (!distArg) {
    console.error("usage: check-packaged-runtime-deps.mjs <dist-dir>");
    process.exit(2);
  }

  const distDir = path.resolve(distArg);
  if (!fs.existsSync(distDir)) {
    console.error(`dist directory does not exist: ${distDir}`);
    process.exit(2);
  }

  // Test seams (production never sets these): override the package.json
  // source and the installer-stub root used for the runtime-closure walk.
  // Production callers pass only `<dist-dir>`; test callers can scope the
  // policy/closure to a fixture without touching the real workspace tree.
  //
  // Refuse the seam in production mode. Without this, an env-var leak
  // (e.g. a prior step exporting these for a self-test) would point the
  // gate at a fixture and silently approve a real release artifact that
  // doesn't match the production policy. R3 codex (security lens) flagged.
  const seamPackageJson = process.env.WUPHF_PACKAGED_DEPS_PACKAGE_JSON;
  const seamStubRoot = process.env.WUPHF_PACKAGED_DEPS_INSTALLER_STUB_ROOT;
  if (
    process.env.WUPHF_RELEASE_MODE === "production" &&
    (seamPackageJson !== undefined || seamStubRoot !== undefined)
  ) {
    console.error(
      "WUPHF_PACKAGED_DEPS_{PACKAGE_JSON,INSTALLER_STUB_ROOT} test seam env vars " +
        "must NOT be set in production mode. Refusing to run with a non-production policy source.",
    );
    process.exit(2);
  }

  const installerStubRoot = seamStubRoot ?? path.resolve(__dirname, "..");
  const packageJsonPath = seamPackageJson ?? path.join(installerStubRoot, "package.json");

  const packageJson = JSON.parse(fs.readFileSync(path.resolve(packageJsonPath), "utf8"));
  const allowlist = packageJson.wuphfRuntimeDependenciesAllowlist;
  // FAIL CLOSED: a missing or empty allowlist with non-empty `dependencies`
  // is a contract bug, not a no-op. The companion check-invariants.sh
  // separately requires every dep to also appear in the allowlist; if this
  // script fired with an empty allowlist while deps existed, the policy
  // surface would silently shrink to "anything goes".
  if (!Array.isArray(allowlist)) {
    console.error(
      "wuphfRuntimeDependenciesAllowlist is missing or not an array; " +
        "the packaged-runtime-deps gate refuses to run without an explicit allowlist",
    );
    process.exit(2);
  }
  if (allowlist.length === 0) {
    if (
      packageJson.dependencies &&
      typeof packageJson.dependencies === "object" &&
      Object.keys(packageJson.dependencies).length > 0
    ) {
      console.error(
        "wuphfRuntimeDependenciesAllowlist is empty but dependencies block is non-empty; " +
          "every runtime dep must be declared in the allowlist",
      );
      process.exit(2);
    }
    console.log("packaged-runtime-deps OK (empty allowlist + empty dependencies)");
    process.exit(0);
  }

  const resourceDirs = locateAllResourcesDirs(distDir);
  if (resourceDirs.length === 0) {
    console.error(
      `could not locate any Electron resources/ directory under ${distDir}; ` +
        "supported layouts: macOS .app/Contents/Resources, Linux *-unpacked/resources, Windows win-unpacked/resources",
    );
    process.exit(2);
  }

  // Walk the runtime closure once from package.json so we can assert every
  // transitive dep also lives in the bundle. Without this the gate would
  // pass on a packaged app that has electron-updater/package.json but is
  // missing fs-extra / builder-util-runtime / etc., and the user would still
  // get a "Cannot find module" crash on launch — just one frame deeper.
  const runtimeClosure = computeRuntimeClosure(allowlist, installerStubRoot);

  let allOk = true;
  for (const resourcesDir of resourceDirs) {
    const asarPath = path.join(resourcesDir, "app.asar");
    const unpackedDir = path.join(resourcesDir, "app.asar.unpacked");
    const asarExists = fs.existsSync(asarPath);
    const unpackedExists = fs.existsSync(unpackedDir);

    if (!asarExists && !unpackedExists) {
      console.error(
        `neither app.asar nor app.asar.unpacked/ found under ${resourcesDir}; ` +
          "did electron-builder skip packaging?",
      );
      allOk = false;
      continue;
    }

    const asarEntries = asarExists ? listAsarEntries(asarPath) : new Set();
    const missing = [];
    for (const depName of runtimeClosure) {
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
        `Missing runtime dependencies in packaged app under ${resourcesDir}:\n  - ` +
          missing.join("\n  - ") +
          "\n\n" +
          "Each name is on the runtime-dep closure (allowlist + transitive `dependencies`) " +
          "but was NOT found in either app.asar or app.asar.unpacked/. The packaged " +
          "app will fail at module-load time. Make sure the root dep is declared in " +
          "`dependencies` (not `devDependencies`) and that bun.lock is up to date.",
      );
      allOk = false;
      continue;
    }

    console.log(
      `packaged-runtime-deps OK at ${resourcesDir} (${runtimeClosure.size} closure entries from ${allowlist.length} allowlisted root: ${allowlist.join(", ")})`,
    );
  }

  process.exit(allOk ? 0 : 1);
}

function computeRuntimeClosure(allowlist, installerStubRoot) {
  // Walk each allowlisted dep's package.json from the workspace's
  // node_modules and union all transitive `dependencies` keys into a
  // closure set. Resolution uses Node's algorithm starting from the
  // allowlisted root, which mirrors how electron-builder packages the
  // production tree.
  const closure = new Set();
  const queue = [...allowlist];

  while (queue.length > 0) {
    const name = queue.shift();
    if (closure.has(name)) {
      continue;
    }
    closure.add(name);

    const candidates = [
      path.join(installerStubRoot, "node_modules", name, "package.json"),
      path.resolve(installerStubRoot, "..", "..", "node_modules", name, "package.json"),
    ];
    let pkgJsonPath = null;
    for (const candidate of candidates) {
      if (fs.existsSync(candidate)) {
        pkgJsonPath = candidate;
        break;
      }
    }
    if (pkgJsonPath === null) {
      // The dep itself is missing from node_modules; the gate's later
      // bundle-presence check will surface this with a more useful message.
      continue;
    }
    let depPkg;
    try {
      depPkg = JSON.parse(fs.readFileSync(pkgJsonPath, "utf8"));
    } catch {
      continue;
    }
    const transitive = depPkg.dependencies;
    if (transitive && typeof transitive === "object") {
      for (const childName of Object.keys(transitive)) {
        if (!closure.has(childName)) {
          queue.push(childName);
        }
      }
    }
  }

  return closure;
}

function locateAllResourcesDirs(distDir) {
  const candidates = [];

  // Walk one level deep looking for known electron-builder output shapes.
  // Returns ALL discovered resource dirs so multi-arch / multi-platform
  // builds verify each bundle independently. A stale dist with a matching
  // first candidate would otherwise mask a missing dep in a sibling bundle.
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

  return candidates.filter((candidate) => fs.existsSync(candidate));
}

function listAsarEntries(asarPath) {
  // @electron/asar.listPackage returns one entry per file/directory inside
  // the asar with a leading slash, e.g. "/node_modules/electron-updater/package.json".
  // Strip the leading slash so callers can use a forward-slash relative key.
  const lines = listPackage(asarPath, { isPack: false });
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
