"use strict";

// Downloads the wuphf binary that matches the current package version
// from the corresponding GitHub release and extracts it into bin/.
// GoReleaser archive name: wuphf_<version>_<os>_<arch>.tar.gz
// where <version> is the tag without the leading 'v'.
//
// ---------------------------------------------------------------------------
// Integrity verification contract
// ---------------------------------------------------------------------------
// Every download is verified against the `checksums.txt` file published as a
// sibling asset on the same GitHub release. That file is produced by
// `goreleaser release` (see `.goreleaser.yml` `checksum.name_template`) and
// contains one line per archive in the format:
//
//     <sha256-hex>  <archive-filename>
//
// Verification flow:
//   1. Download the per-platform archive (wuphf_<ver>_<os>_<arch>.tar.gz).
//   2. Download checksums.txt from the same release.
//   3. Compute SHA256 of the downloaded archive locally.
//   4. Compare against the hash listed for that archive in checksums.txt.
//   5. If they differ, or checksums.txt is unreachable, or the archive is not
//      listed in it: delete the archive and abort with a non-zero exit.
//
// This guards against release-asset tampering: even if a compromised release
// token replaces the tarball, the mismatch causes `npm install wuphf` to fail
// loudly rather than silently install a backdoored binary.
//
// To regenerate checksums.txt, run `goreleaser release` (or `goreleaser
// release --snapshot` for a dry run). Never hand-edit the published file.
// ---------------------------------------------------------------------------

const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const { execFileSync } = require("node:child_process");

const REPO = "nex-crm/wuphf";
const CHECKSUMS_FILENAME = "checksums.txt";

function detectPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  const osMap = { darwin: "darwin", linux: "linux", win32: "windows" };
  const archMap = { x64: "amd64", arm64: "arm64" };

  if (!osMap[platform]) {
    throw new Error(
      `Unsupported platform: ${platform}. wuphf supports darwin, linux, and windows.`,
    );
  }
  if (!archMap[arch]) {
    throw new Error(
      `Unsupported architecture: ${arch}. wuphf supports x64 (amd64) and arm64.`,
    );
  }
  return { os: osMap[platform], arch: archMap[arch] };
}

function packageVersion() {
  const pkg = JSON.parse(
    fs.readFileSync(path.join(__dirname, "..", "package.json"), "utf8"),
  );
  return pkg.version;
}

// goreleaser packages Windows builds as .zip and everything else as
// .tar.gz (see .goreleaser.yml format_overrides). Stay consistent with
// what's actually published.
function archiveExtension() {
  return process.platform === "win32" ? "zip" : "tar.gz";
}

function archiveName(version) {
  const { os: goOs, arch: goArch } = detectPlatform();
  return `wuphf_${version}_${goOs}_${goArch}.${archiveExtension()}`;
}

// Go names Windows binaries with a .exe suffix; the rest get the bare name.
function binaryFilename() {
  return process.platform === "win32" ? "wuphf.exe" : "wuphf";
}

function releaseAssetUrl(version, filename) {
  return `https://github.com/${REPO}/releases/download/v${version}/${filename}`;
}

async function fetchToFile(url, dest) {
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok) {
    throw new Error(`Download failed: ${res.status} ${res.statusText} (${url})`);
  }
  const buf = Buffer.from(await res.arrayBuffer());
  await fsp.writeFile(dest, buf);
}

async function fetchText(url) {
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok) {
    throw new Error(`Download failed: ${res.status} ${res.statusText} (${url})`);
  }
  return res.text();
}

async function sha256OfFile(filePath) {
  const hash = crypto.createHash("sha256");
  const stream = fs.createReadStream(filePath);
  for await (const chunk of stream) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

// Parse a GoReleaser-style checksums.txt.
// Each non-empty line is:  <sha256hex><whitespace><filename>
// We look up the filename (basename) and return the hex hash, or null.
function expectedHashFor(checksumsText, filename) {
  const lines = checksumsText.split(/\r?\n/);
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    // Split on first whitespace run. GoReleaser uses two spaces; be lenient.
    const match = trimmed.match(/^([0-9a-fA-F]{64})\s+(?:\*)?(.+)$/);
    if (!match) continue;
    const [, hex, name] = match;
    // Match on basename to tolerate optional path prefixes.
    if (path.basename(name) === filename) {
      return hex.toLowerCase();
    }
  }
  return null;
}

async function verifyArchive({ version, archivePath, archiveBasename, silent }) {
  const checksumsUrl = releaseAssetUrl(version, CHECKSUMS_FILENAME);
  if (!silent) {
    process.stderr.write(`wuphf: verifying ${archiveBasename} against ${CHECKSUMS_FILENAME}\n`);
  }

  let checksumsText;
  try {
    checksumsText = await fetchText(checksumsUrl);
  } catch (err) {
    throw new Error(
      `Cannot verify download integrity: failed to fetch ${CHECKSUMS_FILENAME} ` +
        `(${err.message}). Refusing to install an unverified binary.`,
    );
  }

  const expected = expectedHashFor(checksumsText, archiveBasename);
  if (!expected) {
    throw new Error(
      `Cannot verify download integrity: ${archiveBasename} not listed in ` +
        `${checksumsUrl}. Refusing to install an unverified binary.`,
    );
  }

  const actual = await sha256OfFile(archivePath);
  if (actual.toLowerCase() !== expected) {
    // Scrub the tampered/corrupt archive before aborting.
    await fsp.rm(archivePath, { force: true });
    throw new Error(
      `SHA256 mismatch for ${archiveBasename}.\n` +
        `  expected: ${expected}\n` +
        `  actual:   ${actual}\n` +
        `Refusing to install. This may indicate a tampered release asset or ` +
        `a corrupted download. Re-run the install on a clean network; if the ` +
        `mismatch persists, file an issue at https://github.com/${REPO}/issues.`,
    );
  }
}

// Options:
//   silent      — suppress progress output on stderr.
//   version     — download a specific tagged release instead of the one
//                 recorded in package.json. Used by bin/wuphf.js to fetch a
//                 newer release into an out-of-tree cache when npm's latest
//                 has moved past the installed version.
//   targetPath  — where to place the extracted binary. Defaults to
//                 bin/wuphf inside this package. The out-of-tree cache uses
//                 a version-keyed path so multiple versions can coexist.
// Extract a goreleaser archive into tmpDir. Picks the right tool by
// extension: tar for .tar.gz (built into macOS + Linux), and PowerShell
// Expand-Archive on Windows because Node has no built-in unzip and we
// don't want to drag in a dependency. Windows 10 1803+ ships with
// `tar.exe` (bsdtar) which can also extract .zip — we prefer that on
// Windows because it doesn't need PowerShell's slower startup.
function extractArchive(archivePath, tmpDir, silent) {
  const stdio = silent ? "ignore" : "inherit";
  if (archivePath.endsWith(".zip")) {
    if (process.platform === "win32") {
      // Windows 10 1803+ has tar.exe (bsdtar). It accepts .zip natively.
      execFileSync("tar", ["-xf", archivePath, "-C", tmpDir], { stdio });
      return;
    }
    // macOS / Linux fallback: use unzip if available.
    execFileSync("unzip", ["-q", "-o", archivePath, "-d", tmpDir], { stdio });
    return;
  }
  // Default: gzipped tar. Works on darwin + linux.
  execFileSync("tar", ["-xzf", archivePath, "-C", tmpDir], { stdio });
}

async function downloadBinary({ silent = false, version, targetPath } = {}) {
  const resolvedVersion = version ?? packageVersion();
  const archiveBasename = archiveName(resolvedVersion);
  const url = releaseAssetUrl(resolvedVersion, archiveBasename);
  const binaryName = binaryFilename();
  const binaryPath = targetPath ?? path.join(__dirname, "..", "bin", binaryName);
  const binDir = path.dirname(binaryPath);

  await fsp.mkdir(binDir, { recursive: true });

  const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), "wuphf-"));
  const archivePath = path.join(tmpDir, archiveBasename);

  try {
    if (!silent) {
      process.stderr.write(`wuphf: downloading ${url}\n`);
    }
    await fetchToFile(url, archivePath);

    // Integrity check BEFORE we extract or execute anything.
    await verifyArchive({
      version: resolvedVersion,
      archivePath,
      archiveBasename,
      silent,
    });

    extractArchive(archivePath, tmpDir, silent);

    const extractedBinary = path.join(tmpDir, binaryName);
    await fsp.copyFile(extractedBinary, binaryPath);

    if (process.platform !== "win32") {
      // chmod is a no-op on Windows (NTFS doesn't track the +x bit) and
      // calling it just emits a warning on some Node versions.
      await fsp.chmod(binaryPath, 0o755);
    }

    // macOS 15+ invalidates GoReleaser's embedded ad-hoc signature after
    // copy+chmod. Re-sign so the kernel does not SIGKILL on exec.
    if (process.platform === "darwin") {
      try {
        execFileSync("codesign", ["--force", "--sign", "-", binaryPath], {
          stdio: "ignore",
        });
      } catch {
        // codesign is optional — binary may still run.
      }
    }

    return binaryPath;
  } finally {
    await fsp.rm(tmpDir, { recursive: true, force: true });
  }
}

module.exports = {
  downloadBinary,
  packageVersion,
  // Exported for tests.
  expectedHashFor,
  sha256OfFile,
  detectPlatform,
  archiveExtension,
  archiveName,
  binaryFilename,
};
