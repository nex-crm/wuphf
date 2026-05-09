const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const yaml = require("js-yaml");

const appRoot = path.resolve(__dirname, "..");
const runBuilder = path.join(__dirname, "run-builder.js");
const releaseMode = process.env.WUPHF_RELEASE_MODE || "pr";

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: appRoot,
    env: process.env,
    stdio: "inherit",
  });

  if (result.error) {
    throw result.error;
  }

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

function fail(message) {
  console.error(message);
  process.exit(1);
}

function artifactMetadata(artifactPath) {
  const artifactBytes = fs.readFileSync(artifactPath);
  return {
    sha512: crypto.createHash("sha512").update(artifactBytes).digest("base64"),
    size: artifactBytes.byteLength,
  };
}

function writeLatestMacManifest(distDir, zipName, dmgName) {
  const zipMetadata = artifactMetadata(path.join(distDir, zipName));
  const dmgMetadata = artifactMetadata(path.join(distDir, dmgName));
  const packageJson = require(path.join(appRoot, "package.json"));

  const manifest = {
    version: packageJson.version,
    files: [
      {
        url: zipName,
        sha512: zipMetadata.sha512,
        size: zipMetadata.size,
      },
      {
        url: dmgName,
        sha512: dmgMetadata.sha512,
        size: dmgMetadata.size,
      },
    ],
    path: zipName,
    sha512: zipMetadata.sha512,
    size: zipMetadata.size,
    releaseDate: new Date().toISOString(),
  };

  fs.writeFileSync(
    path.join(distDir, "latest-mac.yml"),
    yaml.dump(manifest, { lineWidth: -1, noRefs: true, sortKeys: false }),
  );
}

function packageLocalMacInstaller() {
  run(process.execPath, [
    runBuilder,
    "--mac",
    "--dir",
    "--universal",
    "--config",
    "electron-builder.yml",
    "--publish=never",
  ]);

  const packageJson = require(path.join(appRoot, "package.json"));
  const distDir = path.join(appRoot, "dist");
  const appDir = path.join(distDir, "mac-universal", "WUPHF (installer stub).app");
  const artifactStem = `wuphf-installer-stub-${packageJson.version}-mac-universal`;
  const zipName = `${artifactStem}.zip`;
  const dmgName = `${artifactStem}.dmg`;
  const zipPath = path.join(distDir, zipName);
  const dmgPath = path.join(distDir, dmgName);

  if (!fs.existsSync(appDir)) {
    fail(`Missing macOS app bundle: ${appDir}`);
  }

  fs.rmSync(zipPath, { force: true });
  fs.rmSync(dmgPath, { force: true });

  run("ditto", ["-c", "-k", "--sequesterRsrc", "--keepParent", appDir, zipPath]);

  const stageRoot = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-installer-dmg-"));
  try {
    run("ditto", [appDir, path.join(stageRoot, path.basename(appDir))]);
    run("hdiutil", [
      "makehybrid",
      "-hfs",
      "-hfs-volume-name",
      `WUPHF (installer stub) ${packageJson.version}-universal`,
      "-o",
      dmgPath,
      stageRoot,
    ]);
  } finally {
    fs.rmSync(stageRoot, { recursive: true, force: true });
  }

  writeLatestMacManifest(distDir, zipName, dmgName);
}

if (process.platform === "darwin" && releaseMode !== "production") {
  packageLocalMacInstaller();
} else {
  run(process.execPath, [runBuilder, "--config", "electron-builder.yml", "--publish=never"]);
}
