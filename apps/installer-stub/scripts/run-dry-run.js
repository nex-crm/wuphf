const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const yaml = require("js-yaml");

const appRoot = path.resolve(__dirname, "..");
const runBuilder = path.join(__dirname, "run-builder.js");

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: appRoot,
    env: process.env,
    stdio: "inherit",
    ...options,
  });

  if (result.error) {
    throw result.error;
  }

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

function packageMacUpdaterZip() {
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
  const artifactName = `wuphf-installer-stub-${packageJson.version}-mac-universal.zip`;
  const artifactPath = path.join(distDir, artifactName);

  if (!fs.existsSync(appDir)) {
    console.error(`Missing macOS app bundle: ${appDir}`);
    process.exit(1);
  }

  run("ditto", ["-c", "-k", "--sequesterRsrc", "--keepParent", appDir, artifactPath]);

  const artifact = fs.readFileSync(artifactPath);
  const manifest = {
    version: packageJson.version,
    files: [
      {
        url: artifactName,
        sha512: crypto.createHash("sha512").update(artifact).digest("base64"),
        size: artifact.byteLength,
      },
    ],
    path: artifactName,
    sha512: crypto.createHash("sha512").update(artifact).digest("base64"),
    size: artifact.byteLength,
    releaseDate: new Date().toISOString(),
  };

  fs.writeFileSync(
    path.join(distDir, "latest-mac.yml"),
    yaml.dump(manifest, { lineWidth: -1, noRefs: true, sortKeys: false }),
  );
}

if (process.platform === "darwin") {
  packageMacUpdaterZip();
} else {
  const platformArgs = process.platform === "win32" ? ["--win"] : ["--linux"];
  run(process.execPath, [
    runBuilder,
    ...platformArgs,
    "--config",
    "electron-builder.yml",
    "--publish=never",
  ]);
}
