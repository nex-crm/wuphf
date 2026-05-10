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

  // afterSign fires for every Mac build (including `--dir` since
  // electron-builder 26 ad-hoc signs the bundle to satisfy hardened
  // runtime requirements). Stapling is only meaningful in production
  // (where Apple notarytool produced a stapleable ticket). For dev/PR
  // builds we exit silently — the build IS signed (ad-hoc) but there
  // is nothing to staple.
  if (process.env.WUPHF_RELEASE_MODE !== "production") {
    return;
  }

  const appBundle = fs
    .readdirSync(context.appOutDir)
    .find(
      (entry) =>
        entry.endsWith(".app") && fs.statSync(path.join(context.appOutDir, entry)).isDirectory(),
    );

  if (!appBundle) {
    throw new Error(`No .app bundle found in ${context.appOutDir}`);
  }

  const appPath = path.join(context.appOutDir, appBundle);
  run("xcrun", ["stapler", "staple", appPath]);
  run("xcrun", ["stapler", "validate", appPath]);
};
