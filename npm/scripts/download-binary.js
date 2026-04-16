"use strict";

// Downloads the wuphf binary that matches the current package version
// from the corresponding GitHub release and extracts it into bin/.
// GoReleaser archive name: wuphf_<version>_<os>_<arch>.tar.gz
// where <version> is the tag without the leading 'v'.

const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const { execFileSync } = require("node:child_process");

const REPO = "nex-crm/wuphf";

function detectPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  const osMap = { darwin: "darwin", linux: "linux" };
  const archMap = { x64: "amd64", arm64: "arm64" };

  if (!osMap[platform]) {
    throw new Error(
      `Unsupported platform: ${platform}. wuphf supports darwin and linux.`,
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

function archiveUrl(version) {
  const { os: goOs, arch: goArch } = detectPlatform();
  const archive = `wuphf_${version}_${goOs}_${goArch}.tar.gz`;
  return `https://github.com/${REPO}/releases/download/v${version}/${archive}`;
}

async function fetchToFile(url, dest) {
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok) {
    throw new Error(`Download failed: ${res.status} ${res.statusText} (${url})`);
  }
  const buf = Buffer.from(await res.arrayBuffer());
  await fsp.writeFile(dest, buf);
}

async function downloadBinary({ silent = false } = {}) {
  const version = packageVersion();
  const url = archiveUrl(version);
  const binDir = path.join(__dirname, "..", "bin");
  const binaryPath = path.join(binDir, "wuphf");

  await fsp.mkdir(binDir, { recursive: true });

  const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), "wuphf-"));
  const archivePath = path.join(tmpDir, "wuphf.tar.gz");

  try {
    if (!silent) {
      process.stderr.write(`wuphf: downloading ${url}\n`);
    }
    await fetchToFile(url, archivePath);

    // Extract using system tar (available on darwin + linux).
    execFileSync("tar", ["-xzf", archivePath, "-C", tmpDir], {
      stdio: silent ? "ignore" : "inherit",
    });

    const extractedBinary = path.join(tmpDir, "wuphf");
    await fsp.copyFile(extractedBinary, binaryPath);
    await fsp.chmod(binaryPath, 0o755);

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

module.exports = { downloadBinary, packageVersion };
