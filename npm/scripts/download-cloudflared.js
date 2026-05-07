"use strict";

// Downloads the pinned cloudflared release into npm/bin/ so the public-tunnel
// feature works without a separate `brew install cloudflared` step. Mirrors
// download-binary.js in shape (version pin + SHA256 verification + atomic
// place into bin/) but targets a different upstream and uses a JSON manifest
// instead of a goreleaser checksums.txt because cloudflared's GitHub releases
// publish hashes inline in the release notes rather than as a sibling file.
//
// ---------------------------------------------------------------------------
// Integrity contract
// ---------------------------------------------------------------------------
// Both the version tag AND the per-platform SHA256 live in cloudflared.json
// in this directory. The download flow is:
//
//   1. Map (process.platform, process.arch) -> manifest entry. If the
//      current platform isn't listed, exit 0 silently — wuphf core install
//      should still succeed; tunnels will surface a clear error at runtime.
//   2. Fetch the asset from
//      https://github.com/cloudflare/cloudflared/releases/download/<version>/<asset>
//   3. SHA256 the local copy and compare against the manifest hash. Mismatch
//      is FATAL and scrubs the file — same posture as download-binary.js.
//   4. For .tgz assets, extract the inner `cloudflared` binary; for raw
//      (linux + windows) assets, just rename. Place at npm/bin/cloudflared
//      (or .exe on Windows).
//
// Cloudflare's release pipeline publishes the binary and the release-notes
// hashes from the same atomic process, so a tampered asset would have to
// come with a tampered manifest commit in OUR repo to survive — that is a
// weaker guarantee than goreleaser's signed checksums.txt but matches the
// security model of every npm package that bundles a third-party binary.
// ---------------------------------------------------------------------------

const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const { execFileSync } = require("node:child_process");

const MANIFEST_PATH = path.join(__dirname, "cloudflared.json");
const RELEASE_BASE_URL =
  "https://github.com/cloudflare/cloudflared/releases/download";

function loadManifest() {
  const text = fs.readFileSync(MANIFEST_PATH, "utf8");
  return JSON.parse(text);
}

// Translate Node's process.platform / process.arch into the cloudflared
// manifest key. Returns null when the arch/platform is unrecognised (e.g.
// Linux 386). For known-but-unpublished combinations like Windows ARM64 it
// returns the key ("windows-arm64") so downloadCloudflared() can fall through
// the "no manifest entry" branch — keeping the error surface in the Go
// controller where cloudflared is actually required.
function detectManifestKey() {
  const osMap = { darwin: "darwin", linux: "linux", win32: "windows" };
  const archMap = { x64: "amd64", arm64: "arm64" };
  const goOs = osMap[process.platform];
  const goArch = archMap[process.arch];
  if (!goOs || !goArch) return null;
  return `${goOs}-${goArch}`;
}

// Target filename inside npm/bin/. Lower-case "cloudflared" matches the
// upstream binary's name; the .exe suffix is mandatory on Windows so
// CreateProcess will launch it.
function targetBinaryFilename() {
  return process.platform === "win32" ? "cloudflared.exe" : "cloudflared";
}

function targetBinaryPath() {
  return path.join(__dirname, "..", "bin", targetBinaryFilename());
}

// fetchToFileTimeoutMs caps how long a single download attempt can hang
// before the postinstall step gives up. Without this, a slow or unresponsive
// GitHub release CDN would block `npm install` indefinitely with no recovery
// path. 120s is generous — Cloudflare's release tarball is ~30MB, so even a
// 2 Mbps connection finishes well inside the budget.
const fetchToFileTimeoutMs = 120_000;

async function fetchToFile(url, dest) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), fetchToFileTimeoutMs);
  let res;
  try {
    res = await fetch(url, { redirect: "follow", signal: controller.signal });
  } finally {
    clearTimeout(timer);
  }
  if (!res.ok) {
    throw new Error(
      `Download failed: ${res.status} ${res.statusText} (${url})`,
    );
  }
  const buf = Buffer.from(await res.arrayBuffer());
  await fsp.writeFile(dest, buf);
}

async function sha256OfFile(filePath) {
  const hash = crypto.createHash("sha256");
  const stream = fs.createReadStream(filePath);
  for await (const chunk of stream) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

// Extract the `cloudflared` binary from a goreleaser-style .tgz into tmpDir.
// Cloudflared's macOS archives contain a single top-level `cloudflared`
// file, so a bare `tar -xzf` is sufficient.
function extractTgz(archivePath, tmpDir, silent) {
  const stdio = silent ? "ignore" : "inherit";
  execFileSync("tar", ["-xzf", archivePath, "-C", tmpDir], { stdio });
}

async function downloadCloudflared({ silent = false } = {}) {
  const manifestKey = detectManifestKey();
  if (!manifestKey) {
    if (!silent) {
      process.stderr.write(
        `wuphf: cloudflared not bundled for ${process.platform}-${process.arch}; ` +
          `the Public Tunnel feature will report it missing at runtime.\n`,
      );
    }
    return null;
  }
  const manifest = loadManifest();
  const entry = manifest.platforms[manifestKey];
  if (!entry) {
    if (!silent) {
      process.stderr.write(
        `wuphf: no cloudflared asset pinned for ${manifestKey}; ` +
          `the Public Tunnel feature will report it missing at runtime.\n`,
      );
    }
    return null;
  }

  const { asset, sha256: expectedHash } = entry;
  const url = `${RELEASE_BASE_URL}/${manifest.version}/${asset}`;
  const target = targetBinaryPath();
  const binDir = path.dirname(target);
  await fsp.mkdir(binDir, { recursive: true });

  const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), "wuphf-cf-"));
  const downloadPath = path.join(tmpDir, asset);

  try {
    if (!silent) {
      process.stderr.write(
        `wuphf: downloading cloudflared ${manifest.version} (${asset})\n`,
      );
    }
    await fetchToFile(url, downloadPath);

    const actualHash = await sha256OfFile(downloadPath);
    if (actualHash.toLowerCase() !== expectedHash.toLowerCase()) {
      await fsp.rm(downloadPath, { force: true });
      throw new Error(
        `SHA256 mismatch for ${asset}.\n` +
          `  expected: ${expectedHash}\n` +
          `  actual:   ${actualHash}\n` +
          `Refusing to install cloudflared. This may indicate a tampered ` +
          `release asset or a corrupted download.`,
      );
    }

    // Atomic placement: stage the binary at "<target>.tmp" then rename over
    // the final path. If the install is interrupted (Ctrl+C, OOM-kill,
    // power loss) midway through copy/chmod/codesign, `findCloudflared`
    // would otherwise pick up a half-written file on next launch and fail
    // with a confusing exec-format error. `fs.rename` is atomic on POSIX
    // and stagingPath is sibling to target, so EXDEV cannot apply here.
    const stagingPath = `${target}.tmp`;
    if (asset.endsWith(".tgz")) {
      extractTgz(downloadPath, tmpDir, silent);
      const extracted = path.join(tmpDir, "cloudflared");
      await fsp.copyFile(extracted, stagingPath);
    } else {
      // linux + windows assets are already raw binaries.
      await fsp.copyFile(downloadPath, stagingPath);
    }

    if (process.platform !== "win32") {
      await fsp.chmod(stagingPath, 0o755);
    }

    // macOS 15+ invalidates the upstream ad-hoc signature after copy+chmod
    // and the kernel SIGKILLs an unsigned exec. Re-sign locally so the
    // first `Start tunnel` click does not fail with code-signing errors.
    if (process.platform === "darwin") {
      try {
        execFileSync("codesign", ["--force", "--sign", "-", stagingPath], {
          stdio: "ignore",
        });
      } catch {
        // codesign is best-effort.
      }
    }

    // Atomic move into the final path. After this returns, target either
    // is the fully-prepared binary or is the previous version; never a
    // half-written file.
    await fsp.rename(stagingPath, target);

    if (!silent) {
      process.stderr.write(
        `wuphf: cloudflared ${manifest.version} installed at ${target}\n`,
      );
    }
    return target;
  } finally {
    await fsp.rm(tmpDir, { recursive: true, force: true });
  }
}

module.exports = {
  downloadCloudflared,
  // Exported for tests:
  detectManifestKey,
  loadManifest,
  targetBinaryFilename,
  sha256OfFile,
};
