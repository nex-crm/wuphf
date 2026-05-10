const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

function run(command, args) {
  const result = spawnSync(command, args, { stdio: "inherit" });

  if (result.error) {
    throw result.error;
  }

  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} exited ${result.status}`);
  }
}

exports.default = async function stapleMacApp(context) {
  if (process.platform !== "darwin") {
    return;
  }

  const appBundle = fs
    .readdirSync(context.appOutDir)
    .find(
      (entry) =>
        entry.endsWith(".app") && fs.statSync(path.join(context.appOutDir, entry)).isDirectory(),
    );

  if (!appBundle) {
    if (process.env.WUPHF_RELEASE_MODE !== "production") {
      return;
    }
    throw new Error(`No .app bundle found in ${context.appOutDir}`);
  }

  const appPath = path.join(context.appOutDir, appBundle);
  const signatureResources = path.join(appPath, "Contents", "_CodeSignature", "CodeResources");

  if (process.env.WUPHF_RELEASE_MODE !== "production") {
    if (fs.existsSync(signatureResources)) {
      throw new Error(
        `Refusing to skip stapling for signed macOS app outside production mode: ${appPath}`,
      );
    }
    return;
  }

  run("xcrun", ["stapler", "staple", appPath]);
  run("xcrun", ["stapler", "validate", appPath]);
};
