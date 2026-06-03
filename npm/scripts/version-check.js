"use strict";

// Queries npm for the published `latest` wuphf version, with a 24h on-disk
// cache so we don't hammer the registry on every CLI invocation.
//
// Why this exists: `npm install -g wuphf` does not auto-upgrade. A user who
// installed weeks ago runs their old binary forever unless they manually
// re-install. The shim in bin/wuphf.js uses this to transparently serve the
// *latest* release from a verified cache while pointing the user at a real
// fix (`npm install -g wuphf@latest`). See bin/wuphf.js for how the result
// feeds into ensureBinary().
//
// Contract:
//   - getLatestVersion() returns a semver string or null. null means
//     "couldn't check" and the caller MUST fall back to the installed
//     version. Network errors, malformed responses, and fetch timeouts all
//     resolve to null rather than throwing — this path runs before every
//     command and must never break invocation.
//   - compareVersions(a, b) implements major.minor.patch ordering with a
//     lexicographic tiebreaker on pre-release suffixes (SemVer: a release
//     sorts above its pre-releases, matching `0.68.8` > `0.68.8-rc.1`).

const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const { execFile } = require("node:child_process");

const REGISTRY_URL = "https://registry.npmjs.org/wuphf/latest";
// Generous enough to survive a cold TLS handshake on a slow network but
// short enough to not stall the CLI noticeably. Only runs once per 24h
// per user because the result is cached on disk.
const FETCH_TIMEOUT_MS = 3000;
const CACHE_TTL_MS = 24 * 60 * 60 * 1000;
const backupExcludedCacheDirs = new Set();
let backupExclusionRunner = (target) =>
  new Promise((resolve, reject) => {
    execFile(
      "/usr/bin/tmutil",
      ["addexclusion", target],
      { timeout: 2000 },
      (err) => {
        if (err) reject(err);
        else resolve();
      },
    );
  });

function cacheDir() {
  // Sits under the active WUPHF runtime home so workspace/dev overrides get
  // separate caches from prod.
  const runtimeHome = process.env.WUPHF_RUNTIME_HOME?.trim() || os.homedir();
  return path.join(runtimeHome, ".wuphf", "cache");
}

function latestVersionCachePath() {
  return path.join(cacheDir(), "latest-version.json");
}

async function markCacheDirExcludedFromBackup(dir = cacheDir()) {
  if (process.platform !== "darwin") return;
  const candidates = new Set([dir]);
  try {
    candidates.add(await fsp.realpath(dir));
  } catch {
    // The cache write path is best-effort; backup metadata is too.
  }
  for (const candidate of candidates) {
    if (backupExcludedCacheDirs.has(candidate)) continue;
    backupExcludedCacheDirs.add(candidate);
    try {
      await backupExclusionRunner(candidate);
    } catch {
      backupExcludedCacheDirs.delete(candidate);
    }
  }
}

async function ensureCacheDir() {
  const dir = cacheDir();
  await fsp.mkdir(dir, { recursive: true, mode: 0o700 });
  await markCacheDirExcludedFromBackup(dir);
  return dir;
}

async function readCache() {
  try {
    const raw = await fsp.readFile(latestVersionCachePath(), "utf8");
    const data = JSON.parse(raw);
    if (typeof data.version !== "string" || typeof data.checkedAt !== "number") {
      return null;
    }
    const age = Date.now() - data.checkedAt;
    if (age < 0 || age > CACHE_TTL_MS) return null;
    return data.version;
  } catch {
    return null;
  }
}

async function writeCache(version) {
  try {
    await ensureCacheDir();
    const target = latestVersionCachePath();
    const tmp = `${target}.tmp`;
    await fsp.writeFile(tmp, JSON.stringify({ version, checkedAt: Date.now() }));
    await fsp.rename(tmp, target);
  } catch {
    // Cache write is best-effort. A read-only home, full disk, or permission
    // error should not block the user's command.
  }
}

async function fetchLatestFromRegistry() {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
  try {
    const res = await fetch(REGISTRY_URL, {
      signal: controller.signal,
      headers: { Accept: "application/json" },
      redirect: "follow",
    });
    if (!res.ok) return null;
    const data = await res.json();
    return typeof data.version === "string" ? data.version : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

async function getLatestVersion() {
  const cached = await readCache();
  if (cached) return cached;
  const latest = await fetchLatestFromRegistry();
  if (latest) await writeCache(latest);
  return latest;
}

function compareVersions(a, b) {
  const [aCore, aPre = ""] = a.split("-");
  const [bCore, bPre = ""] = b.split("-");
  const aParts = aCore.split(".").map((x) => Number.parseInt(x, 10) || 0);
  const bParts = bCore.split(".").map((x) => Number.parseInt(x, 10) || 0);
  for (let i = 0; i < 3; i += 1) {
    const ap = aParts[i] ?? 0;
    const bp = bParts[i] ?? 0;
    if (ap > bp) return 1;
    if (ap < bp) return -1;
  }
  if (aPre === bPre) return 0;
  // SemVer: a release sorts above its own pre-releases.
  if (!aPre) return 1;
  if (!bPre) return -1;
  return aPre < bPre ? -1 : 1;
}

module.exports = {
  getLatestVersion,
  compareVersions,
  cacheDir,
  ensureCacheDir,
  latestVersionCachePath,
  // Exported for tests.
  markCacheDirExcludedFromBackup,
  fetchLatestFromRegistry,
  readCache,
  writeCache,
  setBackupExclusionRunnerForTest(runner) {
    backupExcludedCacheDirs.clear();
    backupExclusionRunner = runner;
  },
};
